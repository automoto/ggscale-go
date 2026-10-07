package ggscale

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func partyJSON(version int64, state string, members ...map[string]any) map[string]any {
	if members == nil {
		members = []map[string]any{{"player_id": 1, "ready_version": 0}}
	}
	return map[string]any{
		"id": 9, "project_id": 1, "leader_id": 1, "state": state,
		"version": version, "roster_version": 1, "max_members": 4,
		"settings": map[string]any{"mode": "match_only", "min_count": 2, "max_count": 4, "count_multiple": 1},
		"members":  members,
	}
}

// bodyJSON returns the request body as a generic map.
func bodyJSON(t *testing.T, req *Request) map[string]any {
	t.Helper()
	raw, err := json.Marshal(req.Body)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestParties_wrappers_should_send_operation_path_and_version(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		call   func(c *Client) error
		opID   string
		method string
		path   string
	}{
		{"create", func(c *Client) error { _, err := c.Parties.Create(ctx, MatchRequest{Mode: ModeMatchOnly}); return err }, "createParty", http.MethodPost, "/v1/parties"},
		{"current", func(c *Client) error { _, err := c.Parties.Current(ctx); return err }, "getCurrentParty", http.MethodGet, "/v1/parties/current"},
		{"get", func(c *Client) error { _, err := c.Parties.Get(ctx, 9); return err }, "getParty", http.MethodGet, "/v1/parties/9"},
		{"update", func(c *Client) error { _, err := c.Parties.Update(ctx, 9, 3, MatchRequest{}); return err }, "updateParty", http.MethodPatch, "/v1/parties/9"},
		{"disband", func(c *Client) error { _, err := c.Parties.Disband(ctx, 9, 3); return err }, "disbandParty", http.MethodDelete, "/v1/parties/9"},
		{"heartbeat", func(c *Client) error { _, err := c.Parties.Heartbeat(ctx, 9, 3); return err }, "heartbeatParty", http.MethodPost, "/v1/parties/9/heartbeat"},
		{"join", func(c *Client) error { _, err := c.Parties.JoinByCode(ctx, "ABCDEFGHJKLMNPQR"); return err }, "joinPartyCode", http.MethodPost, "/v1/parties/join"},
		{"leave", func(c *Client) error { _, err := c.Parties.Leave(ctx, 9, 3); return err }, "leaveParty", http.MethodDelete, "/v1/parties/9/members/me"},
		{"ready", func(c *Client) error { _, err := c.Parties.SetReady(ctx, 9, 3, true, PartyProperties{}); return err }, "readyPartyMember", http.MethodPut, "/v1/parties/9/members/me/ready"},
		{"kick", func(c *Client) error { _, err := c.Parties.Kick(ctx, 9, 3, 5); return err }, "kickPartyMember", http.MethodDelete, "/v1/parties/9/members/5"},
		{"create code", func(c *Client) error { _, err := c.Parties.CreateCode(ctx, 9, 3, 2); return err }, "createPartyCode", http.MethodPost, "/v1/parties/9/invite-codes"},
		{"revoke code", func(c *Client) error { _, err := c.Parties.RevokeCode(ctx, 9, 3, 4); return err }, "revokePartyCode", http.MethodDelete, "/v1/parties/9/invite-codes/4"},
		{"invite", func(c *Client) error { _, err := c.Parties.InviteFriend(ctx, 9, 3, 5); return err }, "invitePartyFriend", http.MethodPost, "/v1/parties/9/invites"},
		{"list invites", func(c *Client) error { _, err := c.Parties.ListInvites(ctx); return err }, "listPartyInvites", http.MethodGet, "/v1/party-invites"},
		{"accept", func(c *Client) error { _, err := c.Parties.AcceptInvite(ctx, 6, 3); return err }, "acceptPartyInvite", http.MethodPost, "/v1/party-invites/6/accept"},
		{"decline", func(c *Client) error { return c.Parties.DeclineInvite(ctx, 6, 3) }, "declinePartyInvite", http.MethodDelete, "/v1/party-invites/6"},
		{"queue", func(c *Client) error { _, err := c.Parties.Queue(ctx, 9, 3, "k"); return err }, "queueParty", http.MethodPost, "/v1/parties/9/queue"},
		{"cancel queue", func(c *Client) error { _, err := c.Parties.CancelQueue(ctx, 9, 3); return err }, "cancelPartyQueue", http.MethodDelete, "/v1/parties/9/queue"},
		{"rematch", func(c *Client) error { _, err := c.Parties.Rematch(ctx, 9, 3, "m1", "k"); return err }, "rematchParty", http.MethodPost, "/v1/parties/9/rematch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
			c := newClientWithFake(t, ft)

			err := tc.call(c)

			require.NoError(t, err)
			assert.Equal(t, []string{tc.opID, tc.method, tc.path},
				[]string{ft.gotReq.OperationID, ft.gotReq.Method, ft.gotReq.Path})
		})
	}
}

