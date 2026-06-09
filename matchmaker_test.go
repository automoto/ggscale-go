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

// JSON round-trip helper used by RequestMatch in realtime_test.go.
func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
