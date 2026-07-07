package ggscale

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccount_RemoteAddrs_gets_own_addresses(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"addresses": []map[string]any{
					{"type": "ip_lan", "scope": "lan", "address": "192.168.1.10"},
					{"type": "iroh", "scope": "global", "address": "node1abc"},
				},
			}, nil
		},
	}
	c := newClientWithFake(t, ft)

	addrs, err := c.Account.RemoteAddrs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, http.MethodGet, ft.gotReq.Method)
	assert.Equal(t, "/v1/account/remote-addrs", ft.gotReq.Path)
	require.Len(t, addrs, 2)
	assert.Equal(t, "iroh", addrs[1].Type)
}

func TestAccount_SetRemoteAddrs_puts_and_returns_canonical_list(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"addresses": []map[string]any{
					{"type": "ip_public", "scope": "public", "address": "203.0.113.9"},
				},
			}, nil
		},
	}
	c := newClientWithFake(t, ft)

	addrs, err := c.Account.SetRemoteAddrs(context.Background(), []RemoteAddr{
		{Type: "ip_public", Address: "203.0.113.9"},
	})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPut, ft.gotReq.Method)
	assert.Equal(t, "/v1/account/remote-addrs", ft.gotReq.Path)
	body, ok := ft.gotReq.Body.(remoteAddrsPayload)
	require.True(t, ok)
	require.Len(t, body.Addresses, 1)
	assert.Equal(t, "ip_public", body.Addresses[0].Type)

	require.Len(t, addrs, 1)
	assert.Equal(t, "public", addrs[0].Scope, "server-derived scope round-trips back")
}

func TestAccount_RemoteAddrs_forbidden_for_anonymous_player(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return nil, &Error{Status: http.StatusForbidden, Message: "link a gg-scale account to use friends"}
		},
	}
	c := newClientWithFake(t, ft)

	_, err := c.Account.RemoteAddrs(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrForbidden))
}
