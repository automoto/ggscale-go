package ggscale

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// PlayersService exposes project-scoped public player lookup endpoints.
type PlayersService struct{ c *Client }

// PublicPlayer contains only fields safe to expose to other players.
type PublicPlayer struct {
	ID          int64     `json:"id"`
	DisplayName string    `json:"display_name,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Get returns one player's public profile.
func (s *PlayersService) Get(ctx context.Context, playerID int64) (*PublicPlayer, error) {
	var player PublicPlayer
	err := s.c.callProtected(ctx, &Request{
		OperationID: "getPlayer",
		Method:      http.MethodGet,
		Path:        "/v1/players/" + strconv.FormatInt(playerID, 10),
	}, &player)
	if err != nil {
		return nil, err
	}
	return &player, nil
}

// Resolve returns public profiles for up to 100 player IDs. Unknown and
// cross-project IDs are omitted by the server.
func (s *PlayersService) Resolve(ctx context.Context, playerIDs []int64) ([]PublicPlayer, error) {
	ids := make([]string, 0, len(playerIDs))
	for _, id := range playerIDs {
		ids = append(ids, strconv.FormatInt(id, 10))
	}
	query := url.Values{"ids": {strings.Join(ids, ",")}}
	var result struct {
		Players []PublicPlayer `json:"players"`
	}
	err := s.c.callProtected(ctx, &Request{
		OperationID: "resolvePlayers",
		Method:      http.MethodGet,
		Path:        "/v1/players",
		Query:       query,
	}, &result)
	if err != nil {
		return nil, err
	}
	return result.Players, nil
}

// ResolveFriendCode resolves a human-shareable friend code to a public player.
func (s *PlayersService) ResolveFriendCode(ctx context.Context, code string) (*PublicPlayer, error) {
	var player PublicPlayer
	err := s.c.callProtected(ctx, &Request{
		OperationID: "resolveFriendCode",
		Method:      http.MethodGet,
		Path:        "/v1/players/by-code/" + url.PathEscape(code),
	}, &player)
	if err != nil {
		return nil, err
	}
	return &player, nil
}
