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
// with NewClient, optionally call Login to establish a player
// session, and use the service fields (Auth, Storage, Leaderboards,
// Profile) to call the API.
//
// A Client is safe for concurrent use.
type Client struct {
	// Auth exposes the /v1/auth/* operations that are not
	// authentication strategies (signup, verify, refresh, logout).
	Auth *AuthService

	// The remaining service fields are populated by their respective
	// service files (storage.go, friends.go, …). They share this
	// client's transport, API key, and auto-refresh logic.
	Storage      *StorageService
	Leaderboards *LeaderboardsService
	Profile      *ProfileService
	Matchmaker   *MatchmakerService
	Relay        *RelayService
	Fleets       *FleetsService
	Friends      *FriendsService
	GameSessions *GameSessionsService
	Signals      *GameSessionSignalsService
	Invites      *InvitesService
	Presence     *PresenceService
	Account      *AccountService

	// Server exposes server-tier endpoints (player session-token
	// verification, player remote addresses) for game-server
	// workloads. Authenticates with the secret API key only — no
	// player session required.
	Server *ServerService

	transport Transport
	apiKey    string
	baseURL   string

	sessionMu       sync.RWMutex
	session         *Session
	onSessionUpdate func(*Session)
}

// Options configures NewClient.
type Options struct {
	// Transport overrides the default JSON-over-HTTP transport.
	// Leave nil and set BaseURL to use StdNetTransport.
	Transport Transport

	// BaseURL is the ggscale-server base URL (no trailing slash),
	// e.g. "http://localhost:8080". Required when Transport is nil;
	// also used by DialRealtime to derive the WebSocket URL.
	BaseURL string

	// APIKey is the tenant API key. Required.
	APIKey string

	// OnSessionUpdate, if non-nil, is called whenever the client
	// installs or rotates a session — once after Login (or
	// SetSession) and once after each automatic refresh. Useful for
	// persisting the session across process restarts; see
	// AnonymousAuth.SaveSession for a ready-made callback.
	OnSessionUpdate func(*Session)
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
		transport:       t,
		apiKey:          opts.APIKey,
		baseURL:         opts.BaseURL,
		onSessionUpdate: opts.OnSessionUpdate,
	}
	c.Auth = &AuthService{transport: t, apiKey: opts.APIKey}
	c.Storage = &StorageService{c: c}
	c.Leaderboards = &LeaderboardsService{c: c}
	c.Profile = &ProfileService{c: c}
	c.Matchmaker = &MatchmakerService{c: c}
	c.Relay = &RelayService{c: c}
	c.Fleets = &FleetsService{c: c}
	c.Friends = &FriendsService{c: c}
	c.GameSessions = &GameSessionsService{c: c}
	c.Signals = &GameSessionSignalsService{c: c}
	c.Invites = &InvitesService{c: c}
	c.Presence = &PresenceService{c: c}
	c.Account = &AccountService{c: c}
	c.Server = &ServerService{transport: t, apiKey: opts.APIKey}
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
// to clear. Fires OnSessionUpdate when set.
func (c *Client) SetSession(s *Session) {
	c.sessionMu.Lock()
	if s == nil {
		c.session = nil
		c.sessionMu.Unlock()
		c.notifySessionUpdate(nil)
		return
	}
	cp := *s
	c.session = &cp
	c.sessionMu.Unlock()
	c.notifySessionUpdate(&cp)
}

func (c *Client) notifySessionUpdate(s *Session) {
	if c.onSessionUpdate == nil {
		return
	}
	c.onSessionUpdate(s)
}

// Transport returns the underlying transport. Useful when constructing
// an Authenticator outside the SDK or wiring fakes in tests.
func (c *Client) Transport() Transport {
	return c.transport
}

// callProtected sends a request that requires a player session.
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

	updated, err := c.refreshUnderLock(ctx, false)
	if err != nil {
		return err
	}
	if updated != nil {
		c.notifySessionUpdate(updated)
	}
	return nil
}

// refreshNow forces a refresh regardless of expiry. Used after a 401.
func (c *Client) refreshNow(ctx context.Context) error {
	updated, err := c.refreshUnderLock(ctx, true)
	if err != nil {
		return err
	}
	if updated != nil {
		c.notifySessionUpdate(updated)
	}
	return nil
}

// refreshUnderLock performs the refresh and returns a snapshot of the
// new session for the caller to deliver to OnSessionUpdate. force=false
// re-checks the proactive window inside the write lock so refresh fires
// once per expiry boundary; force=true bypasses the check (post-401).
func (c *Client) refreshUnderLock(ctx context.Context, force bool) (*Session, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()

	if c.session == nil || c.session.RefreshToken == "" {
		if force {
			return nil, errors.New("ggscale: cannot refresh — no refresh token")
		}
		return nil, nil
	}
	if !force && time.Until(c.session.ExpiresAt) >= proactiveRefreshWindow {
		return nil, nil
	}

	sess, err := c.Auth.Refresh(ctx, c.session.RefreshToken)
	if err != nil {
		return nil, err
	}
	c.session = sess
	cp := *sess
	return &cp, nil
}
