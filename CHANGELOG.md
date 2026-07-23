# Changelog

All notable changes to `ggscale-go` are documented here. The project is
pre-1.0; minor versions may contain breaking changes until v1.0.0.

## [0.4.0] — unreleased

Synchronizes the SDK with ggscale server **v0.9.0** and prepares for the
peer-to-peer GA. See `docs/ga-readiness.md` for the full checklist.

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

  `Leaderboards.Top` and `Leaderboards.AroundMe` (reads) are unchanged. See
  `docs/leaderboards-p2p.md` for the verify-then-submit pattern in P2P games.

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
