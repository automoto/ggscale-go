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
	Method       string     // GET, POST, PUT, PATCH, DELETE
	Path         string     // "/v1/storage/objects/foo"
	Query        url.Values // optional
	Body         any        // optional, marshalled with encoding/json
	APIKey       string     // required
	SessionToken string     // optional; set for player routes
	IfMatch      string     // optional; only storage.Put uses it
}

// Error is the typed error returned for any non-2xx response. Use
// errors.Is with the package sentinels (ErrUnauthorized, ErrConflict,
// etc.) to branch on common cases; cast to *Error with errors.As to
// read details like RetryAfter or ConflictVersion.
type Error struct {
	Status          int
	Code            string
	Message         string
	RetryAfter      time.Duration
	ConflictVersion int64
	// Details holds the problem-details `errors` array when present. The
	// 409 ticket_already_active response, for example, carries one entry
	// with Location "active_ticket_id".
	Details []ErrorDetail
}

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
	case ErrTicketActive:
		// The stable identifier lives in the machine-readable `code`
		// extension; fall back to Message for the legacy envelope, which
		// carried it there.
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
	// ErrTicketActive is the 409 returned by matchmaker CreateTicket when the
	// player already has an active ticket in the project. Read the active
	// ticket id with (*Error).ActiveTicketID.
	ErrTicketActive = errors.New("ggscale: player already has an active matchmaking ticket")
)
