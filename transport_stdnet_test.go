package ggscale_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ggscale "github.com/automoto/ggscale-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestStdNetTransport_Call_sends_headers_body_and_decodes_response(t *testing.T) {
	type echoReq struct {
		Hello string `json:"hello"`
	}
	type echoResp struct {
		Got string `json:"got"`
	}

	var (
		gotMethod  string
		gotPath    string
		gotAuth    string
		gotSession string
		gotIfMatch string
		gotCT      string
		gotUA      string
		gotBody    echoReq
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotSession = r.Header.Get("X-Session-Token")
		gotIfMatch = r.Header.Get("If-Match")
		gotCT = r.Header.Get("Content-Type")
		gotUA = r.Header.Get("User-Agent")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(echoResp{Got: gotBody.Hello})
	}))
	defer srv.Close()

	tr := &ggscale.StdNetTransport{BaseURL: srv.URL}
	var out echoResp
	err := tr.Call(context.Background(), &ggscale.Request{
		Method:       http.MethodPost,
		Path:         "/v1/echo",
		Body:         echoReq{Hello: "world"},
		APIKey:       "ggs_testkey",
		SessionToken: "jwt-abc",
		IfMatch:      "7",
	}, &out)

	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/v1/echo", gotPath)
	assert.Equal(t, "Bearer ggs_testkey", gotAuth)
	assert.Equal(t, "jwt-abc", gotSession)
	assert.Equal(t, "7", gotIfMatch)
	assert.Equal(t, "application/json", gotCT)
	assert.Equal(t, "ggscale-go/"+ggscale.Version, gotUA)
	assert.Equal(t, "world", gotBody.Hello)
	assert.Equal(t, "world", out.Got)
}

