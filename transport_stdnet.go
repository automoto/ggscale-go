package ggscale

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	userAgent      = "ggscale-go/0.3.0"
	defaultTimeout = 30 * time.Second
)

// StdNetTransport is the only Transport implementation in v0.1: JSON
// over HTTPS via the standard library net/http client. Construct one
// with at minimum a BaseURL; Client is optional.
type StdNetTransport struct {
	BaseURL string
	Client  *http.Client

	once          sync.Once
	defaultClient *http.Client
}

// Call implements Transport.
func (t *StdNetTransport) Call(ctx context.Context, req *Request, out any) error {
	httpReq, err := t.buildRequest(ctx, req)
	if err != nil {
		return err
	}

	resp, err := t.client().Do(httpReq)
	if err != nil {
		// Preserve context.Canceled / context.DeadlineExceeded so
		// callers can errors.Is them without unwrapping our error.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || len(body) == 0 {
			return nil
		}
		return json.Unmarshal(body, out)
	}

	return parseError(resp, body)
}

func (t *StdNetTransport) buildRequest(ctx context.Context, req *Request) (*http.Request, error) {
	u := t.BaseURL + req.Path
	if len(req.Query) > 0 {
		u += "?" + req.Query.Encode()
	}

	var body io.Reader
	if req.Body != nil {
		buf, err := json.Marshal(req.Body)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(buf)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, u, body)
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("User-Agent", userAgent)
	if req.Body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if req.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
	}
	if req.SessionToken != "" {
		httpReq.Header.Set("X-Session-Token", req.SessionToken)
	}
	if req.IfMatch != "" {
		httpReq.Header.Set("If-Match", req.IfMatch)
	}
	return httpReq, nil
}

func (t *StdNetTransport) client() *http.Client {
	if t.Client != nil {
		return t.Client
	}
	t.once.Do(func() {
		t.defaultClient = &http.Client{Timeout: defaultTimeout}
	})
	return t.defaultClient
}

// errBody is the JSON error envelope ggscale-server returns. It covers both
// the canonical Huma problem-details shape (title/detail/errors, plus a
// stable code extension where emitted) and the legacy error/message envelope
// still used by a few middleware writers. All fields are optional; parseError
// only populates *Error fields it can read.
type errBody struct {
	// Huma problem+json.
	Title  string        `json:"title"`
	Detail string        `json:"detail"`
	Code   string        `json:"code"`
	Errors []ErrorDetail `json:"errors"`
	// Legacy envelope.
	Error   string `json:"error"`
	Message string `json:"message"`

	RetryAfterSeconds int   `json:"retry_after_seconds"`
	Version           int64 `json:"version"`
	CurrentVersion    int64 `json:"current_version"`
}

func parseError(resp *http.Response, body []byte) error {
	e := &Error{Status: resp.StatusCode}

	var parsed errBody
	if len(body) > 0 && json.Unmarshal(body, &parsed) == nil {
		// Prefer the problem-details fields, falling back to the legacy
		// envelope. `code` is the stable machine-readable identifier (e.g.
		// "ticket_already_active", "rate_limit_exceeded"); `detail` (or the
		// legacy `message`) is the human-readable explanation. Match on Code,
		// surface Message to humans.
		e.Code = firstNonEmpty(parsed.Code, parsed.Error)
		e.Message = firstNonEmpty(parsed.Detail, parsed.Message, parsed.Title)
		e.Details = parsed.Errors
		if parsed.Version > 0 {
			e.ConflictVersion = parsed.Version
		} else if parsed.CurrentVersion > 0 {
			e.ConflictVersion = parsed.CurrentVersion
		}
		if parsed.RetryAfterSeconds > 0 {
			e.RetryAfter = time.Duration(parsed.RetryAfterSeconds) * time.Second
		}
	}

	if e.Message == "" && e.Code == "" && len(body) > 0 {
		e.Message = string(bytes.TrimSpace(body))
	}

	if h := resp.Header.Get("Retry-After"); h != "" {
		if secs, err := strconv.Atoi(h); err == nil {
			e.RetryAfter = time.Duration(secs) * time.Second
		}
	}

	return e
}

// firstNonEmpty returns the first non-empty string in vs, or "".
func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// Compile-time check.
var _ Transport = (*StdNetTransport)(nil)
