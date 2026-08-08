package ggscale

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// LeaderboardsService exposes the /v1/leaderboards/* endpoints.
type LeaderboardsService struct{ c *Client }

// Entry is one leaderboard row. Rank is zero-based.
type Entry struct {
	PlayerID    int64           `json:"player_id"`
	Score       int64           `json:"score"`
	Rank        int64           `json:"rank"`
	DisplayName string          `json:"display_name,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

// AroundMeResult contains the caller's rank and neighboring entries.
type AroundMeResult struct {
	Entries  []Entry `json:"entries"`
	SelfRank int64   `json:"self_rank"`
}

type submitScoreRequest struct {
	Score    int64           `json:"score"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// ScoreOption configures player- and server-tier score submissions.
type ScoreOption func(*submitScoreRequest)

// WithScoreMetadata attaches an optional JSON object to a score.
func WithScoreMetadata(metadata json.RawMessage) ScoreOption {
	return func(req *submitScoreRequest) { req.Metadata = metadata }
}

// LeaderboardInfo describes one leaderboard available to the project.
type LeaderboardInfo struct {
	ID                int64           `json:"id"`
	Name              string          `json:"name"`
	SortOrder         string          `json:"sort_order"`
	ScoreOperator     string          `json:"score_operator"`
	ClientSubmissions bool            `json:"client_submissions"`
	ResetSchedule     string          `json:"reset_schedule"`
	CurrentPeriod     int             `json:"current_period"`
	AttemptCap        *int            `json:"attempt_cap,omitempty"`
	ScoreMin          *int64          `json:"score_min,omitempty"`
	ScoreMax          *int64          `json:"score_max,omitempty"`
	PeriodStartedAt   *time.Time      `json:"period_started_at,omitempty"`
	NextResetAt       *time.Time      `json:"next_reset_at,omitempty"`
	Metadata          json.RawMessage `json:"metadata,omitempty"`
}

// LeaderboardPeriod describes one completed reset interval.
type LeaderboardPeriod struct {
	Period    int       `json:"period"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
}

// LeaderboardPeriodsOptions configures cursor pagination for periods.
type LeaderboardPeriodsOptions struct {
	Limit  int
	Cursor string
}

// LeaderboardPeriodsPage contains completed periods and current reset state.
type LeaderboardPeriodsPage struct {
	CurrentPeriod   int                 `json:"current_period"`
	ResetSchedule   string              `json:"reset_schedule"`
	PeriodStartedAt *time.Time          `json:"period_started_at,omitempty"`
	NextResetAt     *time.Time          `json:"next_reset_at,omitempty"`
	Periods         []LeaderboardPeriod `json:"periods"`
	NextCursor      string              `json:"next_cursor"`
}

type entriesResponse struct {
	Entries []Entry `json:"entries"`
}

// List returns the leaderboard definitions available to this project.
func (l *LeaderboardsService) List(ctx context.Context) ([]LeaderboardInfo, error) {
	var result struct {
		Leaderboards []LeaderboardInfo `json:"leaderboards"`
	}
	err := l.c.callProtected(ctx, &Request{
		OperationID: "listLeaderboards",
		Method:      http.MethodGet,
		Path:        "/v1/leaderboards",
	}, &result)
	if err != nil {
		return nil, err
	}
	return result.Leaderboards, nil
}

// Submit posts a score as the current player. The leaderboard must explicitly
// permit client submissions. Authoritative games should use Server.SubmitScore
// from a trusted backend.
func (l *LeaderboardsService) Submit(ctx context.Context, leaderboardID, score int64, opts ...ScoreOption) error {
	body := submitScoreRequest{Score: score}
	for _, opt := range opts {
		opt(&body)
	}
	return l.c.callProtected(ctx, &Request{
		OperationID: "submitScore",
		Method:      http.MethodPost,
		Path:        leaderboardPath(leaderboardID) + "/scores",
		Body:        body,
	}, nil)
}

// Top returns the highest-ranked entries for the current period.
func (l *LeaderboardsService) Top(ctx context.Context, leaderboardID int64, limit int) ([]Entry, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var response entriesResponse
	err := l.c.callProtected(ctx, &Request{
		OperationID: "leaderboardTop",
		Method:      http.MethodGet,
		Path:        leaderboardPath(leaderboardID) + "/top",
		Query:       query,
	}, &response)
	if err != nil {
		return nil, err
	}
	return response.Entries, nil
}

// AroundMe returns the caller's rank and entries within radius positions.
func (l *LeaderboardsService) AroundMe(ctx context.Context, leaderboardID int64, radius int) (*AroundMeResult, error) {
	query := url.Values{}
	if radius > 0 {
		query.Set("radius", strconv.Itoa(radius))
	}
	var result AroundMeResult
	err := l.c.callProtected(ctx, &Request{
		OperationID: "leaderboardAroundMe",
		Method:      http.MethodGet,
		Path:        leaderboardPath(leaderboardID) + "/around-me",
		Query:       query,
	}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// Friends returns scores for the caller and accepted friends.
func (l *LeaderboardsService) Friends(ctx context.Context, leaderboardID int64) ([]Entry, error) {
	var response entriesResponse
	err := l.c.callProtected(ctx, &Request{
		OperationID: "leaderboardFriends",
		Method:      http.MethodGet,
		Path:        leaderboardPath(leaderboardID) + "/friends",
	}, &response)
	if err != nil {
		return nil, err
	}
	return response.Entries, nil
}

// Periods returns completed periods plus the current reset state.
func (l *LeaderboardsService) Periods(ctx context.Context, leaderboardID int64, opts LeaderboardPeriodsOptions) (*LeaderboardPeriodsPage, error) {
	query := url.Values{}
	if opts.Limit > 0 {
		query.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		query.Set("cursor", opts.Cursor)
	}
	var page LeaderboardPeriodsPage
	err := l.c.callProtected(ctx, &Request{
		OperationID: "listLeaderboardPeriods",
		Method:      http.MethodGet,
		Path:        leaderboardPath(leaderboardID) + "/periods",
		Query:       query,
	}, &page)
	if err != nil {
		return nil, err
	}
	return &page, nil
}

// AllPeriods iterates completed periods across every cursor page.
func (l *LeaderboardsService) AllPeriods(ctx context.Context, leaderboardID int64, opts LeaderboardPeriodsOptions) iter.Seq2[LeaderboardPeriod, error] {
	return cursorSequence(opts.Cursor, func(cursor string) ([]LeaderboardPeriod, string, error) {
		opts.Cursor = cursor
		page, err := l.Periods(ctx, leaderboardID, opts)
		if err != nil {
			return nil, "", err
		}
		return page.Periods, page.NextCursor, nil
	})
}

// PeriodTop returns the top entries for a completed or current period.
func (l *LeaderboardsService) PeriodTop(ctx context.Context, leaderboardID int64, period, limit int) ([]Entry, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var response entriesResponse
	err := l.c.callProtected(ctx, &Request{
		OperationID: "leaderboardPeriodTop",
		Method:      http.MethodGet,
		Path: leaderboardPath(leaderboardID) + "/periods/" +
			strconv.Itoa(period) + "/top",
		Query: query,
	}, &response)
	if err != nil {
		return nil, err
	}
	return response.Entries, nil
}

func leaderboardPath(id int64) string {
	return "/v1/leaderboards/" + strconv.FormatInt(id, 10)
}
