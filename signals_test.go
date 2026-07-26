package ggscale

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGameSessionSignals_Send_posts_negotiation(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return map[string]any{"id": int64(17)}, nil }}
	c := newClientWithFake(t, ft)

	id, err := c.Signals.Send(context.Background(), "gs_one", SendGameSessionSignal{
		ToPlayerID: 9, NegotiationID: "neg-a", Kind: "offer", Payload: "sdp",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(17), id)
	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/game-session/gs_one/signals", ft.gotReq.Path)
}

func TestGameSessionSignals_Poll_uses_cursor(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) {
		return map[string]any{"signals": []map[string]any{{"id": int64(18), "kind": "answer"}}}, nil
	}}
	c := newClientWithFake(t, ft)

	signals, err := c.Signals.Poll(context.Background(), "gs_one", 17)
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "17", ft.gotReq.Query.Get("after_id"))
	assert.Equal(t, int64(18), signals[0].ID)
}
