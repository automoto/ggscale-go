package ggscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// MatchmakerService exposes the /v1/matchmaker/tickets endpoints. Reach it
// via Client.Matchmaker.
type MatchmakerService struct {
	c *Client
	// pollInterval overrides the WaitForMatch recovery poll cadence; 0 uses
	// defaultMatchPollInterval. Set only in tests.
	pollInterval time.Duration
}

// Mode selects what a matched ticket resolves to.
type Mode string

// Matchmaking result modes.
const (
	// ModeMatchOnly returns a bare roster: peers connect to each other
	// (bring-your-own-signaling P2P). The match names a host.
	ModeMatchOnly Mode = "match_only"
	// ModeGameSession creates a joinable game session for the roster
	// (host/listen-server P2P). The match names a host and a session.
	ModeGameSession Mode = "game_session"
	// ModeFleetAllocation provisions a dedicated server (beta). The match
	// carries the server address, not a host player.
	ModeFleetAllocation Mode = "fleet_allocation"
)

// EventMatchmakerMatched is the realtime envelope type pushed when a ticket is
// matched. WaitForMatch consumes it and also recovers via ticket polling.
const EventMatchmakerMatched = "matchmaker_matched"

// MatchRequest is the input to CreateTicket / WaitForMatch. Zero-valued
// fields fall back to server defaults; leave Mode empty to let the server
// infer it (fleet present → fleet_allocation, otherwise match_only).
type MatchRequest struct {
	Mode             Mode   `json:"mode,omitempty"`
	Fleet            string `json:"fleet,omitempty"`
	Region           string `json:"region,omitempty"`
	AllowCrossRegion *bool  `json:"allow_cross_region,omitempty"`
	GameMode         string `json:"game_mode,omitempty"`
	MinCount         int    `json:"min_count,omitempty"`
	MaxCount         int    `json:"max_count,omitempty"`
	CountMultiple    int    `json:"count_multiple,omitempty"`
	Query            string `json:"query,omitempty"`
	// StringProperties / NumericProperties are matched against other
	// tickets' queries.
	StringProperties  map[string]string  `json:"string_properties,omitempty"`
	NumericProperties map[string]float64 `json:"numeric_properties,omitempty"`
	// Attributes is opaque JSON echoed to matched peers via the roster
	// (visible to every peer; capped at 4 KiB server-side). Use it to carry
	// connect info for match_only P2P.
	Attributes json.RawMessage `json:"attributes,omitempty"`
}

// RosterEntry is one matched player, including the opaque attributes they
// queued with so peers can exchange connect info.
type RosterEntry struct {
	PlayerID          int64              `json:"player_id"`
	Region            string             `json:"region,omitempty"`
	StringProperties  map[string]string  `json:"string_properties,omitempty"`
	NumericProperties map[string]float64 `json:"numeric_properties,omitempty"`
	Attributes        json.RawMessage    `json:"attributes,omitempty"`
}

