// Quickstart is a ~50-line tour of the ggscale Go SDK: signup, login,
// score submit, top-N read. Run against a local `make up` stack:
//
//	export GGSCALE_API_KEY=<key from the dashboard>
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

	ggscale "github.com/ggscale/ggscale-go"
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

	const leaderboardID int64 = 1
	if err := c.Leaderboards.Submit(ctx, leaderboardID, 1500); err != nil {
		log.Fatalf("submit: %v", err)
	}

	top, err := c.Leaderboards.Top(ctx, leaderboardID, 5)
	if err != nil {
		log.Fatalf("top: %v", err)
	}
	for _, e := range top {
		fmt.Printf("#%d  user=%d  score=%d\n", e.Rank+1, e.EndUserID, e.Score)
	}
}
