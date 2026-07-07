package ggscale

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvites_Create_posts_email_and_session(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{"invite_id": int64(31337)}, nil
		},
	}
	c := newClientWithFake(t, ft)

	id, err := c.Invites.Create(context.Background(), "gs_abc123", "friend@example.com")
	require.NoError(t, err)
	assert.Equal(t, int64(31337), id)

	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/invite", ft.gotReq.Path)
	body, ok := ft.gotReq.Body.(inviteCreateBody)
	require.True(t, ok)
	assert.Equal(t, "friend@example.com", body.ToEmail)
	assert.Equal(t, "gs_abc123", body.SessionID)
}

func TestInvites_Create_forbidden_when_not_friends(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return nil, &Error{Status: http.StatusForbidden, Message: "players are not friends"}
		},
	}
	c := newClientWithFake(t, ft)

	_, err := c.Invites.Create(context.Background(), "gs_abc123", "stranger@example.com")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrForbidden))
}

func TestInvites_List_decodes_pending_invites(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"invites": []map[string]any{
					{
						"invite_id":  int64(1),
						"from_email": "host@example.com",
						"session_id": "gs_abc123",
						"join_code":  "XKCD42",
						"expires_at": "2026-07-06T12:05:00Z",
					},
				},
			}, nil
		},
	}
	c := newClientWithFake(t, ft)

	invites, err := c.Invites.List(context.Background())
	require.NoError(t, err)
	assert.Equal(t, http.MethodGet, ft.gotReq.Method)
	assert.Equal(t, "/v1/invite", ft.gotReq.Path)

	require.Len(t, invites, 1)
	assert.Equal(t, int64(1), invites[0].InviteID)
	assert.Equal(t, "host@example.com", invites[0].FromEmail)
	assert.Equal(t, "XKCD42", invites[0].JoinCode)
	assert.True(t, invites[0].ExpiresAt.Equal(time.Date(2026, 7, 6, 12, 5, 0, 0, time.UTC)))
}

func TestInvites_List_empty(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{"invites": []map[string]any{}}, nil
		},
	}
	c := newClientWithFake(t, ft)

	invites, err := c.Invites.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, invites)
}

func TestInvites_Delete_by_id(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c := newClientWithFake(t, ft)

	err := c.Invites.Delete(context.Background(), 31337)
	require.NoError(t, err)
	assert.Equal(t, http.MethodDelete, ft.gotReq.Method)
	assert.Equal(t, "/v1/invite/31337", ft.gotReq.Path)
}
