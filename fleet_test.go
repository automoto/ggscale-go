package ggscale

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFleet_Register_posts_address_and_returns_id(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{"id": "11111111-2222-3333-4444-555555555555"}, nil
		},
	}
	c, err := NewClient(Options{APIKey: "k", Transport: ft})
	require.NoError(t, err)

	id, err := c.Fleet.Register(context.Background(), FleetRegisterRequest{
		Name: "doomerang-1", Address: "localhost:7373", Version: "0.1.0", MaxPlayers: 4,
	})
	require.NoError(t, err)
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", id)

	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/fleet/servers", ft.gotReq.Path)
	assert.Equal(t, "k", ft.gotReq.APIKey)
	body, ok := ft.gotReq.Body.(FleetRegisterRequest)
	require.True(t, ok)
	assert.Equal(t, "localhost:7373", body.Address)
}

func TestFleet_Heartbeat_uses_put_with_id_path(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c, err := NewClient(Options{APIKey: "k", Transport: ft})
	require.NoError(t, err)

	require.NoError(t, c.Fleet.Heartbeat(context.Background(), "abc"))

	assert.Equal(t, http.MethodPut, ft.gotReq.Method)
	assert.Equal(t, "/v1/fleet/servers/abc/heartbeat", ft.gotReq.Path)
}

func TestFleet_Deregister_uses_delete(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c, err := NewClient(Options{APIKey: "k", Transport: ft})
	require.NoError(t, err)

	require.NoError(t, c.Fleet.Deregister(context.Background(), "abc"))

	assert.Equal(t, http.MethodDelete, ft.gotReq.Method)
	assert.Equal(t, "/v1/fleet/servers/abc", ft.gotReq.Path)
}

func TestFleet_List_decodes_servers(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"servers": []map[string]any{
					{"id": "id-1", "name": "a", "address": "h:1", "max_players": 4, "last_heartbeat": "2026-05-04T12:00:00Z"},
					{"id": "id-2", "name": "b", "address": "h:2", "max_players": 8, "last_heartbeat": "2026-05-04T12:00:00Z"},
				},
			}, nil
		},
	}
	c := newClientWithFake(t, ft)

	servers, err := c.Fleet.List(context.Background())
	require.NoError(t, err)
	require.Len(t, servers, 2)
	assert.Equal(t, "a", servers[0].Name)
	assert.Equal(t, "h:2", servers[1].Address)

	assert.Equal(t, http.MethodGet, ft.gotReq.Method)
	assert.Equal(t, "/v1/fleet/servers", ft.gotReq.Path)
	assert.Equal(t, "test-jwt", ft.gotReq.SessionToken)
}
