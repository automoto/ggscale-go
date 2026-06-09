package ggscale

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_requires_api_key(t *testing.T) {
	_, err := NewClient(Options{BaseURL: "http://localhost"})
	require.Error(t, err)
}

func TestNewClient_requires_baseurl_or_transport(t *testing.T) {
	_, err := NewClient(Options{APIKey: "k"})
	require.Error(t, err)
}

func TestNewClient_default_transport_is_stdnet(t *testing.T) {
	c, err := NewClient(Options{APIKey: "k", BaseURL: "http://localhost:8080"})
	require.NoError(t, err)
	_, ok := c.Transport().(*StdNetTransport)
	assert.True(t, ok, "default transport should be *StdNetTransport")
}

func TestClient_Login_populates_session_via_authenticator(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return cannedSession(), nil },
	}
	c, err := NewClient(Options{APIKey: "k", Transport: ft})
	require.NoError(t, err)

	err = c.Login(context.Background(), NewEmailPasswordAuth(ft, "k", "e", "p"))
	require.NoError(t, err)
	require.NotNil(t, c.Session())
	assert.Equal(t, int64(42), c.Session().EndUserID)
}

func TestClient_callProtected_attaches_auth_and_session_headers(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return nil, nil },
	}
	c, err := NewClient(Options{APIKey: "ggs_xyz", Transport: ft})
	require.NoError(t, err)
	c.SetSession(&Session{
		AccessToken:  "live-jwt",
		RefreshToken: "rt",
		EndUserID:    9,
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	})

	err = c.callProtected(context.Background(), &Request{
		Method: http.MethodGet,
		Path:   "/v1/test",
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, "ggs_xyz", ft.gotReq.APIKey)
	assert.Equal(t, "live-jwt", ft.gotReq.SessionToken)
}

func TestClient_callProtected_errors_when_no_session(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	err := c.callProtected(context.Background(), &Request{Method: "GET", Path: "/v1/x"}, nil)
	require.Error(t, err)
}

// dispatchTransport routes requests by path so tests can stage different
// responses for /v1/auth/refresh vs the protected route under test.
type dispatchTransport struct {
	mu        sync.Mutex
	handlers  map[string]func(*Request) (status int, body any)
	callCount map[string]*int64
}

func newDispatchTransport() *dispatchTransport {
	return &dispatchTransport{
		handlers:  map[string]func(*Request) (int, any){},
		callCount: map[string]*int64{},
	}
}

func (d *dispatchTransport) on(path string, h func(*Request) (int, any)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[path] = h
	var c int64
	d.callCount[path] = &c
}

func (d *dispatchTransport) callsTo(path string) int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return atomic.LoadInt64(d.callCount[path])
}

func (d *dispatchTransport) Call(_ context.Context, req *Request, out any) error {
	d.mu.Lock()
	h, ok := d.handlers[req.Path]
	if ok {
		atomic.AddInt64(d.callCount[req.Path], 1)
	}
	d.mu.Unlock()
	if !ok {
		return &Error{Status: 404, Message: "no handler for " + req.Path}
	}
	status, body := h(req)
	if status >= 400 {
		e := &Error{Status: status}
		if msg, isStr := body.(string); isStr {
			e.Message = msg
		}
		return e
	}
	if out == nil || body == nil {
		return nil
	}
	// Round-trip via JSON to populate out the same way StdNetTransport would.
	ft := &fakeTransport{respond: func(*Request) (any, error) { return body, nil }}
	return ft.Call(context.Background(), req, out)
}

func TestClient_proactive_refresh_when_session_about_to_expire(t *testing.T) {
	dt := newDispatchTransport()
	dt.on("/v1/auth/refresh", func(*Request) (int, any) {
		return 200, map[string]any{
			"access_token":  "fresh.jwt",
			"refresh_token": "fresh-refresh",
			"end_user_id":   int64(1),
			"expires_at":    time.Now().Add(15 * time.Minute),
		}
	})
	dt.on("/v1/test", func(req *Request) (int, any) {
		return 200, map[string]any{}
	})

	c, _ := NewClient(Options{APIKey: "k", Transport: dt})
	c.SetSession(&Session{
		AccessToken:  "stale.jwt",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(5 * time.Second), // < 30s window
	})

	err := c.callProtected(context.Background(), &Request{Method: "GET", Path: "/v1/test"}, &map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), dt.callsTo("/v1/auth/refresh"), "refresh fired once proactively")
	assert.Equal(t, "fresh.jwt", c.Session().AccessToken)
}

