//go:build integration

package ggscale_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/turn/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ggscale "github.com/automoto/ggscale-go"
)

// The relay node that scripts/integration-test.sh registers.
const (
	itRelayRegion = "it-east"
	itTURNURL     = "turn:127.0.0.1:13478?transport=udp"
	itSTUNURL     = "stun:127.0.0.1:13478"
)

type e2ePeer struct {
	client *ggscale.Client
	match  *ggscale.MatchResult
	creds  *ggscale.Credentials
}

// TestIntegration_E2E_RelayMatch follows the connect flow of a two-player
// WebRTC game such as gopher bash: anonymous login, a game_session match,
// match-scoped relay credentials, then an offer and an answer over game
// session signals. A WebRTC game sends SDP in the signals; here each peer
// sends its TURN relayed address, which is the candidate a relay-only ICE
// session would use. The peers then exchange datagrams through the relay.
func TestIntegration_E2E_RelayMatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := ggscale.MatchRequest{
		Mode:             ggscale.ModeGameSession,
		GameMode:         "gopher-bash-it",
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

	host, guest := a.peer, b.peer
	if guest.client.Session().PlayerID == guest.match.HostPlayerID {
		host, guest = guest, host
	}
	hostID, guestID := host.client.Session().PlayerID, guest.client.Session().PlayerID
	require.Equal(t, hostID, host.match.HostPlayerID, "one peer hosts")
	require.Equal(t, host.match.SessionID, guest.match.SessionID)
	sessionID := host.match.SessionID

	for _, p := range []e2ePeer{host, guest} {
		assert.Equal(t, []string{itTURNURL}, p.creds.URLs)
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

	hostRelay := allocateRelay(t, host.creds)
	guestRelay := allocateRelay(t, guest.creds)

	_, err = host.client.Signals.Send(ctx, sessionID, ggscale.SendGameSessionSignal{
		ToPlayerID: guestID, NegotiationID: "n1", Kind: ggscale.SignalKindOffer,
		Payload: hostRelay.LocalAddr().String(),
	})
	require.NoError(t, err)
	offer := waitSignal(ctx, t, guest.client, sessionID, hostID, ggscale.SignalKindOffer)

	_, err = guest.client.Signals.Send(ctx, sessionID, ggscale.SendGameSessionSignal{
		ToPlayerID: hostID, NegotiationID: offer.NegotiationID, Kind: ggscale.SignalKindAnswer,
		Payload: guestRelay.LocalAddr().String(),
	})
	require.NoError(t, err)
	answer := waitSignal(ctx, t, host.client, sessionID, guestID, ggscale.SignalKindAnswer)

	hostAddr, err := net.ResolveUDPAddr("udp4", offer.Payload)
	require.NoError(t, err)
	guestAddr, err := net.ResolveUDPAddr("udp4", answer.Payload)
	require.NoError(t, err)

	// The relay drops data from a peer without a permission, and WriteTo
	// creates one. So the guest writes first, then the host's ping can land.
	// The guest's "hello" can land too, so the reads skip other datagrams.
	_, err = guestRelay.WriteTo([]byte("hello"), hostAddr)
	require.NoError(t, err)
	_, err = hostRelay.WriteTo([]byte("ping"), guestAddr)
	require.NoError(t, err)
	readRelay(t, guestRelay, "ping")
	_, err = guestRelay.WriteTo([]byte("pong"), hostAddr)
	require.NoError(t, err)
	readRelay(t, hostRelay, "pong")
}

// allocateRelay opens a TURN allocation with the first TURN URL in creds.
func allocateRelay(t *testing.T, creds *ggscale.Credentials) net.PacketConn {
	t.Helper()
	require.NotEmpty(t, creds.URLs)
	addr, _, _ := strings.Cut(strings.TrimPrefix(creds.URLs[0], "turn:"), "?")

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
