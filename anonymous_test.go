package ggscale

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnonymousAuth_Authenticate_calls_anonymous_endpoint(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return cannedSession(), nil },
	}
	a := NewAnonymousAuth(ft, "ggp_key", "")

	sess, err := a.Authenticate(context.Background())
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/auth/anonymous", ft.gotReq.Path)
	assert.Equal(t, "ggp_key", ft.gotReq.APIKey)
	assert.Empty(t, ft.gotReq.SessionToken)
	assert.Equal(t, "jwt.access.token", sess.AccessToken)
}

func TestAnonymousAuth_persists_first_session_and_reads_back_on_second_call(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return cannedSession(), nil },
	}
	a := NewAnonymousAuth(ft, "ggp_key", path)

	first, err := a.Authenticate(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, ft.callCount)

	// File on disk now exists.
	_, err = os.Stat(path)
	require.NoError(t, err)

	// Second call must NOT hit the network — return canned data the
	// test would notice via callCount.
	second, err := a.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, ft.callCount, "second Authenticate must use the persisted file, not the API")
	assert.Equal(t, first.AccessToken, second.AccessToken)
	assert.Equal(t, first.RefreshToken, second.RefreshToken)
	assert.Equal(t, first.PlayerID, second.PlayerID)
}

func TestAnonymousAuth_empty_storePath_never_touches_disk(t *testing.T) {
	dir := t.TempDir()
	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return cannedSession(), nil },
	}
	a := NewAnonymousAuth(ft, "ggp_key", "")

	_, err := a.Authenticate(context.Background())
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "no files written when storePath is empty")
}

func TestAnonymousAuth_invalid_persisted_file_falls_back_to_api(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	require.NoError(t, os.WriteFile(path, []byte("not-json"), 0o600))

	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return cannedSession(), nil },
	}
	a := NewAnonymousAuth(ft, "ggp_key", path)

	sess, err := a.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, ft.callCount, "garbage file must trigger fresh anonymous registration")
	assert.Equal(t, "jwt.access.token", sess.AccessToken)
}

func TestAnonymousAuth_persisted_file_without_refresh_token_falls_back(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	// Write a syntactically-valid file with no refresh token. We can't
	// recover without it, so AnonymousAuth must mint a fresh identity.
	raw, _ := json.Marshal(persistedSession{
		AccessToken: "stale", PlayerID: 1, ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, os.WriteFile(path, raw, 0o600))

	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return cannedSession(), nil },
	}
	a := NewAnonymousAuth(ft, "ggp_key", path)

	_, err := a.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, ft.callCount)
}

func TestAnonymousAuth_persisted_file_is_mode_0600_on_unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are POSIX-only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return cannedSession(), nil },
	}
	a := NewAnonymousAuth(ft, "ggp_key", path)
	_, err := a.Authenticate(context.Background())
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestAnonymousAuth_SaveSession_writes_to_storePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "session.json")
	a := NewAnonymousAuth(nil, "ggp_key", path)

	a.SaveSession(&Session{
		AccessToken:  "rotated.access",
		RefreshToken: "rotated.refresh",
		PlayerID:     99,
		ExpiresAt:    time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	})

	loaded, ok := loadSessionFile(path)
	require.True(t, ok)
	assert.Equal(t, "rotated.access", loaded.AccessToken)
	assert.Equal(t, "rotated.refresh", loaded.RefreshToken)
	assert.Equal(t, int64(99), loaded.PlayerID)
}

func TestAnonymousAuth_SaveSession_is_noop_with_empty_path(t *testing.T) {
	a := NewAnonymousAuth(nil, "ggp_key", "")
	// Must not panic, must not error — pure no-op.
	a.SaveSession(&Session{AccessToken: "x", RefreshToken: "y"})
}

func TestAnonymousAuth_SaveSession_nil_removes_revoked_session_file(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	a := NewAnonymousAuth(nil, "ggp_key", path)
	a.SaveSession(&Session{
		AccessToken: "revoked.access", RefreshToken: "revoked.refresh",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	require.FileExists(t, path)

	a.SaveSession(nil)
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestDefaultSessionPath_returns_per_game_path(t *testing.T) {
	a := DefaultSessionPath("doomerang-mp")
	b := DefaultSessionPath("other-game")

	assert.NotEqual(t, a, b)
	assert.Contains(t, a, "doomerang-mp")
	assert.Contains(t, b, "other-game")
	assert.True(t, filepath.IsAbs(a), "DefaultSessionPath must be absolute")
}
