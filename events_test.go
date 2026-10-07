package ggscale

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessage_DecodePayload_should_decode_presence(t *testing.T) {
	msg := Message{Type: EventPresence, Payload: json.RawMessage(`{"player_id":5,"status":"online","session_id":null}`)}
	var got PresenceEvent

	err := msg.DecodePayload(&got)

	require.NoError(t, err)
	assert.Equal(t, PresenceEvent{PlayerID: 5, Status: "online"}, got)
}

func TestMessage_DecodePayload_should_decode_game_invite(t *testing.T) {
	msg := Message{Type: EventGameInvite, Payload: json.RawMessage(`{"invite_id":3,"session_id":"gs_1","join_code":"ABC"}`)}
	var got GameInviteEvent

	err := msg.DecodePayload(&got)

	require.NoError(t, err)
	assert.Equal(t, GameInviteEvent{InviteID: 3, SessionID: "gs_1", JoinCode: "ABC"}, got)
}

func TestRealtimeTicketURL_should_carry_only_the_ticket(t *testing.T) {
	got := realtimeTicketURL("wss://api.example.com/v1/ws", "a+b/c")

	assert.Equal(t, "wss://api.example.com/v1/ws?ticket=a%2Bb%2Fc", got)
}

func TestRealtime_CreateTicket_should_post_with_session(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) {
		return map[string]any{"ticket": "t1", "expires_in_seconds": 30}, nil
	}}
	c := newClientWithFake(t, ft)

	got, err := c.Realtime.CreateTicket(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []any{"createRealtimeTicket", http.MethodPost, "/v1/ws/ticket", "test-jwt", "t1"},
		[]any{ft.gotReq.OperationID, ft.gotReq.Method, ft.gotReq.Path, ft.gotReq.SessionToken, got.Ticket})
}

func TestServer_FleetHeartbeat_should_send_secret_key_without_session(t *testing.T) {
	ft := &fakeTransport{respond: func(*Request) (any, error) { return nil, nil }}
	c, err := NewClient(Options{APIKey: "ggs_secret", Transport: ft})
	require.NoError(t, err)

	err = c.Server.FleetHeartbeat(context.Background(), Heartbeat{AgonesName: "gs-1", Fleet: "f", Address: "1.2.3.4:7777", MaxPlayers: 8})

	require.NoError(t, err)
	assert.Equal(t, []string{"fleetHeartbeat", "ggs_secret", ""},
		[]string{ft.gotReq.OperationID, ft.gotReq.APIKey, ft.gotReq.SessionToken})
}

func TestTicket_should_decode_entry_and_party_fields(t *testing.T) {
	raw := `{"id":70,"entry_id":30,"party_id":9,"status":"matched","users":[{"player_id":1,"queue_entry_id":30,"party_id":9}]}`
	var got Ticket

	err := json.Unmarshal([]byte(raw), &got)

	require.NoError(t, err)
	assert.Equal(t, []int64{30, 9, 30, 9}, []int64{got.EntryID, got.PartyID, got.Users[0].QueueEntryID, got.Users[0].PartyID})
}
