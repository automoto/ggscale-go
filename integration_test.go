//go:build integration

// Integration tests run against a real ggscale server + Postgres stack.
// Bring it up (and run these) with:
//
//	make test-integration
//
// which delegates to scripts/integration-test.sh: docker compose up,
// wait for /v1/healthz, seed integration/seed.sql, go test, compose down.
// The API keys below must match the hashes seeded by integration/seed.sql.
package ggscale_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ggscale "github.com/automoto/ggscale-go"
)

const (
	defaultBaseURL        = "http://127.0.0.1:18080"
	defaultPublishableKey = "ggp_integration_publishable_key"
	defaultSecretKey      = "ggs_integration_secret_key"

	// Seeded by integration/seed.sql.
	seededLeaderboardID = int64(1)
)

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func baseURL() string        { return envOr("GGSCALE_IT_BASE_URL", defaultBaseURL) }
func publishableKey() string { return envOr("GGSCALE_IT_PUBLISHABLE_KEY", defaultPublishableKey) }
func secretKey() string      { return envOr("GGSCALE_IT_SECRET_KEY", defaultSecretKey) }

func TestMain(m *testing.M) {
	if err := checkServer(); err != nil {
		fmt.Fprintf(os.Stderr, "ggscale server not reachable at %s: %v\nstart the stack with: make test-integration\n", baseURL(), err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func checkServer() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL()+"/v1/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// The auth routes carry a per-IP rate limit (burst 10), and every test
// here calls from 127.0.0.1 — so the suite shares two anonymous players
// instead of minting one per test.
var (
	playerOnce   [2]sync.Once
	players      [2]*ggscale.Client
	playerErrs   [2]error
	playerLogins [2]error
)

func sharedPlayer(t *testing.T, i int) *ggscale.Client {
	t.Helper()
	playerOnce[i].Do(func() {
		c, err := ggscale.NewClient(ggscale.Options{
			BaseURL: baseURL(),
			APIKey:  publishableKey(),
		})
		if err != nil {
			playerErrs[i] = err
			return
		}
		auth := ggscale.NewAnonymousAuth(c.Transport(), publishableKey(), "")
		playerLogins[i] = c.Login(context.Background(), auth)
		players[i] = c
	})
	require.NoError(t, playerErrs[i])
	require.NoError(t, playerLogins[i])
	return players[i]
}

// newPlayerClient returns the suite's primary anonymous player.
func newPlayerClient(t *testing.T) *ggscale.Client {
	t.Helper()
	return sharedPlayer(t, 0)
}

// secondPlayerClient returns a distinct anonymous player for tests that
// need two parties (e.g. joining a game session).
func secondPlayerClient(t *testing.T) *ggscale.Client {
	t.Helper()
	return sharedPlayer(t, 1)
}

// newServerClient builds a client the way a trusted game-server would:
// the secret API key and no player session.
func newServerClient(t *testing.T) *ggscale.Client {
	t.Helper()
	c, err := ggscale.NewClient(ggscale.Options{
		BaseURL: baseURL(),
		APIKey:  secretKey(),
	})
	require.NoError(t, err)
	return c
}

func TestIntegration_AnonymousAuth_and_Refresh(t *testing.T) {
	ctx := context.Background()
	c := newPlayerClient(t)

	sess := c.Session()
	require.NotNil(t, sess)
	assert.Positive(t, sess.PlayerID, "player_id must round-trip from the real server")
	assert.NotEmpty(t, sess.AccessToken)
	assert.NotEmpty(t, sess.RefreshToken)
	assert.True(t, sess.ExpiresAt.After(time.Now()))

	rotated, err := c.Auth.Refresh(ctx, sess.RefreshToken)
	require.NoError(t, err)
	assert.Equal(t, sess.PlayerID, rotated.PlayerID)
	assert.NotEmpty(t, rotated.AccessToken)
	assert.NotEqual(t, sess.RefreshToken, rotated.RefreshToken, "refresh token rotates")

	// The old refresh token is revoked server-side; keep the shared
	// client on the rotated session for the rest of the suite.
	c.SetSession(rotated)
}

func TestIntegration_Profile_Get_and_Patch_XUID(t *testing.T) {
	ctx := context.Background()
	c := newPlayerClient(t)

	p, err := c.Profile.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, c.Session().PlayerID, p.ID)
	assert.NotEmpty(t, p.ExternalID)
	assert.Empty(t, p.Email, "anonymous player has no email")

	xuid := fmt.Sprintf("it-xuid-%d", p.ID)
	require.NoError(t, c.Profile.Update(ctx, ggscale.ProfilePatch{XUID: &xuid}))

	p, err = c.Profile.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, xuid, p.XUID)
}

func TestIntegration_Storage_CRUD_and_OCC(t *testing.T) {
	ctx := context.Background()
	c := newPlayerClient(t)

	type settings struct {
		Theme  string `json:"theme"`
		Volume int    `json:"volume"`
	}

	obj, err := c.Storage.Put(ctx, "settings", settings{Theme: "dark", Volume: 80})
	require.NoError(t, err)
	firstVersion := obj.Version

	got, err := c.Storage.Get(ctx, "settings")
	require.NoError(t, err)
	var s settings
	require.NoError(t, json.Unmarshal(got.Value, &s))
	assert.Equal(t, "dark", s.Theme)

	// OCC: writing at the current version succeeds and bumps it.
	obj, err = c.Storage.Put(ctx, "settings", settings{Theme: "light", Volume: 60}, ggscale.IfMatch(firstVersion))
	require.NoError(t, err)
	assert.Greater(t, obj.Version, firstVersion)

	// OCC: writing at the stale version conflicts.
	_, err = c.Storage.Put(ctx, "settings", settings{Theme: "auto"}, ggscale.IfMatch(firstVersion))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ggscale.ErrConflict))

	page, err := c.Storage.List(ctx, ggscale.ListOptions{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	// List returns metadata only: key/version/updated_at/size_bytes, no value.
	assert.Equal(t, "settings", page.Items[0].Key)
	assert.Positive(t, page.Items[0].SizeBytes, "list item reports the stored value size")
	assert.Positive(t, page.Items[0].Version)

	require.NoError(t, c.Storage.Delete(ctx, "settings"))
	_, err = c.Storage.Get(ctx, "settings")
	assert.True(t, errors.Is(err, ggscale.ErrNotFound))
}

func TestIntegration_Leaderboards_SubmitScore_Top_AroundMe(t *testing.T) {
	ctx := context.Background()
	player := newPlayerClient(t)
	server := newServerClient(t)
	playerID := player.Session().PlayerID
	token := player.Session().AccessToken

	// Submission is secret-key-only: a publishable-key caller is refused,
	// even via the server-tier method.
	err := player.Server.SubmitScore(ctx, token, seededLeaderboardID, 1500)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ggscale.ErrForbidden))

	// The trusted (secret-key) server submits on the player's behalf.
	require.NoError(t, server.Server.SubmitScore(ctx, token, seededLeaderboardID, 1500))

	top, err := player.Leaderboards.Top(ctx, seededLeaderboardID, 100)
	require.NoError(t, err)
	found := false
	for _, e := range top {
		if e.PlayerID == playerID {
			found = true
			assert.Equal(t, int64(1500), e.Score)
		}
	}
	assert.True(t, found, "submitted score appears in top")

	around, err := player.Leaderboards.AroundMe(ctx, seededLeaderboardID, 5)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, around.SelfRank, int64(0))
	assert.NotEmpty(t, around.Entries)
}