func TestParties_versioned_writes_should_send_expected_version(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c := newClientWithFake(t, ft)

	_, err := c.Parties.Kick(context.Background(), 9, 3, 5)

	require.NoError(t, err)
	assert.Equal(t, map[string]any{"expected_version": float64(3)}, bodyJSON(t, ft.gotReq))
}

func TestParties_Create_should_send_settings_without_version(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c := newClientWithFake(t, ft)

	_, err := c.Parties.Create(context.Background(), MatchRequest{Mode: ModeMatchOnly, MinCount: 2})

	require.NoError(t, err)
	assert.Equal(t, map[string]any{"settings": map[string]any{"mode": "match_only", "min_count": float64(2)}}, bodyJSON(t, ft.gotReq))
}

func TestParties_SetReady_should_send_ready_and_properties(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c := newClientWithFake(t, ft)

	_, err := c.Parties.SetReady(context.Background(), 9, 3, true, PartyProperties{StringProperties: map[string]string{"role": "tank"}})

	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"expected_version": float64(3),
		"ready":            true,
		"properties":       map[string]any{"string_properties": map[string]any{"role": "tank"}},
	}, bodyJSON(t, ft.gotReq))
}

func TestParties_Queue_should_send_given_idempotency_key(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c := newClientWithFake(t, ft)

	_, err := c.Parties.Queue(context.Background(), 9, 3, "key-1")

	require.NoError(t, err)
	assert.Equal(t, "key-1", ft.gotReq.Header.Get("Idempotency-Key"))
}

func TestParties_Rematch_should_make_idempotency_key_when_empty(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c := newClientWithFake(t, ft)

	_, err := c.Parties.Rematch(context.Background(), 9, 3, "m1", "")

	require.NoError(t, err)
	assert.Len(t, ft.gotReq.Header.Get("Idempotency-Key"), 32)
}

func TestParties_Get_should_decode_party(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) {
		return partyJSON(4, PartyIdle, map[string]any{"player_id": 1, "ready_version": 1, "ticket_id": 70}), nil
	}}
	c := newClientWithFake(t, ft)

	got, err := c.Parties.Get(context.Background(), 9)

	require.NoError(t, err)
	member := got.Member(1)
	require.NotNil(t, member)
	assert.Equal(t, []any{int64(4), int64(70), true, 4}, []any{got.Version, member.TicketID, member.Ready(got), got.Settings.MaxCount})
}

func TestErrors_party_slugs_should_match_their_sentinels(t *testing.T) {
	cases := []struct {
		err    *Error
		target error
	}{
		{&Error{Status: http.StatusConflict, Message: "stale_version"}, ErrStaleVersion},
		{&Error{Status: http.StatusServiceUnavailable, Message: "party_enqueue_disabled"}, ErrPartyEnqueueDisabled},
		{&Error{Status: http.StatusTooManyRequests, Message: "code_redemption_cooldown"}, ErrCodeCooldown},
	}
	for _, tc := range cases {
		t.Run(tc.target.Error(), func(t *testing.T) {
			assert.ErrorIs(t, tc.err, tc.target)
		})
	}
}

func TestErrors_party_sentinels_should_not_match_other_slugs(t *testing.T) {
	err := &Error{Status: http.StatusConflict, Message: "party_full"}

	assert.NotErrorIs(t, err, ErrStaleVersion)
}

func TestAuth_CancelDelete_should_report_team_request_on_slug(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) {
		return nil, &Error{Status: http.StatusForbidden, Message: "delete_requested_by_team"}
	}}
	c := newClientWithFake(t, ft)

	err := c.Auth.CancelDelete(context.Background(), "p@example.com", "password")

	assert.True(t, errors.Is(err, ErrDeleteRequestedByTeam) && errors.Is(err, ErrForbidden))
}

