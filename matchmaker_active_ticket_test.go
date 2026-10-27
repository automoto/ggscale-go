package ggscale

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

type activeTicketTransport struct {
	createCalls int
	cancelledID string
}

func (t *activeTicketTransport) Call(_ context.Context, req *Request, out any) error {
	switch {
	case req.Method == http.MethodPost && req.Path == "/v1/matchmaker/tickets":
		t.createCalls++
		if t.createCalls == 1 {
			return &Error{
				Status:  http.StatusConflict,
				Code:    "ticket_already_active",
				Message: "ticket_already_active",
				Details: []ErrorDetail{{
					Location: "active_ticket_id",
					Value:    json.RawMessage("42"),
				}},
			}
		}
		ticket := out.(*Ticket)
		*ticket = Ticket{
			ID: 99, Status: "matched", Mode: string(ModeGameSession),
			MatchID: "match-1", SessionID: "session-1", HostPlayerID: 7,
		}
		return nil
	case req.Method == http.MethodDelete && req.Path == "/v1/matchmaker/tickets/42":
		t.cancelledID = "42"
		return nil
	default:
		return fmt.Errorf("unexpected request %s %s", req.Method, req.Path)
	}
}

func TestWaitForMatchReplacesAnActiveTicket(t *testing.T) {
	transport := &activeTicketTransport{}
	client, err := NewClient(Options{Transport: transport, APIKey: "publishable"})
	if err != nil {
		t.Fatal(err)
	}
	client.SetSession(&Session{
		AccessToken: "player-session", ExpiresAt: time.Now().Add(time.Hour),
	})

	result, err := client.Matchmaker.WaitForMatch(context.Background(), MatchRequest{
		Mode: ModeGameSession, GameMode: "gopher-bash-0.1.0", MinCount: 2, MaxCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if transport.cancelledID != "42" {
		t.Fatalf("cancelled ticket=%q, want 42", transport.cancelledID)
	}
	if transport.createCalls != 2 {
		t.Fatalf("create calls=%d, want 2", transport.createCalls)
	}
	if result.MatchID != "match-1" || result.SessionID != "session-1" {
		t.Fatalf("result=%+v", result)
	}
}
