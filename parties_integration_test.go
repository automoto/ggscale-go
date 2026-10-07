//go:build integration

package ggscale_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ggscale "github.com/automoto/ggscale-go"
)

// partySettings fill a party of two at once. The game mode keeps these
// entries away from the other matchmaker tests.
var partySettings = ggscale.MatchRequest{Mode: ggscale.ModeMatchOnly, MinCount: 2, MaxCount: 2, GameMode: "party-it"}

// newPartyOfTwo makes a party with a fresh leader and a member who joined by
// code. It returns the party as the member sees it.
func newPartyOfTwo(t *testing.T, ctx context.Context) (leader, member *ggscale.Client, party *ggscale.Party) {
	t.Helper()
	leader = newThrowawayPlayerClient(t)
	member = newThrowawayPlayerClient(t)

	party, err := leader.Parties.Create(ctx, partySettings)
	require.NoError(t, err)
	code, err := leader.Parties.CreateCode(ctx, party.ID, party.Version, 1)
	require.NoError(t, err)
	party, err = member.Parties.JoinByCode(ctx, code.Code)
	require.NoError(t, err)
	return leader, member, party
}

func TestIntegration_Parties_queue_should_match_both_members(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	leader, member, party := newPartyOfTwo(t, ctx)

	party, err := leader.Parties.SetReady(ctx, party.ID, party.Version, true, ggscale.PartyProperties{})
	require.NoError(t, err)
	party, err = member.Parties.SetReady(ctx, party.ID, party.Version, true, ggscale.PartyProperties{})
	require.NoError(t, err)

	type outcome struct {
		res *ggscale.MatchResult
		err error
	}
	results := make(chan outcome, 2)
	for _, c := range []*ggscale.Client{leader, member} {
		go func() {
			res, err := c.Parties.WaitForMatch(ctx, party.ID)
			results <- outcome{res, err}
		}()
	}
	_, err = leader.Parties.Queue(ctx, party.ID, party.Version, "")
	require.NoError(t, err)

	a, b := <-results, <-results
	require.NoError(t, a.err)
	require.NoError(t, b.err)
	assert.Equal(t, a.res.MatchID, b.res.MatchID)
	for _, u := range a.res.Users {
		assert.Equal(t, party.ID, u.PartyID, "roster entry %d", u.PlayerID)
	}
}

func TestIntegration_Parties_stale_version_should_conflict(t *testing.T) {
	ctx := context.Background()
	leader, _, party := newPartyOfTwo(t, ctx)

	_, err := leader.Parties.Update(ctx, party.ID, party.Version-1, partySettings)

	assert.ErrorIs(t, err, ggscale.ErrStaleVersion)
}

func TestIntegration_Parties_kicked_member_should_lose_access(t *testing.T) {
	ctx := context.Background()
	leader, member, party := newPartyOfTwo(t, ctx)
	memberID := member.Session().PlayerID

	_, err := leader.Parties.Kick(ctx, party.ID, party.Version, memberID)
	require.NoError(t, err)
	_, err = member.Parties.Get(ctx, party.ID)

	assert.ErrorIs(t, err, ggscale.ErrNotFound)
}

func TestIntegration_Parties_member_should_leave(t *testing.T) {
	ctx := context.Background()
	_, member, party := newPartyOfTwo(t, ctx)

	_, err := member.Parties.Leave(ctx, party.ID, party.Version)
	require.NoError(t, err)
	_, err = member.Parties.Current(ctx)

	assert.ErrorIs(t, err, ggscale.ErrNotFound)
}

func TestIntegration_Parties_disband_should_remove_members(t *testing.T) {
	ctx := context.Background()
	leader, member, party := newPartyOfTwo(t, ctx)

	_, err := leader.Parties.Disband(ctx, party.ID, party.Version)
	require.NoError(t, err)
	_, err = member.Parties.Get(ctx, party.ID)

	assert.ErrorIs(t, err, ggscale.ErrNotFound)
}

func TestIntegration_Parties_wrong_codes_should_start_cooldown(t *testing.T) {
	ctx := context.Background()
	c := newThrowawayPlayerClient(t)

	// The default player limit is 10 wrong codes; the call after it is 429.
	var err error
	for range 12 {
		_, err = c.Parties.JoinByCode(ctx, strings.Repeat("Z", 16))
		if errors.Is(err, ggscale.ErrCodeCooldown) {
			break
		}
	}

	var apiErr *ggscale.Error
	require.ErrorAs(t, err, &apiErr)
	assert.True(t, errors.Is(err, ggscale.ErrCodeCooldown) && apiErr.RetryAfter > 0, "got %v", err)
}

func TestIntegration_Realtime_ticket_should_work_once(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := newThrowawayPlayerClient(t)
	ticket, err := c.Realtime.CreateTicket(ctx)
	require.NoError(t, err)
	wsURL := "ws" + strings.TrimPrefix(baseURL(), "http") + "/v1/ws?ticket=" + ticket.Ticket

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	require.NoError(t, conn.Close(websocket.StatusNormalClosure, ""))
	_, resp, err := websocket.Dial(ctx, wsURL, nil)

	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
