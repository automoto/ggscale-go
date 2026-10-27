package ggscale

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestRelayService_GetCredentials_should_send_region_and_return_served_region(t *testing.T) {
	ft := &fakeTransport{
		respond: func(req *Request) (any, error) {
			return map[string]any{"username": "u", "password": "p", "ttl": int64(300), "region": "us-east"}, nil
		},
	}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	creds, err := c.Relay.GetCredentials(context.Background(), WithRegion("us-east"), WithMatch("m1"))

	require.NoError(t, err)
	assert.Equal(t, "us-east", ft.gotReq.Query.Get("region"))
	assert.Equal(t, "m1", ft.gotReq.Query.Get("match_id"))
	assert.Equal(t, "us-east", creds.Region)
}

func TestRelayService_GetCredentials_should_mark_the_request_replay_safe(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return map[string]any{"ttl": int64(300)}, nil }}
	c, _ := NewClient(Options{APIKey: "k", Transport: ft})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	_, err := c.Relay.GetCredentials(context.Background())

	require.NoError(t, err)
	assert.True(t, ft.gotReq.ReplaySafe)
}

func TestRelayService_GetCredentials_should_retry_when_no_relay_is_eligible(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username":"u","password":"p","ttl":300,"region":""}`))
	}))
	defer srv.Close()
	c, err := NewClient(Options{
		APIKey:      "k",
		BaseURL:     srv.URL,
		RetryPolicy: RetryPolicy{BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, Jitter: func(d time.Duration) time.Duration { return d }},
	})
	require.NoError(t, err)
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})

	creds, err := c.Relay.GetCredentials(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	assert.Equal(t, "u", creds.Username)
}

// relayClientWithFakeClock returns a client whose relay refresh loop waits on
// the returned channel instead of the real clock, recording each wait.
func relayClientWithFakeClock(t *testing.T, respond func(*Request) (any, error)) (*Client, *[]time.Duration) {
	t.Helper()
	c, _ := NewClient(Options{APIKey: "k", Transport: &fakeTransport{respond: respond}})
	c.SetSession(&Session{AccessToken: "tok", RefreshToken: "rt", ExpiresAt: time.Now().Add(time.Hour)})
	waits := &[]time.Duration{}
	c.Relay.after = func(d time.Duration) <-chan time.Time {
		*waits = append(*waits, d)
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
	return c, waits
}

func TestRelayService_KeepFresh_should_refetch_at_80_percent_of_the_ttl(t *testing.T) {
	c, waits := relayClientWithFakeClock(t, func(*Request) (any, error) {
		return map[string]any{"username": "u", "password": "p", "ttl": int64(300)}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	var got []*Credentials

	err := c.Relay.KeepFresh(ctx, func(creds *Credentials) {
		got = append(got, creds)
		if len(got) == 3 {
			cancel()
		}
	})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Len(t, got, 3)
	assert.Equal(t, []time.Duration{240 * time.Second, 240 * time.Second}, *waits)
}

func TestRelayService_KeepFresh_should_back_off_and_retry_after_an_error(t *testing.T) {
	calls := 0
	c, waits := relayClientWithFakeClock(t, func(*Request) (any, error) {
		calls++
		if calls == 1 {
			return nil, &Error{Status: http.StatusServiceUnavailable, RetryAfter: 10 * time.Second}
		}
		return map[string]any{"username": "u", "password": "p", "ttl": int64(300)}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())

	err := c.Relay.KeepFresh(ctx, func(*Credentials) { cancel() })

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []time.Duration{10 * time.Second}, *waits)
}

func TestRelayService_KeepFresh_should_stop_on_a_permanent_error(t *testing.T) {
	c, _ := relayClientWithFakeClock(t, func(*Request) (any, error) {
		return nil, &Error{Status: http.StatusForbidden, Code: "forbidden"}
	})

	err := c.Relay.KeepFresh(context.Background(), func(*Credentials) {})

	assert.ErrorIs(t, err, ErrForbidden)
}
