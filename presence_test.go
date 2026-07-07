package ggscale

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPresence_Set_puts_status(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{"ok": true}, nil
		},
	}
	c := newClientWithFake(t, ft)

	err := c.Presence.Set(context.Background(), "online", nil)
	require.NoError(t, err)

	assert.Equal(t, http.MethodPut, ft.gotReq.Method)
	assert.Equal(t, "/v1/presence", ft.gotReq.Path)
	body, ok := ft.gotReq.Body.(presenceUpdateBody)
	require.True(t, ok)
	assert.Equal(t, "online", body.Status)
	assert.Nil(t, body.SessionID)
}

func TestPresence_Set_with_session_id(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{"ok": true}, nil
		},
	}
	c := newClientWithFake(t, ft)

	sessionID := "gs_abc123"
	err := c.Presence.Set(context.Background(), "in_match", &sessionID)
	require.NoError(t, err)

	body, ok := ft.gotReq.Body.(presenceUpdateBody)
	require.True(t, ok)
	require.NotNil(t, body.SessionID)
	assert.Equal(t, "gs_abc123", *body.SessionID)
}

func TestPresence_Set_bad_request_on_invalid_status(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return nil, &Error{Status: http.StatusBadRequest, Message: "status must be 1–32 characters"}
		},
	}
	c := newClientWithFake(t, ft)

	err := c.Presence.Set(context.Background(), "", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBadRequest))
}
