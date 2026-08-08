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

func TestMatchmakerService_CreateTicket(t *testing.T) {
	ft := &fakeTransport{
		respond: func(req *Request) (any, error) {
			assert.Equal(t, http.MethodPost, req.Method)
			assert.Equal(t, "/v1/matchmaker/tickets", req.Path)
			return map[string]any{
				"id":         int64(7),
				"status":     "queued",
				"region":     "us-east-1",
				"game_mode":  "deathmatch",
				"created_at": time.Now().UTC().Format(time.RFC3339Nano),
			}, nil
		},
	}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	ticket, err := c.Matchmaker.CreateTicket(context.Background(), MatchRequest{
		Fleet: "docker-default", Region: "us-east-1", GameMode: "deathmatch",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(7), ticket.ID)
	assert.Equal(t, "queued", ticket.Status)
}

func TestMatchmakerService_GetTicket(t *testing.T) {
	ft := &fakeTransport{
		respond: func(req *Request) (any, error) {
			assert.Equal(t, http.MethodGet, req.Method)
			assert.Equal(t, "/v1/matchmaker/tickets/42", req.Path)
			return map[string]any{
				"id":            int64(42),
				"status":        "matched",
				"match_address": "10.0.0.1:7777",
				"created_at":    time.Now().UTC().Format(time.RFC3339Nano),
			}, nil
		},
	}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	ticket, err := c.Matchmaker.GetTicket(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, "matched", ticket.Status)
	assert.Equal(t, "10.0.0.1:7777", ticket.MatchAddress)
}

func TestMatchmakerService_CancelTicket(t *testing.T) {
	ft := &fakeTransport{
		respond: func(req *Request) (any, error) {
			assert.Equal(t, http.MethodDelete, req.Method)
			assert.Equal(t, "/v1/matchmaker/tickets/99", req.Path)
			return nil, nil
		},
	}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	err := c.Matchmaker.CancelTicket(context.Background(), 99)
	require.NoError(t, err)
}

func TestMatchmakerService_RequestMatch_success(t *testing.T) {
	// We'll test RequestMatch in realtime_test.go where we have a real WS
	// server. Here we just verify the happy-path HTTP wiring for the lower
	// level methods.
}

func TestMatchmakerService_CreateTicket_401_no_session(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})

	_, err := c.Matchmaker.CreateTicket(context.Background(), MatchRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no session")
}

func TestMatchmakerService_GetTicket_not_found(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return nil, &Error{Status: http.StatusNotFound}
		},
	}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	_, err := c.Matchmaker.GetTicket(context.Background(), 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestMatchmakerService_CancelTicket_conflict(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return nil, &Error{Status: http.StatusConflict, Message: "ticket already finalised"}
		},
	}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	err := c.Matchmaker.CancelTicket(context.Background(), 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConflict))
}

func TestMatchmakerService_GetTicket_parses_GA_fields(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"id":             int64(42),
				"status":         "matched",
				"mode":           "game_session",
				"match_id":       "mm_abc",
				"session_id":     "gs_1",
				"join_code":      "CODE01",
				"host_player_id": int64(41),
				"created_at":     time.Now().UTC().Format(time.RFC3339Nano),
				"users": []any{
					map[string]any{"player_id": int64(41), "attributes": map[string]any{"lobby": "A"}},
					map[string]any{"player_id": int64(42), "attributes": map[string]any{"lobby": "B"}},
				},
			}, nil
		},
	}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	ticket, err := c.Matchmaker.GetTicket(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, "game_session", ticket.Mode)
	assert.Equal(t, "gs_1", ticket.SessionID)
	assert.Equal(t, "CODE01", ticket.JoinCode)
	assert.Equal(t, int64(41), ticket.HostPlayerID)
	require.Len(t, ticket.Users, 2)
	assert.Equal(t, int64(41), ticket.Users[0].PlayerID)
	assert.JSONEq(t, `{"lobby":"A"}`, string(ticket.Users[0].Attributes))
}

func TestMatchmakerService_GetTicket_failure_reason(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"id":             int64(9),
				"status":         "failed",
				"failure_reason": "expired",
				"created_at":     time.Now().UTC().Format(time.RFC3339Nano),
			}, nil
		},
	}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	ticket, err := c.Matchmaker.GetTicket(context.Background(), 9)
	require.NoError(t, err)
	assert.Equal(t, "failed", ticket.Status)
	assert.Equal(t, "expired", ticket.FailureReason)
}

func TestMatchmakerService_CreateTicket_ticket_already_active(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return nil, &Error{
				Status:  http.StatusConflict,
				Message: "ticket_already_active",
				Details: []ErrorDetail{{Location: "active_ticket_id", Value: json.RawMessage("55")}},
			}
		},
	}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	_, err := c.Matchmaker.CreateTicket(context.Background(), MatchRequest{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTicketActive), "should match ErrTicketActive")
	var e *Error
	require.ErrorAs(t, err, &e)
	assert.Equal(t, int64(55), e.ActiveTicketID())
}

