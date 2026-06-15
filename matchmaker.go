package ggscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// MatchmakerService exposes the /v1/matchmaker/tickets endpoints. Reach it
// via Client.Matchmaker.
type MatchmakerService struct {
	c *Client
}

// Ticket is one in-flight matchmaking request.
type Ticket struct {
	ID           int64           `json:"id"`
	Status       string          `json:"status"`
	Region       string          `json:"region"`
	GameMode     string          `json:"game_mode"`
	Attributes   json.RawMessage `json:"attributes,omitempty"`
	MatchAddress string          `json:"match_address"`
	CreatedAt    time.Time       `json:"created_at"`
	MatchedAt    *time.Time      `json:"matched_at,omitempty"`
}

// MatchRequest is the input to MatchmakerService.RequestMatch.
type MatchRequest struct {
	Fleet      string         `json:"fleet"`
	Region     string         `json:"region"`
	GameMode   string         `json:"game_mode"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// MatchReady is delivered when the matchmaker finds a game server for the
// caller's ticket.
type MatchReady struct {
	Address  string
	TicketID int64
}

// CreateTicket enqueues a new matchmaking ticket. The ticket starts in
// "queued" status; use RequestMatch for a high-level helper that blocks
// until the ticket reaches "matched".
func (m *MatchmakerService) CreateTicket(ctx context.Context, req MatchRequest) (*Ticket, error) {
	var ticket Ticket
	err := m.c.callProtected(ctx, &Request{
		Method: http.MethodPost,
		Path:   "/v1/matchmaker/tickets",
		Body:   req,
	}, &ticket)
	if err != nil {
		return nil, err
	}
	return &ticket, nil
}

// GetTicket returns a ticket by id. Returns ErrNotFound if the ticket
// does not exist or belongs to another tenant.
func (m *MatchmakerService) GetTicket(ctx context.Context, id int64) (*Ticket, error) {
	var ticket Ticket
	err := m.c.callProtected(ctx, &Request{
		Method: http.MethodGet,
		Path:   "/v1/matchmaker/tickets/" + strconv.FormatInt(id, 10),
	}, &ticket)
	if err != nil {
		return nil, err
	}
	return &ticket, nil
}

// CancelTicket cancels a queued ticket. Returns ErrNotFound if the ticket
// is unknown, or ErrConflict if it has already reached a terminal status.
func (m *MatchmakerService) CancelTicket(ctx context.Context, id int64) error {
	return m.c.callProtected(ctx, &Request{
		Method: http.MethodDelete,
		Path:   "/v1/matchmaker/tickets/" + strconv.FormatInt(id, 10),
	}, nil)
}

// RequestMatch is a high-level helper that creates a ticket, opens a
// real-time WebSocket connection, and blocks until a match_ready envelope
// arrives (or the context is cancelled). On success it returns the game
// server address. On cancellation it best-effort cancels the ticket.
func (m *MatchmakerService) RequestMatch(ctx context.Context, req MatchRequest) (*MatchReady, error) {
	// Dial the realtime WS BEFORE creating the ticket. ServeWS registers
	// the connection with the hub after the upgrade completes; if we
	// post the ticket first, the matchmaker can allocate and try to push
	// match_ready before we're in the hub — and the push is dropped.
	rc, err := m.c.DialRealtime(ctx)
	if err != nil {
		return nil, fmt.Errorf("dial realtime: %w", err)
	}
	defer func() { _ = rc.Close() }()

	ticket, err := m.CreateTicket(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create ticket: %w", err)
	}

	for {
		msg, err := rc.ReadMessage(ctx)
		if err != nil {
			_ = m.CancelTicket(context.WithoutCancel(ctx), ticket.ID)
			return nil, fmt.Errorf("read realtime: %w", err)
		}
		if msg.Type != "match_ready" {
			continue
		}
		var payload struct {
			Address  string `json:"address"`
			TicketID int64  `json:"ticket_id"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			_ = m.CancelTicket(context.WithoutCancel(ctx), ticket.ID)
			return nil, fmt.Errorf("parse match_ready: %w", err)
		}
		return &MatchReady{
			Address:  payload.Address,
			TicketID: payload.TicketID,
		}, nil
	}
}

// ErrNotConnected is returned by RequestMatch when the realtime
// connection drops before match_ready arrives.
var ErrNotConnected = errors.New("ggscale: realtime connection closed before match_ready")
