package ggscale

import (
	"context"
	"net/http"
	"time"
)

// FleetService exposes the /v1/fleet/servers endpoints. Game servers
// register themselves on boot, send periodic heartbeats, and deregister
// on shutdown. Clients call List to discover available servers.
//
// Reach it via Client.Fleet.
type FleetService struct {
	transport Transport
	apiKey    string
	c         *Client
}

// FleetServer is one row of the fleet listing.
type FleetServer struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Address       string    `json:"address"`
	Version       string    `json:"version"`
	Region        string    `json:"region"`
	MaxPlayers    int       `json:"max_players"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// FleetRegisterRequest is the input to FleetService.Register.
type FleetRegisterRequest struct {
	Name       string `json:"name,omitempty"`
	Address    string `json:"address"`
	Version    string `json:"version,omitempty"`
	Region     string `json:"region,omitempty"`
	MaxPlayers int    `json:"max_players,omitempty"`
}

type fleetRegisterResponse struct {
	ID string `json:"id"`
}

type fleetListResponse struct {
	Servers []FleetServer `json:"servers"`
}

// Register announces a game server to ggscale and returns its assigned
// id. Authentication is by API key (no end-user session required) — game
// servers run with a project-pinned key.
func (f *FleetService) Register(ctx context.Context, req FleetRegisterRequest) (string, error) {
	var resp fleetRegisterResponse
	err := f.transport.Call(ctx, &Request{
		Method: http.MethodPost,
		Path:   "/v1/fleet/servers",
		APIKey: f.apiKey,
		Body:   req,
	}, &resp)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// Heartbeat refreshes the server's TTL. Servers should call this on a
// timer shorter than the server's eviction TTL (default 30s; 10s
// heartbeat is a reasonable default).
func (f *FleetService) Heartbeat(ctx context.Context, id string) error {
	return f.transport.Call(ctx, &Request{
		Method: http.MethodPut,
		Path:   "/v1/fleet/servers/" + id + "/heartbeat",
		APIKey: f.apiKey,
	}, nil)
}

// Deregister removes the server from the registry. Call on graceful
// shutdown; on a crash the entry will expire on its own after the TTL.
func (f *FleetService) Deregister(ctx context.Context, id string) error {
	return f.transport.Call(ctx, &Request{
		Method: http.MethodDelete,
		Path:   "/v1/fleet/servers/" + id,
		APIKey: f.apiKey,
	}, nil)
}

// List returns the active game servers in the caller's project. Used
// by the client-side server browser. Requires an end-user session.
func (f *FleetService) List(ctx context.Context) ([]FleetServer, error) {
	var resp fleetListResponse
	err := f.c.callProtected(ctx, &Request{
		Method: http.MethodGet,
		Path:   "/v1/fleet/servers",
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Servers, nil
}
