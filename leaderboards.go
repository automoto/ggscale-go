package ggscale

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// LeaderboardsService exposes the /v1/leaderboards/* endpoints. Reach
// it via Client.Leaderboards.
type LeaderboardsService struct {
	c *Client
}

// Entry is one row of a leaderboard. Rank is 0-based and matches the
// server's ordering (lower is better).
type Entry struct {
	PlayerID int64 `json:"player_id"`
	Score    int64 `json:"score"`
	Rank     int64 `json:"rank"`
}

// AroundMeResult is the response from AroundMe. SelfRank is -1 when
// the calling user has not submitted a score on this leaderboard.
type AroundMeResult struct {
	Entries  []Entry `json:"entries"`
	SelfRank int64   `json:"self_rank"`
}

type submitScoreRequest struct {
	Score int64 `json:"score"`
}

type entriesResponse struct {
	Entries []Entry `json:"entries"`
}

// Score submission is server-authoritative and lives on the server tier:
// see Client.Server.SubmitScore. A publishable-key game client cannot
// submit scores directly (the server requires a secret key), so it hands
// the player's session token to a trusted holder of the secret key that
// verifies and submits. See docs/leaderboards-p2p.md.

// Top returns the top entries on the leaderboard. limit > 0 sets the
// query limit (server caps at 100); pass 0 for the server default.
func (l *LeaderboardsService) Top(ctx context.Context, leaderboardID int64, limit int) ([]Entry, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var resp entriesResponse
	err := l.c.callProtected(ctx, &Request{
		Method: http.MethodGet,
		Path:   "/v1/leaderboards/" + strconv.FormatInt(leaderboardID, 10) + "/top",
		Query:  q,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Entries, nil
}

// AroundMe returns up to radius entries on either side of the calling
// user's rank. Set radius > 0; the server caps at 50.
func (l *LeaderboardsService) AroundMe(ctx context.Context, leaderboardID int64, radius int) (*AroundMeResult, error) {
	q := url.Values{}
	if radius > 0 {
		q.Set("radius", strconv.Itoa(radius))
	}
	var res AroundMeResult
	err := l.c.callProtected(ctx, &Request{
		Method: http.MethodGet,
		Path:   "/v1/leaderboards/" + strconv.FormatInt(leaderboardID, 10) + "/around-me",
		Query:  q,
	}, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}
