package ggscale

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLeaderboards_Top_returns_entries(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"entries": []map[string]any{
					{"player_id": 1, "score": 9000, "rank": 0},
					{"player_id": 2, "score": 8000, "rank": 1},
					{"player_id": 3, "score": 7000, "rank": 2},
				},
			}, nil
		},
	}
	c := newClientWithFake(t, ft)

	entries, err := c.Leaderboards.Top(context.Background(), 1, 5)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	assert.Equal(t, "/v1/leaderboards/1/top", ft.gotReq.Path)
	assert.Equal(t, "5", ft.gotReq.Query.Get("limit"))
	assert.Equal(t, int64(0), entries[0].Rank)
	assert.Equal(t, int64(9000), entries[0].Score)
}

func TestLeaderboards_Top_omits_limit_when_zero(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{"entries": []map[string]any{}}, nil
		},
	}
	c := newClientWithFake(t, ft)

	_, err := c.Leaderboards.Top(context.Background(), 1, 0)
	require.NoError(t, err)
	assert.Empty(t, ft.gotReq.Query.Get("limit"))
}

func TestLeaderboards_AroundMe_surfaces_self_rank_minus_one(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"entries":   []map[string]any{},
				"self_rank": -1,
			}, nil
		},
	}
	c := newClientWithFake(t, ft)

	res, err := c.Leaderboards.AroundMe(context.Background(), 1, 3)
	require.NoError(t, err)
	assert.Equal(t, "/v1/leaderboards/1/around-me", ft.gotReq.Path)
	assert.Equal(t, "3", ft.gotReq.Query.Get("radius"))
	assert.Equal(t, int64(-1), res.SelfRank)
	assert.Empty(t, res.Entries)
}

func TestLeaderboards_AroundMe_with_entries(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"entries": []map[string]any{
					{"player_id": 1, "score": 100, "rank": 4},
					{"player_id": 7, "score": 90, "rank": 5},
				},
				"self_rank": 5,
			}, nil
		},
	}
	c := newClientWithFake(t, ft)

	res, err := c.Leaderboards.AroundMe(context.Background(), 1, 5)
	require.NoError(t, err)
	require.Len(t, res.Entries, 2)
	assert.Equal(t, int64(5), res.SelfRank)
	assert.Equal(t, int64(7), res.Entries[1].PlayerID)
}

func TestServer_SubmitScore_uses_server_route_and_player_id(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c, _ := NewClient(Options{APIKey: "ggs_secret", Transport: ft})
	// Note: no Client.SetSession — SubmitScore must NOT require a
	// per-Client session, because a game server uses one Client for
	// many concurrent players.

	err := c.Server.SubmitScore(context.Background(), 123, 42, 1500)
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/server/leaderboards/42/scores", ft.gotReq.Path)
	assert.Equal(t, "ggs_secret", ft.gotReq.APIKey)
	assert.Empty(t, ft.gotReq.SessionToken)
	body, ok := ft.gotReq.Body.(serverSubmitScoreRequest)
	require.True(t, ok)
	assert.Equal(t, int64(123), body.PlayerID)
	assert.Equal(t, int64(1500), body.Score)
}

func TestServer_SubmitScore_requires_player_id(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c, _ := NewClient(Options{APIKey: "ggs_secret", Transport: ft})

	err := c.Server.SubmitScore(context.Background(), 0, 1, 100)
	require.Error(t, err)
	assert.Nil(t, ft.gotReq, "no request is sent without a player ID")
}

func TestServer_SubmitScore_propagates_403_from_publishable_key(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("leaderboard submit requires secret key"))
	}))
	defer srv.Close()

	c, _ := NewClient(Options{APIKey: "ggp_publishable", BaseURL: srv.URL})
	err := c.Server.SubmitScore(context.Background(), 42, 1, 100)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrForbidden)
}

func TestServer_SubmitScore_does_not_mutate_client_session(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c, _ := NewClient(Options{APIKey: "ggs_secret", Transport: ft})
	clientSession := &Session{
		AccessToken:  "operator.jwt",
		RefreshToken: "rt",
	}
	c.SetSession(clientSession)

	err := c.Server.SubmitScore(context.Background(), 42, 1, 100)
	require.NoError(t, err)

	assert.Equal(t, "operator.jwt", c.Session().AccessToken)
	assert.Empty(t, ft.gotReq.SessionToken)
}

func TestLeaderboards_ListPage_passes_limit_and_cursor(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) {
		return map[string]any{"leaderboards": []map[string]any{}, "next_cursor": ""}, nil
	}}
	c := newClientWithFake(t, ft)

	_, err := c.Leaderboards.ListPage(context.Background(), LeaderboardListOptions{Limit: 25, Cursor: "weekly"})

	require.NoError(t, err)
	assert.Equal(t, url.Values{"limit": {"25"}, "cursor": {"weekly"}}, ft.gotReq.Query)
}

func TestLeaderboards_ListPage_returns_next_cursor(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) {
		return map[string]any{
			"leaderboards": []map[string]any{{"id": 1, "name": "alltime"}},
			"next_cursor":  "alltime",
		}, nil
	}}
	c := newClientWithFake(t, ft)

	page, err := c.Leaderboards.ListPage(context.Background(), LeaderboardListOptions{})

	require.NoError(t, err)
	assert.Equal(t, "alltime", page.NextCursor)
}

func TestLeaderboards_List_walks_every_page(t *testing.T) {
	var cursors []string
	ft := &fakeTransport{respond: func(req *Request) (any, error) {
		cursor := req.Query.Get("cursor")
		cursors = append(cursors, cursor)
		if cursor == "" {
			return map[string]any{"leaderboards": []map[string]any{{"id": 1, "name": "alltime"}}, "next_cursor": "alltime"}, nil
		}
		return map[string]any{"leaderboards": []map[string]any{{"id": 2, "name": "weekly"}}, "next_cursor": ""}, nil
	}}
	c := newClientWithFake(t, ft)

	boards, err := c.Leaderboards.List(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"", "alltime"}, cursors)
	assert.Equal(t, []string{"alltime", "weekly"}, []string{boards[0].Name, boards[1].Name})
}

func TestLeaderboards_All_stops_when_the_caller_breaks(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) {
		return map[string]any{
			"leaderboards": []map[string]any{{"id": 1, "name": "alltime"}, {"id": 2, "name": "daily"}},
			"next_cursor":  "daily",
		}, nil
	}}
	c := newClientWithFake(t, ft)

	for _, err := range c.Leaderboards.All(context.Background(), LeaderboardListOptions{}) {
		require.NoError(t, err)
		break
	}

	assert.Equal(t, 1, ft.callCount)
}

func TestLeaderboards_List_returns_error_from_a_later_page(t *testing.T) {
	ft := &fakeTransport{respond: func(req *Request) (any, error) {
		if req.Query.Get("cursor") == "" {
			return map[string]any{"leaderboards": []map[string]any{{"id": 1, "name": "alltime"}}, "next_cursor": "alltime"}, nil
		}
		return nil, &APIError{Status: http.StatusServiceUnavailable}
	}}
	c := newClientWithFake(t, ft)

	_, err := c.Leaderboards.List(context.Background())

	assert.Error(t, err)
}