// Ticket is one in-flight (or settled) matchmaking request as returned by
// CreateTicket and GetTicket. Once matched it carries the full result so a
// missed WebSocket push is recoverable by polling.
type Ticket struct {
	ID               int64  `json:"id"`
	Status           string `json:"status"`
	Mode             string `json:"mode"`
	Region           string `json:"region"`
	AllowCrossRegion bool   `json:"allow_cross_region"`
	GameMode         string `json:"game_mode"`
	MinCount         int    `json:"min_count"`
	MaxCount         int    `json:"max_count"`
	CountMultiple    int    `json:"count_multiple"`
	Query            string `json:"query,omitempty"`

	StringProperties  map[string]string  `json:"string_properties,omitempty"`
	NumericProperties map[string]float64 `json:"numeric_properties,omitempty"`
	Attributes        json.RawMessage    `json:"attributes,omitempty"`

	MatchID      string `json:"match_id,omitempty"`
	MatchAddress string `json:"match_address"`
	ProtocolHint string `json:"protocol_hint,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	JoinCode     string `json:"join_code,omitempty"`
	// HostPlayerID is the player peers connect to for matched match_only and
	// game_session tickets. Zero for fleet_allocation.
	HostPlayerID int64 `json:"host_player_id,omitempty"`
	// FailureReason is set for failed tickets ("expired",
	// "attempts_exhausted", …). Treat as an open enum.
	FailureReason string        `json:"failure_reason,omitempty"`
	Users         []RosterEntry `json:"users,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	MatchedAt *time.Time `json:"matched_at,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// MatchResult is the unified outcome returned by WaitForMatch across every
// mode, parsed from the matchmaker_matched event or recovered from a polled
// ticket.
type MatchResult struct {
	TicketID int64
	MatchID  string
	Mode     string
	// Address / ProtocolHint are set for fleet_allocation.
	Address      string
	ProtocolHint string
	// SessionID / JoinCode are set for game_session.
	SessionID string
	JoinCode  string
	// HostPlayerID is the player peers connect to for match_only and
	// game_session. Zero for fleet_allocation.
	HostPlayerID int64
	// Users is the full roster, each entry carrying the peer's attributes.
	Users []RosterEntry
}

// MatchFailedError is returned by WaitForMatch when the ticket ends in a
// failed state. Reason is the server's machine-readable failure_reason when
// available.
type MatchFailedError struct{ Reason string }

func (e *MatchFailedError) Error() string {
	if e.Reason != "" {
		return "ggscale: matchmaking failed: " + e.Reason
	}
	return "ggscale: matchmaking failed"
}

// ErrMatchCancelled is returned by WaitForMatch when the ticket was cancelled
// (e.g. by another device) before a match formed.
var ErrMatchCancelled = errors.New("ggscale: matchmaking ticket cancelled")

// defaultMatchPollInterval is how often WaitForMatch polls the ticket as a
// recovery path alongside the realtime push.
const defaultMatchPollInterval = 2 * time.Second

// CreateTicket enqueues a new matchmaking ticket. The ticket starts "queued";
// use WaitForMatch for a helper that blocks until it is matched. A player may
// hold only one active ticket per project: a second create returns an *Error
// matching ErrTicketActive, whose ActiveTicketID names the ticket to cancel.
func (m *MatchmakerService) CreateTicket(ctx context.Context, req MatchRequest) (*Ticket, error) {
	var ticket Ticket
	err := m.c.callProtected(ctx, &Request{
		Method: http.MethodPost,
		Path:   "/v1/matchmaker/tickets",
		Body:   req,
	}, &ticket)
	if err != nil {
		return nil, err
	}
	return &ticket, nil
}

// GetTicket returns a ticket by id. Returns ErrNotFound if the ticket does
// not exist or belongs to another tenant. A matched ticket carries the full
// result (host, roster, session/address) for missed-push recovery.
func (m *MatchmakerService) GetTicket(ctx context.Context, id int64) (*Ticket, error) {
	var ticket Ticket
	err := m.c.callProtected(ctx, &Request{
		Method: http.MethodGet,
		Path:   "/v1/matchmaker/tickets/" + strconv.FormatInt(id, 10),
	}, &ticket)
	if err != nil {
		return nil, err
	}
	return &ticket, nil
}

// CancelTicket cancels a queued ticket. Returns ErrNotFound if the ticket is
// unknown, or ErrConflict if it has already reached a terminal status.
func (m *MatchmakerService) CancelTicket(ctx context.Context, id int64) error {
	return m.c.callProtected(ctx, &Request{
		Method: http.MethodDelete,
		Path:   "/v1/matchmaker/tickets/" + strconv.FormatInt(id, 10),
	}, nil)
}

// WaitForMatch creates a ticket and blocks until it is matched, returning the
// unified result for any mode. It combines the realtime push with periodic
// authenticated ticket polling, so a dropped WebSocket still returns the
// persisted match before its TTL. On context cancellation it best-effort
// cancels a still-queued ticket. A failed ticket returns *MatchFailedError; a
// cancelled ticket returns ErrMatchCancelled.
func (m *MatchmakerService) WaitForMatch(ctx context.Context, req MatchRequest) (*MatchResult, error) {
	// Dial realtime BEFORE creating the ticket so the push can't beat our hub
	// registration. Best-effort: if the dial fails we fall back to polling.
	rc, _ := m.c.DialRealtime(ctx)
	if rc != nil {
		defer func() { _ = rc.Close() }()
	}

	ticket, err := m.CreateTicket(ctx, req)
	if err != nil {
		return nil, err
	}
	if res, done, rerr := resultFromTicket(ticket); done {
		return res, rerr
	}

	var msgs chan Message
	if rc != nil {
		msgs = make(chan Message, 8)
		go readMatchMessages(ctx, rc, msgs)
	}

	interval := m.pollInterval
	if interval <= 0 {
		interval = defaultMatchPollInterval
	}
	poll := time.NewTicker(interval)
	defer poll.Stop()

	for {
		select {
		case <-ctx.Done():
			m.cancelBestEffort(ctx, ticket.ID)
			return nil, ctx.Err()
		case msg, ok := <-msgs:
			if !ok {
				msgs = nil // WS closed; keep polling
				continue
			}
			if msg.Type != EventMatchmakerMatched {
				continue
			}
			pushed, perr := parseMatchedPayload(msg.Payload)
			if perr != nil || (pushed.TicketID != 0 && pushed.TicketID != ticket.ID) {
				continue // malformed or another ticket; polling will recover
			}
			// The push only signals that the ticket settled; read the ticket
			// for the authoritative, complete result (mode, host, roster) so a
			// lightweight push can't degrade what we return. If the read can't
			// settle it (transient error or a not-yet-visible write), return
			// the pushed payload directly — it already carries the full result
			// from the current server.
			if got, gerr := m.GetTicket(ctx, ticket.ID); gerr == nil {
				if res, done, rerr := resultFromTicket(got); done {
					return res, rerr
				}
			}
			return pushed, nil
		case <-poll.C:
			got, gerr := m.GetTicket(ctx, ticket.ID)
			if gerr != nil {
				continue // transient; retry on the next tick
			}
			if res, done, rerr := resultFromTicket(got); done {
				return res, rerr
			}
		}
	}
}

// RequestMatch is retained for compatibility.
//
// Deprecated: use WaitForMatch, which returns the same unified result and adds
// polling recovery.
func (m *MatchmakerService) RequestMatch(ctx context.Context, req MatchRequest) (*MatchResult, error) {
	return m.WaitForMatch(ctx, req)
}

// P2PMatch bundles everything a peer needs to connect after a peer-to-peer
// match: the unified result (host + roster), TURN relay credentials scoped to
// the match (nil when the relay is disabled), and — for game_session — the
// joined session with the current peer endpoints. The SDK gathers the
// coordination data; opening the actual peer connections (direct where
// possible, relayed via Relay as a fallback) is the game's responsibility.
type P2PMatch struct {
	Result  *MatchResult
	Relay   *Credentials
	Session *GameSession
	// IsHost reports whether the local player is the designated host.
	IsHost bool
}

// ConnectP2P waits for a peer-to-peer match (match_only or game_session),
// fetches TURN relay credentials scoped to the match, and — for game_session —
// joins the session announcing selfAddr so peers can discover this player's
// endpoint. selfAddr is the local player's public address (typically learned
// via STUN); it is used only for game_session and may be zero for match_only.
// Returns an error for fleet_allocation matches (use MatchResult.Address).
func (m *MatchmakerService) ConnectP2P(ctx context.Context, req MatchRequest, selfAddr GameSessionAddr) (*P2PMatch, error) {
	res, err := m.WaitForMatch(ctx, req)
	if err != nil {
		return nil, err
	}
	if res.Mode == string(ModeFleetAllocation) {
		return nil, errors.New("ggscale: ConnectP2P does not apply to fleet_allocation matches; use MatchResult.Address")
	}

	out := &P2PMatch{Result: res}
	if sess := m.c.Session(); sess != nil {
		out.IsHost = res.HostPlayerID == sess.PlayerID
	}
	// Relay credentials are best-effort: the relay may be disabled for the
	// project, and direct connections often need no relay at all.
	if creds, rerr := m.c.Relay.GetCredentials(ctx, WithMatch(res.MatchID)); rerr == nil {
		out.Relay = creds
	}
	if res.Mode == string(ModeGameSession) && res.SessionID != "" {
		sess, jerr := m.c.GameSessions.Join(ctx, res.SessionID, selfAddr)
		if jerr != nil {
			return nil, fmt.Errorf("join match session: %w", jerr)
		}
		out.Session = sess
	}
	return out, nil
}

func (m *MatchmakerService) cancelBestEffort(ctx context.Context, id int64) {
	_ = m.CancelTicket(context.WithoutCancel(ctx), id)
}

// readMatchMessages forwards realtime messages to out until the connection
// drops or ctx is cancelled, then closes out.
func readMatchMessages(ctx context.Context, rc *RealtimeClient, out chan<- Message) {
	defer close(out)
	for {
		msg, err := rc.ReadMessage(ctx)
		if err != nil {
			return
		}
		select {
		case out <- msg:
		case <-ctx.Done():
			return
		}
	}
}

// resultFromTicket derives a terminal result from a ticket. done is true once
// the ticket has settled; a matched ticket yields the result, failed yields
// *MatchFailedError, cancelled yields ErrMatchCancelled.
func resultFromTicket(t *Ticket) (res *MatchResult, done bool, err error) {
	switch t.Status {
	case "matched":
		return &MatchResult{
			TicketID:     t.ID,
			MatchID:      t.MatchID,
			Mode:         t.Mode,
			Address:      t.MatchAddress,
			ProtocolHint: t.ProtocolHint,
			SessionID:    t.SessionID,
			JoinCode:     t.JoinCode,
			HostPlayerID: t.HostPlayerID,
			Users:        t.Users,
		}, true, nil
	case "failed":
		// Includes TTL expiry, which the server settles as status "failed"
		// with failure_reason "expired".
		return nil, true, &MatchFailedError{Reason: t.FailureReason}
	case "cancelled":
		return nil, true, ErrMatchCancelled
	}
	return nil, false, nil
}

// parseMatchedPayload decodes a matchmaker_matched event payload. It is a
// best-effort fallback for WaitForMatch when the authoritative ticket read
// can't settle the push; the polled ticket (via resultFromTicket) is the
// primary result path, so keep the two field mappings in sync.
func parseMatchedPayload(raw json.RawMessage) (*MatchResult, error) {
	var p struct {
		TicketID     int64         `json:"ticket_id"`
		MatchID      string        `json:"match_id"`
		Mode         string        `json:"mode"`
		Address      string        `json:"address"`
		ProtocolHint string        `json:"protocol_hint"`
		SessionID    string        `json:"session_id"`
		JoinCode     string        `json:"join_code"`
		HostPlayerID int64         `json:"host_player_id"`
		Users        []RosterEntry `json:"users"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &MatchResult{
		TicketID:     p.TicketID,
		MatchID:      p.MatchID,
		Mode:         p.Mode,
		Address:      p.Address,
		ProtocolHint: p.ProtocolHint,
		SessionID:    p.SessionID,
		JoinCode:     p.JoinCode,
		HostPlayerID: p.HostPlayerID,
		Users:        p.Users,
	}, nil
}
