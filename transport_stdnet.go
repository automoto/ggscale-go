package ggscale

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	userAgent                    = "ggscale-go/" + Version
	defaultTimeout               = 30 * time.Second
	defaultConnectTimeout        = 10 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultResponseHeaderTimeout = 15 * time.Second
	defaultMaxResponseBodyBytes  = int64(64 << 20)
	maxDiagnosticBodyBytes       = 4 << 10
)

// RetryPolicy controls automatic retries. Zero values select the documented
// defaults: 3 total attempts, 250ms base delay, and a 10s cap. Set MaxAttempts
// to 1 to disable retries. Mutating methods are never retried unless the
// request explicitly sets ReplaySafe.
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration

	// Jitter is primarily useful for deterministic tests. When nil, the
	// transport selects a cryptographically random delay in [0, cap].
	Jitter func(cap time.Duration) time.Duration
}

// LogEvent is emitted to StdNetTransport.Logger. Events deliberately exclude
// URLs, headers, and bodies so secrets cannot be logged accidentally.
type LogEvent struct {
	Level       string
	Event       string
	OperationID string
	Method      string
	Status      int
	Duration    time.Duration
	Attempt     int
	Attempts    int
	RequestID   string
	RetryReason string
	RetryDelay  time.Duration
	ErrorKind   FailureKind
}

// LogFunc receives structured transport events. The transport is silent when
// Logger is nil.
type LogFunc func(LogEvent)

// StdNetTransport is the default Transport: JSON over HTTP(S) via net/http.
// It is safe for concurrent use. Supply Client to customize proxy, TLS, and
// connection-pool behavior; the SDK clones it and installs redirect redaction.
type StdNetTransport struct {
	BaseURL string
	Client  *http.Client

	UserAgent            string
	CallTimeout          time.Duration
	MaxResponseBodyBytes int64
	RetryPolicy          RetryPolicy
	Logger               LogFunc

	once          sync.Once
	defaultClient *http.Client
}

// Call implements Transport.
func (t *StdNetTransport) Call(parent context.Context, req *Request, out any) (retErr error) {
	if req == nil {
		return &RequestError{Kind: FailureProtocol, Err: errors.New("nil request")}
	}
	requestID := req.RequestID
	if requestID == "" {
		requestID = newRequestID()
	}

	ctx, cancel := withCallTimeout(parent, t.CallTimeout)
	defer cancel()

	started := time.Now()
	attempts := 0
	lastStatus := 0
	defer func() {
		t.safeLog(LogEvent{
			Level:       "info",
			Event:       "http.complete",
			OperationID: req.OperationID,
			Method:      req.Method,
			Status:      lastStatus,
			Duration:    time.Since(started),
			Attempts:    attempts,
			RequestID:   requestID,
			ErrorKind:   failureKind(retErr),
		})
	}()

	maxAttempts, baseDelay, maxDelay := t.retryDefaults()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attempts = attempt
		httpReq, err := t.buildRequest(ctx, req, requestID)
		if err != nil {
			return wrapRequestError(req, requestID, classifyBuildError(err), err)
		}

		resp, err := t.client().Do(httpReq)
		if err != nil {
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return wrapRequestError(req, requestID, classifyContextError(ctxErr), ctxErr)
			}
			if attempt < maxAttempts && requestIsReplayable(req) && retryableTransportError(err) {
				delay := t.retryDelay(attempt, baseDelay, maxDelay)
				t.logRetry(req, requestID, attempt, "transport", delay, 0)
				if err := sleepContext(ctx, delay); err != nil {
					return wrapRequestError(req, requestID, classifyContextError(err), err)
				}
				continue
			}
			return wrapRequestError(req, requestID, classifyTransportError(err), err)
		}

		lastStatus = resp.StatusCode
		body, readErr := readResponseBody(resp.Body, t.maxBodyBytes())
		_ = resp.Body.Close()
		responseRequestID := firstNonEmpty(resp.Header.Get("X-Request-Id"), requestID)
		populateResponseMetadata(req, resp, responseRequestID)
		if readErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return wrapRequestError(req, responseRequestID, classifyContextError(ctxErr), ctxErr)
			}
			return wrapRequestError(req, responseRequestID, FailureProtocol, readErr)
		}

		success := resp.StatusCode >= 200 && resp.StatusCode < 300
		if resp.StatusCode == http.StatusNotModified && req.AllowNotModified {
			success = true
		}
		if success {
			if out == nil || len(body) == 0 {
				return nil
			}
			if err := json.Unmarshal(body, out); err != nil {
				return wrapRequestError(req, responseRequestID, FailureDecode, err)
			}
			return nil
		}

		apiErr := parseError(resp, body)
		if apiErr.RequestID == "" {
			apiErr.RequestID = responseRequestID
		}
		if attempt < maxAttempts && requestIsReplayable(req) && retryableStatus(resp.StatusCode) {
			delay := t.retryDelay(attempt, baseDelay, maxDelay)
			if apiErr.RetryAfter > 0 && !retryFitsDeadline(ctx, apiErr.RetryAfter) {
				// Preserve the actionable HTTP error and Retry-After value instead
				// of spending the rest of the call budget on a server-mandated wait
				// after which no retry can run.
				return apiErr
			}
			if apiErr.RetryAfter > delay {
				delay = apiErr.RetryAfter
			}
			t.logRetry(req, requestID, attempt, http.StatusText(resp.StatusCode), delay, resp.StatusCode)
			if err := sleepContext(ctx, delay); err != nil {
				return wrapRequestError(req, responseRequestID, classifyContextError(err), err)
			}
			continue
		}
		return apiErr
	}

	return wrapRequestError(req, requestID, FailureProtocol, errors.New("retry loop exhausted"))
}

