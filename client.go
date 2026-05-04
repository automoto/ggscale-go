package ggscale

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// proactiveRefreshWindow is how long before ExpiresAt the client will
// fire a refresh on its own. Picked so we never hand out a token
// that's about to expire mid-call, but not so wide that we refresh on
// every request.
const proactiveRefreshWindow = 30 * time.Second

// Client is the entry point for the ggscale Go SDK. Construct one
// with NewClient, optionally call Login to establish an end-user
// session, and use the service fields (Auth, Storage, Leaderboards,
// Profile) to call the API.
//
// A Client is safe for concurrent use.
type Client struct {
	// Auth exposes the /v1/auth/* operations that are not
	// authentication strategies (signup, verify, refresh, logout).
	Auth *AuthService

	// Storage, Leaderboards, Profile are populated by their
	// respective service files (storage.go, leaderboards.go,
	// profile.go). They share this client's transport, API key,
	// and auto-refresh logic.
	Storage      *StorageService
	Leaderboards *LeaderboardsService
	Profile      *ProfileService

	transport Transport
	apiKey    string

	sessionMu sync.RWMutex
	session   *Session
}

// Options configures NewClient.
type Options struct {
	// Transport overrides the default JSON-over-HTTP transport.
	// Leave nil and set BaseURL to use StdNetTransport.
	Transport Transport

	// BaseURL is the ggscale-server base URL (no trailing slash),
	// e.g. "http://localhost:8080". Required when Transport is nil.
	BaseURL string

	// APIKey is the tenant API key. Required.
	APIKey string
}

// NewClient builds a Client. The returned Client has no session yet;
// call Login or SetSession before invoking Storage/Leaderboards/
// Profile methods.
func NewClient(opts Options) (*Client, error) {
	if opts.APIKey == "" {
		return nil, errors.New("ggscale: APIKey is required")
	}
	t := opts.Transport
	if t == nil {
		if opts.BaseURL == "" {
			return nil, errors.New("ggscale: either Transport or BaseURL is required")
		}
		t = &StdNetTransport{BaseURL: opts.BaseURL}
	}

	c := &Client{
		transport: t,
		apiKey:    opts.APIKey,
	}
	c.Auth = &AuthService{transport: t, apiKey: opts.APIKey}
	c.Storage = &StorageService{c: c}
	c.Leaderboards = &LeaderboardsService{c: c}
	c.Profile = &ProfileService{c: c}
	return c, nil
}

// Login establishes a session by running the supplied Authenticator.
// Subsequent service calls (Storage, Leaderboards, Profile) use the
// resulting session automatically.
func (c *Client) Login(ctx context.Context, a Authenticator) error {
	sess, err := a.Authenticate(ctx)
	if err != nil {
		return err
	}
	c.SetSession(sess)
	return nil
}

// Session returns a copy of the current session, or nil if none is
// set. Returning a copy prevents callers from mutating the client's
// internal state.
func (c *Client) Session() *Session {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	if c.session == nil {
		return nil
	}
	cp := *c.session
	return &cp
}

// SetSession installs a session previously captured via Session.
// Useful for restoring a session across process restarts. Pass nil
// to clear.
func (c *Client) SetSession(s *Session) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if s == nil {
		c.session = nil
		return
	}
	cp := *s
	c.session = &cp
}

// Transport returns the underlying transport. Useful when constructing
// an Authenticator outside the SDK or wiring fakes in tests.
func (c *Client) Transport() Transport {
	return c.transport
}

// callProtected sends a request that requires an end-user session.
// Attaches the API key and session token, fires a proactive refresh
// when the session is within the refresh window, and retries once on
// a 401 after refreshing.
func (c *Client) callProtected(ctx context.Context, req *Request, out any) error {
	if err := c.refreshIfNeeded(ctx); err != nil {
		return err
	}

	if err := c.attachSession(req); err != nil {
		return err
	}
	err := c.transport.Call(ctx, req, out)
	if err == nil {
		return nil
	}

	var sdkErr *Error
	if !errors.As(err, &sdkErr) || sdkErr.Status != http.StatusUnauthorized {
		return err
	}

	// 401 — refresh once and retry once.
	if rerr := c.refreshNow(ctx); rerr != nil {
		return err // surface the original 401
	}
	if err := c.attachSession(req); err != nil {
		return err
	}
	return c.transport.Call(ctx, req, out)
}

func (c *Client) attachSession(req *Request) error {
	c.sessionMu.RLock()
	defer c.sessionMu.RUnlock()
	if c.session == nil {
		return errors.New("ggscale: no session — call Login or SetSession first")
	}
	req.APIKey = c.apiKey
	req.SessionToken = c.session.AccessToken
	return nil
}

// refreshIfNeeded fires a refresh when the session is inside the
// proactive window. Concurrent callers wait on the same write lock,
// and the slow path re-checks the window so refresh fires once per
// expiry boundary, not once per goroutine.
func (c *Client) refreshIfNeeded(ctx context.Context) error {
	c.sessionMu.RLock()
	stale := c.session != nil &&
		c.session.RefreshToken != "" &&
		time.Until(c.session.ExpiresAt) < proactiveRefreshWindow
	c.sessionMu.RUnlock()
	if !stale {
		return nil
	}

	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	// Re-check under the write lock — another goroutine may have
	// refreshed while we were waiting.
	if c.session == nil || c.session.RefreshToken == "" {
		return nil
	}
	if time.Until(c.session.ExpiresAt) >= proactiveRefreshWindow {
		return nil
	}
	return c.doRefreshLocked(ctx)
}

// refreshNow forces a refresh regardless of expiry. Used after a 401.
func (c *Client) refreshNow(ctx context.Context) error {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.session == nil || c.session.RefreshToken == "" {
		return errors.New("ggscale: cannot refresh — no refresh token")
	}
	return c.doRefreshLocked(ctx)
}

// doRefreshLocked does the actual refresh; the caller must hold the
// write lock.
func (c *Client) doRefreshLocked(ctx context.Context) error {
	sess, err := c.Auth.Refresh(ctx, c.session.RefreshToken)
	if err != nil {
		return err
	}
	c.session = sess
	return nil
}