func TestClient_no_proactive_refresh_when_session_is_fresh(t *testing.T) {
	dt := newDispatchTransport()
	dt.on("/v1/auth/refresh", func(*Request) (int, any) {
		t.Fatal("refresh should not have fired")
		return 200, nil
	})
	dt.on("/v1/test", func(*Request) (int, any) { return 200, map[string]any{} })

	c, _ := NewClient(Options{APIKey: "k", Transport: dt})
	c.SetSession(&Session{
		AccessToken:  "fresh.jwt",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	})

	err := c.callProtected(context.Background(), &Request{Method: "GET", Path: "/v1/test"}, &map[string]any{})
	require.NoError(t, err)
}

func TestClient_reactive_refresh_on_401(t *testing.T) {
	dt := newDispatchTransport()
	dt.on("/v1/auth/refresh", func(*Request) (int, any) {
		return 200, map[string]any{
			"access_token":  "after-401.jwt",
			"refresh_token": "rotated",
			"end_user_id":   int64(1),
			"expires_at":    time.Now().Add(15 * time.Minute),
		}
	})

	var attempts int64
	dt.on("/v1/protected", func(req *Request) (int, any) {
		n := atomic.AddInt64(&attempts, 1)
		if n == 1 {
			return 401, "unauthorized"
		}
		// On the retry attempt, the session token should be the
		// post-refresh one.
		if req.SessionToken != "after-401.jwt" {
			return 500, "wrong session token on retry: " + req.SessionToken
		}
		return 200, map[string]any{}
	})

	c, _ := NewClient(Options{APIKey: "k", Transport: dt})
	c.SetSession(&Session{
		AccessToken:  "stale.jwt",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(10 * time.Minute), // not in proactive window
	})

	err := c.callProtected(context.Background(), &Request{Method: "GET", Path: "/v1/protected"}, &map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), dt.callsTo("/v1/auth/refresh"), "refresh fired once reactively")
	assert.Equal(t, int64(2), dt.callsTo("/v1/protected"), "request retried once")
}

func TestClient_second_401_after_refresh_surfaces_to_caller(t *testing.T) {
	dt := newDispatchTransport()
	dt.on("/v1/auth/refresh", func(*Request) (int, any) {
		return 200, map[string]any{
			"access_token":  "still-bad.jwt",
			"refresh_token": "rt2",
			"end_user_id":   int64(1),
			"expires_at":    time.Now().Add(15 * time.Minute),
		}
	})
	dt.on("/v1/protected", func(*Request) (int, any) {
		return 401, "still unauthorized"
	})

	c, _ := NewClient(Options{APIKey: "k", Transport: dt})
	c.SetSession(&Session{
		AccessToken:  "stale.jwt",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	})

	err := c.callProtected(context.Background(), &Request{Method: "GET", Path: "/v1/protected"}, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnauthorized))
	assert.Equal(t, int64(2), dt.callsTo("/v1/protected"), "no infinite retry loop")
}

func TestClient_concurrent_refresh_fires_once(t *testing.T) {
	dt := newDispatchTransport()
	dt.on("/v1/auth/refresh", func(*Request) (int, any) {
		return 200, map[string]any{
			"access_token":  "shared.jwt",
			"refresh_token": "rotated",
			"end_user_id":   int64(1),
			"expires_at":    time.Now().Add(15 * time.Minute),
		}
	})
	dt.on("/v1/test", func(*Request) (int, any) { return 200, map[string]any{} })

	c, _ := NewClient(Options{APIKey: "k", Transport: dt})
	c.SetSession(&Session{
		AccessToken:  "stale.jwt",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().Add(2 * time.Second), // proactive window
	})

	const N = 10
	var wg sync.WaitGroup
	wg.Add(N)
	for range N {
		go func() {
			defer wg.Done()
			_ = c.callProtected(context.Background(), &Request{Method: "GET", Path: "/v1/test"}, &map[string]any{})
		}()
	}
	wg.Wait()
	assert.Equal(t, int64(1), dt.callsTo("/v1/auth/refresh"),
		"10 concurrent goroutines must trigger exactly one refresh")
	assert.Equal(t, int64(N), dt.callsTo("/v1/test"))
}

