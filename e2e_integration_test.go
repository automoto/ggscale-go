//go:build integration

package ggscale_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/turn/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ggscale "github.com/automoto/ggscale-go"
)

// The relay nodes that scripts/integration-test.sh registers. Both are in one
// region. Peers must allocate on the same relay: each relay's relayed
// addresses are loopback inside its own container.
const (
	itRelayRegion = "it-east"
	itTURNURL     = "turn:127.0.0.1:13478?transport=udp"
	itTURNURL2    = "turn:127.0.0.1:13480?transport=udp"
	itSTUNURL     = "stun:127.0.0.1:13478"

	// relay2's health listener, published for the failover test.
	itRelay2Lifecycle = "http://127.0.0.1:19091/lifecycle"
	itLifecycleToken  = "integration-relay-lifecycle-token-0123456789"
	itAdminToken      = "integration-platform-admin-token-0123456789"
)

type e2ePeer struct {
	client *ggscale.Client
	match  *ggscale.MatchResult
	creds  *ggscale.Credentials
}

func (p e2ePeer) id() int64 { return p.client.Session().PlayerID }

// TestIntegration_E2E_RelayMatch follows the connect flow of a two-player
// WebRTC game such as gopher bash: anonymous login, a game_session match,
// match-scoped relay credentials, then an offer and an answer over game
// session signals. A WebRTC game sends SDP in the signals; here each peer
// sends its TURN relayed address, which is the candidate a relay-only ICE
// session would use. The peers then exchange datagrams through the relay.
func TestIntegration_E2E_RelayMatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	host, guest := matchPair(ctx, t, "gopher-bash-it")
	for _, p := range []e2ePeer{host, guest} {
		assert.ElementsMatch(t, []string{itTURNURL, itTURNURL2}, p.creds.URLs)
		assert.Equal(t, []string{itSTUNURL}, p.creds.STUNURLs)
		assert.Equal(t, itRelayRegion, p.creds.Region)
	}

	// A long session renews its credentials with KeepFresh; the first
	// callback comes at once. The guest allocates with those.
	keepCtx, stop := context.WithCancel(ctx)
	err := guest.client.Relay.KeepFresh(keepCtx, func(c *ggscale.Credentials) {
		guest.creds = c
		stop()
	}, ggscale.WithMatch(guest.match.MatchID), ggscale.WithRegion(itRelayRegion))
	require.ErrorIs(t, err, context.Canceled)

	connectThroughRelay(ctx, t, host, guest, allocateRelay(t, host.creds, itTURNURL),
		allocateRelay(t, guest.creds, itTURNURL), ggscale.SignalKindOffer, ggscale.SignalKindAnswer)
}

// TestIntegration_E2E_RelayFailover drains the relay a match uses and
// recovers the session on the region's other relay with the README's ICE
// restart recipe: fresh credentials (they list only relays that take new
// sessions), then restart_offer from the peer with the lower player ID and
// restart_answer from the other.
func TestIntegration_E2E_RelayFailover(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	host, guest := matchPair(ctx, t, "gopher-bash-it-failover")
	hostRelay := allocateRelay(t, host.creds, itTURNURL2)
	guestRelay := allocateRelay(t, guest.creds, itTURNURL2)
	_, guestAddr := connectThroughRelay(ctx, t, host, guest, hostRelay, guestRelay,
		ggscale.SignalKindOffer, ggscale.SignalKindAnswer)

	t.Cleanup(func() {
		lifecycle(t, "resume")
		waitRelay2Eligible(t, true)
	})
	lifecycle(t, "drain")
	waitRelay2Eligible(t, false)

	// A drain refuses new allocations; the live one keeps relaying until the
	// relay stops.
	_, err := hostRelay.WriteTo([]byte("still here"), guestAddr)
	require.NoError(t, err)
	readRelay(t, guestRelay, "still here")

	for _, p := range []*e2ePeer{&host, &guest} {
		p.creds, err = p.client.Relay.GetCredentials(ctx, ggscale.WithMatch(p.match.MatchID), ggscale.WithRegion(itRelayRegion))
		require.NoError(t, err)
		require.Equal(t, []string{itTURNURL}, p.creds.URLs, "fresh credentials skip the draining relay")
	}

	offerer, answerer := host, guest
	if guest.id() < host.id() {
		offerer, answerer = guest, host
	}
	connectThroughRelay(ctx, t, offerer, answerer, allocateRelay(t, offerer.creds, offerer.creds.URLs[0]),
		allocateRelay(t, answerer.creds, answerer.creds.URLs[0]), ggscale.SignalKindRestartOffer, ggscale.SignalKindRestartAnswer)
}

// matchPair matches two throwaway players into one game session and gets each
// one match-scoped credentials for the test region. It returns the host first.
func matchPair(ctx context.Context, t *testing.T, gameMode string) (host, guest e2ePeer) {
	t.Helper()
	req := ggscale.MatchRequest{
		Mode:             ggscale.ModeGameSession,
		GameMode:         gameMode,
		MinCount:         2,
		MaxCount:         2,
		CountMultiple:    2,
		StringProperties: map[string]string{"transport": "webrtc"},
	}
	type outcome struct {
		peer e2ePeer
		err  error
	}
	ch := make(chan outcome, 2)
	for range 2 {
		c := newThrowawayPlayerClient(t)
		go func() {
			m, err := c.Matchmaker.WaitForMatch(ctx, req)
			if err != nil {
				ch <- outcome{err: err}
				return
			}
			creds, err := c.Relay.GetCredentials(ctx, ggscale.WithMatch(m.MatchID), ggscale.WithRegion(itRelayRegion))
			ch <- outcome{e2ePeer{c, m, creds}, err}
		}()
	}
	a, b := <-ch, <-ch
	require.NoError(t, a.err)
	require.NoError(t, b.err)

	host, guest = a.peer, b.peer
	if guest.id() == guest.match.HostPlayerID {
		host, guest = guest, host
	}
	require.Equal(t, host.id(), host.match.HostPlayerID, "one peer hosts")
	require.Equal(t, host.match.SessionID, guest.match.SessionID)
	return host, guest
}

