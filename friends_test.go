package ggscale

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFriends_List_decodes_page(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"items": []map[string]any{
					{
						"id":         int64(1),
						"account_id": "9a1e3f60-0000-0000-0000-000000000001",
						"player_id":  int64(11),
						"status":     "accepted",
						"email":      "friend@example.com",
						"presence":   map[string]any{"status": "online", "session_id": "gs_abc"},
						"created_at": "2026-07-01T10:00:00Z",
						"updated_at": "2026-07-02T10:00:00Z",
					},
					{
						"id":         int64(2),
						"account_id": "9a1e3f60-0000-0000-0000-000000000002",
						"status":     "accepted",
						"created_at": "2026-07-01T10:00:00Z",
						"updated_at": "2026-07-01T10:00:00Z",
					},
				},
				"next_cursor": "2",
			}, nil
		},
	}
	c := newClientWithFake(t, ft)

	page, err := c.Friends.List(context.Background(), FriendsListOptions{Status: "accepted", Limit: 2})
	require.NoError(t, err)

	assert.Equal(t, http.MethodGet, ft.gotReq.Method)
	assert.Equal(t, "/v1/friends", ft.gotReq.Path)
	assert.Equal(t, "accepted", ft.gotReq.Query.Get("status"))
	assert.Equal(t, "2", ft.gotReq.Query.Get("limit"))

	require.Len(t, page.Items, 2)
	first := page.Items[0]
	assert.Equal(t, int64(1), first.ID)
	require.NotNil(t, first.PlayerID)
	assert.Equal(t, int64(11), *first.PlayerID)
	require.NotNil(t, first.Email)
	assert.Equal(t, "friend@example.com", *first.Email)
	require.NotNil(t, first.Presence)
	assert.Equal(t, "online", first.Presence.Status)
	assert.Nil(t, page.Items[1].PlayerID, "friend without a player in this project has nil PlayerID")
	assert.Equal(t, "2", page.NextCursor)
}

func TestFriends_List_passes_cursor(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{"items": []map[string]any{}, "next_cursor": ""}, nil
		},
	}
	c := newClientWithFake(t, ft)

	_, err := c.Friends.List(context.Background(), FriendsListOptions{Cursor: "17"})
	require.NoError(t, err)
	assert.Equal(t, "17", ft.gotReq.Query.Get("cursor"))
	assert.Empty(t, ft.gotReq.Query.Get("status"), "empty status is omitted — server defaults to accepted")
}

func TestFriends_Request_returns_status(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{"status": "pending"}, nil
		},
	}
	c := newClientWithFake(t, ft)

	status, err := c.Friends.Request(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, "pending", status)
	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/friends/42/request", ft.gotReq.Path)
	assert.Nil(t, ft.gotReq.Body)
}

func TestFriends_Accept_posts_to_accept(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{"status": "accepted"}, nil
		},
	}
	c := newClientWithFake(t, ft)

	err := c.Friends.Accept(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/friends/42/accept", ft.gotReq.Path)
}

func TestFriends_Reject_posts_to_reject(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{"status": "rejected"}, nil
		},
	}
	c := newClientWithFake(t, ft)

	err := c.Friends.Reject(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, "/v1/friends/42/reject", ft.gotReq.Path)
}

func TestFriends_Remove_deletes_edge(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c := newClientWithFake(t, ft)

	err := c.Friends.Remove(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, http.MethodDelete, ft.gotReq.Method)
	assert.Equal(t, "/v1/friends/42", ft.gotReq.Path)
}

func TestFriends_Block_and_Unblock(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{"status": "blocked"}, nil
		},
	}
	c := newClientWithFake(t, ft)

	require.NoError(t, c.Friends.Block(context.Background(), 42))
	assert.Equal(t, "/v1/friends/42/block", ft.gotReq.Path)

	require.NoError(t, c.Friends.Unblock(context.Background(), 42))
	assert.Equal(t, "/v1/friends/42/unblock", ft.gotReq.Path)
}

func TestFriends_Accept_conflict_on_illegal_transition(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return nil, &Error{Status: http.StatusConflict, Message: "illegal transition"}
		},
	}
	c := newClientWithFake(t, ft)

	err := c.Friends.Accept(context.Background(), 42)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConflict))
}

func TestFriends_Request_forbidden_for_anonymous_caller(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return nil, &Error{Status: http.StatusForbidden, Message: "link a gg-scale account to use friends"}
		},
	}
	c := newClientWithFake(t, ft)

	_, err := c.Friends.Request(context.Background(), 42)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrForbidden))
}

func TestFriends_RemoteAddrs_gets_friend_addresses(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"addresses": []map[string]any{
					{"type": "ip_lan", "scope": "lan", "address": "192.168.1.20"},
				},
			}, nil
		},
	}
	c := newClientWithFake(t, ft)

	addrs, err := c.Friends.RemoteAddrs(context.Background(), 42)
	require.NoError(t, err)
	assert.Equal(t, "/v1/friends/42/remote-addrs", ft.gotReq.Path)
	require.Len(t, addrs, 1)
	assert.Equal(t, "ip_lan", addrs[0].Type)
	assert.Equal(t, "192.168.1.20", addrs[0].Address)
}
