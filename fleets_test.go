package ggscale

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFleets_ListServers_DecodesResponse(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"servers": []map[string]any{
					{
						"name":            "Server A",
						"address":         "10.0.0.1:7777",
						"region":          "us-east",
						"current_players": 2,
						"max_players":     4,
						"game_mode":       "deathmatch",
						"level":           "arena_battle_starter",
						"version":         "v0.2.0",
					},
					{
						"name":            "Server B",
						"address":         "10.0.0.2:7777",
						"region":          "us-west",
						"current_players": 4,
						"max_players":     4,
					},
				},
			}, nil
		},
	}
	c := newClientWithFake(t, ft)

	servers, err := c.Fleets.ListServers(context.Background(), "doomerang-east")
	require.NoError(t, err)
	require.Len(t, servers, 2)

	assert.Equal(t, http.MethodGet, ft.gotReq.Method)
	assert.Equal(t, "/v1/fleets/doomerang-east/servers", ft.gotReq.Path)
	assert.Equal(t, "Server A", servers[0].Name)
	assert.Equal(t, 2, servers[0].CurrentPlayers)
	assert.Equal(t, 4, servers[0].MaxPlayers)
	assert.Equal(t, "us-east", servers[0].Region)
	assert.Equal(t, "Server B", servers[1].Name)
	assert.Equal(t, 4, servers[1].CurrentPlayers)
}

func TestFleets_ListServers_RejectsEmptyFleet(t *testing.T) {
	c := newClientWithFake(t, &fakeTransport{})
	_, err := c.Fleets.ListServers(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fleet")
}

func TestFleets_SendHeartbeat_PostsBody(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c, err := NewClient(Options{APIKey: "secret", Transport: ft})
	require.NoError(t, err)

	hb := Heartbeat{
		AgonesName:     "doomerang-east-abc",
		Fleet:          "doomerang-east",
		Address:        "10.0.0.1:7777",
		Region:         "us-east",
		Name:           "Doomerang Server",
		CurrentPlayers: 2,
		MaxPlayers:     4,
		GameMode:       "deathmatch",
		Level:          "arena_battle_starter",
		Version:        "v0.2.0",
	}
	require.NoError(t, c.Fleets.SendHeartbeat(context.Background(), hb))

	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/fleets/heartbeat", ft.gotReq.Path)
	assert.Equal(t, "secret", ft.gotReq.APIKey, "heartbeat must carry the secret api_key")
	got, ok := ft.gotReq.Body.(Heartbeat)
	require.True(t, ok)
	assert.Equal(t, hb, got)
}

// SendHeartbeat does NOT require a session — it's a server-tier call.
// This guards against accidentally routing it through callProtected,
// which would require a session.
func TestFleets_SendHeartbeat_NoSessionRequired(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c, err := NewClient(Options{APIKey: "secret", Transport: ft})
	require.NoError(t, err)
	// Note: c.SetSession is never called. SendHeartbeat must still work.

	err = c.Fleets.SendHeartbeat(context.Background(), Heartbeat{
		AgonesName: "g", Fleet: "f", Address: "a", MaxPlayers: 4,
	})
	require.NoError(t, err, "heartbeat must not require a session")
}

func TestFleets_SendHeartbeat_RejectsInvalid(t *testing.T) {
	c := newClientWithFake(t, &fakeTransport{})
	cases := []struct {
		name string
		hb   Heartbeat
	}{
		{"missing agones_name", Heartbeat{Fleet: "f", Address: "a", MaxPlayers: 4}},
		{"missing fleet", Heartbeat{AgonesName: "g", Address: "a", MaxPlayers: 4}},
		{"missing address", Heartbeat{AgonesName: "g", Fleet: "f", MaxPlayers: 4}},
		{"max_players zero", Heartbeat{AgonesName: "g", Fleet: "f", Address: "a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.Fleets.SendHeartbeat(context.Background(), tc.hb)
			require.Error(t, err)
		})
	}
}
