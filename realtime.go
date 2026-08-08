//go:build !js

package ggscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const realtimeOperationID = "realtimeWebSocket"
const stableRealtimeConnection = 30 * time.Second

// Message is the wire envelope pushed by the server over the WebSocket.
// Type discriminates payloads (matchmaker_matched, presence, chat …).
// Payload is opaque JSON.
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// RealtimeClient is a WebSocket connection to /v1/ws. Construct one with
// Client.DialRealtime. Reads are serialized; after a retryable closure the
// client reconnects according to Options.ReconnectPolicy.
type RealtimeClient struct {
	conn *websocket.Conn
	mu   sync.Mutex

	readMu          sync.Mutex
	owner           *Client
	reconnectPolicy ReconnectPolicy
	closed          bool
	done            chan struct{}
	connectedAt     time.Time
	reconnectCount  int
	requestID       string
}

// DialRealtime opens a WebSocket connection to the server's /v1/ws
// endpoint. The connection carries the API key and current session token
// as headers. Requires a player session.
//
// Refreshes the session proactively when it's near expiry, and retries
// once on a 401 after a forced refresh — mirroring callProtected so
// dialers don't fail on a stale session that the next REST call would
// have silently refreshed.
func (c *Client) DialRealtime(ctx context.Context) (result *RealtimeClient, retErr error) {
	return c.dialRealtime(ctx, newRequestID())
}

func (c *Client) dialRealtime(ctx context.Context, requestID string) (result *RealtimeClient, retErr error) {
	ctx, cancel := context.WithTimeout(ctx, c.webSocketHandshakeTimeout)
	defer cancel()
	started := time.Now()
	attempts := 0
	status := 0
	defer func() {
		c.safeLog(LogEvent{
			Level:       "info",
			Event:       "websocket.complete",
			OperationID: realtimeOperationID,
			Method:      http.MethodGet,
			Status:      status,
			Duration:    time.Since(started),
			Attempts:    attempts,
			RequestID:   requestID,
		})
	}()
	if err := c.refreshIfNeeded(ctx); err != nil {
		return nil, err
	}
	attempts++
	rc, dialStatus, usedAccessToken, err := c.dialRealtimeOnce(ctx, requestID)
	status = dialStatus
	if err == nil {
		status = http.StatusSwitchingProtocols
		return rc, nil
	}
	if status != http.StatusUnauthorized {
		return nil, err
	}
	if rerr := c.refreshAfterUnauthorized(ctx, usedAccessToken); rerr != nil {
		return nil, err // surface the original 401
	}
	attempts++
	rc, status, _, err = c.dialRealtimeOnce(ctx, requestID)
	if err == nil {
		status = http.StatusSwitchingProtocols
	}
	return rc, err
}