func TestClient_OnSessionUpdate_fires_on_login_and_refresh(t *testing.T) {
	dt := newDispatchTransport()
	dt.on("/v1/auth/login", func(*Request) (int, any) {
		return 200, map[string]any{
			"access_token":  "login.jwt",
			"refresh_token": "login-refresh",
			"end_user_id":   int64(7),
			"expires_at":    time.Now().Add(15 * time.Minute),
		}
	})
	dt.on("/v1/auth/refresh", func(*Request) (int, any) {
		return 200, map[string]any{
			"access_token":  "refreshed.jwt",
			"refresh_token": "rotated-refresh",
			"end_user_id":   int64(7),
			"expires_at":    time.Now().Add(15 * time.Minute),
		}
	})
	dt.on("/v1/test", func(*Request) (int, any) { return 200, map[string]any{} })

	var mu sync.Mutex
	var saw []string
	c, _ := NewClient(Options{
		APIKey:    "k",
		Transport: dt,
		OnSessionUpdate: func(s *Session) {
			mu.Lock()
			defer mu.Unlock()
			if s == nil {
				saw = append(saw, "<cleared>")
				return
			}
			saw = append(saw, s.AccessToken)
		},
	})

	require.NoError(t, c.Login(context.Background(), NewEmailPasswordAuth(dt, "k", "e", "p")))

	// Force the proactive refresh window.
	c.SetSession(&Session{
		AccessToken:  "stale.jwt",
		RefreshToken: "stale-refresh",
		ExpiresAt:    time.Now().Add(2 * time.Second),
	})
	require.NoError(t, c.callProtected(context.Background(), &Request{Method: "GET", Path: "/v1/test"}, &map[string]any{}))

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, saw, 3, "OnSessionUpdate must fire for login, manual SetSession, and refresh")
	assert.Equal(t, "login.jwt", saw[0])
	assert.Equal(t, "stale.jwt", saw[1])
	assert.Equal(t, "refreshed.jwt", saw[2])
}

func TestClient_OnSessionUpdate_fires_with_nil_on_clear(t *testing.T) {
	var saw []*Session
	c, _ := NewClient(Options{
		APIKey:          "k",
		BaseURL:         "http://localhost",
		OnSessionUpdate: func(s *Session) { saw = append(saw, s) },
	})
	c.SetSession(&Session{AccessToken: "x", RefreshToken: "y"})
	c.SetSession(nil)

	require.Len(t, saw, 2)
	assert.NotNil(t, saw[0])
	assert.Nil(t, saw[1])
}

func TestClient_SetSession_round_trip(t *testing.T) {
	dt := newDispatchTransport()
	dt.on("/v1/test", func(req *Request) (int, any) {
		if req.SessionToken != "restored.jwt" {
			return 500, "wrong token"
		}
		return 200, map[string]any{}
	})

	c, _ := NewClient(Options{APIKey: "k", Transport: dt})
	captured := &Session{
		AccessToken:  "restored.jwt",
		RefreshToken: "rt",
		EndUserID:    77,
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	}
	c.SetSession(captured)

	got := c.Session()
	require.NotNil(t, got)
	assert.Equal(t, "restored.jwt", got.AccessToken)
	assert.Equal(t, int64(77), got.EndUserID)

	err := c.callProtected(context.Background(), &Request{Method: "GET", Path: "/v1/test"}, &map[string]any{})
	require.NoError(t, err)
}

// Compile-time guard that the fakeTransport type from auth_test.go still
// satisfies Transport (used here too).
var _ Transport = (*fakeTransport)(nil)

// Sanity import to avoid "imported and not used" if url drops out later.
var _ = url.Values(nil)
