// Package ggscale is the official Go client for the ggscale API. It
// covers the v1 surface needed by game code: end-user authentication,
// per-user JSON storage, leaderboards, profile management, matchmaking,
// real-time WebSocket events, and TURN relay credentials.
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
