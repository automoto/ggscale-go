package ggscale

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealtimeClient_ReadMessage(t *testing.T) {
	msg := Message{Type: "match_ready", Payload: mustMarshal(map[string]any{
		"address":   "10.0.0.1:7777",
		"ticket_id": int64(7),
	})}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/ws", r.URL.Path)
		require.Equal(t, "Bearer k", r.Header.Get("Authorization"))
		require.Equal(t, "tok", r.Header.Get("X-Session-Token"))

		conn, err := websocket.Accept(w, r, nil)
		require.NoError(t, err)
		defer conn.Close(websocket.StatusNormalClosure, "")

		data, _ := json.Marshal(msg)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		require.NoError(t, conn.Write(ctx, websocket.MessageText, data))
	}))
	defer server.Close()

	c, _ := NewClient(Options{APIKey: "k", BaseURL: server.URL})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rc, err := c.DialRealtime(ctx)
	require.NoError(t, err)
	defer rc.Close()

	got, err := rc.ReadMessage(ctx)
	require.NoError(t, err)
	assert.Equal(t, "match_ready", got.Type)

	var payload struct {
		Address  string `json:"address"`
		TicketID int64  `json:"ticket_id"`
	}
	require.NoError(t, json.Unmarshal(got.Payload, &payload))
	assert.Equal(t, "10.0.0.1:7777", payload.Address)
	assert.Equal(t, int64(7), payload.TicketID)
}

