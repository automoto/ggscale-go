package ggscale

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// AnonymousAuth authenticates via POST /v1/auth/anonymous. The server
// creates an end_user with a random external_id on first call. When
// storePath is non-empty, the session (including refresh token) is
// persisted to disk so subsequent runs of the same game binary resume
// the same identity instead of registering a fresh anonymous user.
//
// Wire AnonymousAuth.SaveSession to Options.OnSessionUpdate so the
// rotated refresh token is rewritten to disk after every refresh:
//
//	auth := ggscale.NewAnonymousAuth(transport, apiKey,
//	    ggscale.DefaultSessionPath("my-game"))
//	c, err := ggscale.NewClient(ggscale.Options{
//	    BaseURL: "...", APIKey: apiKey,
//	    OnSessionUpdate: auth.SaveSession,
//	})
//	_ = c.Login(ctx, auth)
type AnonymousAuth struct {
	transport Transport
	apiKey    string
	storePath string
}

// NewAnonymousAuth builds an Authenticator that calls /v1/auth/anonymous.
// storePath is where the SDK persists the session between runs; pass ""
// for ephemeral (test) use. See DefaultSessionPath for a sensible default.
func NewAnonymousAuth(t Transport, apiKey, storePath string) *AnonymousAuth {
	return &AnonymousAuth{transport: t, apiKey: apiKey, storePath: storePath}
}

// Authenticate implements Authenticator. Returns a persisted session if
// one is on disk; otherwise mints a new one via /v1/auth/anonymous and
// (best-effort) writes it to storePath.
func (a *AnonymousAuth) Authenticate(ctx context.Context) (*Session, error) {
	if a.storePath != "" {
		if sess, ok := loadSessionFile(a.storePath); ok {
			return sess, nil
		}
	}
	var resp sessionResponse
	err := a.transport.Call(ctx, &Request{
		Method: http.MethodPost,
		Path:   "/v1/auth/anonymous",
		APIKey: a.apiKey,
	}, &resp)
	if err != nil {
		return nil, err
	}
	sess := resp.toSession()
	if a.storePath != "" {
		_ = saveSessionFile(a.storePath, sess)
	}
	return sess, nil
}

// SaveSession writes s to the configured storePath. Suitable as
// Options.OnSessionUpdate so refreshed sessions are persisted; no-op
// when storePath is empty.
func (a *AnonymousAuth) SaveSession(s *Session) {
	if a.storePath == "" || s == nil {
		return
	}
	_ = saveSessionFile(a.storePath, s)
}

// DefaultSessionPath returns a per-game session file under
// os.UserConfigDir(), falling back to os.TempDir() when no config dir
// is available. The directory is created on save.
func DefaultSessionPath(gameID string) string {
	cfg, err := os.UserConfigDir()
	if err != nil || cfg == "" {
		cfg = os.TempDir()
	}
	return filepath.Join(cfg, "ggscale", gameID, "session.json")
}

type persistedSession struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	EndUserID    int64     `json:"end_user_id"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func loadSessionFile(path string) (*Session, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var p persistedSession
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, false
	}
	// Without a refresh token there's no way to recover from an
	// expired access token; treat as no persisted session and let the
	// caller mint a fresh identity.
	if p.RefreshToken == "" {
		return nil, false
	}
	return &Session{
		AccessToken:  p.AccessToken,
		RefreshToken: p.RefreshToken,
		EndUserID:    p.EndUserID,
		ExpiresAt:    p.ExpiresAt,
	}, true
}

func saveSessionFile(path string, s *Session) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(persistedSession{
		AccessToken:  s.AccessToken,
		RefreshToken: s.RefreshToken,
		EndUserID:    s.EndUserID,
		ExpiresAt:    s.ExpiresAt,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}
