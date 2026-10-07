package ggscale

import (
	"context"
	"net/http"
)

// RealtimeService exposes the realtime REST operations. Reach it via
// Client.Realtime.
type RealtimeService struct {
	c *Client
}

// RealtimeTicket is a one-time ticket for /v1/ws?ticket=<ticket>.
type RealtimeTicket struct {
	Ticket           string `json:"ticket"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

// CreateTicket gets a one-time WebSocket ticket for the current player. The
// ticket works once, within ExpiresInSeconds. A browser build of
// DialRealtime calls it for each dial; a native build does not need it.
func (r *RealtimeService) CreateTicket(ctx context.Context) (*RealtimeTicket, error) {
	var out RealtimeTicket
	err := r.c.callProtected(ctx, &Request{
		OperationID: "createRealtimeTicket",
		Method:      http.MethodPost,
		Path:        "/v1/ws/ticket",
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
