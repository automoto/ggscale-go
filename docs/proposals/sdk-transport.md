# Proposal: Go SDK v0.1 — Transport and Authenticator interfaces

**Status:** draft, blocking m1.md §8.2+
**Author:** ggscale team
**Date:** 2026-05-03

## What this proposal locks down

Two interfaces — `Transport` and `Authenticator` — and the small set of types that go with them (`Request`, `Session`, `Error`). Once these are accepted, the rest of m1.md §8 (auth, storage, leaderboards, profile, client, quickstart) is mechanical — it cannot reshape the surface, only fill it in.

The reason for a proposal at all: every other piece of the SDK depends on these two interfaces. Reworking them after `Storage`, `Leaderboards`, etc. exist means rewriting every service file, and reworking them after the SDK has tagged a release means breaking every consumer. doomerang-mp is the first such consumer (see m1.md §9), and the eventual public SDK ships on top of this.

## Goals

- A v0.1 surface stable enough that doomerang-mp's `network/client.go` rewrite (m1.md §9.3) doesn't have to change again before v1.0.
- A single concrete `Transport` (`StdNetTransport` — JSON over HTTP). Anything more clever is deferred.
- Three concrete `Authenticator`s — `EmailPasswordAuth`, `CustomTokenAuth`, `OfflineAuth` — covering the three flows that exist today.
- No transport-layer cleverness in the service files. They build a `Request`, hand it to `Transport.Call`, and unmarshal what comes back.

## Non-goals

- Streaming or WebSocket transport. Phase 2.
- Per-tenant key rotation. v1.1.
- Friends API. The server endpoints exist; the SDK adds them in a follow-up once the player-relationship UX is more settled.
- Retries with backoff. Game code typically has its own retry policy and would rather see the error than have the SDK swallow it.

## Why a separate Go module

`sdk-go/` ships with its own `go.mod` (`github.com/automoto/ggscale-go`, Go 1.24). It depends on nothing in the parent module and the parent depends on nothing in it.

Two reasons:

1. The directory is likely to be split into its own repo later. With its own module from day one, the split is a `git filter-repo` operation that doesn't change a single import path on the consumer side.
2. doomerang-mp is on Go 1.24. If the SDK shared the parent's `go 1.26.2`, every game project on a slightly older toolchain would be locked out. The server can move ahead independently.

The rule that follows: nothing in `sdk-go/` may import `github.com/ggscale/ggscale/...`, and nothing in the parent module may import `github.com/automoto/ggscale-go`. Test fixtures in the SDK that need server behavior use `httptest.NewServer` with hand-rolled handlers, not the real `internal/httpapi` router.

## `Transport`

```go
// Transport sends a single HTTP request to the ggscale API.
// Implementations must be safe for concurrent use.
type Transport interface {
    // Call performs the request and decodes the response.
    //
    // req.APIKey is sent as Authorization: Bearer.
    // req.SessionToken (if non-empty) is sent as X-Session-Token.
    // req.Body is JSON-marshalled (skipped if nil).
    // out is JSON-unmarshalled from a 2xx response (skipped if nil).
    //
    // Any non-2xx response returns *Error.
    Call(ctx context.Context, req *Request, out any) error
}

type Request struct {
    Method       string     // GET, POST, PUT, PATCH, DELETE
    Path         string     // "/v1/storage/objects/foo"
    Query        url.Values // optional
    Body         any        // optional, marshalled with encoding/json
    APIKey       string     // required on every call
    SessionToken string     // optional; set for end-user routes
    IfMatch      string     // optional; only storage.Put uses it
}
```

Why this shape:

- It hides the JSON marshal/unmarshal from every service method. `Storage.Get` becomes three lines: build a `Request`, call `Transport.Call`, return.
- One fake to write for tests. Service tests don't go through the real HTTP stack — they assert against the `Request` the fake captured.
- Composition. A logging or metrics wrapper is `func (l *LoggingTransport) Call(...) error` that delegates to an inner `Transport`. No reflection, no plugin registry.

Alternatives considered:

- A `RoundTripper`-shaped interface (`Do(req *http.Request) (*http.Response, error)`). Rejected because every service method would still have to do its own JSON marshal/unmarshal, and the fake transport would have to construct full `*http.Response` values.
- `Call(ctx, method, path, body, out any) error`. Rejected because the optional headers (`Authorization`, `X-Session-Token`, `If-Match`) and query params end up as variadic options or extra params, both of which are uglier than a struct.

## `Authenticator`

