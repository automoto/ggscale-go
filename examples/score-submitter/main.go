// Score-submitter is a minimal trusted backend for authoritative leaderboard
// submission in a peer-to-peer game. It holds the ggscale SECRET API key —
// which must never ship in a game client — and exposes a single endpoint the
// game client calls at match end.
//
// Flow per request:
//
//	game client ──POST /submit (its own player session token)──▶ this service
//	this service ──VerifySession──▶ ggscale        (who is this player?)
//	this service ──(your validation)──▶                          (is the score sane?)
//	this service ──SubmitScore──▶ ggscale                        (authoritative write)
//
// The player can only ever write their own score (ggscale derives the player
// from the verified session token), and only scores that pass the validation
// below. Run one instance; a single ggscale.Client serves all players.
//
//	export GGSCALE_BASE_URL=https://api.ggscale.example
//	export GGSCALE_SECRET_KEY=ggs_...        # secret key — server-side only
//	go run ./examples/score-submitter
package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	ggscale "github.com/automoto/ggscale-go"
)

// maxPlausibleScore is a stand-in for your game's real anti-cheat rule. The
// whole point of an authoritative submitter is that this check runs on trusted
// compute, not on the player's machine — replace it with whatever bounds,
// rate limits, or replay validation your game needs.
const maxPlausibleScore = 1_000_000

type submitRequest struct {
	LeaderboardID int64 `json:"leaderboard_id"`
	Score         int64 `json:"score"`
}

type server struct {
	gg *ggscale.Client
}

func main() {
	baseURL := os.Getenv("GGSCALE_BASE_URL")
	secretKey := os.Getenv("GGSCALE_SECRET_KEY")
	if baseURL == "" || secretKey == "" {
		log.Fatal("GGSCALE_BASE_URL and GGSCALE_SECRET_KEY are required")
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8090"
	}

	// One Client, built with the secret key and no player session. Server-tier
	// calls take the player's token per request, so this is safe to share
	// across all concurrent players.
	gg, err := ggscale.NewClient(ggscale.Options{BaseURL: baseURL, APIKey: secretKey})
	if err != nil {
		log.Fatal(err)
	}

	s := &server{gg: gg}
	http.HandleFunc("POST /submit", s.handleSubmit)

	log.Printf("score-submitter listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func (s *server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	// The game client sends its OWN player session token — the value of
	// ggscale.Client.Session().AccessToken — as a bearer token. This is your
	// service's contract with your client; it is not the ggscale API key.
	playerToken := bearerToken(r)
	if playerToken == "" {
		http.Error(w, "missing player session token", http.StatusUnauthorized)
		return
	}

	var req submitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// 1. Authenticate the player from their session token. This proves the
	//    token is genuine and tells us which player is submitting.
	who, err := s.gg.Server.VerifySession(ctx, playerToken)
	if err != nil {
		if errors.Is(err, ggscale.ErrUnauthorized) {
			http.Error(w, "invalid or expired session", http.StatusUnauthorized)
			return
		}
		log.Printf("verify session: %v", err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}

	// 2. Apply your trusted validation before writing anything.
	if req.Score < 0 || req.Score > maxPlausibleScore {
		log.Printf("rejecting implausible score %d from player %d", req.Score, who.PlayerID)
		http.Error(w, "score out of range", http.StatusBadRequest)
		return
	}

	// 3. Submit authoritatively on the player's behalf.
	if err := s.gg.Server.SubmitScore(ctx, playerToken, req.LeaderboardID, req.Score); err != nil {
		switch {
		case errors.Is(err, ggscale.ErrNotFound):
			http.Error(w, "leaderboard not found", http.StatusNotFound)
		case errors.Is(err, ggscale.ErrValidation), errors.Is(err, ggscale.ErrBadRequest):
			http.Error(w, "invalid submission", http.StatusBadRequest)
		default:
			log.Printf("submit score: %v", err)
			http.Error(w, "upstream error", http.StatusBadGateway)
		}
		return
	}

	log.Printf("accepted score %d for player %d on leaderboard %d", req.Score, who.PlayerID, req.LeaderboardID)
	w.WriteHeader(http.StatusNoContent)
}

// bearerToken pulls the token out of an "Authorization: Bearer <token>" header.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	after, ok := strings.CutPrefix(h, "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(after)
}
