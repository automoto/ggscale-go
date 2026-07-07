package ggscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

// Message is the wire envelope pushed by the server over the WebSocket.
// Type discriminates payloads (match_ready, presence, chat …). Payload is
// opaque JSON.
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// RealtimeClient is a WebSocket connection to /v1/ws. Construct one with
// Client.DialRealtime. The client is not safe for concurrent use — one
// goroutine should call ReadMessage at a time.
type RealtimeClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// DialRealtime opens a WebSocket connection to the server's /v1/ws
// endpoint. The connection carries the API key and current session token
// as headers. Requires a player session.
//
// Refreshes the session proactively when it's near expiry, and retries
// once on a 401 after a forced refresh — mirroring callProtected so
// dialers don't fail on a stale session that the next REST call would
// have silently refreshed.
func (c *Client) DialRealtime(ctx context.Context) (*RealtimeClient, error) {
	if err := c.refreshIfNeeded(ctx); err != nil {
		return nil, err
	}
	rc, status, err := c.dialRealtimeOnce(ctx)
	if err == nil {
		return rc, nil
	}
	if status != http.StatusUnauthorized {
		return nil, err
	}
	if rerr := c.refreshNow(ctx); rerr != nil {
		return nil, err // surface the original 401
	}
	rc, _, err = c.dialRealtimeOnce(ctx)
	return rc, err
}

// dialRealtimeOnce performs a single WS dial attempt and returns the HTTP
// status code if the upgrade was rejected (so the caller can decide
// whether to refresh + retry). status is 0 on success or when the failure
// has no associated HTTP response (e.g. network error).
func (c *Client) dialRealtimeOnce(ctx context.Context) (*RealtimeClient, int, error) {
	baseURL := c.baseURL
	if baseURL == "" {
		if t, ok := c.transport.(*StdNetTransport); ok {
			baseURL = t.BaseURL
		}
	}
	if baseURL == "" {
		return nil, 0, errors.New("ggscale: cannot determine WebSocket URL — set Options.BaseURL")
	}

	wsURL := baseURL
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL += "/v1/ws"

	c.sessionMu.RLock()
	sess := c.session
	c.sessionMu.RUnlock()
	if sess == nil {
		return nil, 0, errors.New("ggscale: no session — call Login or SetSession first")
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.apiKey)
	headers.Set("X-Session-Token", sess.AccessToken)

	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return nil, status, fmt.Errorf("dial websocket: %w", err)
	}

	return &RealtimeClient{conn: conn}, 0, nil
}

// ReadMessage blocks until the server sends a message or the context is
// cancelled. Returns ErrConnectionClosed when the server drops the
// connection.
func (r *RealtimeClient) ReadMessage(ctx context.Context) (Message, error) {
	r.mu.Lock()
	conn := r.conn
	r.mu.Unlock()
	if conn == nil {
		return Message{}, ErrConnectionClosed
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Message{}, err
		}
		return Message{}, ErrConnectionClosed
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return Message{}, fmt.Errorf("unmarshal message: %w", err)
	}
	return msg, nil
}

// Close cleanly closes the WebSocket connection.
func (r *RealtimeClient) Close() error {
	r.mu.Lock()
	conn := r.conn
	r.conn = nil
	r.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close(websocket.StatusNormalClosure, "")
}

// ErrConnectionClosed is returned by ReadMessage after the connection has
// been closed or dropped.
var ErrConnectionClosed = errors.New("ggscale: realtime connection closed")