func TestStdNetTransport_Call_omits_optional_headers_when_empty(t *testing.T) {
	var (
		gotAuth    string
		gotSession string
		gotIfMatch string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotSession = r.Header.Get("X-Session-Token")
		gotIfMatch = r.Header.Get("If-Match")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	tr := &ggscale.StdNetTransport{BaseURL: srv.URL}
	err := tr.Call(context.Background(), &ggscale.Request{
		Method: http.MethodGet,
		Path:   "/v1/healthz",
	}, nil)

	require.NoError(t, err)
	assert.Empty(t, gotAuth)
	assert.Empty(t, gotSession)
	assert.Empty(t, gotIfMatch)
}

func TestStdNetTransport_Call_sends_query_string(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	tr := &ggscale.StdNetTransport{BaseURL: srv.URL}
	q := make(map[string][]string)
	q["limit"] = []string{"50"}
	q["cursor"] = []string{"abc"}
	err := tr.Call(context.Background(), &ggscale.Request{
		Method: http.MethodGet,
		Path:   "/v1/storage/objects",
		Query:  q,
	}, nil)

	require.NoError(t, err)
	// url.Values.Encode sorts keys alphabetically.
	assert.Equal(t, "cursor=abc&limit=50", gotQuery)
}

func TestStdNetTransport_Call_error_mapping(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		retryAfter string
		wantErr    error
		check      func(t *testing.T, e *ggscale.Error)
	}{
		{
			name:    "401 unauthorized — plain text",
			status:  http.StatusUnauthorized,
			body:    "unauthorized",
			wantErr: ggscale.ErrUnauthorized,
		},
		{
			name:    "404 not found",
			status:  http.StatusNotFound,
			body:    "not found",
			wantErr: ggscale.ErrNotFound,
		},
		{
			name:    "412 precondition failed with version body",
			status:  http.StatusPreconditionFailed,
			body:    `{"error":"version_mismatch","version":1}`,
			wantErr: ggscale.ErrConflict,
			check: func(t *testing.T, e *ggscale.Error) {
				assert.Equal(t, int64(1), e.ConflictVersion)
				assert.Equal(t, "version_mismatch", e.Code)
			},
		},
		{
			name:       "429 with Retry-After header",
			status:     http.StatusTooManyRequests,
			body:       `{"error":"rate_limit_exceeded","retry_after_seconds":5}`,
			retryAfter: "5",
			wantErr:    ggscale.ErrRateLimited,
			check: func(t *testing.T, e *ggscale.Error) {
				assert.Equal(t, 5*time.Second, e.RetryAfter)
				assert.Equal(t, "rate_limit_exceeded", e.Code)
			},
		},
		{
			name:    "429 with body retry_after_seconds (no header)",
			status:  http.StatusTooManyRequests,
			body:    `{"error":"rate_limit_exceeded","retry_after_seconds":3}`,
			wantErr: ggscale.ErrRateLimited,
			check: func(t *testing.T, e *ggscale.Error) {
				assert.Equal(t, 3*time.Second, e.RetryAfter)
			},
		},
		{
			name:    "400 bad request",
			status:  http.StatusBadRequest,
			body:    `{"error":"invalid_payload"}`,
			wantErr: ggscale.ErrBadRequest,
		},
		{
			name:    "400 problem-details detail becomes Message",
			status:  http.StatusBadRequest,
			body:    `{"title":"Bad Request","status":400,"detail":"attributes must be valid JSON"}`,
			wantErr: ggscale.ErrBadRequest,
			check: func(t *testing.T, e *ggscale.Error) {
				assert.Equal(t, "attributes must be valid JSON", e.Message)
			},
		},
		{
			// Real Huma validation shape captured from ggscale v0.9.0.
			name:    "422 validation error with field details",
			status:  http.StatusUnprocessableEntity,
			body:    `{"title":"Unprocessable Entity","status":422,"detail":"validation failed","errors":[{"message":"expected length >= 1","location":"body.status","value":""}]}`,
			wantErr: ggscale.ErrValidation,
			check: func(t *testing.T, e *ggscale.Error) {
				assert.Equal(t, "validation failed", e.Message)
				require.NotEmpty(t, e.Details)
				assert.Equal(t, "body.status", e.Details[0].Location)
				assert.Equal(t, "expected length >= 1", e.Details[0].Message)
			},
		},
		{
			name:    "409 ticket_already_active problem-details",
			status:  http.StatusConflict,
			body:    `{"title":"Conflict","status":409,"detail":"ticket_already_active","errors":[{"message":"player already has an active matchmaking ticket in this project","location":"active_ticket_id","value":55}]}`,
			wantErr: ggscale.ErrTicketActive,
			check: func(t *testing.T, e *ggscale.Error) {
				assert.Equal(t, "ticket_already_active", e.Message)
				assert.Equal(t, int64(55), e.ActiveTicketID())
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.retryAfter != "" {
					w.Header().Set("Retry-After", c.retryAfter)
				}
				w.WriteHeader(c.status)
				_, _ = io.WriteString(w, c.body)
			}))
			defer srv.Close()

			tr := &ggscale.StdNetTransport{
				BaseURL:     srv.URL,
				RetryPolicy: ggscale.RetryPolicy{MaxAttempts: 1},
			}
			err := tr.Call(context.Background(), &ggscale.Request{
				Method: http.MethodGet,
				Path:   "/v1/anything",
				APIKey: "k",
			}, nil)

			require.Error(t, err)
			assert.True(t, errors.Is(err, c.wantErr), "want errors.Is(err, %v)", c.wantErr)

			var sdkErr *ggscale.Error
			require.True(t, errors.As(err, &sdkErr))
			assert.Equal(t, c.status, sdkErr.Status)
			if c.check != nil {
				c.check(t, sdkErr)
			}
		})
	}
}

func TestStdNetTransport_Call_context_cancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // hang until client cancels
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	tr := &ggscale.StdNetTransport{BaseURL: srv.URL}
	err := tr.Call(ctx, &ggscale.Request{Method: http.MethodGet, Path: "/v1/hang"}, nil)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled), "want context.Canceled, got %v", err)
}

func TestStdNetTransport_Call_default_http_client_handles_network_failure(t *testing.T) {
	tr := &ggscale.StdNetTransport{BaseURL: "http://localhost:1"}
	// A Call against a closed port fails with a typed network error rather
	// than panicking while the default client is initialized lazily.
	err := tr.Call(context.Background(), &ggscale.Request{
		Method: http.MethodGet,
		Path:   "/v1/anything",
	}, nil)
	require.Error(t, err)
	assert.False(t, errors.Is(err, context.Canceled))
}

