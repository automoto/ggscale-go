# Score submitter

A runnable reference for **authoritative leaderboard submission in a
peer-to-peer game**. See [`docs/leaderboards-p2p.md`](../../docs/leaderboards-p2p.md)
for the why.

Leaderboard writes are server-authoritative: ggscale requires a **secret** API
key to submit a score, so a player client cannot submit directly (and must
never hold the secret key). This small service holds the secret key on trusted
compute and submits on a player's behalf after verifying their session.

```
game client ──POST /submit (its own session token)──▶ score-submitter
score-submitter ──VerifySession──▶ ggscale       (who is this player?)
score-submitter ──your validation──▶              (is the score sane?)
score-submitter ──SubmitScore──▶ ggscale          (authoritative write)
```

The player can only ever write **their own** score — ggscale derives the player
from the verified token — and only scores that pass your validation. Deploy one
instance anywhere with outbound HTTPS (Cloud Run, Lambda, Fly, a small VPS).

## Run it

```sh
export GGSCALE_BASE_URL=https://api.ggscale.example
export GGSCALE_SECRET_KEY=ggs_...        # secret key — SERVER-SIDE ONLY
go run ./examples/score-submitter        # listens on :8090 (override LISTEN_ADDR)
```

## Call it from the game client

The game client sends **its own** player session token — never the API key — to
your submitter at match end:

```go
// In the game client (built with your PUBLISHABLE key).
func reportScore(ctx context.Context, c *ggscale.Client, submitterURL string, leaderboardID, score int64) error {
    body, _ := json.Marshal(map[string]int64{
        "leaderboard_id": leaderboardID,
        "score":          score,
    })
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, submitterURL+"/submit", bytes.NewReader(body))
    if err != nil {
        return err
    }
    // The player's session token authenticates them to YOUR submitter.
    req.Header.Set("Authorization", "Bearer "+c.Session().AccessToken)
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusNoContent {
        return fmt.Errorf("submit rejected: %s", resp.Status)
    }
    return nil
}
```

## Notes

- **Replace the validation.** `maxPlausibleScore` in `main.go` is a placeholder.
  Put your real anti-cheat here (bounds, rate limits, replay checks) — it runs
  on trusted compute, which is the entire point.
- **Reads stay client-side.** `Leaderboards.Top` / `Leaderboards.AroundMe` go
  directly from the game client with the publishable key; only writes route
  through this service.
- **Protect this endpoint** as you would any backend: the session-token check
  authenticates the player, but add your own rate limiting and TLS in front.
