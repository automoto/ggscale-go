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
	EndUserID int64 `json:"end_user_id"`
	Score     int64 `json:"score"`
	Rank      int64 `json:"rank"`
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

// Submit posts a score for the calling end-user. Each call inserts a
// new row; the server keeps the player's best score for ranking
// purposes.
//
// Server policy: leaderboard score submission requires a secret-tier
// API key. Calls from a publishable-key client return ErrForbidden.
// Game clients should hand the player's session token to a trusted
// game server, which submits via SubmitFor instead.
func (l *LeaderboardsService) Submit(ctx context.Context, leaderboardID, score int64) error {
	return l.c.callProtected(ctx, &Request{
		Method: http.MethodPost,
		Path:   "/v1/leaderboards/" + strconv.FormatInt(leaderboardID, 10) + "/scores",
		Body:   submitScoreRequest{Score: score},
	}, nil)
}

// SubmitFor posts a score on behalf of the player identified by
// playerSessionToken. The Client's own configured API key authorises
// the caller (must be secret-tier) and the supplied session token
// identifies the subject. Use from a dedicated game server processing
// match-end results for many concurrent players, where mutating the
// shared Client.Session() per submission would be incorrect.
func (l *LeaderboardsService) SubmitFor(ctx context.Context, playerSessionToken string, leaderboardID, score int64) error {
	return l.c.transport.Call(ctx, &Request{
		Method:       http.MethodPost,
		Path:         "/v1/leaderboards/" + strconv.FormatInt(leaderboardID, 10) + "/scores",
		Body:         submitScoreRequest{Score: score},
		APIKey:       l.c.apiKey,
		SessionToken: playerSessionToken,
	}, nil)
}

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
