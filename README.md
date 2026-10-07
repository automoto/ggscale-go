# ggscale-go

Official Go client for the [ggscale](https://github.com/automoto/gg-scale) API. Covers the v1 surface used by game code: player authentication, per-player JSON storage, leaderboards, profiles, friends, presence, player-hosted game sessions with invites, matchmaking, real-time events, and server-tier session verification.

The SDK's only runtime dependency is [`github.com/coder/websocket`](https://github.com/coder/websocket), used by the realtime client. Everything else is the Go standard library.

## Install

```sh
go get github.com/automoto/ggscale-go
```

Requires Go 1.26.6 or later.

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
| `Client.Auth` | `Signup`, `Verify`, `ResendVerification`, `Refresh`, `Logout`, `LinkEmail`, `LinkSteam`, `ChangePassword`, `RequestPasswordReset`, `ConfirmPasswordReset`, `Disable`, `RequestDelete`, `CancelDelete` |
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
| `Client.Parties` | `Create`, `Current`, `Get`, `Update`, `Disband`, `Heartbeat`, `JoinByCode`, `Leave`, `SetReady`, `Kick`, `CreateCode`, `RevokeCode`, `InviteFriend`, `ListInvites`, `AcceptInvite`, `DeclineInvite`, `Queue`, `CancelQueue`, `Rematch`, `Watch`, `WaitForMatch` |
| `Client.Fleets` | `ListServers` (`SendHeartbeat` is deprecated; use `Server.FleetHeartbeat`) |
| `Client.Relay` | `GetCredentials` |
| `Client.Realtime` | `CreateTicket` (one-time WebSocket ticket; browser builds use it for you) |
| `Client.Server` | `VerifySession`, `FleetHeartbeat`, `SubmitScore`, `PlayerRemoteAddrs`, `StorageGet`, `StoragePut`, `StorageList`, `StorageAll` (server-tier, secret API key) |
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
1 MiB by default. After an abnormal close, the client reconnects with capped
full-jitter backoff (five attempts by default). The server does not replay
events sent during an outage, so use `Options.OnRealtimeReconnect` to re-read
authoritative matchmaking, invite, friend, and presence state after recovery.
Set `ReconnectPolicy{Disabled: true}` to turn reconnect off. Hooks run asynchronously so a slow
or re-entrant hook cannot stop the receive loop. Keep exactly one goroutine
calling `ReadMessage` for the connection's lifetime so control frames and
server events are continuously processed.

The server keeps one realtime socket per player: a new dial closes the
player's older socket. `DialRealtime`, `Matchmaker.WaitForMatch`,
`Parties.Watch` and `Parties.WaitForMatch` each open a socket, so use one of
them at a time.

## Which key?

A game ships the **publishable key**. Use the **secret key** only on a game
server or backend, never in a game build. Operations on `Client.Server` need
the secret key, and a publishable key gets 403 there. Every other service
takes the publishable key; a secret key also works, but it must never ship in
a game.

## Realtime events

Every message has a `Type` and a JSON `Payload`. Decode the payload with
`Message.DecodePayload`:

| Type | Payload type |
|---|---|
| `EventMatchmakerMatched` | read by `Matchmaker.WaitForMatch` |
| `EventPresence` | `PresenceEvent` |
| `EventGameInvite` | `GameInviteEvent` |
| `EventPartyChanged` | `PartyChangedEvent` |
| `EventPartyInvite` | `PartyInviteEvent` |

Events are best effort. A client that misses one recovers the state with the
matching GET.

Browser builds (`GOOS=js GOARCH=wasm`) cannot set WebSocket headers. There,
`DialRealtime` gets a one-time ticket with `Realtime.CreateTicket` for each
dial, reconnects included, and opens `/v1/ws?ticket=...`. Add the page origin
of your game to the Game Project's allowed origins (in the dashboard, or with
the MCP `set_allowed_origins` tool), or the server refuses the WebSocket.

## Parties

A party queues as one unit. Most writes take the party version you last saw;
a stale version returns `ErrStaleVersion`, so read the party again and retry.
Only the leader can update, disband, kick, invite, create or revoke codes,
queue, cancel the queue and rematch.