// A revoked key or a disabled tenant is also 403, without the slug.
func TestAuth_CancelDelete_should_not_report_team_request_on_other_403(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) {
		return nil, &Error{Status: http.StatusForbidden, Message: "forbidden"}
	}}
	c := newClientWithFake(t, ft)

	err := c.Auth.CancelDelete(context.Background(), "p@example.com", "password")

	assert.NotErrorIs(t, err, ErrDeleteRequestedByTeam)
}

func TestParties_Watch_invites_only_should_return_dial_error(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c := newWatchClient(t, watchTransport([]any{partyJSON(1, PartyIdle)}, nil))

	err := c.Parties.Watch(ctx, 0, func(PartyEvent) error { return nil })

	assert.True(t, err != nil && !errors.Is(err, context.DeadlineExceeded), "got %v", err)
}

func TestAuth_CancelDelete_should_not_report_team_request_on_404(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) {
		return nil, &Error{Status: http.StatusNotFound}
	}}
	c := newClientWithFake(t, ft)

	err := c.Auth.CancelDelete(context.Background(), "p@example.com", "password")

	assert.NotErrorIs(t, err, ErrDeleteRequestedByTeam)
}

// watchTransport answers party reads from a list of responses, one per
// call, repeating the last one.
func watchTransport(parties []any, ticket map[string]any) *fakeTransport {
	i := 0
	return &fakeTransport{respond: func(req *Request) (any, error) {
		switch req.OperationID {
		case "getParty", "heartbeatParty":
			resp := parties[min(i, len(parties)-1)]
			i++
			if err, ok := resp.(error); ok {
				return nil, err
			}
			return resp, nil
		case "getMatchmakerTicket":
			return ticket, nil
		}
		return nil, nil
	}}
}

func newWatchClient(t *testing.T, ft *fakeTransport) *Client {
	t.Helper()
	c := newClientWithFake(t, ft)
	c.Parties.heartbeatInterval = time.Millisecond
	return c
}

func TestParties_Watch_should_report_each_version_once(t *testing.T) {
	notFound := &Error{Status: http.StatusNotFound}
	ft := watchTransport([]any{partyJSON(1, PartyIdle), partyJSON(1, PartyIdle), partyJSON(2, PartyIdle), notFound}, nil)
	c := newWatchClient(t, ft)
	var versions []int64

	err := c.Parties.Watch(context.Background(), 9, func(ev PartyEvent) error {
		if ev.Party != nil {
			versions = append(versions, ev.Party.Version)
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2}, versions)
}

func TestParties_Watch_should_report_removed_on_404(t *testing.T) {
	ft := watchTransport([]any{partyJSON(1, PartyIdle), &Error{Status: http.StatusNotFound}}, nil)
	c := newWatchClient(t, ft)
	removed := false

	err := c.Parties.Watch(context.Background(), 9, func(ev PartyEvent) error {
		removed = removed || ev.Removed
		return nil
	})

	require.NoError(t, err)
	assert.True(t, removed)
}

func TestParties_Watch_should_read_party_again_after_stale_heartbeat(t *testing.T) {
	stale := &Error{Status: http.StatusConflict, Message: "stale_version"}
	ft := watchTransport([]any{partyJSON(1, PartyIdle), stale, partyJSON(5, PartyIdle), &Error{Status: http.StatusNotFound}}, nil)
	c := newWatchClient(t, ft)
	var versions []int64

	err := c.Parties.Watch(context.Background(), 9, func(ev PartyEvent) error {
		if ev.Party != nil {
			versions = append(versions, ev.Party.Version)
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []int64{1, 5}, versions)
}

func matchedTicketJSON(id int64, matchID string) map[string]any {
	return map[string]any{
		"id": id, "entry_id": 30, "party_id": 9, "status": "matched", "mode": "match_only",
		"match_id": matchID, "host_player_id": 1, "match_address": "",
		"users":      []map[string]any{{"player_id": 1, "queue_entry_id": 30, "party_id": 9}},
		"created_at": "2026-10-07T10:00:00Z",
	}
}

func TestParties_WaitForMatch_should_return_match_from_ticket(t *testing.T) {
	queued := partyJSON(2, PartyQueued, map[string]any{"player_id": 1, "ticket_id": 70})
	matched := partyJSON(3, PartyMatched, map[string]any{"player_id": 1, "ticket_id": 70})
	matched["last_match_id"] = "m1"
	ft := watchTransport([]any{queued, matched}, matchedTicketJSON(70, "m1"))
	c := newWatchClient(t, ft)

	res, err := c.Parties.WaitForMatch(context.Background(), 9)

	require.NoError(t, err)
	assert.Equal(t, []any{"m1", int64(9)}, []any{res.MatchID, res.Users[0].PartyID})
}

func TestParties_WaitForMatch_should_return_failure_when_entry_leaves_queue(t *testing.T) {
	queued := partyJSON(2, PartyQueued, map[string]any{"player_id": 1, "ticket_id": 70})
	idle := partyJSON(3, PartyIdle, map[string]any{"player_id": 1, "ticket_id": 70})
	failed := map[string]any{"id": 70, "status": "failed", "failure_reason": "expired", "created_at": "2026-10-07T10:00:00Z"}
	ft := watchTransport([]any{queued, idle}, failed)
	c := newWatchClient(t, ft)

	_, err := c.Parties.WaitForMatch(context.Background(), 9)

	var failure *MatchFailedError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, "expired", failure.Reason)
}

func TestParties_WaitForMatch_should_return_not_member_when_removed(t *testing.T) {
	ft := watchTransport([]any{&Error{Status: http.StatusNotFound}}, nil)
	c := newWatchClient(t, ft)

	_, err := c.Parties.WaitForMatch(context.Background(), 9)

	assert.ErrorIs(t, err, ErrNotPartyMember)
}

func matchedMessage(t *testing.T, partyID int64) Message {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"ticket_id": 70, "match_id": "m1", "mode": "match_only",
		"users": []map[string]any{{"player_id": 1, "queue_entry_id": 30, "party_id": partyID}},
	})
	require.NoError(t, err)
	return Message{Type: EventMatchmakerMatched, Payload: raw}
}

