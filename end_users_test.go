package ggscale

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndUsers_VerifySession_posts_token_and_returns_user(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"user_id":     int64(7),
				"external_id": "steam:1234567890",
				"email":       "demo@example.com",
			}, nil
		},
	}
	c, err := NewClient(Options{APIKey: "ggs_secret_xyz", Transport: ft})
	require.NoError(t, err)

	res, err := c.EndUsers.VerifySession(context.Background(), "player.session.jwt")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, int64(7), res.UserID)
	assert.Equal(t, "steam:1234567890", res.ExternalID)
	assert.Equal(t, "demo@example.com", res.Email)

	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/end-users/verify", ft.gotReq.Path)
	assert.Equal(t, "ggs_secret_xyz", ft.gotReq.APIKey)
	assert.Empty(t, ft.gotReq.SessionToken, "verify is server-tier — no end-user session attached")

	body, ok := ft.gotReq.Body.(endUserVerifyRequestBody)
	require.True(t, ok)
	assert.Equal(t, "player.session.jwt", body.SessionToken)
}

func TestEndUsers_VerifySession_omits_email_when_absent(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"user_id":     int64(9),
				"external_id": "anon_abc",
			}, nil
		},
	}
	c, err := NewClient(Options{APIKey: "k", Transport: ft})
	require.NoError(t, err)

	res, err := c.EndUsers.VerifySession(context.Background(), "tok")
	require.NoError(t, err)
	assert.Empty(t, res.Email)
	assert.Equal(t, "anon_abc", res.ExternalID)
}

func TestEndUsers_VerifySession_propagates_unauthorized(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return nil, &Error{Status: http.StatusUnauthorized, Message: "invalid session"}
		},
	}
	c, err := NewClient(Options{APIKey: "k", Transport: ft})
	require.NoError(t, err)

	res, err := c.EndUsers.VerifySession(context.Background(), "bad.token")
	require.Error(t, err)
	assert.Nil(t, res)
	assert.True(t, errors.Is(err, ErrUnauthorized))
}

func TestEndUsers_VerifySession_rejects_empty_token(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			t.Fatal("transport should not be called when token is empty")
			return nil, nil
		},
	}
	c, err := NewClient(Options{APIKey: "k", Transport: ft})
	require.NoError(t, err)

	_, err = c.EndUsers.VerifySession(context.Background(), "")
	require.Error(t, err)
	assert.Equal(t, 0, ft.callCount)
}
