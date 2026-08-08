package ggscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Transport sends a single HTTP request to the ggscale API and decodes
// the response. Implementations must be safe for concurrent use.
type Transport interface {
	// Call performs req and decodes the response.
	//
	// req.APIKey is sent as Authorization: Bearer.
	// req.SessionToken (if non-empty) is sent as X-Session-Token.
	// req.IfMatch (if non-empty) is sent as If-Match.
	// req.Body is JSON-marshalled (skipped if nil).
	// out is JSON-unmarshalled from a 2xx response (skipped if nil).
	//
	// Any non-2xx response returns *Error.
	Call(ctx context.Context, req *Request, out any) error
}

// Request describes a single API call. Service methods build one of
// these and hand it to Transport.Call; tests can capture and inspect
// it directly without going through HTTP.
type Request struct {
	OperationID  string     // stable OpenAPI operationId, used for telemetry
	Method       string     // GET, POST, PUT, PATCH, DELETE
	Path         string     // "/v1/storage/objects/foo"
	Query        url.Values // optional
	Body         any        // optional, marshalled with encoding/json
	APIKey       string     // required
	SessionToken string     // optional; set for player routes
	IfMatch      string     // optional; only storage.Put uses it
	IfNoneMatch  string     // optional; remote config conditional request

	// ReplaySafe explicitly opts a mutating request into transport retries.
	// Leave false unless the operation is safe to repeat across concurrent
	// writers, not merely HTTP-idempotent in isolation.
	ReplaySafe bool

	// RequestID overrides the generated X-Request-Id. It is retained across
	// every attempt of one logical call.
	RequestID string

	// AllowNotModified treats 304 as a successful bodyless response.
	AllowNotModified bool

	// Response, when non-nil, receives response metadata.
	Response *ResponseMetadata
}

// ResponseMetadata captures response details that are useful without exposing
// response bodies or authentication headers.
type ResponseMetadata struct {
	StatusCode   int
	RequestID    string
	ETag         string
	CacheControl string
	NotModified  bool
}

// Error is the typed error returned for any non-2xx response. Use
// errors.Is with the package sentinels (ErrUnauthorized, ErrConflict,
// etc.) to branch on common cases; cast to *Error with errors.As to
// read details like RetryAfter or ConflictVersion.
type Error struct {
	Status   int
	Type     string
	Title    string
	Detail   string
	Instance string
	Code     string
	// Message is retained for source compatibility. It mirrors Detail when a
	// Problem Details response is returned.
	Message         string
	RequestID       string
	RetryAfter      time.Duration
	ConflictVersion int64
	// DiagnosticBody is a bounded response-body excerpt used only when the
	// server did not return parseable Problem Details.
	DiagnosticBody string
	// Details holds the problem-details `errors` array when present. The
	// 409 ticket_already_active response, for example, carries one entry
	// with Location "active_ticket_id".
	Details []ErrorDetail
}

// APIError is the preferred descriptive name for Error.
type APIError = Error

// FailureKind classifies failures that happen before an HTTP error response is
// available.
type FailureKind string

// FailureCanceled through FailureProtocol are the stable failure classes
// reported by RequestError.Kind.
const (
	FailureCanceled FailureKind = "canceled"
	FailureTimeout  FailureKind = "timeout"
	FailureDNS      FailureKind = "dns"
	FailureConnect  FailureKind = "connect"
	FailureTLS      FailureKind = "tls"
	FailureEncode   FailureKind = "encode"
	FailureDecode   FailureKind = "decode"
	FailureProtocol FailureKind = "protocol"
)

// RequestError is returned for transport, encoding, decoding, cancellation,
// and protocol failures. Unwrap exposes the original cause to errors.Is/As.
type RequestError struct {
	Kind        FailureKind
	OperationID string
	RequestID   string
	Err         error
}

func (e *RequestError) Error() string {
	if e.OperationID != "" {
		return fmt.Sprintf("ggscale: %s %s: %v", e.OperationID, e.Kind, e.Err)
	}
	return fmt.Sprintf("ggscale: %s: %v", e.Kind, e.Err)
}

func (e *RequestError) Unwrap() error { return e.Err }

// ErrorDetail is one entry from a problem-details `errors` array — a
// validation failure or a structured extension. Value is the raw JSON of
// the offending/related value.
type ErrorDetail struct {
	Message  string          `json:"message"`
	Location string          `json:"location"`
	Value    json.RawMessage `json:"value"`
}

// ActiveTicketID returns the id of the ticket already queued when this error
// is a 409 ticket_already_active conflict, or 0 otherwise.
func (e *Error) ActiveTicketID() int64 {
	for _, d := range e.Details {
		if d.Location != "active_ticket_id" {
			continue
		}
		var id int64
		if json.Unmarshal(d.Value, &id) == nil {
			return id
		}
	}
	return 0
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("ggscale: %d %s: %s", e.Status, e.Code, e.Message)
	}
	if e.Message != "" {
		return fmt.Sprintf("ggscale: %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("ggscale: %d", e.Status)
}

// Is reports whether the underlying HTTP status matches one of the
// package sentinels. Lets callers write errors.Is(err, ErrConflict)
// without type-asserting.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrUnauthorized:
		return e.Status == http.StatusUnauthorized
	case ErrForbidden:
		return e.Status == http.StatusForbidden
	case ErrNotFound:
		return e.Status == http.StatusNotFound
	case ErrConflict:
		return e.Status == http.StatusPreconditionFailed || e.Status == http.StatusConflict
	case ErrRateLimited:
		return e.Status == http.StatusTooManyRequests
	case ErrBadRequest:
		return e.Status == http.StatusBadRequest
	case ErrValidation:
		return e.Status == http.StatusUnprocessableEntity
	case ErrTicketActive:
		// v0.9.4 puts the stable slug in Problem Details `detail`, mirrored to
		// Message here. Accept Code as well for installations that emit the
		// optional machine-readable extension.
		return e.Status == http.StatusConflict &&
			(e.Code == "ticket_already_active" || e.Message == "ticket_already_active")
	}
	return false
}

// Sentinel errors for the common ggscale API failure modes. Match
// against an *Error using errors.Is.
var (
	ErrUnauthorized = errors.New("ggscale: unauthorized")
	ErrForbidden    = errors.New("ggscale: forbidden")
	ErrNotFound     = errors.New("ggscale: not found")
	ErrConflict     = errors.New("ggscale: conflict")
	ErrRateLimited  = errors.New("ggscale: rate limited")
	ErrBadRequest   = errors.New("ggscale: bad request")
	// ErrValidation is a 422 Unprocessable Entity: the request was
	// well-formed but failed field validation (e.g. an empty required
	// field). The offending fields are in Error.Details — each entry's
	// Location (e.g. "body.status") and Message name what to fix.
	ErrValidation = errors.New("ggscale: validation failed")
	// ErrTicketActive is the 409 returned by matchmaker CreateTicket when the
	// player already has an active ticket in the project. Read the active
	// ticket id with (*Error).ActiveTicketID.
	ErrTicketActive = errors.New("ggscale: player already has an active matchmaking ticket")
)