func TestPartyWatch_should_report_match_event_for_this_party(t *testing.T) {
	ft := watchTransport([]any{partyJSON(1, PartyQueued)}, matchedTicketJSON(70, "m1"))
	c := newClientWithFake(t, ft)
	var got *MatchResult
	w := &partyWatch{p: c.Parties, partyID: 9, me: 1, fn: func(ev PartyEvent) error { got = ev.Match; return nil }}

	err := w.handle(context.Background(), matchedMessage(t, 9))

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "m1", got.MatchID)
}

func TestPartyWatch_should_ignore_match_event_for_another_party(t *testing.T) {
	ft := watchTransport([]any{partyJSON(1, PartyQueued)}, nil)
	c := newClientWithFake(t, ft)
	called := false
	w := &partyWatch{p: c.Parties, partyID: 9, me: 1, fn: func(PartyEvent) error { called = true; return nil }}

	err := w.handle(context.Background(), matchedMessage(t, 8))

	require.NoError(t, err)
	assert.False(t, called)
}

func TestPartyWatch_should_report_party_invite_event(t *testing.T) {
	c := newClientWithFake(t, &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }})
	var got *PartyInviteEvent
	w := &partyWatch{p: c.Parties, me: 1, fn: func(ev PartyEvent) error { got = ev.Invite; return nil }}

	err := w.handle(context.Background(), Message{Type: EventPartyInvite, Payload: json.RawMessage(`{"invite_id":6,"party_id":9,"from_player_id":2}`)})

	require.NoError(t, err)
	assert.Equal(t, &PartyInviteEvent{InviteID: 6, PartyID: 9, FromPlayerID: 2}, got)
}

func TestPartyWatch_should_read_party_on_newer_changed_event(t *testing.T) {
	ft := watchTransport([]any{partyJSON(4, PartyIdle)}, nil)
	c := newClientWithFake(t, ft)
	var got int64
	w := &partyWatch{p: c.Parties, partyID: 9, me: 1, version: 3, fn: func(ev PartyEvent) error { got = ev.Party.Version; return nil }}

	err := w.handle(context.Background(), Message{Type: EventPartyChanged, Payload: json.RawMessage(`{"party_id":9,"version":4,"state":"idle"}`)})

	require.NoError(t, err)
	assert.Equal(t, int64(4), got)
}

func TestPartyWatch_should_skip_changed_event_with_old_version(t *testing.T) {
	ft := watchTransport([]any{partyJSON(4, PartyIdle)}, nil)
	c := newClientWithFake(t, ft)
	w := &partyWatch{p: c.Parties, partyID: 9, me: 1, version: 4, fn: func(PartyEvent) error { return nil }}

	err := w.handle(context.Background(), Message{Type: EventPartyChanged, Payload: json.RawMessage(`{"party_id":9,"version":4,"state":"idle"}`)})

	require.NoError(t, err)
	assert.Equal(t, 0, ft.callCount)
}