func TestStdNetTransport_retries_safe_request_with_stable_request_id(t *testing.T) {
	var hits atomic.Int32
	var requestID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if requestID == "" {
			requestID = r.Header.Get("X-Request-Id")
		} else {
			assert.Equal(t, requestID, r.Header.Get("X-Request-Id"))
		}
		if n < 3 {
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	var events []ggscale.LogEvent
	tr := &ggscale.StdNetTransport{
		BaseURL: srv.URL,
		RetryPolicy: ggscale.RetryPolicy{
			MaxAttempts: 3,
			Jitter:      func(time.Duration) time.Duration { return 0 },
		},
		Logger: func(event ggscale.LogEvent) { events = append(events, event) },
	}
	var out struct {
		OK bool `json:"ok"`
	}
	err := tr.Call(context.Background(), &ggscale.Request{
		OperationID: "getRemoteConfig",
		Method:      http.MethodGet,
		Path:        "/retry",
	}, &out)
	require.NoError(t, err)
	assert.True(t, out.OK)
	assert.Equal(t, int32(3), hits.Load())
	require.Len(t, events, 3)
	assert.Equal(t, "http.retry", events[0].Event)
	assert.Equal(t, "http.retry", events[1].Event)
	assert.Equal(t, "http.complete", events[2].Event)
	assert.Equal(t, 3, events[2].Attempts)
}

func TestStdNetTransport_does_not_retry_post_by_default(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "try again", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	tr := &ggscale.StdNetTransport{
		BaseURL: srv.URL,
		RetryPolicy: ggscale.RetryPolicy{
			MaxAttempts: 3,
			Jitter:      func(time.Duration) time.Duration { return 0 },
		},
	}
	err := tr.Call(context.Background(), &ggscale.Request{
		OperationID: "authRefresh",
		Method:      http.MethodPost,
		Path:        "/v1/auth/refresh",
		Body:        map[string]string{"refresh_token": "secret"},
	}, nil)
	require.Error(t, err)
	assert.Equal(t, int32(1), hits.Load())
}

func TestStdNetTransport_does_not_retry_mutating_methods_without_opt_in(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				http.Error(w, "try again", http.StatusServiceUnavailable)
			}))
			defer srv.Close()

			tr := &ggscale.StdNetTransport{
				BaseURL: srv.URL,
				RetryPolicy: ggscale.RetryPolicy{
					MaxAttempts: 3,
					Jitter:      func(time.Duration) time.Duration { return 0 },
				},
			}
			err := tr.Call(context.Background(), &ggscale.Request{
				Method: method, Path: "/mutate", Body: map[string]int{"version": 1},
			}, nil)
			require.Error(t, err)
			assert.Equal(t, int32(1), hits.Load())
		})
	}
}

func TestStdNetTransport_retries_mutating_method_only_with_explicit_opt_in(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	tr := &ggscale.StdNetTransport{
		BaseURL: srv.URL,
		RetryPolicy: ggscale.RetryPolicy{
			MaxAttempts: 2,
			Jitter:      func(time.Duration) time.Duration { return 0 },
		},
	}
	err := tr.Call(context.Background(), &ggscale.Request{
		Method: http.MethodPut, Path: "/mutate", Body: map[string]int{"version": 1}, ReplaySafe: true,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(2), hits.Load())
}

func TestStdNetTransport_does_not_replay_put_after_ambiguous_transport_error(t *testing.T) {
	var attempts atomic.Int32
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts.Add(1)
		return nil, &net.OpError{Op: "write", Net: "tcp", Err: errors.New("connection reset")}
	})}
	tr := &ggscale.StdNetTransport{
		BaseURL: "http://ggscale.invalid", Client: httpClient,
		RetryPolicy: ggscale.RetryPolicy{
			MaxAttempts: 3,
			Jitter:      func(time.Duration) time.Duration { return 0 },
		},
	}

	err := tr.Call(context.Background(), &ggscale.Request{
		Method: http.MethodPut, Path: "/v1/storage/objects/save",
		Body: map[string]int{"version": 1},
	}, nil)
	require.Error(t, err)
	assert.Equal(t, int32(1), attempts.Load(), "ambiguous PUT outcome must be returned to the caller")
}

