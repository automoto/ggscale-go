// Package ggscale is the official Go client for the ggscale API. It
// covers the v1 surface needed by game code: player authentication,
// account deletion, per-player JSON storage, leaderboards, profile
// management, friends and presence, player-hosted game sessions with
// invites, matchmaking, parties, real-time WebSocket events (also in
// browser builds), TURN relay credentials, and server-tier calls such as
// player-session verification and the fleet heartbeat.
//
// Start with NewClient, then call Login with one of the supplied
// Authenticator implementations:
//
//	c, _ := ggscale.NewClient(ggscale.Options{
//	    BaseURL: "http://localhost:8080",
//	    APIKey:  os.Getenv("GGSCALE_API_KEY"),
//	})
//	_ = c.Login(ctx, ggscale.NewEmailPasswordAuth(c.Transport(), apiKey, email, password))
//	res, _ := c.Matchmaker.WaitForMatch(ctx, ggscale.MatchRequest{
//	    Mode: ggscale.ModeGameSession, GameMode: "deathmatch",
//	    MinCount: 2, MaxCount: 4,
//	})
//	fmt.Println(res.HostPlayerID, res.SessionID, res.Users)
//
// WaitForMatch combines the realtime push with polling recovery, so a
// dropped WebSocket still returns the persisted match. For peer-to-peer
// modes ConnectP2P additionally gathers TURN relay credentials and joins
// the game session so peers can discover each other's endpoints. A party
// queues as one unit: Client.Parties.Watch keeps each member in the party
// and reports its changes and its match.
//
// The package is safe for concurrent use. Sessions auto-refresh
// behind the scenes; callers who need to persist a session across
// process restarts can read it via Client.Session and feed it back
// with Client.SetSession.
//
// See examples/quickstart for a working end-to-end demo.
package ggscale
