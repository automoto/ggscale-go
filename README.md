# ggscale-go

Official Go client for the [ggscale](https://github.com/automoto/gg-scale) API. Covers the v1 surface used by game code: end-user authentication, per-user JSON storage, leaderboards, and profile management.

The SDK has zero third-party runtime dependencies — just the standard library.

> **Status:** v0.1, alpha. The public surface is frozen by [`docs/proposals/sdk-transport.md`](docs/proposals/sdk-transport.md). Breaking changes between v0.x tags are possible until v1.0.

## Install

```sh
go get github.com/ggscale/ggscale-go
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

    ggscale "github.com/ggscale/ggscale-go"
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
        fmt.Printf("#%d  user=%d  score=%d\n", e.Rank+1, e.EndUserID, e.Score)
    }
}
```

A runnable version of this lives in [`examples/quickstart/`](examples/quickstart/).

## What's in v0.1

| Service | Methods |
|---|---|
| `Client.Auth` | `Signup`, `Verify`, `Refresh`, `Logout` |
| `Client.Storage` | `Get`, `Put`, `Delete`, `List` (cursor-paginated, OCC via `IfMatch`) |
| `Client.Leaderboards` | `Submit`, `Top`, `AroundMe` |
| `Client.Profile` | `Get`, `Update` |

Three `Authenticator` strategies for `Client.Login`:

- `NewEmailPasswordAuth(...)` — standard email + password
- `NewCustomTokenAuth(...)` — tenant-signed HS256 JWT
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
make check       # lint + vet + test
make test        # go test -race ./...
make lint        # golangci-lint
make quickstart  # GGSCALE_API_KEY=... make quickstart
```

Tests use `httptest.NewServer` and a fake `Transport`; they do not require a running ggscale server. The `quickstart` target does.

## License

Apache 2.0. See [LICENSE](LICENSE).
