// Quickstart is a short tour of the ggscale Go SDK: signup, login,
// per-player storage, and a leaderboard read. Run against a local
// `make up` stack:
//
//	export GGSCALE_API_KEY=<key from the control panel>
//	cd sdk-go && go run ./examples/quickstart
//
// See ../../docs/SMOKE_TESTS.md §3 for the manual email-verification
// step between signup and login on a fresh stack.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	ggscale "github.com/automoto/ggscale-go"
)

func main() {
	apiKey := os.Getenv("GGSCALE_API_KEY")
	if apiKey == "" {
		log.Fatal("GGSCALE_API_KEY is required")
	}

	ctx := context.Background()
	c, err := ggscale.NewClient(ggscale.Options{
		BaseURL: "http://localhost:8080",
		APIKey:  apiKey,
	})
	if err != nil {
		log.Fatal(err)
	}

	const (
		email    = "demo@example.com"
		password = "hunter2hunter2"
	)

	if err := c.Auth.Signup(ctx, email, password); err != nil && !errors.Is(err, ggscale.ErrConflict) {
		log.Printf("signup: %v (continuing — likely already exists)", err)
	}

	if err := c.Login(ctx, ggscale.NewEmailPasswordAuth(c.Transport(), apiKey, email, password)); err != nil {
		log.Fatalf("login: %v", err)
	}

	// Per-player storage: write a value and read it back.
	if _, err := c.Storage.Put(ctx, "settings", map[string]any{"theme": "dark"}); err != nil {
		log.Fatalf("storage put: %v", err)
	}
	settings, err := c.Storage.Get(ctx, "settings")
	if err != nil {
		log.Fatalf("storage get: %v", err)
	}
	fmt.Printf("settings: %s\n", settings.Value)

	// Read a leaderboard. Competitive submission should use the server tier
	// (Client.Server.SubmitScore with a secret key). Leaderboards.Submit is
	// for boards explicitly configured for untrusted client writes.
	const leaderboardID int64 = 1
	top, err := c.Leaderboards.Top(ctx, leaderboardID, 5)
	if err != nil {
		log.Fatalf("top: %v", err)
	}
	for _, e := range top {
		fmt.Printf("#%d  player=%d  score=%d\n", e.Rank+1, e.PlayerID, e.Score)
	}
}
