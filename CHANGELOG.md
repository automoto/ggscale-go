# Changelog

All notable changes to `ggscale-go` are documented here. The project is
pre-1.0; minor versions may contain breaking changes until v1.0.0.

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
</content>
