package ggscale

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayService_GetCredentials(t *testing.T) {
	ft := &fakeTransport{
		respond: func(req *Request) (any, error) {
			assert.Equal(t, http.MethodPost, req.Method)
			assert.Equal(t, "/v1/relay/credentials", req.Path)
			return map[string]any{
				"username": "user-1",
				"password": "hmac-secret",
				"ttl":      int64(3600),
				"realm":    "ggscale",
				"urls":     []string{"turn:localhost:3478"},
			}, nil
		},
	}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	creds, err := c.Relay.GetCredentials(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "user-1", creds.Username)
	assert.Equal(t, "hmac-secret", creds.Password)
	assert.Equal(t, int64(3600), creds.TTL)
	assert.Equal(t, []string{"turn:localhost:3478"}, creds.URLs)
}

func TestRelayService_GetCredentials_no_session(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})

	_, err := c.Relay.GetCredentials(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no session")
}
