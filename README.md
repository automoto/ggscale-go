# ggscale-go

Official Go client for the [ggscale](https://github.com/automoto/gg-scale) API. Covers the v1 surface used by game code: player authentication, per-player JSON storage, leaderboards, profiles, friends, presence, player-hosted game sessions with invites, matchmaking, real-time events, and server-tier session verification.

The SDK's only runtime dependency is [`github.com/coder/websocket`](https://github.com/coder/websocket), used by the realtime client. Everything else is the Go standard library.

## Install

```sh
go get github.com/automoto/ggscale-go
```

Requires Go 1.26 or later.

## Quickstart

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    ggscale "github.com/automoto/ggscale-go"
)

func main() {
    c, err := ggscale.NewClient(ggscale.Options{
        BaseURL: "http://localhost:8080",
        APIKey:  os.Getenv("GGSCALE_API_KEY"),
    })
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()
    if err := c.Login(ctx, ggscale.NewEmailPasswordAuth(
        c.Transport(), os.Getenv("GGSCALE_API_KEY"),
        "demo@example.com", "hunter2hunter2",
    )); err != nil {
        log.Fatal(err)
    }

    top, _ := c.Leaderboards.Top(ctx, 1, 5)
    for _, e := range top {
        fmt.Printf("#%d  player=%d  score=%d\n", e.Rank+1, e.PlayerID, e.Score)
    }
}
```

A runnable version of this lives in [`examples/quickstart/`](examples/quickstart/).

## Services

| Service | Methods |
|---|---|
| `Client.Auth` | `Signup`, `Verify`, `ResendVerification`, `Refresh`, `Logout`, `LinkEmail`, `LinkSteam`, `ChangePassword`, `RequestPasswordReset`, `ConfirmPasswordReset`, `Disable` |
| `Client.Config` | `Get` (`ETag` / `If-None-Match`, no player login required) |
| `Client.Storage` | `Get`, `Put`, `Delete`, `List`, `All` (metadata-only, cursor-paginated, OCC via `IfMatch`) |
| `Client.Leaderboards` | `List`, `Submit` (when enabled), `Top`, `AroundMe`, `Friends`, `Periods`, `AllPeriods`, `PeriodTop` |
| `Client.Profile` | `Get`, `Update`, `RegenerateFriendCode` |
| `Client.Players` | `Get`, `Resolve`, `ResolveFriendCode` |
| `Client.Friends` | `List`, `All`, `Request`, `Accept`, `Reject`, `Remove`, `Block`, `Unblock`, `RemoteAddrs` |
| `Client.GameSessions` | `Create`, `List`, `All`, `Get`, `Resolve`, `Join`, `Heartbeat`, `Leave` |
| `Client.Invites` | `Create`, `List`, `Delete` |
| `Client.Presence` | `Set` |
| `Client.Account` | `RemoteAddrs`, `SetRemoteAddrs` |
| `Client.Matchmaker` | `CreateTicket`, `GetTicket`, `CancelTicket`, `WaitForMatch`, `ConnectP2P` |
| `Client.Fleets` | `SendHeartbeat`, `ListServers` |
| `Client.Relay` | `GetCredentials` |
| `Client.Server` | `VerifySession`, `SubmitScore`, `PlayerRemoteAddrs`, `StorageGet`, `StoragePut`, `StorageList`, `StorageAll` (server-tier, secret API key) |
| `Client.Health` | `Get` |

Game-session lifetime: a session lives in a one-hour sliding window — member
`Heartbeat` calls extend it while the match runs, and an idle session expires
within the hour. When the match ends, the host should call `Leave` (DELETE)
so the session stops counting against the project's open-session limit
immediately.

Five `Authenticator` strategies for `Client.Login`:

- `NewEmailPasswordAuth(...)` — standard email + password
- `NewCustomTokenAuth(...)` — tenant-signed HS256 JWT
- `NewSteamAuth(...)` — Steamworks session ticket
- `NewAnonymousAuth(...)` — anonymous player with an on-disk persisted session
- `NewOfflineAuth()` — synthetic local session for LAN games and self-hosted installs without a central directory

The `Client` is safe for concurrent use. Sessions auto-refresh: a proactive refresh fires when a session is within 30 s of expiry, and a 401 response triggers exactly one reactive refresh + retry.

`DialRealtime` uses the same proxy, TLS, authentication, request-ID, and
redacted logging configuration as REST calls. Incoming messages are capped at
1 MiB by default. Reconnect is off by default because events emitted during an
outage cannot be replayed. Opt in with `ReconnectPolicy{Enabled: true}` and use
`Options.OnRealtimeReconnect` to re-read authoritative matchmaking, invite,
friend, and presence state after recovery. Hooks run asynchronously so a slow
or re-entrant hook cannot stop the receive loop. Keep exactly one goroutine
calling `ReadMessage` for the connection's lifetime so control frames and
server events are continuously processed.

## Errors

Every non-success HTTP response returns a `*ggscale.Error` carrying RFC 9457
`Type`, `Title`, `Detail`, `Instance`, validation `Details`, `Status`,
`RequestID`, and (when relevant) `RetryAfter`. Transport and decode failures
return `*ggscale.RequestError` with a machine-readable `Kind`. Match common
HTTP cases with `errors.Is`:

```go
_, err := c.Storage.Put(ctx, "k", v, ggscale.IfMatch(2))
switch {
case errors.Is(err, ggscale.ErrConflict):
    // version mismatch — re-read and retry
case errors.Is(err, ggscale.ErrRateLimited):
    var sdkErr *ggscale.Error
    errors.As(err, &sdkErr)
    time.Sleep(sdkErr.RetryAfter)
}
```

Sentinels: `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`, `ErrConflict`, `ErrRateLimited`, `ErrBadRequest`, `ErrValidation`.

Field validation failures come back as `ErrValidation` (HTTP 422); the offending fields are in `err.(*ggscale.Error).Details`, each naming a `Location` (e.g. `body.status`) and `Message`.

## Pluggable transport

The `Transport` interface is one method:

```go
type Transport interface {
    Call(ctx context.Context, req *Request, out any) error
}
```

The default `StdNetTransport` is JSON over HTTP with three-attempt GET/HEAD
retries, full-jitter backoff, a 30-second fallback call timeout, a 64 MiB body
limit, request IDs, and optional redacted structured logging. A caller-supplied
deadline is preserved unless `Options.CallTimeout` explicitly sets a tighter
budget. Mutating requests are never retried unless the request explicitly opts
into safe replay. Configure these bounds through `Options`; inject a custom
`Transport` for tests or a completely custom runtime.

## Persisting a session

```go
sess := c.Session()    // capture
// ... persist sess somewhere ...
c.SetSession(sess)     // restore on a new Client
```

Useful for game clients that reconnect across process restarts.

Store access and refresh tokens with platform-secure storage. For anonymous
sessions, prefer `NewAnonymousAuthWithStore` with a keychain/credential-vault
backed `SessionStore`; never put tokens in URLs or logs.

## Development

```sh
make check             # lint + vet + test
make test              # go test -race ./...
make test-integration  # full-stack tests against a real server (Docker)
make openapi-check     # fail if the v0.9.4 operation manifest drifted
make lint              # golangci-lint
make quickstart        # GGSCALE_API_KEY=... make quickstart
```

For all future SDK contract updates, use the gg-scale repository's
[OpenAPI specification](https://github.com/automoto/gg-scale/blob/main/openapi.yaml)
as the source of truth. Refresh this repository's pinned `openapi.yaml` snapshot
from that remote specification before regenerating or checking operation
coverage.

Unit tests use `httptest.NewServer` and a fake `Transport`; they do not require
a running ggscale server.

### Integration tests

`make test-integration` brings up a minimal stack with docker compose —
Postgres plus `buildwrangler/ggscale:v0.9.4` pulled from Docker Hub (the
server applies its own migrations at startup) — seeds a tenant, project,
and API keys directly via `integration/seed.sql`, runs the
`-tags=integration` tests in `integration_test.go` against it on
`127.0.0.1:18080`, and tears everything down. Set `KEEP_STACK=1` to leave
the stack running for debugging.

## License

Apache 2.0. See [LICENSE](LICENSE).
