package ggscale

import (
	"context"
	"errors"
	"net/http"
)

// FleetsService exposes the server-browser endpoints.
//
// ListServers is consumed by game clients: it takes the current end-user
// session (publishable api_key + X-Session-Token) and returns the live
// servers for the given fleet, with player counts.
//
// SendHeartbeat is consumed by game-server processes: it takes a secret
// api_key (no session) and announces the server's address + current
// player count. Servers should heartbeat every 5 s; ggscale-server's
// registry expires entries that go ~15 s without a heartbeat.
type FleetsService struct {
	c *Client
}

// Heartbeat is the payload SendHeartbeat sends to ggscale-server.
// AgonesName is the unique key — use the Agones GameServer CR name so
// duplicate-pod scenarios upsert instead of double-listing.
type Heartbeat struct {
	AgonesName     string `json:"agones_name"`
	Fleet          string `json:"fleet"`
	Address        string `json:"address"`
	Region         string `json:"region"`
	Name           string `json:"name"`
	CurrentPlayers int    `json:"current_players"`
	MaxPlayers     int    `json:"max_players"`
	GameMode       string `json:"game_mode"`
	Level          string `json:"level"`
	Version        string `json:"version"`
}

// Server is one entry in a ListServers response.
type Server struct {
	Name           string `json:"name"`
	Address        string `json:"address"`
	Region         string `json:"region"`
	CurrentPlayers int    `json:"current_players"`
	MaxPlayers     int    `json:"max_players"`
	GameMode       string `json:"game_mode"`
	Level          string `json:"level"`
	Version        string `json:"version"`
}

type listServersResponse struct {
	Servers []Server `json:"servers"`
}

// SendHeartbeat announces this game-server's liveness + player count.
// Requires a SECRET-tier api_key on the client (publishable keys cannot
// heartbeat). Idempotent — call every 5 s on a ticker.
func (s *FleetsService) SendHeartbeat(ctx context.Context, hb Heartbeat) error {
	if hb.AgonesName == "" || hb.Fleet == "" || hb.Address == "" {
		return errors.New("ggscale: heartbeat requires agones_name, fleet, and address")
	}
	if hb.MaxPlayers <= 0 {
		return errors.New("ggscale: heartbeat max_players must be > 0")
	}
	return s.c.transport.Call(ctx, &Request{
		Method: http.MethodPost,
		Path:   "/v1/fleets/heartbeat",
		APIKey: s.c.apiKey,
		Body:   hb,
	}, nil)
}

// ListServers returns the live game-servers for the given fleet, scoped
// to this client's tenant. Requires an established end-user session
// (call Login or SetSession first).
func (s *FleetsService) ListServers(ctx context.Context, fleet string) ([]Server, error) {
	if fleet == "" {
		return nil, errors.New("ggscale: ListServers requires fleet")
	}
	var resp listServersResponse
	err := s.c.callProtected(ctx, &Request{
		Method: http.MethodGet,
		Path:   "/v1/fleets/" + fleet + "/servers",
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Servers, nil
}