func TestIntegration_Presence_Set(t *testing.T) {
	ctx := context.Background()
	c := newPlayerClient(t)

	require.NoError(t, c.Presence.Set(ctx, "online", nil))

	// An empty status fails Huma field validation → 422 with the offending
	// field named in Error.Details.
	err := c.Presence.Set(ctx, "", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ggscale.ErrValidation))
	var sdkErr *ggscale.Error
	require.ErrorAs(t, err, &sdkErr)
	require.NotEmpty(t, sdkErr.Details, "validation error names the bad field")
	assert.Equal(t, "body.status", sdkErr.Details[0].Location)
}

func TestIntegration_GameSession_Lifecycle(t *testing.T) {
	ctx := context.Background()
	host := newPlayerClient(t)
	joiner := secondPlayerClient(t)

	sess, err := host.GameSessions.Create(ctx, ggscale.GameSessionCreate{
		TitleID:    "integration",
		PublicAddr: ggscale.GameSessionAddr{IP: "203.0.113.1", Port: 7777},
		MaxPlayers: 4,
		Props:      json.RawMessage(`{"map":"it_lobby"}`),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, sess.SessionID)
	assert.NotEmpty(t, sess.JoinCode)
	assert.Equal(t, "open", sess.State)
	require.Len(t, sess.Peers, 1, "host is the first peer")

	// Second player resolves the shared join code and joins.
	resolvedID, err := joiner.GameSessions.Resolve(ctx, sess.JoinCode)
	require.NoError(t, err)
	assert.Equal(t, sess.SessionID, resolvedID)

	joined, err := joiner.GameSessions.Join(ctx, resolvedID, ggscale.GameSessionAddr{IP: "198.51.100.7", Port: 7778})
	require.NoError(t, err)
	require.Len(t, joined.Peers, 2)

	// Heartbeats keep both peers on the roster and return it.
	peers, err := host.GameSessions.Heartbeat(ctx, sess.SessionID, json.RawMessage(`{"rtt_ms":20}`))
	require.NoError(t, err)
	assert.Len(t, peers, 2)

	require.NoError(t, joiner.GameSessions.Leave(ctx, sess.SessionID))
	require.NoError(t, host.GameSessions.Leave(ctx, sess.SessionID))

	// Host leaving ends the session.
	ended, err := host.GameSessions.Get(ctx, sess.SessionID)
	require.NoError(t, err)
	assert.Equal(t, "ended", ended.State)
}

func TestIntegration_Invites_List_empty(t *testing.T) {
	c := newPlayerClient(t)
	invites, err := c.Invites.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, invites)
}

