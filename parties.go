package ggscale

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// PartiesService exposes the /v1/parties and /v1/party-invites endpoints.
// Reach it via Client.Parties.
//
// Most writes take the party version you last saw. A stale version returns
// an error matching ErrStaleVersion: read the party again and retry. Only the
// leader can update, disband, kick, invite, create or revoke codes, queue,
// cancel the queue, and rematch; other members get ErrForbidden.
type PartiesService struct {
	c *Client
	// heartbeatInterval overrides the Watch heartbeat; 0 uses
	// defaultPartyHeartbeatInterval. Set only in tests.
	heartbeatInterval time.Duration
}

// defaultPartyHeartbeatInterval keeps a member well inside the server's
// 30-second disconnect deadline.
const defaultPartyHeartbeatInterval = 10 * time.Second

// Party states. Treat State as an open enum.
const (
	PartyIdle    = "idle"
	PartyQueued  = "queued"
	PartyMatched = "matched"
)

// ErrNotPartyMember is returned by Parties.WaitForMatch when the player is no
// longer a member of the party (left, kicked, disbanded, or removed by the
// heartbeat sweep).
var ErrNotPartyMember = errors.New("ggscale: not a member of this party")

// errWatchDone stops Watch without an error.
var errWatchDone = errors.New("ggscale: party watch done")

// PartySettings are the queue criteria the whole party shares.
type PartySettings struct {
	Mode             string `json:"mode"`
	FleetID          int64  `json:"fleet_id,omitempty"`
	Region           string `json:"region,omitempty"`
	GameMode         string `json:"game_mode,omitempty"`
	MinCount         int    `json:"min_count"`
	MaxCount         int    `json:"max_count"`
	CountMultiple    int    `json:"count_multiple"`
	AllowCrossRegion bool   `json:"allow_cross_region"`
	Query            string `json:"query,omitempty"`
}

// PartyMember is one member of a party.
type PartyMember struct {
	// TicketID is the member's latest matchmaking ticket in this party, or 0.
	TicketID int64 `json:"ticket_id,omitempty"`
	PlayerID int64 `json:"player_id"`
	// ReadyVersion equals Party.RosterVersion when the member is ready for
	// the current roster.
	ReadyVersion      int64              `json:"ready_version"`
	StringProperties  map[string]string  `json:"string_properties,omitempty"`
	NumericProperties map[string]float64 `json:"numeric_properties,omitempty"`
	// Attributes come back exactly as the member sent them, HTML included.
	// Escape them before you show them.
	Attributes         json.RawMessage `json:"attributes,omitempty"`
	JoinedAt           time.Time       `json:"joined_at"`
	LastSeenAt         time.Time       `json:"last_seen_at"`
	DisconnectDeadline time.Time       `json:"disconnect_deadline"`
}

// Ready reports whether the member is ready for the party's current roster.
func (m PartyMember) Ready(p *Party) bool { return m.ReadyVersion == p.RosterVersion }

// Party is the current state of a party.
type Party struct {
	ID                  int64         `json:"id"`
	ProjectID           int64         `json:"project_id"`
	LeaderID            int64         `json:"leader_id"`
	State               string        `json:"state"`
	Version             int64         `json:"version"`
	RosterVersion       int64         `json:"roster_version"`
	Settings            PartySettings `json:"settings"`
	MaxMembers          int           `json:"max_members"`
	CurrentQueueEntryID int64         `json:"current_queue_entry_id,omitempty"`
	LastMatchID         string        `json:"last_match_id,omitempty"`
	Members             []PartyMember `json:"members"`
}

// Member returns the member with playerID, or nil.
func (p *Party) Member(playerID int64) *PartyMember {
	for i := range p.Members {
		if p.Members[i].PlayerID == playerID {
			return &p.Members[i]
		}
	}
	return nil
}

// PartyCode is an invite code that other players redeem with JoinByCode.
type PartyCode struct {
	ID           int64     `json:"id"`
	Code         string    `json:"code"`
	ExpiresAt    time.Time `json:"expires_at"`
	PartyVersion int64     `json:"party_version"`
}