func TestStdNetTransport_deadline_cancels_retry_backoff(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "try again", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	tr := &ggscale.StdNetTransport{
		BaseURL: srv.URL,
		RetryPolicy: ggscale.RetryPolicy{
			MaxAttempts: 3,
			Jitter:      func(cap time.Duration) time.Duration { return cap },
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := tr.Call(ctx, &ggscale.Request{Method: http.MethodGet, Path: "/retry"}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	var requestErr *ggscale.RequestError
	require.ErrorAs(t, err, &requestErr)
	assert.Equal(t, ggscale.FailureTimeout, requestErr.Kind)
	assert.Equal(t, int32(1), hits.Load())
}

func TestStdNetTransport_retry_after_beyond_deadline_returns_rate_limit_immediately(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "60")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	tr := &ggscale.StdNetTransport{BaseURL: srv.URL}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	err := tr.Call(ctx, &ggscale.Request{Method: http.MethodGet, Path: "/limited"}, nil)
	elapsed := time.Since(started)

	require.Error(t, err)
	var apiErr *ggscale.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusTooManyRequests, apiErr.Status)
	assert.Equal(t, time.Minute, apiErr.RetryAfter)
	assert.Less(t, elapsed, 500*time.Millisecond)
	assert.Equal(t, int32(1), hits.Load())
}

func TestStdNetTransport_bounds_response_body(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 32))
	}))
	defer srv.Close()

	tr := &ggscale.StdNetTransport{BaseURL: srv.URL, MaxResponseBodyBytes: 8}
	err := tr.Call(context.Background(), &ggscale.Request{Method: http.MethodGet, Path: "/large"}, nil)
	require.Error(t, err)
	var requestErr *ggscale.RequestError
	require.ErrorAs(t, err, &requestErr)
	assert.Equal(t, ggscale.FailureProtocol, requestErr.Kind)
}

func TestStdNetTransport_strips_credentials_on_cross_origin_redirect(t *testing.T) {
	var authorization, session string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		session = r.Header.Get("X-Session-Token")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/target", http.StatusFound)
	}))
	defer source.Close()

	tr := &ggscale.StdNetTransport{BaseURL: source.URL}
	err := tr.Call(context.Background(), &ggscale.Request{
		Method:       http.MethodGet,
		Path:         "/redirect",
		APIKey:       "api-secret",
		SessionToken: "player-secret",
	}, nil)
	require.NoError(t, err)
	assert.Empty(t, authorization)
	assert.Empty(t, session)
}

func TestStdNetTransport_redirect_loop_returns_typed_protocol_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer srv.Close()

	tr := &ggscale.StdNetTransport{BaseURL: srv.URL}
	err := tr.Call(context.Background(), &ggscale.Request{Method: http.MethodGet, Path: "/loop"}, nil)
	require.Error(t, err)
	var requestErr *ggscale.RequestError
	require.ErrorAs(t, err, &requestErr)
	assert.Equal(t, ggscale.FailureProtocol, requestErr.Kind)
	assert.Contains(t, requestErr.Error(), "stopped after 10 redirects")
}

func TestStdNetTransport_problem_details_and_retry_after_date(t *testing.T) {
	retryAt := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.Header().Set("X-Request-Id", "server-request-id")
		w.Header().Set("Retry-After", retryAt)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"https://ggscale.dev/problems/rate-limit","title":"Rate limited","status":429,"detail":"slow down","instance":"urn:request:123"}`)
	}))
	defer srv.Close()

	tr := &ggscale.StdNetTransport{
		BaseURL:     srv.URL,
		RetryPolicy: ggscale.RetryPolicy{MaxAttempts: 1},
	}
	err := tr.Call(context.Background(), &ggscale.Request{Method: http.MethodGet, Path: "/limited"}, nil)
	require.Error(t, err)
	var apiErr *ggscale.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "https://ggscale.dev/problems/rate-limit", apiErr.Type)
	assert.Equal(t, "Rate limited", apiErr.Title)
	assert.Equal(t, "slow down", apiErr.Detail)
	assert.Equal(t, "urn:request:123", apiErr.Instance)
	assert.Equal(t, "server-request-id", apiErr.RequestID)
	assert.Positive(t, apiErr.RetryAfter)
}