func TestIntegration_Friends_and_Account_require_linked_account(t *testing.T) {
	// Friends and remote-addrs are account-scoped features; an anonymous
	// player is refused with 403 until they link a gg-scale account.
	ctx := context.Background()
	c := newPlayerClient(t)

	_, err := c.Friends.List(ctx, ggscale.FriendsListOptions{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ggscale.ErrForbidden))

	_, err = c.Account.RemoteAddrs(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ggscale.ErrForbidden))
}

func TestIntegration_Server_VerifySession(t *testing.T) {
	ctx := context.Background()
	player := newPlayerClient(t)
	server := newServerClient(t)

	res, err := server.Server.VerifySession(ctx, player.Session().AccessToken)
	require.NoError(t, err)
	assert.Equal(t, player.Session().PlayerID, res.PlayerID)
	assert.NotEmpty(t, res.ExternalID)
	assert.Empty(t, res.Email)

	// Garbage tokens collapse to the opaque 401.
	_, err = server.Server.VerifySession(ctx, "not.a.real.token")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ggscale.ErrUnauthorized))

	// The publishable key is kept off the verify oracle entirely.
	_, err = player.Server.VerifySession(ctx, player.Session().AccessToken)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ggscale.ErrForbidden))
}

// TestIntegration_Matchmaker_Tickets exercises the ticket lifecycle
// deterministically, without needing a match to form: a lone min_count=2
// ticket stays queued, so we can verify create, get, the one-active-ticket
// 409, and cancel.
func TestIntegration_Matchmaker_Tickets(t *testing.T) {
	ctx := context.Background()
	c := newPlayerClient(t)

	req := ggscale.MatchRequest{Mode: ggscale.ModeMatchOnly, MinCount: 2, MaxCount: 2}
	ticket, err := c.Matchmaker.CreateTicket(ctx, req)
	require.NoError(t, err)
	require.Positive(t, ticket.ID)
	assert.Equal(t, "queued", ticket.Status)
	// Never leave an active ticket behind — later matchmaker tests reuse
	// this player and would hit the one-active-ticket limit.
	defer func() { _ = c.Matchmaker.CancelTicket(context.Background(), ticket.ID) }()

	got, err := c.Matchmaker.GetTicket(ctx, ticket.ID)
	require.NoError(t, err)
	assert.Equal(t, ticket.ID, got.ID)
	assert.Equal(t, "match_only", got.Mode)

	// One active ticket per player: a second create is refused, and the
	// 409 names the active ticket so the caller can cancel it.
	_, err = c.Matchmaker.CreateTicket(ctx, req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ggscale.ErrTicketActive))
	var sdkErr *ggscale.Error
	require.ErrorAs(t, err, &sdkErr)
	assert.Equal(t, ticket.ID, sdkErr.ActiveTicketID(), "409 carries the active ticket id")

	require.NoError(t, c.Matchmaker.CancelTicket(ctx, ticket.ID))
	after, err := c.Matchmaker.GetTicket(ctx, ticket.ID)
	if err == nil {
		assert.Equal(t, "cancelled", after.Status)
	} else {
		assert.True(t, errors.Is(err, ggscale.ErrNotFound), "cancelled ticket is terminal or gone")
	}
}

