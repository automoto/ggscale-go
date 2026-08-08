package ggscale

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIAuthenticationAndAccountLifecycleOperations(t *testing.T) {
	ft := &fakeTransport{respond: func(req *Request) (any, error) {
		if req.OperationID == "authSteam" {
			return cannedSession(), nil
		}
		return nil, nil
	}}
	ctx := context.Background()

	steamSession, err := NewSteamAuth(ft, "key", "14000000048bcd42aabbccdd").Authenticate(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(42), steamSession.PlayerID)
	assert.Equal(t, "authSteam", ft.gotReq.OperationID)
	assert.Equal(t, "/v1/auth/steam", ft.gotReq.Path)

	c := newClientWithFake(t, ft)
	require.NoError(t, c.Auth.ResendVerification(ctx, "p@example.com"))
	assert.Equal(t, "resendVerification", ft.gotReq.OperationID)
	require.NoError(t, c.Auth.RequestPasswordReset(ctx, "p@example.com"))
	assert.Equal(t, "requestPasswordReset", ft.gotReq.OperationID)
	require.NoError(t, c.Auth.ConfirmPasswordReset(ctx, "p@example.com", "483920", "new-password"))
	assert.Equal(t, "confirmPasswordReset", ft.gotReq.OperationID)
	require.NoError(t, c.Auth.LinkEmail(ctx, "p@example.com", "password"))
	assert.Equal(t, "linkEmail", ft.gotReq.OperationID)
	assert.Equal(t, "test-jwt", ft.gotReq.SessionToken)
	require.NoError(t, c.Auth.LinkSteam(ctx, "14000000048bcd42aabbccdd"))
	assert.Equal(t, "linkSteam", ft.gotReq.OperationID)
	require.NoError(t, c.Auth.ChangePassword(ctx, "old-password", "new-password"))
	assert.Equal(t, "changePassword", ft.gotReq.OperationID)
	require.NoError(t, c.Auth.Disable(ctx, "new-password"))
	assert.Equal(t, "disablePlayer", ft.gotReq.OperationID)
	assert.Nil(t, c.Session())
}

func TestAPIPlayerProfileAndPublicSessionOperations(t *testing.T) {
	ft := &fakeTransport{respond: func(req *Request) (any, error) {
		switch req.OperationID {
		case "getPlayer", "resolveFriendCode":
			return map[string]any{
				"id": 42, "display_name": "Nova", "created_at": "2026-01-02T15:04:05Z",
			}, nil
		case "resolvePlayers":
			return map[string]any{"players": []map[string]any{{
				"id": 42, "display_name": "Nova", "created_at": "2026-01-02T15:04:05Z",
			}}}, nil
		case "regenerateFriendCode":
			return map[string]any{"friend_code": "XKCD4242"}, nil
		case "listGameSessions":
			return map[string]any{
				"items": []map[string]any{{
					"session_id": "gs_1", "props": map[string]any{"map": "arena"},
					"player_count": 2, "max_players": 8, "host_player_id": 42,
					"host_display_name": "Nova", "created_at": "2026-01-02T15:04:05Z",
				}},
				"next_cursor": "gs_1",
			}, nil
		default:
			return nil, nil
		}
	}}
	c := newClientWithFake(t, ft)
	ctx := context.Background()

	player, err := c.Players.Get(ctx, 42)
	require.NoError(t, err)
	assert.Equal(t, "Nova", player.DisplayName)
	assert.Equal(t, "/v1/players/42", ft.gotReq.Path)

	players, err := c.Players.Resolve(ctx, []int64{42, 87})
	require.NoError(t, err)
	require.Len(t, players, 1)
	assert.Equal(t, "42,87", ft.gotReq.Query.Get("ids"))

	_, err = c.Players.ResolveFriendCode(ctx, "XKD-4242")
	require.NoError(t, err)
	assert.Equal(t, "resolveFriendCode", ft.gotReq.OperationID)

	code, err := c.Profile.RegenerateFriendCode(ctx)
	require.NoError(t, err)
	assert.Equal(t, "XKCD4242", code)

	page, err := c.GameSessions.List(ctx, GameSessionListOptions{TitleID: "game", Limit: 10})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, int64(42), page.Items[0].HostPlayerID)
	assert.Equal(t, "game", ft.gotReq.Query.Get("title_id"))
}