func (t *StdNetTransport) buildRequest(ctx context.Context, req *Request, requestID string) (*http.Request, error) {
	if req.Path == "" || !strings.HasPrefix(req.Path, "/") {
		return nil, errors.New("request path must start with /")
	}
	u := strings.TrimRight(t.BaseURL, "/") + req.Path
	if len(req.Query) > 0 {
		u += "?" + req.Query.Encode()
	}

	var body io.Reader
	if req.Body != nil {
		buf, err := json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		body = bytes.NewReader(buf)
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, u, body)
	if err != nil {
		return nil, err
	}

	ua := t.UserAgent
	if ua == "" {
		ua = userAgent
	}
	httpReq.Header.Set("User-Agent", ua)
	httpReq.Header.Set("X-Request-Id", requestID)
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
	if req.IfNoneMatch != "" {
		httpReq.Header.Set("If-None-Match", req.IfNoneMatch)
	}
	return httpReq, nil
}

func (t *StdNetTransport) client() *http.Client {
	t.once.Do(func() {
		var base *http.Client
		if t.Client != nil {
			clone := *t.Client
			base = &clone
			if base.Transport == nil {
				base.Transport = boundedDefaultHTTPTransport()
			}
		} else {
			// The request context owns the overall deadline. Keeping Client.Timeout
			// unset lets callers increase CallTimeout without an undocumented
			// 30-second ceiling while the per-stage transport bounds remain active.
			base = &http.Client{Transport: boundedDefaultHTTPTransport()}
		}

		previousRedirect := base.CheckRedirect
		base.CheckRedirect = func(next *http.Request, via []*http.Request) error {
			if previousRedirect != nil {
				if err := previousRedirect(next, via); err != nil {
					return err
				}
			}
			if len(via) >= 10 {
				return errRedirectLimit
			}
			if len(via) > 0 && !sameOrigin(via[0].URL, next.URL) {
				next.Header.Del("Authorization")
				next.Header.Del("X-Session-Token")
			}
			return nil
		}
		t.defaultClient = base
	})
	return t.defaultClient
}

func boundedDefaultHTTPTransport() *http.Transport {
	httpTransport := http.DefaultTransport.(*http.Transport).Clone()
	httpTransport.Proxy = http.ProxyFromEnvironment
	httpTransport.DialContext = (&net.Dialer{
		Timeout:   defaultConnectTimeout,
		KeepAlive: 30 * time.Second,
	}).DialContext
	httpTransport.TLSHandshakeTimeout = defaultTLSHandshakeTimeout
	httpTransport.ResponseHeaderTimeout = defaultResponseHeaderTimeout
	return httpTransport
}

func (t *StdNetTransport) retryDefaults() (int, time.Duration, time.Duration) {
	attempts := t.RetryPolicy.MaxAttempts
	if attempts <= 0 {
		attempts = 3
	}
	base := t.RetryPolicy.BaseDelay
	if base <= 0 {
		base = 250 * time.Millisecond
	}
	capDelay := t.RetryPolicy.MaxDelay
	if capDelay <= 0 {
		capDelay = 10 * time.Second
	}
	return attempts, base, capDelay
}

func (t *StdNetTransport) retryDelay(attempt int, base, maximum time.Duration) time.Duration {
	capDelay := base
	for i := 1; i < attempt && capDelay < maximum; i++ {
		if capDelay > maximum/2 {
			capDelay = maximum
			break
		}
		capDelay *= 2
	}
	if capDelay > maximum {
		capDelay = maximum
	}
	if t.RetryPolicy.Jitter != nil {
		d := t.RetryPolicy.Jitter(capDelay)
		if d < 0 {
			return 0
		}
		if d > capDelay {
			return capDelay
		}
		return d
	}
	return fullJitter(capDelay)
}

func fullJitter(capDelay time.Duration) time.Duration {
	if capDelay <= 0 {
		return 0
	}
	n, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(capDelay)+1))
	if err != nil {
		return capDelay / 2
	}
	return time.Duration(n.Int64())
}

func (t *StdNetTransport) maxBodyBytes() int64 {
	if t.MaxResponseBodyBytes > 0 {
		return t.MaxResponseBodyBytes
	}
	return defaultMaxResponseBodyBytes
}

func (t *StdNetTransport) logRetry(req *Request, requestID string, attempt int, reason string, delay time.Duration, status int) {
	t.safeLog(LogEvent{
		Level:       "debug",
		Event:       "http.retry",
		OperationID: req.OperationID,
		Method:      req.Method,
		Status:      status,
		Attempt:     attempt,
		RequestID:   requestID,
		RetryReason: reason,
		RetryDelay:  delay,
	})
}