func TestRealtimeClient_DialRealtime_no_baseurl(t *testing.T) {
	c, _ := NewClient(Options{APIKey: "k", Transport: &fakeTransport{}})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	_, err := c.DialRealtime(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot determine WebSocket URL")
}

func TestRealtimeClient_DialRealtime_no_session(t *testing.T) {
	c, _ := NewClient(Options{APIKey: "k", BaseURL: "http://localhost:8080"})

	_, err := c.DialRealtime(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no session")
}

// Regression: a stale session must not block WS dial. callProtected
// already refreshes on 401; DialRealtime now mirrors that behaviour so
// RequestMatch (which dials WS first) works against an expired session
// instead of failing on the upgrade.
func TestRealtimeClient_DialRealtime_RefreshesAndRetriesOn401(t *testing.T) {
	var (
		dialAttempts    int32
		refreshAttempts int32
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/auth/refresh" && r.Method == http.MethodPost:
			atomic.AddInt32(&refreshAttempts, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "fresh-tok",
				"refresh_token": "fresh-rt",
				"end_user_id":   int64(42),
				"expires_at":    time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
			})
		case r.URL.Path == "/v1/ws":
			n := atomic.AddInt32(&dialAttempts, 1)
			if n == 1 && r.Header.Get("X-Session-Token") == "stale-tok" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			conn, err := websocket.Accept(w, r, nil)
			require.NoError(t, err)
			conn.Close(websocket.StatusNormalClosure, "")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c, _ := NewClient(Options{APIKey: "k", BaseURL: server.URL})
	c.SetSession(&Session{
		AccessToken:  "stale-tok",
		RefreshToken: "stale-rt",
		ExpiresAt:    time.Now().Add(time.Hour), // not in proactive window
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rc, err := c.DialRealtime(ctx)
	require.NoError(t, err)
	defer rc.Close()

	assert.Equal(t, int32(2), atomic.LoadInt32(&dialAttempts),
		"DialRealtime must retry the dial once after refreshing on 401")
	assert.Equal(t, int32(1), atomic.LoadInt32(&refreshAttempts),
		"refresh must fire exactly once between the two dials")
}

func TestRealtimeClient_ReadMessage_connection_closed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		require.NoError(t, err)
		conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer server.Close()

	c, _ := NewClient(Options{APIKey: "k", BaseURL: server.URL})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rc, err := c.DialRealtime(ctx)
	require.NoError(t, err)
	defer rc.Close()

	_, err = rc.ReadMessage(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConnectionClosed))
}

func TestRealtimeClient_Close_idempotent(t *testing.T) {
	rc := &RealtimeClient{}
	require.NoError(t, rc.Close())
	require.NoError(t, rc.Close())
}

func TestRealtimeClient_ws_url_from_http(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		require.NoError(t, err)
		conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer server.Close()

	// httptest.Server uses http:// URLs.
	assert.True(t, strings.HasPrefix(server.URL, "http://"))

	c, _ := NewClient(Options{APIKey: "k", BaseURL: server.URL})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// DialRealtime should convert http:// -> ws:// internally.
	rc, err := c.DialRealtime(ctx)
	require.NoError(t, err)
	rc.Close()
}

func TestMatchmakerService_RequestMatch(t *testing.T) {
	// Build a server that upgrades WS and sends match_ready after a small delay.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/matchmaker/tickets" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         int64(7),
				"status":     "queued",
				"created_at": time.Now().UTC().Format(time.RFC3339Nano),
			})
			return
		}
		if r.URL.Path == "/v1/matchmaker/tickets/7" && r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/v1/ws" {
			conn, err := websocket.Accept(w, r, nil)
			require.NoError(t, err)
			defer conn.Close(websocket.StatusNormalClosure, "")

			// Small delay to simulate matchmaking time.
			time.Sleep(50 * time.Millisecond)

			data, _ := json.Marshal(Message{
				Type: "match_ready",
				Payload: mustMarshal(map[string]any{
					"address":   "192.168.1.100:7777",
					"ticket_id": int64(7),
				}),
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = conn.Write(ctx, websocket.MessageText, data)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	c, _ := NewClient(Options{APIKey: "k", BaseURL: server.URL})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ready, err := c.Matchmaker.RequestMatch(ctx, MatchRequest{
		Fleet: "docker-default", GameMode: "deathmatch",
	})
	require.NoError(t, err)
	assert.Equal(t, "192.168.1.100:7777", ready.Address)
	assert.Equal(t, int64(7), ready.TicketID)
}

// Regression: the matchmaker hub registers a client connection at WS
// upgrade. If the SDK POSTs the ticket before opening WS, the matchmaker
// can allocate and push match_ready before the client is registered —
// the push is dropped and the client times out. This test records the
// order of incoming requests and asserts /v1/ws hit the server BEFORE
// POST /v1/matchmaker/tickets.
func TestMatchmakerService_RequestMatch_DialsWSBeforePostingTicket(t *testing.T) {
	var order []string
	var orderMu sync.Mutex
	record := func(s string) {
		orderMu.Lock()
		defer orderMu.Unlock()
		order = append(order, s)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/ws" {
			record("ws")
			conn, err := websocket.Accept(w, r, nil)
			require.NoError(t, err)
			defer conn.Close(websocket.StatusNormalClosure, "")
			data, _ := json.Marshal(Message{
				Type: "match_ready",
				Payload: mustMarshal(map[string]any{
					"address":   "10.0.0.1:7777",
					"ticket_id": int64(9),
				}),
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = conn.Write(ctx, websocket.MessageText, data)
			return
		}
		if r.URL.Path == "/v1/matchmaker/tickets" && r.Method == http.MethodPost {
			record("ticket")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         int64(9),
				"status":     "queued",
				"created_at": time.Now().UTC().Format(time.RFC3339Nano),
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	c, _ := NewClient(Options{APIKey: "k", BaseURL: server.URL})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := c.Matchmaker.RequestMatch(ctx, MatchRequest{Fleet: "f", GameMode: "g"})
	require.NoError(t, err)

	orderMu.Lock()
	defer orderMu.Unlock()
	require.GreaterOrEqual(t, len(order), 2, "expected both ws upgrade and ticket POST")
	assert.Equal(t, "ws", order[0],
		"WS upgrade must happen before ticket POST — otherwise match_ready pushes race the hub registration")
	assert.Equal(t, "ticket", order[1])
}

func TestMatchmakerService_RequestMatch_cancel_on_context(t *testing.T) {
	var cancelCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/matchmaker/tickets" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         int64(8),
				"status":     "queued",
				"created_at": time.Now().UTC().Format(time.RFC3339Nano),
			})
			return
		}
		if r.URL.Path == "/v1/matchmaker/tickets/8" && r.Method == http.MethodDelete {
			cancelCalled = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/v1/ws" {
			conn, err := websocket.Accept(w, r, nil)
			require.NoError(t, err)
			// Never send match_ready; just wait for client to close.
			<-r.Context().Done()
			conn.Close(websocket.StatusNormalClosure, "")
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	c, _ := NewClient(Options{APIKey: "k", BaseURL: server.URL})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := c.Matchmaker.RequestMatch(ctx, MatchRequest{Fleet: "docker-default"})
	require.Error(t, err)
	assert.True(t, cancelCalled, "ticket should be cancelled when context expires")
}
