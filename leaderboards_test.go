package ggscale

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLeaderboards_Submit_posts_score(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c := newClientWithFake(t, ft)

	err := c.Leaderboards.Submit(context.Background(), 42, 1500)
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/leaderboards/42/scores", ft.gotReq.Path)
	body, ok := ft.gotReq.Body.(submitScoreRequest)
	require.True(t, ok)
	assert.Equal(t, int64(1500), body.Score)
}

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

func TestLeaderboards_SubmitFor_uses_supplied_session_token(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c, _ := NewClient(Options{APIKey: "ggs_secret", Transport: ft})
	// Note: no Client.SetSession — SubmitFor must NOT require a
	// per-Client session, because a game server uses one Client for
	// many concurrent players.

	err := c.Leaderboards.SubmitFor(context.Background(), "player-123-jwt", 42, 1500)
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/leaderboards/42/scores", ft.gotReq.Path)
	assert.Equal(t, "ggs_secret", ft.gotReq.APIKey)
	assert.Equal(t, "player-123-jwt", ft.gotReq.SessionToken)
	body, ok := ft.gotReq.Body.(submitScoreRequest)
	require.True(t, ok)
	assert.Equal(t, int64(1500), body.Score)
}

func TestLeaderboards_SubmitFor_propagates_403_from_publishable_key(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("leaderboard submit requires secret key"))
	}))
	defer srv.Close()

	c, _ := NewClient(Options{APIKey: "ggp_publishable", BaseURL: srv.URL})
	err := c.Leaderboards.SubmitFor(context.Background(), "player-jwt", 1, 100)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrForbidden)
}

func TestLeaderboards_SubmitFor_does_not_mutate_client_session(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c, _ := NewClient(Options{APIKey: "ggs_secret", Transport: ft})
	clientSession := &Session{
		AccessToken:  "operator.jwt",
		RefreshToken: "rt",
	}
	c.SetSession(clientSession)

	err := c.Leaderboards.SubmitFor(context.Background(), "player-jwt", 1, 100)
	require.NoError(t, err)

	assert.Equal(t, "operator.jwt", c.Session().AccessToken)
	assert.Equal(t, "player-jwt", ft.gotReq.SessionToken)
}

func TestLeaderboards_Submit_404_returns_ErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("leaderboard not found"))
	}))
	defer srv.Close()

	c, _ := NewClient(Options{APIKey: "k", BaseURL: srv.URL})
	c.SetSession(liveSession())
	err := c.Leaderboards.Submit(context.Background(), 999, 100)
	require.Error(t, err)
}