// PartyInvite is a pending invite from a party leader to a friend.
type PartyInvite struct {
	ID int64 `json:"id"`
	// PartyID and PartyVersion are what AcceptInvite and DeclineInvite need.
	PartyID      int64     `json:"party_id"`
	PartyVersion int64     `json:"party_version"`
	TargetID     int64     `json:"target_id"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// PartyProperties are one member's matchmaking properties, sent with
// SetReady.
type PartyProperties struct {
	StringProperties  map[string]string  `json:"string_properties,omitempty"`
	NumericProperties map[string]float64 `json:"numeric_properties,omitempty"`
	Attributes        json.RawMessage    `json:"attributes,omitempty"`
}

type partySettingsBody struct {
	ExpectedVersion int64        `json:"expected_version,omitempty"`
	Settings        MatchRequest `json:"settings"`
}

type partyVersionBody struct {
	ExpectedVersion int64 `json:"expected_version"`
}

type partyReadyBody struct {
	ExpectedVersion int64           `json:"expected_version"`
	Ready           bool            `json:"ready"`
	Properties      PartyProperties `json:"properties"`
}

type partyInviteBody struct {
	ExpectedVersion int64 `json:"expected_version"`
	PlayerID        int64 `json:"player_id"`
}

type partyCodeBody struct {
	ExpectedVersion int64 `json:"expected_version"`
	MaxUses         int   `json:"max_uses,omitempty"`
}

type partyJoinBody struct {
	Code string `json:"code"`
}

type partyRematchBody struct {
	ExpectedVersion int64  `json:"expected_version"`
	LastMatchID     string `json:"last_match_id"`
}

func partyPath(id int64, suffix string) string {
	return "/v1/parties/" + strconv.FormatInt(id, 10) + suffix
}

func partyInvitePath(id int64, suffix string) string {
	return "/v1/party-invites/" + strconv.FormatInt(id, 10) + suffix
}

// idempotencyHeader uses key, or a random key when key is empty.
func idempotencyHeader(key string) http.Header {
	if key == "" {
		key = newRequestID()
	}
	return http.Header{"Idempotency-Key": []string{key}}
}

func (p *PartiesService) party(ctx context.Context, req *Request) (*Party, error) {
	var out Party
	if err := p.c.callProtected(ctx, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Create makes a party with the caller as leader. settings are the shared
// queue criteria; each member sends their own properties with SetReady.
func (p *PartiesService) Create(ctx context.Context, settings MatchRequest) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "createParty",
		Method:      http.MethodPost,
		Path:        "/v1/parties",
		Body:        partySettingsBody{Settings: settings},
	})
}

// Current returns the caller's party. Use it to recover the party after a
// restart. Returns ErrNotFound when the caller is in no party.
func (p *PartiesService) Current(ctx context.Context) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "getCurrentParty",
		Method:      http.MethodGet,
		Path:        "/v1/parties/current",
	})
}

// Get returns a party. Returns ErrNotFound when the party does not exist or
// the caller is not a member, for example after a kick.
func (p *PartiesService) Get(ctx context.Context, id int64) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "getParty",
		Method:      http.MethodGet,
		Path:        partyPath(id, ""),
	})
}

// Update replaces the queue settings. Leader only. It resets every member's
// readiness.
func (p *PartiesService) Update(ctx context.Context, id, version int64, settings MatchRequest) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "updateParty",
		Method:      http.MethodPatch,
		Path:        partyPath(id, ""),
		Body:        partySettingsBody{ExpectedVersion: version, Settings: settings},
	})
}

// Disband removes every member. Leader only.
func (p *PartiesService) Disband(ctx context.Context, id, version int64) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "disbandParty",
		Method:      http.MethodDelete,
		Path:        partyPath(id, ""),
		Body:        partyVersionBody{ExpectedVersion: version},
	})
}

// Heartbeat keeps the caller in the party and returns the full party. Each
// member must call it within 30 seconds or the server removes the member.
// Watch calls it for you.
func (p *PartiesService) Heartbeat(ctx context.Context, id, version int64) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "heartbeatParty",
		Method:      http.MethodPost,
		Path:        partyPath(id, "/heartbeat"),
		Body:        partyVersionBody{ExpectedVersion: version},
	})
}

// JoinByCode joins the party of an invite code. Returns ErrNotFound when the
// code is unknown, expired, revoked, or used up. After too many wrong codes
// the error matches ErrCodeCooldown; wait for (*Error).RetryAfter.
func (p *PartiesService) JoinByCode(ctx context.Context, code string) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "joinPartyCode",
		Method:      http.MethodPost,
		Path:        "/v1/parties/join",
		Body:        partyJoinBody{Code: code},
	})
}

// Leave removes the caller from the party.
func (p *PartiesService) Leave(ctx context.Context, id, version int64) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "leaveParty",
		Method:      http.MethodDelete,
		Path:        partyPath(id, "/members/me"),
		Body:        partyVersionBody{ExpectedVersion: version},
	})
}

// SetReady sets the caller's readiness and matchmaking properties. The
// other members see props.Attributes exactly as sent.
func (p *PartiesService) SetReady(ctx context.Context, id, version int64, ready bool, props PartyProperties) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "readyPartyMember",
		Method:      http.MethodPut,
		Path:        partyPath(id, "/members/me/ready"),
		Body:        partyReadyBody{ExpectedVersion: version, Ready: ready, Properties: props},
	})
}

// Kick removes playerID from the party. Leader only.
func (p *PartiesService) Kick(ctx context.Context, id, version, playerID int64) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "kickPartyMember",
		Method:      http.MethodDelete,
		Path:        partyPath(id, "/members/"+strconv.FormatInt(playerID, 10)),
		Body:        partyVersionBody{ExpectedVersion: version},
	})
}

// CreateCode makes a new invite code and revokes the older ones. Leader
// only. maxUses is 1 to 7; 0 uses the server default (7).
func (p *PartiesService) CreateCode(ctx context.Context, id, version int64, maxUses int) (*PartyCode, error) {
	var out PartyCode
	err := p.c.callProtected(ctx, &Request{
		OperationID: "createPartyCode",
		Method:      http.MethodPost,
		Path:        partyPath(id, "/invite-codes"),
		Body:        partyCodeBody{ExpectedVersion: version, MaxUses: maxUses},
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeCode revokes an invite code. Leader only. Returns ErrNotFound when
// the code is unknown or already revoked.
func (p *PartiesService) RevokeCode(ctx context.Context, id, version, codeID int64) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "revokePartyCode",
		Method:      http.MethodDelete,
		Path:        partyPath(id, "/invite-codes/"+strconv.FormatInt(codeID, 10)),
		Body:        partyVersionBody{ExpectedVersion: version},
	})
}

// InviteFriend invites an accepted friend. Leader only. A re-invite of a
// pending invite only refreshes its expiry and sends no new event.
func (p *PartiesService) InviteFriend(ctx context.Context, id, version, playerID int64) (*PartyInvite, error) {
	var out PartyInvite
	err := p.c.callProtected(ctx, &Request{
		OperationID: "invitePartyFriend",
		Method:      http.MethodPost,
		Path:        partyPath(id, "/invites"),
		Body:        partyInviteBody{ExpectedVersion: version, PlayerID: playerID},
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListInvites returns the caller's pending party invites. It is the source
// of truth; the party_invite event is only a hint.
func (p *PartiesService) ListInvites(ctx context.Context) ([]PartyInvite, error) {
	var out []PartyInvite
	err := p.c.callProtected(ctx, &Request{
		OperationID: "listPartyInvites",
		Method:      http.MethodGet,
		Path:        "/v1/party-invites",
	}, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AcceptInvite joins the party of an invite. version is the party version,
// for example PartyInvite.PartyVersion. Returns ErrNotFound when the invite
// is unknown, expired, or not for the caller.
func (p *PartiesService) AcceptInvite(ctx context.Context, inviteID, version int64) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "acceptPartyInvite",
		Method:      http.MethodPost,
		Path:        partyInvitePath(inviteID, "/accept"),
		Body:        partyVersionBody{ExpectedVersion: version},
	})
}

// DeclineInvite declines an invite, or revokes it when the caller is the
// leader. version is the party version. Returns ErrNotFound for any other
// player, and when the invite is unknown or expired.
func (p *PartiesService) DeclineInvite(ctx context.Context, inviteID, version int64) error {
	return p.c.callProtected(ctx, &Request{
		OperationID: "declinePartyInvite",
		Method:      http.MethodDelete,
		Path:        partyInvitePath(inviteID, ""),
		Body:        partyVersionBody{ExpectedVersion: version},
	}, nil)
}

// Queue puts the ready party in the matchmaking queue. Leader only. key is
// the Idempotency-Key; pass the same key to retry safely, or "" to let the
// SDK make one. Returns an error matching ErrPartyEnqueueDisabled when the
// server turns party queue off.
func (p *PartiesService) Queue(ctx context.Context, id, version int64, key string) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "queueParty",
		Method:      http.MethodPost,
		Path:        partyPath(id, "/queue"),
		Body:        partyVersionBody{ExpectedVersion: version},
		Header:      idempotencyHeader(key),
	})
}

// CancelQueue takes the whole party out of the queue. Leader only.
func (p *PartiesService) CancelQueue(ctx context.Context, id, version int64) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "cancelPartyQueue",
		Method:      http.MethodDelete,
		Path:        partyPath(id, "/queue"),
		Body:        partyVersionBody{ExpectedVersion: version},
	})
}

// Rematch queues the party again after lastMatchID. Leader only. key works
// as in Queue.
func (p *PartiesService) Rematch(ctx context.Context, id, version int64, lastMatchID, key string) (*Party, error) {
	return p.party(ctx, &Request{
		OperationID: "rematchParty",
		Method:      http.MethodPost,
		Path:        partyPath(id, "/rematch"),
		Body:        partyRematchBody{ExpectedVersion: version, LastMatchID: lastMatchID},
		Header:      idempotencyHeader(key),
	})
}

// PartyEvent is one update from Watch. One field is set.
type PartyEvent struct {
	// Party is set when the party has a newer version than the last one
	// reported.
	Party *Party
	// Invite is set when a friend invites the caller to a party.
	Invite *PartyInviteEvent
	// Match is set once for each match of the party.
	Match *MatchResult
	// Removed is true when the caller is no longer a member. Watch returns
	// after it.
	Removed bool
}

// Watch reports changes to party partyID to fn until ctx ends, fn returns
// an error, or the caller is no longer a member. fn runs serially.
//
// Watch reads the party first, then heartbeats every 10 seconds, and reads
// party_changed, party_invite and matchmaker_matched on one realtime
// socket. A Party is reported only when its version is newer than the last
// one reported. Events are a hint: when one is lost, the next heartbeat
// catches up. When the party is matched, Match is reported once, from the
// realtime event or from the caller's ticket.
//
// With partyID 0, Watch reports party invites only and does not heartbeat;
// it returns the dial error when the realtime socket cannot open. With a
// party, a failed dial is logged and Watch continues on the heartbeat
// alone, without invites (use ListInvites). Watch opens its own realtime
// socket, which closes an older socket of this player (see DialRealtime).
func (p *PartiesService) Watch(ctx context.Context, partyID int64, fn func(PartyEvent) error) error {
	sess := p.c.Session()
	if sess == nil {
		return errors.New("ggscale: no session — call Login or SetSession first")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	w := &partyWatch{p: p, partyID: partyID, me: sess.PlayerID, fn: fn}
	err := w.run(ctx)
	if errors.Is(err, errWatchDone) {
		return nil
	}
	return err
}

// WaitForMatch blocks until party partyID is matched and returns the match.
// Call it when the party is queued, for example after a party_changed event
// with state "queued". If the party is already matched, it returns that
// match. When the queue entry fails or is cancelled, it returns
// *MatchFailedError or ErrMatchCancelled. When the caller is no longer a
// member, it returns ErrNotPartyMember. It uses Watch, so it also heartbeats.
func (p *PartiesService) WaitForMatch(ctx context.Context, partyID int64) (*MatchResult, error) {
	sess := p.c.Session()
	if sess == nil {
		return nil, errors.New("ggscale: no session — call Login or SetSession first")
	}
	var result *MatchResult
	var queuedTicket int64
	err := p.Watch(ctx, partyID, func(ev PartyEvent) error {
		switch {
		case ev.Match != nil:
			result = ev.Match
			return errWatchDone
		case ev.Removed:
			return ErrNotPartyMember
		case ev.Party == nil:
			return nil
		}
		me := ev.Party.Member(sess.PlayerID)
		if ev.Party.State == PartyQueued && me != nil {
			queuedTicket = me.TicketID
			return nil
		}
		if ev.Party.State != PartyIdle || queuedTicket == 0 {
			return nil
		}
		// The entry left the queue without a match.
		t, err := p.c.Matchmaker.GetTicket(ctx, queuedTicket)
		queuedTicket = 0
		if err != nil {
			return nil
		}
		if _, done, rerr := resultFromTicket(t); done && rerr != nil {
			return rerr
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type partyWatch struct {
	p       *PartiesService
	partyID int64
	me      int64
	fn      func(PartyEvent) error
	// version is the last reported party version; lastMatch is the last
	// reported match ID.
	version   int64
	lastMatch string
}

func (w *partyWatch) run(ctx context.Context) error {
	var msgs chan Message
	rc, err := w.p.c.DialRealtime(ctx)
	switch {
	case err == nil:
		defer func() { _ = rc.Close() }()
		msgs = make(chan Message, 8)
		go readMatchMessages(ctx, rc, msgs)
	case w.partyID == 0:
		// An invites-only watch has no heartbeat to fall back on.
		return err
	default:
		// The heartbeat keeps the party current; invites need ListInvites.
		event := LogEvent{Level: "warn", Event: "party.watch.realtime_unavailable", OperationID: realtimeOperationID}
		var dialErr *RealtimeDialError
		if errors.As(err, &dialErr) {
			event.Status = dialErr.Status
		}
		w.p.c.safeLog(event)
	}

	var tick <-chan time.Time
	if w.partyID != 0 {
		if err := w.refresh(ctx); err != nil {
			return err
		}
		interval := w.p.heartbeatInterval
		if interval <= 0 {
			interval = defaultPartyHeartbeatInterval
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		tick = ticker.C
	}

	for {
		var err error
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-msgs:
			if !ok {
				msgs = nil // socket closed; the heartbeat keeps the state current
				continue
			}
			err = w.handle(ctx, msg)
		case <-tick:
			err = w.heartbeat(ctx)
		}
		if err != nil {
			return err
		}
	}
}

func (w *partyWatch) handle(ctx context.Context, msg Message) error {
	switch msg.Type {
	case EventPartyInvite:
		var ev PartyInviteEvent
		if msg.DecodePayload(&ev) != nil {
			return nil
		}
		return w.fn(PartyEvent{Invite: &ev})
	case EventPartyChanged:
		var ev PartyChangedEvent
		if msg.DecodePayload(&ev) != nil || w.partyID == 0 || ev.PartyID != w.partyID || ev.Version <= w.version {
			return nil
		}
		return w.refresh(ctx)
	case EventMatchmakerMatched:
		if w.partyID == 0 {
			return nil
		}
		return w.matched(ctx, msg)
	}
	return nil
}

// matched reports a matchmaker_matched event when the roster has the
// caller as a member of this party.
func (w *partyWatch) matched(ctx context.Context, msg Message) error {
	pushed, err := parseMatchedPayload(msg.Payload)
	if err != nil || pushed.MatchID == w.lastMatch {
		return nil
	}
	inParty := false
	for _, u := range pushed.Users {
		if u.PlayerID == w.me && u.PartyID == w.partyID {
			inParty = true
			break
		}
	}
	if !inParty {
		return nil
	}
	// As in WaitForMatch, the ticket is the complete result; the push is the
	// fallback.
	result := pushed
	if t, gerr := w.p.c.Matchmaker.GetTicket(ctx, pushed.TicketID); gerr == nil {
		if res, done, _ := resultFromTicket(t); done && res != nil {
			result = res
		}
	}
	w.lastMatch = pushed.MatchID
	return w.fn(PartyEvent{Match: result})
}

func (w *partyWatch) refresh(ctx context.Context) error {
	party, err := w.p.Get(ctx, w.partyID)
	if errors.Is(err, ErrNotFound) {
		return w.removed()
	}
	if err != nil {
		return nil // transient; the next heartbeat tries again
	}
	return w.update(ctx, party)
}

func (w *partyWatch) heartbeat(ctx context.Context) error {
	party, err := w.p.Heartbeat(ctx, w.partyID, w.version)
	switch {
	case err == nil:
		return w.update(ctx, party)
	case errors.Is(err, ErrNotFound):
		return w.removed()
	case errors.Is(err, ErrStaleVersion):
		return w.refresh(ctx)
	}
	return nil // transient; the next heartbeat tries again
}

func (w *partyWatch) removed() error {
	if err := w.fn(PartyEvent{Removed: true}); err != nil {
		return err
	}
	return errWatchDone
}

// update reports a newer party, then reports its match from the caller's
// ticket when the realtime event did not.
func (w *partyWatch) update(ctx context.Context, party *Party) error {
	if party.Version > w.version {
		w.version = party.Version
		if err := w.fn(PartyEvent{Party: party}); err != nil {
			return err
		}
	}
	if party.State != PartyMatched || party.LastMatchID == "" || party.LastMatchID == w.lastMatch {
		return nil
	}
	me := party.Member(w.me)
	if me == nil || me.TicketID == 0 {
		return nil
	}
	t, err := w.p.c.Matchmaker.GetTicket(ctx, me.TicketID)
	if err != nil || t.MatchID != party.LastMatchID {
		return nil // the next heartbeat tries again
	}
	res, done, _ := resultFromTicket(t)
	if !done || res == nil {
		return nil
	}
	w.lastMatch = t.MatchID
	return w.fn(PartyEvent{Match: res})
}
