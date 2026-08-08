package ggscale

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// InvitesService exposes the /v1/invite endpoints: invite an accepted
// friend into a game session and list/dismiss pending invites. Reach
// it via Client.Invites.
//
// Invitees connected to the realtime WebSocket also receive a
// "game_invite" message the moment an invite is created; List covers
// players who were offline at the time.
type InvitesService struct {
	c *Client
}

// Invite is one pending game-session invite addressed to the caller.
type Invite struct {
	InviteID  int64     `json:"invite_id"`
	FromEmail string    `json:"from_email,omitempty"`
	FromXUID  string    `json:"from_xuid,omitempty"`
	SessionID string    `json:"session_id"`
	JoinCode  string    `json:"join_code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type inviteCreateBody struct {
	ToEmail   string `json:"to_email"`
	SessionID string `json:"session_id"`
}

type inviteCreateResponse struct {
	InviteID int64 `json:"invite_id"`
}

type inviteListResponse struct {
	Invites []Invite `json:"invites"`
}

// Create invites the player registered under toEmail into the given
// session and returns the invite id. The recipient must be an accepted
// friend (ErrForbidden otherwise) and the caller must be in the
// session; a closed or expired session returns ErrConflict.
func (i *InvitesService) Create(ctx context.Context, sessionID, toEmail string) (int64, error) {
	var res inviteCreateResponse
	err := i.c.callProtected(ctx, &Request{
		OperationID: "createGameInvite",
		Method:      http.MethodPost,
		Path:        "/v1/invite",
		Body:        inviteCreateBody{ToEmail: toEmail, SessionID: sessionID},
	}, &res)
	if err != nil {
		return 0, err
	}
	return res.InviteID, nil
}

// List returns the caller's pending, unexpired invites.
func (i *InvitesService) List(ctx context.Context) ([]Invite, error) {
	var res inviteListResponse
	err := i.c.callProtected(ctx, &Request{
		OperationID: "listGameInvites",
		Method:      http.MethodGet,
		Path:        "/v1/invite",
	}, &res)
	if err != nil {
		return nil, err
	}
	return res.Invites, nil
}

// Delete removes an invite — the sender cancels it, or the recipient
// declines/dismisses it.
func (i *InvitesService) Delete(ctx context.Context, inviteID int64) error {
	return i.c.callProtected(ctx, &Request{
		OperationID: "deleteGameInvite",
		Method:      http.MethodDelete,
		Path:        "/v1/invite/" + strconv.FormatInt(inviteID, 10),
	}, nil)
}