// TestIntegration_Matchmaker_MatchOnly_and_Relay is the core P2P flow: two
// players queue for a match_only match and connect. Both must land in the
// same match with the same roster and host, and — since the stack enables
// the relay — each gets match-scoped TURN credentials.
func TestIntegration_Matchmaker_MatchOnly_and_Relay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	p0 := newPlayerClient(t)
	p1 := secondPlayerClient(t)

	// max_count == min_count == 2 forms a full group immediately; a larger
	// max would make the worker wait out its relax window before pairing.
	req := ggscale.MatchRequest{Mode: ggscale.ModeMatchOnly, MinCount: 2, MaxCount: 2}

	type outcome struct {
		m   *ggscale.P2PMatch
		err error
	}
	ch := make(chan outcome, 2)
	for _, client := range []*ggscale.Client{p0, p1} {
		client := client
		go func() {
			m, err := client.Matchmaker.ConnectP2P(ctx, req, ggscale.GameSessionAddr{})
			ch <- outcome{m, err}
		}()
	}
	a, b := <-ch, <-ch
	require.NoError(t, a.err, "first peer connects")
	require.NoError(t, b.err, "second peer connects")

	// Both peers see one shared match_only match with the full roster.
	assert.Equal(t, string(ggscale.ModeMatchOnly), a.m.Result.Mode)
	assert.NotEmpty(t, a.m.Result.MatchID)
	assert.Equal(t, a.m.Result.MatchID, b.m.Result.MatchID, "peers share one match_id")
	assert.Len(t, a.m.Result.Users, 2, "roster carries both players")
	assert.Len(t, b.m.Result.Users, 2)

	// Peers agree on the host, and exactly one of them is it.
	assert.Positive(t, a.m.Result.HostPlayerID)
	assert.Equal(t, a.m.Result.HostPlayerID, b.m.Result.HostPlayerID, "peers agree on the host")
	assert.NotEqual(t, a.m.IsHost, b.m.IsHost, "exactly one peer hosts")

	// Relay is enabled in the stack, so ConnectP2P gathered match-scoped
	// credentials for each peer. (No real TURN server here, so urls is empty.)
	require.NotNil(t, a.m.Relay, "relay enabled → credentials fetched")
	assert.NotEmpty(t, a.m.Relay.Username)
	assert.NotEmpty(t, a.m.Relay.Password)
}

// TestIntegration_Relay_Credentials verifies unscoped TURN credential
// issuance directly: the publishable key's p2p_relay scope, the project's
// p2p_relay feature grant, and metering all have to line up.
func TestIntegration_Relay_Credentials(t *testing.T) {
	ctx := context.Background()
	c := newPlayerClient(t)

	creds, err := c.Relay.GetCredentials(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, creds.Username)
	assert.NotEmpty(t, creds.Password)
	assert.Positive(t, creds.TTL)
	assert.Equal(t, "ggscale", creds.Realm)
	// urls is empty in the single-node dev stack (no reachable TURN listener).
}

func TestIntegration_Realtime_Dial(t *testing.T) {
	c := newPlayerClient(t)
	rc, err := c.DialRealtime(context.Background())
	require.NoError(t, err)
	require.NoError(t, rc.Close())
}
