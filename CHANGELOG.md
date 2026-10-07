# Changelog

All notable changes to `ggscale-go` are documented here. The project is
pre-1.0; minor versions may contain breaking changes until v1.0.0.

## [Unreleased]

### Changed

- A write is now retried when the request was never sent: a DNS failure, or
  a connection that could not be opened. Such a failure proves the server did
  not run the write. A write is still not retried after a failure on an open
  connection, or after an HTTP response, unless it has an `Idempotency-Key`
  or sets `ReplaySafe`.

## [0.7.0]

Synchronizes the SDK with ggscale server **v0.9.71**: player data deletion,
parties, realtime events, and WebSocket tickets for browser builds. Version
0.6.0 was never released; its changes are part of this release.

### Breaking

- Realtime reconnect is on by default, with capped full-jitter backoff.
  `ReconnectPolicy.Enabled` is replaced by `ReconnectPolicy.Disabled`: remove
  `Enabled: true`, and set `Disabled: true` to keep reconnect off. Use
  `OnRealtimeReconnect` to read the state again after a reconnect.
- A write with an `Idempotency-Key` header is retried like a read
  (`Parties.Queue`, `Parties.Rematch`). A 503 `party_enqueue_disabled` is
  never retried, because it is a server setting.
- `Fleets.SendHeartbeat` is deprecated; use `Server.FleetHeartbeat`. The
  heartbeat needs a secret key, so it is on the server client now. The old
  method still works and calls the new one.
- The browser build (`GOOS=js`) now has a working `DialRealtime` (see Added).
  Before, it always returned an error.

### Added

- `Client.Parties` with all 19 party operations: `Create`, `Current`, `Get`,
  `Update`, `Disband`, `Heartbeat`, `JoinByCode`, `Leave`, `SetReady`, `Kick`,
  `CreateCode`, `RevokeCode`, `InviteFriend`, `ListInvites`, `AcceptInvite`,
  `DeclineInvite`, `Queue`, `CancelQueue`, `Rematch`. Writes take the party
  version. `Queue` and `Rematch` send an `Idempotency-Key` (given, or made by
  the SDK).
- `Parties.Watch` reports newer party versions, party invites and the
  party's match on one realtime socket, and heartbeats every 10 seconds.
  An invites-only watch (party ID 0) returns the dial error when the socket
  cannot open; a party watch logs it and continues on the heartbeat.
  `Parties.WaitForMatch` returns the party's match, or `*MatchFailedError`,
  `ErrMatchCancelled` or `ErrNotPartyMember`.
- Error sentinels `ErrStaleVersion` (409), `ErrPartyEnqueueDisabled` (503),
  `ErrCodeCooldown` (429; read `RetryAfter`) and `ErrDeleteRequestedByTeam`.
- Realtime event constants and payload types: `EventPresence`
  (`PresenceEvent`), `EventGameInvite` (`GameInviteEvent`),
  `EventPartyChanged` (`PartyChangedEvent`), `EventPartyInvite`
  (`PartyInviteEvent`), and `Message.DecodePayload`.
- `Realtime.CreateTicket` (`POST /v1/ws/ticket`). The browser build of
  `DialRealtime` gets a new ticket for each dial, reconnects included, and
  sends no headers. The game's page origin must be in the Game Project's
  allowed origins.
- `Ticket.EntryID`, `Ticket.PartyID`, `RosterEntry.QueueEntryID` and
  `RosterEntry.PartyID`.
- `Request.Header` for extra request headers.
- `Auth.RequestDelete` schedules permanent deletion of the calling player's
  data in the current project (`POST /v1/auth/delete`) and returns the request
  and purge timestamps as `PendingDelete`. The server revokes every session,
  so the local session is cleared on success.
- `Auth.CancelDelete` clears a pending deletion with email and password
  (`POST /v1/auth/delete/cancel`). It sends no session token. The server
  answers 404 for an unknown email, a wrong password and no pending deletion
  alike. When the game's team requested the deletion (403 with detail
  `delete_requested_by_team`), the error matches
  `ErrDeleteRequestedByTeam` and `ErrForbidden`. A 403 for a revoked key or a
  disabled tenant matches only `ErrForbidden`.

### Changed

- `go.mod` requires Go 1.26.6, the same as the gg-scale server.
- `openapi.yaml` in the gg-scale repository is the only contract. The vendored
  spec, the operation manifest and `internal/cmd/openapi-operations` are
  removed. `TestOpenAPIOperationCoverage` reads the spec from `GGSCALE_SPEC`;
  `make openapi-check` downloads it for `SPEC_REF` (`v0.9.71`), and CI runs
  it. The test also checks that secret-key operations are on `Client.Server`.
  `make openapi-generate` is removed.
- `make check` and CI also vet the browser build. CI reads the Go version
  from `go.mod`.
