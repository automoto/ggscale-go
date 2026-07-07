package ggscale

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer_VerifySession_posts_token_and_returns_player(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"player_id":   int64(7),
				"external_id": "steam:1234567890",
				"email":       "demo@example.com",
			}, nil
		},
	}
	c, err := NewClient(Options{APIKey: "ggs_secret_xyz", Transport: ft})
	require.NoError(t, err)

	res, err := c.Server.VerifySession(context.Background(), "player.session.jwt")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, int64(7), res.PlayerID)
	assert.Equal(t, "steam:1234567890", res.ExternalID)
	assert.Equal(t, "demo@example.com", res.Email)

	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/server/player-sessions/verify", ft.gotReq.Path)
	assert.Equal(t, "ggs_secret_xyz", ft.gotReq.APIKey)
	assert.Empty(t, ft.gotReq.SessionToken, "verify is server-tier — no player session attached")

	body, ok := ft.gotReq.Body.(playerVerifyRequestBody)
	require.True(t, ok)
	assert.Equal(t, "player.session.jwt", body.SessionToken)
}

func TestServer_VerifySession_omits_email_when_absent(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"player_id":   int64(9),
				"external_id": "anon_abc",
			}, nil
		},
	}
	c, err := NewClient(Options{APIKey: "k", Transport: ft})
	require.NoError(t, err)

	res, err := c.Server.VerifySession(context.Background(), "tok")
	require.NoError(t, err)
	assert.Empty(t, res.Email)
	assert.Equal(t, "anon_abc", res.ExternalID)
}

func TestServer_VerifySession_propagates_unauthorized(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return nil, &Error{Status: http.StatusUnauthorized, Message: "invalid session"}
		},
	}
	c, err := NewClient(Options{APIKey: "k", Transport: ft})
	require.NoError(t, err)

	res, err := c.Server.VerifySession(context.Background(), "bad.token")
	require.Error(t, err)
	assert.Nil(t, res)
	assert.True(t, errors.Is(err, ErrUnauthorized))
}

func TestServer_VerifySession_rejects_empty_token(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			t.Fatal("transport should not be called when token is empty")
			return nil, nil
		},
	}
	c, err := NewClient(Options{APIKey: "k", Transport: ft})
	require.NoError(t, err)

	_, err = c.Server.VerifySession(context.Background(), "")
	require.Error(t, err)
	assert.Equal(t, 0, ft.callCount)
}

func TestServer_PlayerRemoteAddrs_gets_addresses(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"addresses": []map[string]any{
					{"type": "ip_public", "scope": "public", "address": "203.0.113.9"},
					{"type": "dns", "scope": "public", "address": "player.example.com"},
				},
			}, nil
		},
	}
	c, err := NewClient(Options{APIKey: "ggs_secret_xyz", Transport: ft})
	require.NoError(t, err)

	addrs, err := c.Server.PlayerRemoteAddrs(context.Background(), 42)
	require.NoError(t, err)

	assert.Equal(t, http.MethodGet, ft.gotReq.Method)
	assert.Equal(t, "/v1/server/players/42/remote-addrs", ft.gotReq.Path)
	assert.Empty(t, ft.gotReq.SessionToken)
	require.Len(t, addrs, 2)
	assert.Equal(t, "ip_public", addrs[0].Type)
	assert.Equal(t, "public", addrs[0].Scope)
	assert.Equal(t, "203.0.113.9", addrs[0].Address)
}

func TestServer_PlayerRemoteAddrs_not_found(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return nil, &Error{Status: http.StatusNotFound}
		},
	}
	c, err := NewClient(Options{APIKey: "k", Transport: ft})
	require.NoError(t, err)

	_, err = c.Server.PlayerRemoteAddrs(context.Background(), 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}