func (t *StdNetTransport) safeLog(event LogEvent) {
	if t.Logger == nil {
		return
	}
	defer func() { _ = recover() }()
	t.Logger(event)
}

// errBody covers canonical RFC 9457 Problem Details and legacy middleware
// envelopes still returned by older ggscale installations.
type errBody struct {
	Type     string        `json:"type"`
	Title    string        `json:"title"`
	Status   int           `json:"status"`
	Detail   string        `json:"detail"`
	Instance string        `json:"instance"`
	Code     string        `json:"code"`
	Errors   []ErrorDetail `json:"errors"`

	Error   string `json:"error"`
	Message string `json:"message"`

	RetryAfterSeconds int   `json:"retry_after_seconds"`
	Version           int64 `json:"version"`
	CurrentVersion    int64 `json:"current_version"`
}

func parseError(resp *http.Response, body []byte) *Error {
	e := &Error{
		Status:    resp.StatusCode,
		RequestID: resp.Header.Get("X-Request-Id"),
	}

	var parsed errBody
	if len(body) > 0 && json.Unmarshal(body, &parsed) == nil {
		e.Type = parsed.Type
		e.Title = parsed.Title
		e.Detail = firstNonEmpty(parsed.Detail, parsed.Message)
		e.Instance = parsed.Instance
		e.Code = firstNonEmpty(parsed.Code, parsed.Error)
		e.Message = firstNonEmpty(e.Detail, parsed.Title)
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
		diagnostic := bytes.TrimSpace(body)
		if len(diagnostic) > maxDiagnosticBodyBytes {
			diagnostic = diagnostic[:maxDiagnosticBodyBytes]
		}
		e.DiagnosticBody = string(diagnostic)
		e.Message = e.DiagnosticBody
	}

	if retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); retryAfter > e.RetryAfter {
		e.RetryAfter = retryAfter
	}
	return e
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
		return 0
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}

func readResponseBody(body io.Reader, maximum int64) ([]byte, error) {
	limited := io.LimitReader(body, maximum+1)
	b, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maximum {
		return nil, fmt.Errorf("response body exceeds %d bytes", maximum)
	}
	return b, nil
}

func populateResponseMetadata(req *Request, resp *http.Response, requestID string) {
	if req.Response == nil {
		return
	}
	req.Response.StatusCode = resp.StatusCode
	req.Response.RequestID = requestID
	req.Response.ETag = resp.Header.Get("ETag")
	req.Response.CacheControl = resp.Header.Get("Cache-Control")
	req.Response.NotModified = resp.StatusCode == http.StatusNotModified
}

func requestIsReplayable(req *Request) bool {
	switch req.Method {
	case http.MethodGet, http.MethodHead:
		return true
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return req.ReplaySafe
	default:
		return false
	}
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryableTransportError(err error) bool {
	if isTLSFailure(err) {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.IsTimeout || dnsErr.IsTemporary
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}
		var opErr *net.OpError
		return errors.As(err, &opErr)
	}
	return false
}

func classifyTransportError(err error) FailureKind {
	if errors.Is(err, errRedirectLimit) {
		return FailureProtocol
	}
	if isTLSFailure(err) {
		return FailureTLS
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return FailureDNS
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return FailureTimeout
	}
	return FailureConnect
}

func classifyBuildError(err error) FailureKind {
	if strings.Contains(err.Error(), "encode request body") {
		return FailureEncode
	}
	return FailureProtocol
}

func classifyContextError(err error) FailureKind {
	if errors.Is(err, context.DeadlineExceeded) {
		return FailureTimeout
	}
	return FailureCanceled
}

func isTLSFailure(err error) bool {
	var recordErr tls.RecordHeaderError
	var verificationErr *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var certificateErr x509.CertificateInvalidError
	return errors.As(err, &recordErr) || errors.As(err, &verificationErr) ||
		errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostnameErr) || errors.As(err, &certificateErr)
}

func failureKind(err error) FailureKind {
	var requestErr *RequestError
	if errors.As(err, &requestErr) {
		return requestErr.Kind
	}
	return ""
}

func wrapRequestError(req *Request, requestID string, kind FailureKind, err error) *RequestError {
	operationID := ""
	if req != nil {
		operationID = req.OperationID
	}
	return &RequestError{Kind: kind, OperationID: operationID, RequestID: requestID, Err: err}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func withCallTimeout(parent context.Context, configured time.Duration) (context.Context, context.CancelFunc) {
	if configured > 0 {
		return context.WithTimeout(parent, configured)
	}
	if _, hasDeadline := parent.Deadline(); hasDeadline {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, defaultTimeout)
}

func retryFitsDeadline(ctx context.Context, delay time.Duration) bool {
	deadline, ok := ctx.Deadline()
	if !ok {
		return true
	}
	remaining := time.Until(deadline)
	return remaining > 0 && delay < remaining
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func newRequestID() string {
	var b [16]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

var errRedirectLimit = errors.New("ggscale: stopped after 10 redirects")

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