- Integration tests default to `ghcr.io/automoto/gg-scale:v0.9.71`; override
  with `GGSCALE_IMAGE`. New tests cover the party flow, a version conflict,
  kick, leave, disband, the party-code cooldown, and one-time WebSocket
  tickets.
- `github.com/coder/websocket` v1.8.12 → v1.8.15, matching the server.
- README: "Which key?", "Realtime events", "Parties" and "Defaults"
  sections.

## [0.5.0]

Synchronizes the SDK with ggscale server **v0.9.4** and expands the default
HTTP runtime with bounded responses, RFC 9457 errors, request IDs, safe
retries, conditional remote-config requests, and redacted structured logging.

### Breaking

- `Server.SubmitScore` now matches the v0.9.4 server-tier contract: it accepts
  a player ID and posts to `/v1/server/leaderboards/{id}/scores`. Backends that
  start with a player token should call `Server.VerifySession` first.
- `ReconnectPolicy.Disabled` has been replaced by `ReconnectPolicy.Enabled`,
  and reconnect is now off by default. Remove `Disabled: true` to keep
  reconnect disabled; callers that previously relied on the default or set
  `Disabled: false` must now set `Enabled: true`.

### Added

- Remote config with `ETag` / `If-None-Match`, Steam authentication and account
  linking, account lifecycle methods, player and friend-code lookup, public
  game-session browsing, expanded leaderboard reads/submissions, and
  server-tier player storage.
- Configurable HTTP client, overall call timeout, response-size cap, full-
  jitter safe retries, structured logging, and typed non-HTTP failure classes.
- Bounded WebSocket handshakes and message sizes, opt-in abnormal-close
  recovery, stable request IDs, and an asynchronous post-reconnect
  authoritative-resync hook.
- Mutating HTTP retries now require explicit replay-safety opt-in; caller
  deadlines are preserved by default, oversized `Retry-After` delays return
  immediately, and the compatibility-oriented default body cap is 64 MiB.
- Anonymous logout/disable clears persisted sessions, and terminal realtime
  reads return `ErrConnectionClosed` directly.
- `P2PMatch.RelayError` preserves best-effort relay credential failures so
  relay-dependent games can distinguish a direct-only result from success.
- A pinned OpenAPI 3.1 snapshot plus generated 70-operation coverage manifest
  and drift check (`make openapi-check`).
- Integration tests now target `buildwrangler/ggscale:v0.9.4` by default.

## [0.4.0] — unreleased

Synchronizes the SDK with ggscale server **v0.9.0** and prepares for the
peer-to-peer GA.

### Breaking

- **Storage `List` is now metadata-only.** `ObjectPage.Items` is now
  `[]StorageObjectMetadata` (`Key`, `Version`, `UpdatedAt`, `SizeBytes`)
  instead of `[]Object`. The server no longer returns object values in list
  responses. Call `Storage.Get(key)` to read a value.

  ```go
  // before
  page, _ := c.Storage.List(ctx, ggscale.ListOptions{})
  for _, obj := range page.Items {
      use(obj.Value) // was always populated
  }

  // after
  page, _ := c.Storage.List(ctx, ggscale.ListOptions{})
  for _, meta := range page.Items {
      full, _ := c.Storage.Get(ctx, meta.Key) // fetch value on demand
      use(full.Value)
  }
  ```

- **Leaderboard submission moved to the server tier.** Removed
  `Leaderboards.Submit` and `Leaderboards.SubmitFor`. Score writes are
  server-authoritative and require a secret API key, so submission now lives on
  `Client.Server`:

  ```go
  // before (would 403 from a publishable-key client)
  c.Leaderboards.Submit(ctx, leaderboardID, score)
  c.Leaderboards.SubmitFor(ctx, playerToken, leaderboardID, score)

  // after — from a trusted holder of the secret key
  c.Server.SubmitScore(ctx, playerToken, leaderboardID, score)
  ```

  `Leaderboards.Top` and `Leaderboards.AroundMe` (reads) are unchanged.

### Added

- `ErrValidation` sentinel for HTTP 422 field-validation failures (Huma's
  response for invalid request bodies). The offending fields are in
  `Error.Details`. Previously these 422s matched no sentinel.

### Changed

- Centralized the SDK version in the exported `Version` constant; the
  `User-Agent` is now `ggscale-go/0.4.0`.
- Integration stack pins `buildwrangler/ggscale:v0.9.0` (was `:latest`);
  override with `GGSCALE_IMAGE`.
- Integration tests now exercise matchmaking and relay in addition to auth,
  storage, leaderboards, presence, game sessions, and server verification.

### Fixed

- README no longer claims zero third-party runtime dependencies; the realtime
  client depends on `github.com/coder/websocket`.
- Corrected a stale `match_ready` reference in the realtime doc comment (the
  server emits `matchmaker_matched`).
