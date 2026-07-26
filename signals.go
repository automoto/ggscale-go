package ggscale

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// GameSessionSignalsService exchanges short-lived negotiation data between
// authenticated members of a game session.
type GameSessionSignalsService struct{ c *Client }

type GameSessionSignal struct {
	ID            int64     `json:"id"`
	FromPlayerID  int64     `json:"from_player_id"`
	ToPlayerID    int64     `json:"to_player_id"`
	NegotiationID string    `json:"negotiation_id"`
	Kind          string    `json:"kind"`
	Payload       string    `json:"payload"`
	CreatedAt     time.Time `json:"created_at"`
}

type SendGameSessionSignal struct {
	ToPlayerID    int64  `json:"to_player_id"`
	NegotiationID string `json:"negotiation_id"`
	Kind          string `json:"kind"`
	Payload       string `json:"payload"`
}

func (s *GameSessionSignalsService) Send(ctx context.Context, sessionID string, signal SendGameSessionSignal) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	err := s.c.callProtected(ctx, &Request{
		Method: http.MethodPost,
		Path:   "/v1/game-session/" + url.PathEscape(sessionID) + "/signals",
		Body:   signal,
	}, &out)
	return out.ID, err
}

func (s *GameSessionSignalsService) Poll(ctx context.Context, sessionID string, afterID int64) ([]GameSessionSignal, error) {
	query := url.Values{}
	if afterID > 0 {
		query.Set("after_id", strconv.FormatInt(afterID, 10))
	}
	var out struct {
		Signals []GameSessionSignal `json:"signals"`
	}
	err := s.c.callProtected(ctx, &Request{
		Method: http.MethodGet,
		Path:   "/v1/game-session/" + url.PathEscape(sessionID) + "/signals",
		Query:  query,
	}, &out)
	return out.Signals, err
}
