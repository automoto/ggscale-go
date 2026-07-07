package ggscale

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cannedGameSession() map[string]any {
	return map[string]any{
		"session_id": "gs_abc123",
		"join_code":  "XKCD42",
		"state":      "open",
		"peers": []map[string]any{
			{"player_id": int64(1), "xuid": "x1", "addr": map[string]any{"ip": "203.0.113.1", "port": 7777}},
		},
	}
}

func TestGameSessions_Create_posts_body_and_decodes_session(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return cannedGameSession(), nil },
	}
	c := newClientWithFake(t, ft)

	sess, err := c.GameSessions.Create(context.Background(), GameSessionCreate{
		TitleID:    "my-game",
		PublicAddr: GameSessionAddr{IP: "203.0.113.1", Port: 7777},
		MaxPlayers: 4,
		Private:    true,
		Props:      json.RawMessage(`{"map":"dm_lobby"}`),
	})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/game-session", ft.gotReq.Path)
	body, ok := ft.gotReq.Body.(GameSessionCreate)
	require.True(t, ok)
	assert.Equal(t, "my-game", body.TitleID)
	assert.Equal(t, 7777, body.PublicAddr.Port)
	assert.True(t, body.Private)

	assert.Equal(t, "gs_abc123", sess.SessionID)
	assert.Equal(t, "XKCD42", sess.JoinCode)
	assert.Equal(t, "open", sess.State)
	require.Len(t, sess.Peers, 1)
	assert.Equal(t, int64(1), sess.Peers[0].PlayerID)
	assert.Equal(t, 7777, sess.Peers[0].Addr.Port)
}

func TestGameSessions_Get_by_id(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return cannedGameSession(), nil },
	}
	c := newClientWithFake(t, ft)

	sess, err := c.GameSessions.Get(context.Background(), "gs_abc123")
	require.NoError(t, err)
	assert.Equal(t, http.MethodGet, ft.gotReq.Method)
	assert.Equal(t, "/v1/game-session/gs_abc123", ft.gotReq.Path)
	assert.Equal(t, "gs_abc123", sess.SessionID)
}

func TestGameSessions_Resolve_join_code_to_session_id(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{"session_id": "gs_abc123"}, nil
		},
	}
	c := newClientWithFake(t, ft)

	id, err := c.GameSessions.Resolve(context.Background(), "XKCD42")
	require.NoError(t, err)
	assert.Equal(t, "gs_abc123", id)
	assert.Equal(t, http.MethodGet, ft.gotReq.Method)
	assert.Equal(t, "/v1/game-session", ft.gotReq.Path)
	assert.Equal(t, "XKCD42", ft.gotReq.Query.Get("joinCode"))
}

func TestGameSessions_Join_posts_public_addr(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return cannedGameSession(), nil },
	}
	c := newClientWithFake(t, ft)

	sess, err := c.GameSessions.Join(context.Background(), "gs_abc123", GameSessionAddr{IP: "198.51.100.7", Port: 7778})
	require.NoError(t, err)
	assert.Equal(t, "/v1/game-session/gs_abc123/join", ft.gotReq.Path)

	body, ok := ft.gotReq.Body.(gameSessionJoinBody)
	require.True(t, ok)
	assert.Equal(t, "198.51.100.7", body.PublicAddr.IP)
	assert.Equal(t, "gs_abc123", sess.SessionID)
}

func TestGameSessions_Join_gone_when_session_expired(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return nil, &Error{Status: http.StatusGone, Message: "session no longer joinable"}
		},
	}
	c := newClientWithFake(t, ft)

	_, err := c.GameSessions.Join(context.Background(), "gs_dead", GameSessionAddr{IP: "1.2.3.4", Port: 1})
	require.Error(t, err)
	var sdkErr *Error
	require.True(t, errors.As(err, &sdkErr))
	assert.Equal(t, http.StatusGone, sdkErr.Status)
}

func TestGameSessions_Heartbeat_returns_live_roster(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"ok": true,
				"peers": []map[string]any{
					{"player_id": int64(1), "addr": map[string]any{"ip": "203.0.113.1", "port": 7777}},
					{"player_id": int64(2), "addr": map[string]any{"ip": "198.51.100.7", "port": 7778}},
				},
			}, nil
		},
	}
	c := newClientWithFake(t, ft)

	peers, err := c.GameSessions.Heartbeat(context.Background(), "gs_abc123", json.RawMessage(`{"rtt_ms":23}`))
	require.NoError(t, err)
	assert.Equal(t, "/v1/game-session/gs_abc123/heartbeat", ft.gotReq.Path)

	body, ok := ft.gotReq.Body.(gameSessionHeartbeatBody)
	require.True(t, ok)
	require.NotNil(t, body.QoS)
	assert.JSONEq(t, `{"rtt_ms":23}`, string(*body.QoS))
	require.Len(t, peers, 2)
	assert.Equal(t, int64(2), peers[1].PlayerID)
}

func TestGameSessions_Heartbeat_nil_qos_sends_empty_object(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{"ok": true, "peers": []map[string]any{}}, nil
		},
	}
	c := newClientWithFake(t, ft)

	_, err := c.GameSessions.Heartbeat(context.Background(), "gs_abc123", nil)
	require.NoError(t, err)

	body, ok := ft.gotReq.Body.(gameSessionHeartbeatBody)
	require.True(t, ok)
	assert.Nil(t, body.QoS, "nil QoS marshals to {} so the server preserves the stored value")
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(raw))
}

func TestGameSessions_Leave_deletes_session(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c := newClientWithFake(t, ft)

	err := c.GameSessions.Leave(context.Background(), "gs_abc123")
	require.NoError(t, err)
	assert.Equal(t, http.MethodDelete, ft.gotReq.Method)
	assert.Equal(t, "/v1/game-session/gs_abc123", ft.gotReq.Path)
}

func TestGameSessions_Create_rate_limited_at_project_cap(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return nil, &Error{Status: http.StatusTooManyRequests, Message: "session limit reached for this project"}
		},
	}
	c := newClientWithFake(t, ft)

	_, err := c.GameSessions.Create(context.Background(), GameSessionCreate{
		PublicAddr: GameSessionAddr{IP: "1.2.3.4", Port: 7777},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRateLimited))
}