// connectThroughRelay sends offerer's relayed address in an offerKind signal
// and answerer's in the answerKind reply, then checks datagrams pass both ways.
// It returns both relayed addresses.
func connectThroughRelay(ctx context.Context, t *testing.T, offerer, answerer e2ePeer,
	offererRelay, answererRelay net.PacketConn, offerKind, answerKind string,
) (offererAddr, answererAddr net.Addr) {
	t.Helper()
	sessionID := offerer.match.SessionID
	_, err := offerer.client.Signals.Send(ctx, sessionID, ggscale.SendGameSessionSignal{
		ToPlayerID: answerer.id(), NegotiationID: offerKind, Kind: offerKind,
		Payload: offererRelay.LocalAddr().String(),
	})
	require.NoError(t, err)
	offer := waitSignal(ctx, t, answerer.client, sessionID, offerer.id(), offerKind)

	_, err = answerer.client.Signals.Send(ctx, sessionID, ggscale.SendGameSessionSignal{
		ToPlayerID: offerer.id(), NegotiationID: offer.NegotiationID, Kind: answerKind,
		Payload: answererRelay.LocalAddr().String(),
	})
	require.NoError(t, err)
	answer := waitSignal(ctx, t, offerer.client, sessionID, answerer.id(), answerKind)

	offererAddr, err = net.ResolveUDPAddr("udp4", offer.Payload)
	require.NoError(t, err)
	answererAddr, err = net.ResolveUDPAddr("udp4", answer.Payload)
	require.NoError(t, err)

	// The relay drops data from a peer without a permission, and WriteTo
	// creates one. So the answerer writes first, then the offerer's ping can
	// land. The "hello" can land too, so the reads skip other datagrams.
	_, err = answererRelay.WriteTo([]byte("hello"), offererAddr)
	require.NoError(t, err)
	_, err = offererRelay.WriteTo([]byte("ping"), answererAddr)
	require.NoError(t, err)
	readRelay(t, answererRelay, "ping")
	_, err = answererRelay.WriteTo([]byte("pong"), offererAddr)
	require.NoError(t, err)
	readRelay(t, offererRelay, "pong")
	return offererAddr, answererAddr
}

// allocateRelay opens a TURN allocation on url, which must be in creds.
func allocateRelay(t *testing.T, creds *ggscale.Credentials, url string) net.PacketConn {
	t.Helper()
	require.Contains(t, creds.URLs, url)
	addr, _, _ := strings.Cut(strings.TrimPrefix(url, "turn:"), "?")

	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	require.NoError(t, err)
	client, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr: addr,
		TURNServerAddr: addr,
		Conn:           conn,
		Username:       creds.Username,
		Password:       creds.Password,
		Realm:          creds.Realm,
		LoggerFactory:  logging.NewDefaultLoggerFactory(),
	})
	require.NoError(t, err)
	require.NoError(t, client.Listen())
	relayConn, err := client.Allocate()
	require.NoError(t, err, "TURN allocate with ggscale credentials")
	t.Cleanup(func() {
		_ = relayConn.Close()
		client.Close()
		_ = conn.Close()
	})
	return relayConn
}

// readRelay reads datagrams from conn until want arrives.
func readRelay(t *testing.T, conn net.PacketConn, want string) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFrom(buf)
		require.NoError(t, err, "waiting for %q through the relay", want)
		if string(buf[:n]) == want {
			return
		}
	}
}

// waitSignal polls the session's signals until one of kind arrives from
// fromID, as the game's signal loop does.
func waitSignal(ctx context.Context, t *testing.T, c *ggscale.Client, sessionID string, fromID int64, kind string) ggscale.GameSessionSignal {
	t.Helper()
	var cursor int64
	for {
		signals, err := c.Signals.Poll(ctx, sessionID, cursor)
		require.NoError(t, err)
		for _, s := range signals {
			cursor = max(cursor, s.ID)
			if s.FromPlayerID == fromID && s.Kind == kind {
				return s
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("no %s signal from player %d: %v", kind, fromID, ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// lifecycle posts a drain or resume to relay2, as an operator's deploy does.
func lifecycle(t *testing.T, action string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, itRelay2Lifecycle+"/"+action, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+itLifecycleToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "relay2 %s", action)
}

// waitRelay2Eligible waits until the registry's probe (every 10 s) shows
// relay2 as eligible or not, plus the API server's 2 s credential cache.
func waitRelay2Eligible(t *testing.T, want bool) {
	t.Helper()
	require.Eventually(t, func() bool {
		req, err := http.NewRequest(http.MethodGet, baseURL()+"/v1/admin/relay-nodes", nil)
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "Bearer "+itAdminToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		var list struct {
			Nodes []struct {
				TURNURLs []string `json:"turn_urls"`
				State    string   `json:"state"`
				Eligible bool     `json:"eligible"`
			} `json:"nodes"`
		}
		if json.NewDecoder(resp.Body).Decode(&list) != nil {
			return false
		}
		for _, n := range list.Nodes {
			if n.State == "known" && slices.Contains(n.TURNURLs, itTURNURL2) {
				return n.Eligible == want
			}
		}
		return false
	}, 30*time.Second, time.Second, "relay2 eligible=%v", want)
	time.Sleep(3 * time.Second)
}
