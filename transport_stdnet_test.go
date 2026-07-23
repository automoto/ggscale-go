package ggscale_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ggscale "github.com/automoto/ggscale-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

			tr := &ggscale.StdNetTransport{BaseURL: srv.URL}
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

func TestStdNetTransport_Call_default_http_client_has_timeout(t *testing.T) {
	tr := &ggscale.StdNetTransport{BaseURL: "http://localhost:1"}
	// The default client should be set lazily on first use; we can't read
	// it from the outside, but we can verify a Call against a closed port
	// fails with a network error (not a panic).
	err := tr.Call(context.Background(), &ggscale.Request{
		Method: http.MethodGet,
		Path:   "/v1/anything",
	}, nil)
	require.Error(t, err)
	assert.False(t, errors.Is(err, context.Canceled))
}