```go
party, _ := leader.Parties.Create(ctx, ggscale.MatchRequest{Mode: ggscale.ModeMatchOnly, MinCount: 2, MaxCount: 4})
code, _ := leader.Parties.CreateCode(ctx, party.ID, party.Version, 0)
// Share code.Code; a friend calls member.Parties.JoinByCode(ctx, code.Code).

// Each member: watch the party (this also sends the required heartbeat).
go member.Parties.Watch(ctx, party.ID, func(ev ggscale.PartyEvent) error {
    switch {
    case ev.Party != nil:
        // newer party state: members, readiness, state
    case ev.Match != nil:
        // the party matched; ev.Match is the same result as WaitForMatch
    case ev.Removed:
        // kicked, left, disbanded, or swept
    }
    return nil
})
```

- Each member must heartbeat within 30 seconds or the server removes the
  member. `Watch` heartbeats every 10 seconds. With party ID 0 it reports
  party invites only.
- `Queue` and `Rematch` send an `Idempotency-Key`. Pass your own key to retry
  a call safely, or `""` to let the SDK make one. They return
  `ErrPartyEnqueueDisabled` when the server turns party queue off.
- `JoinByCode` returns `ErrCodeCooldown` after too many wrong codes. Wait for
  `(*ggscale.Error).RetryAfter`; the server sets the cooldown.
- `ListInvites` is the source of truth for invites. A re-invite of a pending
  invite sends no new `party_invite` event.
- `PartyMember.Attributes` come back exactly as the member sent them, HTML
  included. Escape them before you show them.

## Defaults

- **HTTP retries:** up to three attempts in total, with capped full-jitter
  exponential backoff, inside one call budget (`CallTimeout`, 30 seconds when
  the caller sets no deadline). Only requests that are safe to repeat are
  retried: `GET` and `HEAD`, and writes with an `Idempotency-Key`
  (`Parties.Queue`, `Parties.Rematch`) or `Request.ReplaySafe`. Only
  connection failures and 408, 429, 502, 503 and 504 are retried, and a
  `Retry-After` from the server is followed. Any request, writes included, is
  also retried when it was never sent: a DNS failure, or a connection that
  could not be opened. Other writes are not retried after a failure, because
  a lost response does not show whether the write ran. Configure with
  `Options.RetryPolicy`.
- **Realtime reconnect:** on. See above; turn it off with
  `ReconnectPolicy{Disabled: true}`.

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

Sentinels: `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`, `ErrConflict`, `ErrRateLimited`, `ErrBadRequest`, `ErrValidation`, `ErrTicketActive`, `ErrStaleVersion`, `ErrPartyEnqueueDisabled`, `ErrCodeCooldown`, `ErrDeleteRequestedByTeam`.

`Auth.CancelDelete` returns `ErrDeleteRequestedByTeam` (it also matches `ErrForbidden`) when the game's team requested the deletion. Only the team can cancel it.

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
make check             # lint + vet (native and browser) + test
make test              # go test -race ./...
make test-integration  # full-stack tests against a real server (Docker)
make openapi-check     # every operation in the server spec has a wrapper
make lint              # golangci-lint
make quickstart        # GGSCALE_API_KEY=... make quickstart
```

### API contract

The [gg-scale repository](https://github.com/automoto/gg-scale/blob/main/openapi.yaml)
owns `openapi.yaml`, the only contract for this SDK. There is no copy of it
here. `make openapi-check` downloads the spec of the server tag `SPEC_REF`
(now `v0.9.71`) and runs `TestOpenAPIOperationCoverage`. The test checks that
each operation ID has an SDK wrapper, that each secret-key operation is on
`Client.Server`, and that no other operation is only there. CI runs it.

Use a local spec with `make openapi-check SPEC=../ggscale/openapi.yaml`. A sync
with a new server release changes only `SPEC_REF`. Without `GGSCALE_SPEC`,
`make test` skips the coverage test, so unit tests stay offline.

Unit tests use `httptest.NewServer` and a fake `Transport`; they do not require
a running ggscale server.

### Integration tests

`make test-integration` brings up a minimal stack with docker compose —
Postgres plus `ghcr.io/automoto/gg-scale:v0.9.71` pulled from GHCR (the
server applies its own migrations at startup) — seeds a tenant, project,
and API keys directly via `integration/seed.sql`, runs the
`-tags=integration` tests against it on
`127.0.0.1:18080`, and tears everything down. Set `KEEP_STACK=1` to leave
the stack running for debugging.

## License

Apache 2.0. See [LICENSE](LICENSE).