func TestAPILeaderboardOperations(t *testing.T) {
	ft := &fakeTransport{respond: func(req *Request) (any, error) {
		switch req.OperationID {
		case "listLeaderboards":
			return map[string]any{"leaderboards": []map[string]any{{
				"id": 1, "name": "weekly", "sort_order": "desc",
				"score_operator": "best", "client_submissions": true,
				"reset_schedule": "weekly", "current_period": 2,
			}}}, nil
		case "leaderboardFriends", "leaderboardPeriodTop":
			return map[string]any{"entries": []map[string]any{{
				"player_id": 42, "score": 1500, "rank": 0,
				"display_name": "Nova", "metadata": map[string]any{"ghost": "r-42"},
			}}}, nil
		case "listLeaderboardPeriods":
			return map[string]any{
				"current_period": 2, "reset_schedule": "weekly", "next_cursor": "1",
				"periods": []map[string]any{{
					"period": 1, "started_at": "2026-01-01T00:00:00Z", "ended_at": "2026-01-08T00:00:00Z",
				}},
			}, nil
		default:
			return nil, nil
		}
	}}
	c := newClientWithFake(t, ft)
	ctx := context.Background()

	boards, err := c.Leaderboards.List(ctx)
	require.NoError(t, err)
	require.Len(t, boards, 1)
	assert.True(t, boards[0].ClientSubmissions)

	require.NoError(t, c.Leaderboards.Submit(ctx, 1, 1500, WithScoreMetadata(json.RawMessage(`{"ghost":"r-42"}`))))
	assert.Equal(t, "submitScore", ft.gotReq.OperationID)

	friends, err := c.Leaderboards.Friends(ctx, 1)
	require.NoError(t, err)
	require.Len(t, friends, 1)
	assert.Equal(t, "Nova", friends[0].DisplayName)
	assert.JSONEq(t, `{"ghost":"r-42"}`, string(friends[0].Metadata))

	periods, err := c.Leaderboards.Periods(ctx, 1, LeaderboardPeriodsOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, periods.Periods, 1)
	assert.Equal(t, "10", ft.gotReq.Query.Get("limit"))

	top, err := c.Leaderboards.PeriodTop(ctx, 1, 1, 5)
	require.NoError(t, err)
	require.Len(t, top, 1)
	assert.Equal(t, "/v1/leaderboards/1/periods/1/top", ft.gotReq.Path)
}

func TestAPIServerStorageOperations(t *testing.T) {
	ft := &fakeTransport{respond: func(req *Request) (any, error) {
		switch req.OperationID {
		case "serverGetStorageObject", "serverPutStorageObject":
			return map[string]any{
				"key": "slot/1", "value": map[string]any{"hp": 100},
				"version": 7, "updated_at": "2026-01-02T15:04:05Z",
			}, nil
		case "serverListStorageObjects":
			return map[string]any{"items": []map[string]any{}, "next_cursor": ""}, nil
		default:
			return nil, nil
		}
	}}
	c, err := NewClient(Options{APIKey: "secret", Transport: ft})
	require.NoError(t, err)
	ctx := context.Background()

	object, err := c.Server.StorageGet(ctx, 42, "slot/1")
	require.NoError(t, err)
	assert.Equal(t, int64(7), object.Version)
	assert.Equal(t, "/v1/server/players/42/storage/objects/slot%2F1", ft.gotReq.Path)

	_, err = c.Server.StoragePut(ctx, 42, "slot/1", map[string]int{"hp": 100}, IfMatch(6))
	require.NoError(t, err)
	assert.Equal(t, "6", ft.gotReq.IfMatch)

	_, err = c.Server.StoragePut(ctx, 42, "slot/1", nil)
	require.NoError(t, err)
	raw, ok := ft.gotReq.Body.(json.RawMessage)
	require.True(t, ok)
	assert.JSONEq(t, "null", string(raw))

	_, err = c.Server.StorageList(ctx, 42, ListOptions{KeyPrefix: "slot", Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, "slot", ft.gotReq.Query.Get("key_prefix"))
}

func TestAPIRemoteConfigETagAndHealth(t *testing.T) {
	const etag = `"remote-config-test"`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/config":
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write([]byte(`{"maintenance_mode":false}`))
		case "/v1/healthz":
			_, _ = w.Write([]byte(`{"status":"ok","version":"0.9.4","commit":"abc123"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, err := NewClient(Options{APIKey: "key", BaseURL: srv.URL})
	require.NoError(t, err)
	first, err := c.Config.Get(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, etag, first.ETag)
	assert.JSONEq(t, `false`, string(first.Values["maintenance_mode"]))

	second, err := c.Config.Get(context.Background(), first.ETag)
	require.NoError(t, err)
	assert.True(t, second.NotModified)
	assert.Nil(t, second.Values)
	assert.Equal(t, first.ETag, second.ETag, "304 retains the validator for the next poll")

	health, err := c.Health.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "0.9.4", health.Version)
}
