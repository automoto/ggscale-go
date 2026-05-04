package ggscale

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveSession installs a non-expiring session on the client so
// callProtected does not try to refresh during the test.
func liveSession() *Session {
	return &Session{
		AccessToken:  "test-jwt",
		RefreshToken: "rt",
		EndUserID:    1,
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}
}

// newClientWithFake wires a Client around a fakeTransport with a
// preset live session.
func newClientWithFake(t *testing.T, ft *fakeTransport) *Client {
	t.Helper()
	c, err := NewClient(Options{APIKey: "k", Transport: ft})
	require.NoError(t, err)
	c.SetSession(liveSession())
	return c
}

func TestStorage_Get_decodes_object(t *testing.T) {
	updated := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"key":        "prefs",
				"value":      json.RawMessage(`{"theme":"dark"}`),
				"version":    int64(3),
				"updated_at": updated.Format(time.RFC3339),
			}, nil
		},
	}
	c := newClientWithFake(t, ft)

	obj, err := c.Storage.Get(context.Background(), "prefs")
	require.NoError(t, err)
	assert.Equal(t, "prefs", obj.Key)
	assert.JSONEq(t, `{"theme":"dark"}`, string(obj.Value))
	assert.Equal(t, int64(3), obj.Version)
	assert.True(t, obj.UpdatedAt.Equal(updated))

	assert.Equal(t, http.MethodGet, ft.gotReq.Method)
	assert.Equal(t, "/v1/storage/objects/prefs", ft.gotReq.Path)
	assert.Equal(t, "test-jwt", ft.gotReq.SessionToken)
}

func TestStorage_Get_url_escapes_key(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"key":        "with/slash",
				"value":      json.RawMessage(`{}`),
				"version":    int64(1),
				"updated_at": time.Now().UTC().Format(time.RFC3339),
			}, nil
		},
	}
	c := newClientWithFake(t, ft)
	_, err := c.Storage.Get(context.Background(), "with/slash")
	require.NoError(t, err)
	assert.Equal(t, "/v1/storage/objects/with%2Fslash", ft.gotReq.Path)
}

func TestStorage_Get_404_returns_ErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer srv.Close()

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	c.SetSession(liveSession())
	_, err := c.Storage.Get(context.Background(), "missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestStorage_Put_sends_value_as_body_and_returns_version(t *testing.T) {
	type prefs struct {
		Theme string `json:"theme"`
	}
	updated := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	ft := &fakeTransport{
		respond: func(req *Request) (any, error) {
			return map[string]any{
				"key":        "prefs",
				"value":      req.Body,
				"version":    int64(1),
				"updated_at": updated.Format(time.RFC3339),
			}, nil
		},
	}
	c := newClientWithFake(t, ft)

	obj, err := c.Storage.Put(context.Background(), "prefs", prefs{Theme: "dark"})
	require.NoError(t, err)
	assert.Equal(t, http.MethodPut, ft.gotReq.Method)
	assert.Equal(t, "/v1/storage/objects/prefs", ft.gotReq.Path)
	assert.Empty(t, ft.gotReq.IfMatch)

	body, ok := ft.gotReq.Body.(prefs)
	require.True(t, ok)
	assert.Equal(t, "dark", body.Theme)

	assert.Equal(t, int64(1), obj.Version)
}

func TestStorage_Put_with_IfMatch_sends_if_match_header(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"key":        "k",
				"value":      json.RawMessage(`{}`),
				"version":    int64(2),
				"updated_at": time.Now().UTC().Format(time.RFC3339),
			}, nil
		},
	}
	c := newClientWithFake(t, ft)
	_, err := c.Storage.Put(context.Background(), "k", map[string]any{}, IfMatch(1))
	require.NoError(t, err)
	assert.Equal(t, "1", ft.gotReq.IfMatch)
}

func TestStorage_Put_OCC_conflict_returns_ErrConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
		_, _ = w.Write([]byte("version mismatch"))
	}))
	defer srv.Close()

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	c.SetSession(liveSession())
	_, err := c.Storage.Put(context.Background(), "k", map[string]any{}, IfMatch(999))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConflict))
}

func TestStorage_Delete_returns_no_error_on_204(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/v1/storage/objects/k", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	c.SetSession(liveSession())
	err := c.Storage.Delete(context.Background(), "k")
	require.NoError(t, err)
}

func TestStorage_List_first_page_with_cursor_advances_to_empty(t *testing.T) {
	page1 := map[string]any{
		"items": []map[string]any{
			{"key": "a", "value": json.RawMessage(`1`), "version": 1, "updated_at": time.Now().UTC().Format(time.RFC3339)},
			{"key": "b", "value": json.RawMessage(`2`), "version": 1, "updated_at": time.Now().UTC().Format(time.RFC3339)},
		},
		"next_cursor": "10",
	}
	page2 := map[string]any{
		"items":       []map[string]any{},
		"next_cursor": "",
	}

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "" {
			_ = json.NewEncoder(w).Encode(page1)
			return
		}
		assert.Equal(t, "10", r.URL.Query().Get("cursor"))
		assert.Equal(t, "abc", r.URL.Query().Get("key_prefix"))
		_ = json.NewEncoder(w).Encode(page2)
	}))
	defer srv.Close()

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	c.SetSession(liveSession())

	first, err := c.Storage.List(context.Background(), ListOptions{KeyPrefix: "abc", Limit: 50})
	require.NoError(t, err)
	assert.Len(t, first.Items, 2)
	assert.Equal(t, "10", first.NextCursor)

	second, err := c.Storage.List(context.Background(), ListOptions{KeyPrefix: "abc", Cursor: "10"})
	require.NoError(t, err)
	assert.Empty(t, second.Items)
	assert.Empty(t, second.NextCursor)
	assert.Equal(t, 2, hits)
}

// Sanity guard so io stays imported if we drop one assertion.
var _ = io.Discard
