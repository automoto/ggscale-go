package ggscale

import (
	"context"
	"net/http"
)

// PresenceService exposes PUT /v1/presence. Reach it via
// Client.Presence.
type PresenceService struct {
	c *Client
}

type presenceUpdateBody struct {
	Status    string  `json:"status"`
	SessionID *string `json:"session_id"`
}

// Set publishes the calling player's presence — a free-form status of
// 1–32 characters (e.g. "online", "in_match") and, optionally, the
// game session they are in. Accepted friends connected to the realtime
// WebSocket receive the update as a "presence" message.
func (p *PresenceService) Set(ctx context.Context, status string, sessionID *string) error {
	return p.c.callProtected(ctx, &Request{
		OperationID: "updatePresence",
		Method:      http.MethodPut,
		Path:        "/v1/presence",
		Body:        presenceUpdateBody{Status: status, SessionID: sessionID},
	}, nil)
}
