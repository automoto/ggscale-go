# Code Review Fixes — Matchmaker & Transport

Summary of fixes applied in response to the `/code-review` findings on the
working-tree diff (`git diff HEAD`). All changes verified with
`go build ./...`, `go vet ./...`, and `go test -race ./...` (green).

## Fixed

### 1. `ErrTicketActive` sentinel now matches the machine-readable code
**Findings #1 & #6 · `transport.go`, `transport_stdnet.go`**

`(*Error).Is` matched `ErrTicketActive` on `e.Message == "ticket_already_active"`.
For a real problem-details 409, the stable identifier lands in the `code`
extension while `detail` (mapped to `e.Message`) holds the human sentence — so
`errors.Is(err, ErrTicketActive)` returned `false` and callers never entered
their cancel-and-retry path.

- `Is()` now matches on `e.Code == "ticket_already_active"`, with a fallback to
  `e.Message` for the legacy envelope.
- Corrected the misleading `parseError` comment: `code` is the machine-readable
  identifier; `detail` / legacy `message` is the human explanation. `e.Message`
  correctly retains the human text.

### 2. WebSocket push no longer degrades the match result
**Findings #2 & #4 · `matchmaker.go`**

`WaitForMatch` returned the WS-parsed payload directly. A lightweight
`matchmaker_matched` push (e.g. only `{address, ticket_id}`) produced a
`MatchResult` with empty `Mode` / `HostPlayerID` / `Users`, silently degrading
`ConnectP2P` (wrong `IsHost`, skipped session join, unscoped relay credentials).
It also duplicated the field mapping across two paths that could drift.

- The push is now treated as a **settle signal**: it triggers an authoritative
  `GetTicket`, and the full result is derived via `resultFromTicket` (the single
  primary mapping path).
- The parsed push is used only as a fallback when the read can't settle
  (transient error or not-yet-visible write); the next poll then recovers.
- This keeps the legacy WS tests passing and reduces mapping drift.

### 3. `expired` treated as a terminal ticket status
**Finding #3 · `matchmaker.go`**

`resultFromTicket` only treated `matched` / `failed` / `cancelled` as terminal.
If a ticket settled into `expired` as a status, every poll returned
`done=false`, so `WaitForMatch` blocked until the context deadline before
best-effort-cancelling an already-terminal ticket.

- `expired` is now terminal, mapping to `*MatchFailedError` (reason from
  `failure_reason`, defaulting to `"expired"`).
- Scoped narrowly to `expired` — genuinely non-terminal states (`queued`, …)
  still poll rather than being force-settled.

### 4. Regression test added
**`realtime_test.go`**

`TestMatchmakerService_WaitForMatch_push_triggers_authoritative_read`: a
lightweight `{ticket_id}`-only push must resolve to the complete `game_session`
result (mode, host, roster) via the authoritative ticket read.

## Deliberately not changed

### 5. `RequestMatch` return-type break / removed `MatchReady` & `ErrNotConnected`
**Finding #5**

Flagged as a hard public-API break. Left as-is because:

- This is a pre-1.0 module (`0.3.0`); the entire matchmaker was rewritten in the
  `init v1` commit — an intentional breaking redesign, acceptable under semver v0.
- `MatchReady` and `ErrNotConnected` have **zero** in-repo references.
- Resurrecting them as dead compatibility shims adds clutter for no in-tree
  benefit.

If source compatibility for external consumers is desired, a follow-up could add
a `MatchReady = MatchResult` type alias and re-export the sentinel.

## Verification

```
go build ./...        # ok
go vet ./...          # ok
go test -race ./...   # ok (github.com/automoto/ggscale-go)
```