// dialRealtimeOnce performs a single WS dial attempt and returns the HTTP
// status code if the upgrade was rejected (so the caller can decide
// whether to refresh + retry). status is 0 on success or when the failure
// has no associated HTTP response (e.g. network error).
func (c *Client) dialRealtimeOnce(ctx context.Context, requestID string) (*RealtimeClient, int, string, error) {
	baseURL := c.baseURL
	if baseURL == "" {
		if t, ok := c.transport.(*StdNetTransport); ok {
			baseURL = t.BaseURL
		}
	}
	if baseURL == "" {
		return nil, 0, "", errors.New("ggscale: cannot determine WebSocket URL — set Options.BaseURL")
	}

	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, 0, "", fmt.Errorf("parse WebSocket base URL: %w", err)
	}
	switch parsedURL.Scheme {
	case "http":
		parsedURL.Scheme = "ws"
	case "https":
		parsedURL.Scheme = "wss"
	default:
		return nil, 0, "", fmt.Errorf("unsupported WebSocket base URL scheme %q", parsedURL.Scheme)
	}
	parsedURL.Path = strings.TrimRight(parsedURL.Path, "/") + "/v1/ws"
	parsedURL.RawQuery = ""
	parsedURL.Fragment = ""
	wsURL := parsedURL.String()

	c.sessionMu.RLock()
	sess := c.session
	c.sessionMu.RUnlock()
	if sess == nil {
		return nil, 0, "", errors.New("ggscale: no session — call Login or SetSession first")
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.apiKey)
	headers.Set("X-Session-Token", sess.AccessToken)
	headers.Set("X-Request-Id", requestID)
	ua := userAgent
	var httpClient *http.Client
	if transport, ok := c.transport.(*StdNetTransport); ok {
		httpClient = transport.client()
		if transport.UserAgent != "" {
			ua = transport.UserAgent
		}
	}
	headers.Set("User-Agent", ua)

	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: httpClient,
		HTTPHeader: headers,
	})
	if err != nil {
		status := 0
		retryAfter := time.Duration(0)
		if resp != nil {
			status = resp.StatusCode
			retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		}
		return nil, status, sess.AccessToken, &RealtimeDialError{
			Status: status, RetryAfter: retryAfter, Err: fmt.Errorf("dial websocket: %w", err),
		}
	}
	conn.SetReadLimit(c.realtimeReadLimit)

	return &RealtimeClient{
		conn: conn, owner: c, reconnectPolicy: c.reconnectPolicy,
		done: make(chan struct{}), connectedAt: time.Now(), requestID: requestID,
	}, 0, sess.AccessToken, nil
}

func (c *Client) safeLog(event LogEvent) {
	if c.logger == nil {
		return
	}
	defer func() { _ = recover() }()
	c.logger(event)
}

func (c *Client) notifyRealtimeReconnect(ctx context.Context) {
	if c.onRealtimeReconnect == nil {
		return
	}
	c.realtimeHookMu.Lock()
	defer c.realtimeHookMu.Unlock()
	defer func() { _ = recover() }()
	c.onRealtimeReconnect(ctx, c)
}

// ReadMessage blocks until the server sends a message or the context is
// cancelled. Returns ErrConnectionClosed when the server drops the
// connection.
func (r *RealtimeClient) ReadMessage(ctx context.Context) (Message, error) {
	r.readMu.Lock()
	defer r.readMu.Unlock()
	for {
		r.mu.Lock()
		conn := r.conn
		closed := r.closed
		r.mu.Unlock()
		if conn == nil || closed {
			return Message{}, ErrConnectionClosed
		}

		_, data, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return Message{}, err
			}
			closeCode := int(websocket.CloseStatus(err))
			if !r.shouldReconnect(closeCode) {
				r.markClosed(conn)
				return Message{}, ErrConnectionClosed
			}
			if reconnectErr := r.reconnect(ctx, conn); reconnectErr != nil {
				r.markClosed(conn)
				if errors.Is(reconnectErr, context.Canceled) || errors.Is(reconnectErr, context.DeadlineExceeded) {
					return Message{}, reconnectErr
				}
				return Message{}, &RealtimeError{CloseCode: closeCode, Err: reconnectErr}
			}
			continue
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			return Message{}, fmt.Errorf("unmarshal message: %w", err)
		}
		if msg.Type == "" {
			return Message{}, errors.New("ggscale: realtime message is missing type")
		}
		return msg, nil
	}
}

func (r *RealtimeClient) shouldReconnect(closeCode int) bool {
	if r.owner == nil || !r.reconnectPolicy.Enabled {
		return false
	}
	r.mu.Lock()
	closed := r.closed
	if !closed && !r.connectedAt.IsZero() && time.Since(r.connectedAt) >= stableRealtimeConnection {
		r.reconnectCount = 0
	}
	exhausted := r.reconnectCount >= r.reconnectPolicy.MaxAttempts
	r.mu.Unlock()
	if closed || exhausted {
		return false
	}
	switch websocket.StatusCode(closeCode) {
	case -1, websocket.StatusGoingAway, websocket.StatusAbnormalClosure,
		websocket.StatusInternalError, websocket.StatusServiceRestart,
		websocket.StatusTryAgainLater:
		return true
	default:
		return false
	}
}

