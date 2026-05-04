package ggscale

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfile_Get_decodes_full_response(t *testing.T) {
	verified := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	created := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"id":                42,
				"project_id":        7,
				"external_id":       "ext-abc",
				"email":             "demo@example.com",
				"email_verified_at": verified.Format(time.RFC3339),
				"created_at":        created.Format(time.RFC3339),
			}, nil
		},
	}
	c := newClientWithFake(t, ft)

	p, err := c.Profile.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "/v1/profile", ft.gotReq.Path)
	assert.Equal(t, http.MethodGet, ft.gotReq.Method)
	assert.Equal(t, int64(42), p.ID)
	assert.Equal(t, int64(7), p.ProjectID)
	assert.Equal(t, "ext-abc", p.ExternalID)
	assert.Equal(t, "demo@example.com", p.Email)
	require.NotNil(t, p.EmailVerifiedAt)
	assert.True(t, p.EmailVerifiedAt.Equal(verified))
	assert.True(t, p.CreatedAt.Equal(created))
}

func TestProfile_Get_unverified_email_leaves_pointer_nil(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"id":          42,
				"project_id":  7,
				"external_id": "ext-abc",
				"email":       "unverified@example.com",
				"created_at":  time.Now().UTC().Format(time.RFC3339),
				// email_verified_at intentionally omitted
			}, nil
		},
	}
	c := newClientWithFake(t, ft)
	p, err := c.Profile.Get(context.Background())
	require.NoError(t, err)
	assert.Nil(t, p.EmailVerifiedAt)
}

func TestProfile_Update_sends_patch_with_email(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c := newClientWithFake(t, ft)

	newEmail := "new@example.com"
	err := c.Profile.Update(context.Background(), ProfilePatch{Email: &newEmail})
	require.NoError(t, err)
	assert.Equal(t, http.MethodPatch, ft.gotReq.Method)
	assert.Equal(t, "/v1/profile", ft.gotReq.Path)

	body, ok := ft.gotReq.Body.(ProfilePatch)
	require.True(t, ok)
	require.NotNil(t, body.Email)
	assert.Equal(t, "new@example.com", *body.Email)
}

func TestProfile_Update_empty_patch_400_returns_ErrBadRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("no editable fields"))
	}))
	defer srv.Close()

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	c.SetSession(liveSession())
	err := c.Profile.Update(context.Background(), ProfilePatch{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBadRequest))
}
