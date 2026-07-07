// Package ggscale is the official Go client for the ggscale API. It
// covers the v1 surface needed by game code: player authentication,
// per-player JSON storage, leaderboards, profile management, friends
// and presence, player-hosted game sessions with invites, matchmaking,
// real-time WebSocket events, TURN relay credentials, and server-tier
// player-session verification.
//
// Start with NewClient, then call Login with one of the supplied
// Authenticator implementations:
//
//	c, _ := ggscale.NewClient(ggscale.Options{
//	    BaseURL: "http://localhost:8080",
//	    APIKey:  os.Getenv("GGSCALE_API_KEY"),
//	})
//	_ = c.Login(ctx, ggscale.NewEmailPasswordAuth(c.Transport(), apiKey, email, password))
//	ready, _ := c.Matchmaker.RequestMatch(ctx, ggscale.MatchRequest{
//	    Fleet: "docker-default", GameMode: "deathmatch",
//	})
//	fmt.Println(ready.Address)
//
// The package is safe for concurrent use. Sessions auto-refresh
// behind the scenes; callers who need to persist a session across
// process restarts can read it via Client.Session and feed it back
// with Client.SetSession.
//
// See examples/quickstart for a working end-to-end demo.
package ggscale