func (r *RealtimeClient) reconnect(ctx context.Context, failed *websocket.Conn) error {
	_ = failed.CloseNow()
	var lastErr = ErrConnectionClosed
	minimumDelay := time.Duration(0)
	r.mu.Lock()
	firstAttempt := r.reconnectCount + 1
	r.mu.Unlock()
	for attempt := firstAttempt; attempt <= r.reconnectPolicy.MaxAttempts; attempt++ {
		r.mu.Lock()
		r.reconnectCount = attempt
		r.mu.Unlock()
		delay := r.reconnectDelay(attempt)
		if minimumDelay > delay {
			delay = minimumDelay
		}
		if !retryFitsDeadline(ctx, delay) {
			return lastErr
		}
		r.owner.safeLog(LogEvent{
			Level: "debug", Event: "websocket.retry", OperationID: realtimeOperationID,
			Method: http.MethodGet, Attempt: attempt, RetryDelay: delay, RequestID: r.requestID,
		})
		if err := sleepRealtime(ctx, r.done, delay); err != nil {
			return err
		}

		next, err := r.owner.dialRealtime(ctx, r.requestID)
		if err != nil {
			lastErr = err
			var dialErr *RealtimeDialError
			if errors.As(err, &dialErr) {
				if dialErr.Status != 0 && !retryableStatus(dialErr.Status) {
					return err
				}
				minimumDelay = dialErr.RetryAfter
			}
			continue
		}

		next.mu.Lock()
		conn := next.conn
		next.conn = nil
		next.closed = true
		if next.done != nil {
			close(next.done)
		}
		next.mu.Unlock()

		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return ErrConnectionClosed
		}
		r.conn = conn
		r.connectedAt = time.Now()
		r.reconnectCount = attempt
		r.mu.Unlock()
		// Hooks may perform network I/O or accidentally re-enter the realtime
		// client. Never run caller code while ReadMessage holds readMu.
		go r.owner.notifyRealtimeReconnect(ctx)
		return nil
	}
	return lastErr
}

func (r *RealtimeClient) markClosed(conn *websocket.Conn) {
	r.mu.Lock()
	if r.conn == conn {
		r.conn = nil
	}
	if !r.closed {
		r.closed = true
		if r.done != nil {
			close(r.done)
		}
	}
	r.mu.Unlock()
	if conn != nil {
		_ = conn.CloseNow()
	}
}

func sleepRealtime(ctx context.Context, done <-chan struct{}, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return ErrConnectionClosed
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return ErrConnectionClosed
	case <-timer.C:
		return nil
	}
}

func (r *RealtimeClient) reconnectDelay(attempt int) time.Duration {
	capDelay := r.reconnectPolicy.FirstDelayMax
	if attempt > 1 {
		capDelay = r.reconnectPolicy.BaseDelay
		for i := 2; i < attempt && capDelay < r.reconnectPolicy.MaxDelay; i++ {
			if capDelay > r.reconnectPolicy.MaxDelay/2 {
				capDelay = r.reconnectPolicy.MaxDelay
				break
			}
			capDelay *= 2
		}
		if capDelay > r.reconnectPolicy.MaxDelay {
			capDelay = r.reconnectPolicy.MaxDelay
		}
	}
	if r.reconnectPolicy.Jitter != nil {
		delay := r.reconnectPolicy.Jitter(capDelay)
		if delay < 0 {
			return 0
		}
		if delay > capDelay {
			return capDelay
		}
		return delay
	}
	return fullJitter(capDelay)
}

// Close cleanly closes the WebSocket connection.
func (r *RealtimeClient) Close() error {
	r.mu.Lock()
	conn := r.conn
	r.conn = nil
	if !r.closed {
		r.closed = true
		if r.done != nil {
			close(r.done)
		}
	}
	r.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close(websocket.StatusNormalClosure, "")
}