```go
// Authenticator establishes a session with ggscale. Implementations
// either call /v1/auth/* (EmailPassword, CustomToken) or return a
// synthetic local session (Offline).
type Authenticator interface {
    Authenticate(ctx context.Context) (*Session, error)
}

type Session struct {
    AccessToken  string    // JWT, sent as X-Session-Token
    RefreshToken string    // opaque; "" for OfflineAuth
    EndUserID    int64
    ExpiresAt    time.Time
}
```

The three v0.1 implementations:

| Constructor | Server call | Notes |
|---|---|---|
| `NewEmailPasswordAuth(t Transport, apiKey, email, password string)` | `POST /v1/auth/login` | Standard email+password flow. |
| `NewCustomTokenAuth(t Transport, apiKey, signedToken string)` | `POST /v1/auth/custom-token` | Tenant signs an HS256 JWT carrying `external_id`; ggscale verifies and issues its own session. |
| `NewOfflineAuth()` | none | Synthetic local session, random `EndUserID`, no refresh token, `ExpiresAt` ≈ never. For LAN parties and self-hosted installs without a central directory. |

Open question — does `Authenticator` take a `Transport` or capture it at construction? Capture wins: `EmailPasswordAuth` and `CustomTokenAuth` are useless without a transport, and `OfflineAuth` doesn't want one. Forcing the call site to pass a transport every `Authenticate(ctx)` invocation would be noise. This matches `golang.org/x/oauth2`, where `TokenSource.Token()` takes no arguments and the transport is captured by the source.

## Auto-refresh contract

The `Client` wraps the current session behind a `sync.RWMutex`. Every protected-route call goes through one private method, which is the only place that reads or writes the session.

The contract:

1. **Proactive refresh.** Before each call, if `time.Until(session.ExpiresAt) < 30s`, refresh once. Concurrent callers wait on the same write lock, so refresh fires once per expiry window — not once per goroutine.
2. **Reactive refresh.** A 401 response triggers a single refresh + retry. A second 401 surfaces to the caller. The retry happens once, not in a loop, because if the refresh succeeded and the request still 401s, the problem is not stale tokens.
3. **No refresh for `OfflineAuth`.** Its session has no `RefreshToken`. The refresh path checks for the empty string and returns the existing session unchanged.

The 30-second window is a guess at the right value: long enough that we don't hand out tokens about to expire mid-call, short enough that we don't refresh constantly. Revisit if traces show too many or too few refreshes in production.

## Error model

```go
type Error struct {
    Status          int
    Code            string        // "rate_limit_exceeded"; "" if server returned plain text
    Message         string
    RetryAfter      time.Duration // populated on 429
    ConflictVersion int64         // populated on 412 storage OCC if present
}

func (e *Error) Error() string { ... }
func (e *Error) Is(target error) bool { ... } // matches sentinels below

var (
    ErrUnauthorized = errors.New("ggscale: unauthorized")
    ErrNotFound     = errors.New("ggscale: not found")
    ErrConflict     = errors.New("ggscale: conflict")
    ErrRateLimited  = errors.New("ggscale: rate limited")
    ErrBadRequest   = errors.New("ggscale: bad request")
)
```

The sentinels exist so callers can write `if errors.Is(err, ggscale.ErrConflict)` without type-asserting. The `*Error` value carries the details (Retry-After seconds, conflict version) for callers that need them.

The server is not consistent about error envelopes — most handlers use `http.Error` with plain text, a few return JSON. The transport handles both: tries to JSON-decode into `{"error": "...", "retry_after_seconds": N}` first, falls back to treating the body as the message.

## Surface diagram

```
                    ┌─────────────────────┐
                    │  ggscale.Client     │
                    │  - Auth             │
                    │  - Storage          │
                    │  - Leaderboards     │
                    │  - Profile          │
                    │  - session (RWMutex)│
                    └──────────┬──────────┘
                               │ uses
                    ┌──────────▼──────────┐
                    │   Transport         │  ← interface
                    │   (StdNetTransport) │  ← only impl in v0.1
                    └──────────┬──────────┘
                               │
                          HTTP/JSON
                               │
                    ┌──────────▼──────────┐
                    │  ggscale-server     │
                    │  /v1/*              │
                    └─────────────────────┘
```

Authenticators live alongside the client, not inside it: `Client.Login(ctx, NewEmailPasswordAuth(...))` lets the caller pick the auth flow, and the same client can be re-`Login`'d with a different authenticator later.

## What this proposal does not pick

- The exact text of doc comments.
- The internal layout of `client.go` (private helpers, etc.).
- Which lints to disable in `sdk-go/.golangci.yml`. The plan is to copy the parent's config verbatim and tighten only if something proves noisy.

These are all implementation details that the §8.2+ tasks settle without re-litigating the interfaces above.
