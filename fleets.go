package ggscale

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// FleetsService exposes the server-browser endpoints.
//
// ListServers is consumed by game clients: it takes the current player
// session (publishable api_key + X-Session-Token) and returns the live
// servers for the given fleet, with player counts.
//
// The heartbeat for game-server processes is Client.Server.FleetHeartbeat.
type FleetsService struct {
	c *Client
}

// Heartbeat is the payload FleetHeartbeat sends to ggscale-server.
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

// SendHeartbeat is retained for compatibility.
//
// Deprecated: use Client.Server.FleetHeartbeat. The heartbeat needs a secret
// key, so it belongs on the server client.
func (s *FleetsService) SendHeartbeat(ctx context.Context, hb Heartbeat) error {
	return s.c.Server.FleetHeartbeat(ctx, hb)
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
		OperationID: "fleetServersList",
		Method:      http.MethodGet,
		Path:        "/v1/fleets/" + url.PathEscape(fleet) + "/servers",
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Servers, nil
}