func TestMatchmakerService_WaitForMatch_recovers_by_polling(t *testing.T) {
	// No BaseURL → the realtime dial fails and WaitForMatch falls back to
	// polling. The ticket is queued on create, then matched on the next poll.
	calls := 0
	ft := &fakeTransport{
		respond: func(req *Request) (any, error) {
			if req.Method == http.MethodPost {
				return map[string]any{"id": int64(7), "status": "queued", "created_at": time.Now().UTC().Format(time.RFC3339Nano)}, nil
			}
			calls++
			status := "queued"
			if calls >= 2 {
				status = "matched"
			}
			return map[string]any{
				"id": int64(7), "status": status, "mode": "match_only",
				"match_id": "mm_xy", "host_player_id": int64(7),
				"created_at": time.Now().UTC().Format(time.RFC3339Nano),
				"users":      []any{map[string]any{"player_id": int64(7)}, map[string]any{"player_id": int64(8)}},
			}, nil
		},
	}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})
	c.Matchmaker.pollInterval = 5 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := c.Matchmaker.WaitForMatch(ctx, MatchRequest{Mode: ModeMatchOnly})

	require.NoError(t, err)
	assert.Equal(t, "match_only", res.Mode)
	assert.Equal(t, int64(7), res.HostPlayerID)
	require.Len(t, res.Users, 2)
}

func TestMatchmakerService_WaitForMatch_failed_ticket(t *testing.T) {
	ft := &fakeTransport{
		respond: func(req *Request) (any, error) {
			if req.Method == http.MethodPost {
				return map[string]any{"id": int64(7), "status": "queued", "created_at": time.Now().UTC().Format(time.RFC3339Nano)}, nil
			}
			return map[string]any{"id": int64(7), "status": "failed", "failure_reason": "attempts_exhausted", "created_at": time.Now().UTC().Format(time.RFC3339Nano)}, nil
		},
	}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})
	c.Matchmaker.pollInterval = 5 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.Matchmaker.WaitForMatch(ctx, MatchRequest{})

	require.Error(t, err)
	var mfe *MatchFailedError
	require.ErrorAs(t, err, &mfe)
	assert.Equal(t, "attempts_exhausted", mfe.Reason)
}

func TestMatchmakerService_ConnectP2P_game_session_joins(t *testing.T) {
	var joinAddr map[string]any
	ft := &fakeTransport{
		respond: func(req *Request) (any, error) {
			switch {
			case req.Path == "/v1/matchmaker/tickets" && req.Method == http.MethodPost:
				return map[string]any{"id": int64(7), "status": "queued", "created_at": time.Now().UTC().Format(time.RFC3339Nano)}, nil
			case req.Path == "/v1/matchmaker/tickets/7" && req.Method == http.MethodGet:
				return map[string]any{
					"id": int64(7), "status": "matched", "mode": "game_session",
					"match_id": "mm_room", "session_id": "gs_9", "join_code": "JC01",
					"host_player_id": int64(41), "created_at": time.Now().UTC().Format(time.RFC3339Nano),
					"users": []any{map[string]any{"player_id": int64(41)}, map[string]any{"player_id": int64(42)}},
				}, nil
			case req.Path == "/v1/relay/credentials" && req.Method == http.MethodPost:
				assert.Equal(t, "mm_room", req.Query.Get("match_id"), "relay creds scoped to the match")
				return map[string]any{"username": "u", "password": "p", "ttl": int64(300), "realm": "ggscale"}, nil
			case req.Path == "/v1/game-session/gs_9/join" && req.Method == http.MethodPost:
				b, _ := json.Marshal(req.Body)
				_ = json.Unmarshal(b, &joinAddr)
				return map[string]any{
					"session_id": "gs_9", "join_code": "JC01", "state": "open",
					"peers": []any{
						map[string]any{"player_id": int64(41), "addr": map[string]any{"ip": "1.1.1.1", "port": 40000}},
						map[string]any{"player_id": int64(42), "addr": map[string]any{"ip": "2.2.2.2", "port": 40001}},
					},
				}, nil
			}
			return nil, &Error{Status: http.StatusNotFound}
		},
	}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", PlayerID: 41, ExpiresAt: time.Now().Add(time.Hour)})
	c.Matchmaker.pollInterval = 5 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p2p, err := c.Matchmaker.ConnectP2P(ctx, MatchRequest{Mode: ModeGameSession}, GameSessionAddr{IP: "3.3.3.3", Port: 50000})

	require.NoError(t, err)
	assert.True(t, p2p.IsHost, "player 41 is the designated host")
	require.NotNil(t, p2p.Relay)
	assert.Equal(t, "u", p2p.Relay.Username)
	require.NotNil(t, p2p.Session)
	require.Len(t, p2p.Session.Peers, 2)
	assert.NotNil(t, joinAddr["public_addr"], "join announces the local public address")
}

func TestMatchmakerService_ConnectP2P_surfaces_best_effort_relay_error(t *testing.T) {
	ft := &fakeTransport{
		respond: func(req *Request) (any, error) {
			switch req.OperationID {
			case "createMatchmakerTicket":
				return map[string]any{
					"id": int64(7), "status": "matched", "mode": "match_only",
					"match_id": "mm_room", "host_player_id": int64(41),
					"created_at": time.Now().UTC().Format(time.RFC3339Nano),
				}, nil
			case "issueRelayCredentials":
				return nil, &Error{Status: http.StatusForbidden, Message: "relay disabled"}
			default:
				return nil, &Error{Status: http.StatusNotFound}
			}
		},
	}
	c, err := NewClient(Options{APIKey: "k", Transport: ft})
	require.NoError(t, err)
	c.SetSession(&Session{AccessToken: "tok", PlayerID: 41, ExpiresAt: time.Now().Add(time.Hour)})

	p2p, err := c.Matchmaker.ConnectP2P(context.Background(), MatchRequest{Mode: ModeMatchOnly}, GameSessionAddr{})
	require.NoError(t, err, "relay remains best-effort when a direct connection may work")
	assert.Nil(t, p2p.Relay)
	require.Error(t, p2p.RelayError)
	assert.ErrorIs(t, p2p.RelayError, ErrForbidden)
}

// JSON round-trip helper used by RequestMatch in realtime_test.go.
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
