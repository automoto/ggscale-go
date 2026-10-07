package ggscale

import "encoding/json"

// Realtime event types the server sends over /v1/ws, besides
// EventMatchmakerMatched. Events are best effort: a client that misses one
// recovers the state with the matching GET.
const (
	// EventPresence: a friend changed status. Payload: PresenceEvent.
	EventPresence = "presence"
	// EventGameInvite: a friend invited you to a game session. Payload:
	// GameInviteEvent.
	EventGameInvite = "game_invite"
	// EventPartyChanged: a party you are in, or were removed from, changed.
	// Payload: PartyChangedEvent.
	EventPartyChanged = "party_changed"
	// EventPartyInvite: a friend invited you to a party. Payload:
	// PartyInviteEvent. A re-invite of a pending invite sends no new event.
	EventPartyInvite = "party_invite"
)

// PresenceEvent is the payload of EventPresence.
type PresenceEvent struct {
	PlayerID  int64   `json:"player_id"`
	Status    string  `json:"status"`
	SessionID *string `json:"session_id"`
}

// GameInviteEvent is the payload of EventGameInvite.
type GameInviteEvent struct {
	InviteID  int64  `json:"invite_id"`
	SessionID string `json:"session_id"`
	JoinCode  string `json:"join_code"`
}

// PartyChangedEvent is the payload of EventPartyChanged. It does not carry
// the party: call Parties.Get when Version is newer than yours. A 404 means
// you are no longer a member.
type PartyChangedEvent struct {
	PartyID int64  `json:"party_id"`
	Version int64  `json:"version"`
	State   string `json:"state"`
}

// PartyInviteEvent is the payload of EventPartyInvite.
type PartyInviteEvent struct {
	InviteID     int64 `json:"invite_id"`
	PartyID      int64 `json:"party_id"`
	FromPlayerID int64 `json:"from_player_id"`
}

// DecodePayload decodes the message payload into v, for example a
// *PresenceEvent when Type is EventPresence.
func (m Message) DecodePayload(v any) error {
	return json.Unmarshal(m.Payload, v)
}
