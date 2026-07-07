# ggscale-go

Official Go client for the [ggscale](https://github.com/automoto/gg-scale) API. Covers the v1 surface used by game code: player authentication, per-player JSON storage, leaderboards, profiles, friends, presence, player-hosted game sessions with invites, matchmaking, real-time events, and server-tier session verification.

The SDK has zero third-party runtime dependencies — just the standard library.

## Install

```sh
go get github.com/automoto/ggscale-go
```

Requires Go 1.24 or later.

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

    if err := c.Leaderboards.Submit(ctx, 1, 1500); err != nil {
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
| `Client.Auth` | `Signup`, `Verify`, `Refresh`, `Logout` |
| `Client.Storage` | `Get`, `Put`, `Delete`, `List` (cursor-paginated, OCC via `IfMatch`) |
| `Client.Leaderboards` | `Submit`, `SubmitFor`, `Top`, `AroundMe` |
| `Client.Profile` | `Get`, `Update` |
| `Client.Friends` | `List`, `Request`, `Accept`, `Reject`, `Remove`, `Block`, `Unblock`, `RemoteAddrs` |
| `Client.GameSessions` | `Create`, `Get`, `Resolve`, `Join`, `Heartbeat`, `Leave` |
| `Client.Invites` | `Create`, `List`, `Delete` |
| `Client.Presence` | `Set` |
| `Client.Account` | `RemoteAddrs`, `SetRemoteAddrs` |
| `Client.Matchmaker` | `CreateTicket`, `GetTicket`, `CancelTicket`, `RequestMatch` |
| `Client.Fleets` | `SendHeartbeat`, `ListServers` |
| `Client.Relay` | `GetCredentials` |
| `Client.Server` | `VerifySession`, `PlayerRemoteAddrs` (server-tier, secret API key) |

Four `Authenticator` strategies for `Client.Login`:

- `NewEmailPasswordAuth(...)` — standard email + password
- `NewCustomTokenAuth(...)` — tenant-signed HS256 JWT
- `NewAnonymousAuth(...)` — anonymous player with an on-disk persisted session
- `NewOfflineAuth()` — synthetic local session for LAN games and self-hosted installs without a central directory

The `Client` is safe for concurrent use. Sessions auto-refresh: a proactive refresh fires when a session is within 30 s of expiry, and a 401 response triggers exactly one reactive refresh + retry.

## Errors

Every non-2xx response returns a `*ggscale.Error` carrying `Status`, server `Code`, `Message`, and (when relevant) `RetryAfter`. Match common cases with `errors.Is`:

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

Sentinels: `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`, `ErrConflict`, `ErrRateLimited`, `ErrBadRequest`.

## Pluggable transport

The `Transport` interface is one method:

```go
type Transport interface {
    Call(ctx context.Context, req *Request, out any) error
}
```

The default `StdNetTransport` is JSON over HTTP. Wrap it for logging, metrics, or retries by implementing `Transport` around an inner one — no SDK changes needed. See [`docs/proposals/sdk-transport.md`](docs/proposals/sdk-transport.md) for the full design rationale.

## Persisting a session

```go
sess := c.Session()    // capture
// ... persist sess somewhere ...
c.SetSession(sess)     // restore on a new Client
```

Useful for game clients that reconnect across process restarts.

## Development

```sh
make check             # lint + vet + test
make test              # go test -race ./...
make test-integration  # full-stack tests against a real server (Docker)
make lint              # golangci-lint
make quickstart        # GGSCALE_API_KEY=... make quickstart
```

Unit tests use `httptest.NewServer` and a fake `Transport`; they do not require a running ggscale server.

### Integration tests

`make test-integration` brings up a minimal stack with docker compose —
Postgres plus `buildwrangler/ggscale:latest` pulled from Docker Hub (the
server applies its own migrations at startup) — seeds a tenant, project,
and API keys directly via `integration/seed.sql`, runs the
`-tags=integration` tests in `integration_test.go` against it on
`127.0.0.1:18080`, and tears everything down. Set `KEEP_STACK=1` to leave
the stack running for debugging.

## License

Apache 2.0. See [LICENSE](LICENSE).
