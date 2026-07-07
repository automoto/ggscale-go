package ggscale

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// FriendsService exposes the /v1/friends/* endpoints. Reach it via
// Client.Friends.
//
// Friendships are account-scoped: both players must have linked
// (non-anonymous) gg-scale accounts, otherwise the server answers 403
// (ErrForbidden). Target players are addressed by their per-project
// player ID (Friend.PlayerID, PlayerVerifyResult.PlayerID, etc.).
type FriendsService struct {
	c *Client
}

// FriendPresence is a friend's live presence, present only on accepted
// friendships. SessionID is non-nil while the friend is in a game
// session they chose to share.
type FriendPresence struct {
	Status    string  `json:"status"`
	SessionID *string `json:"session_id"`
}

// Friend is one edge in the caller's friends list. PlayerID is nil
// when the friend's account has no player in the calling project;
// Presence is nil unless the friendship is accepted and the friend is
// known to the presence system.
type Friend struct {
	ID          int64           `json:"id"`
	AccountID   string          `json:"account_id"`
	PlayerID    *int64          `json:"player_id,omitempty"`
	Status      string          `json:"status"`
	Email       *string         `json:"email,omitempty"`
	DisplayName *string         `json:"display_name,omitempty"`
	Presence    *FriendPresence `json:"presence,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// FriendsPage is one page of List results. NextCursor is empty when
// there are no further pages.
type FriendsPage struct {
	Items      []Friend `json:"items"`
	NextCursor string   `json:"next_cursor"`
}

// FriendsListOptions configures Friends.List. Status filters the edge
// state (pending, accepted, rejected, blocked); empty means the server
// default (accepted). Limit defaults server-side to 50 and is capped
// at 100; Cursor is the NextCursor from a prior page.
type FriendsListOptions struct {
	Status string
	Limit  int
	Cursor string
}

// List returns the caller's friend edges filtered by status.
func (f *FriendsService) List(ctx context.Context, opts FriendsListOptions) (*FriendsPage, error) {
	q := url.Values{}
	if opts.Status != "" {
		q.Set("status", opts.Status)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		q.Set("cursor", opts.Cursor)
	}
	var page FriendsPage
	err := f.c.callProtected(ctx, &Request{
		Method: http.MethodGet,
		Path:   "/v1/friends",
		Query:  q,
	}, &page)
	if err != nil {
		return nil, err
	}
	return &page, nil
}

// Request sends (or re-sends) a friend request to playerID and returns
// the resulting edge status — "pending" for a fresh request, or the
// existing status when an edge already exists. A blocked or unknown
// target returns ErrNotFound; the server never reveals which.
func (f *FriendsService) Request(ctx context.Context, playerID int64) (string, error) {
	var res friendStatusResponse
	err := f.c.callProtected(ctx, &Request{
		Method: http.MethodPost,
		Path:   friendPath(playerID) + "/request",
	}, &res)
	if err != nil {
		return "", err
	}
	return res.Status, nil
}

// Accept accepts a pending friend request from playerID. Returns
// ErrConflict when the edge is not in a state that can be accepted.
func (f *FriendsService) Accept(ctx context.Context, playerID int64) error {
	return f.c.callProtected(ctx, &Request{
		Method: http.MethodPost,
		Path:   friendPath(playerID) + "/accept",
	}, nil)
}

// Reject declines a pending (or revokes an accepted) friend request
// from playerID.
func (f *FriendsService) Reject(ctx context.Context, playerID int64) error {
	return f.c.callProtected(ctx, &Request{
		Method: http.MethodPost,
		Path:   friendPath(playerID) + "/reject",
	}, nil)
}

// Remove deletes the friend edge with playerID in either direction.
func (f *FriendsService) Remove(ctx context.Context, playerID int64) error {
	return f.c.callProtected(ctx, &Request{
		Method: http.MethodDelete,
		Path:   friendPath(playerID),
	}, nil)
}

// Block blocks playerID: any existing friendship is severed and future
// requests from them are silently swallowed (they see ErrNotFound, not
// the block).
func (f *FriendsService) Block(ctx context.Context, playerID int64) error {
	return f.c.callProtected(ctx, &Request{
		Method: http.MethodPost,
		Path:   friendPath(playerID) + "/block",
	}, nil)
}

// Unblock removes a previous block on playerID. It does not restore
// any friendship the block severed.
func (f *FriendsService) Unblock(ctx context.Context, playerID int64) error {
	return f.c.callProtected(ctx, &Request{
		Method: http.MethodPost,
		Path:   friendPath(playerID) + "/unblock",
	}, nil)
}

// RemoteAddrs returns the remote addresses an ACCEPTED friend has
// published (see AccountService.SetRemoteAddrs). Non-friends and
// blocked pairs get ErrForbidden.
func (f *FriendsService) RemoteAddrs(ctx context.Context, playerID int64) ([]RemoteAddr, error) {
	var payload remoteAddrsPayload
	err := f.c.callProtected(ctx, &Request{
		Method: http.MethodGet,
		Path:   friendPath(playerID) + "/remote-addrs",
	}, &payload)
	if err != nil {
		return nil, err
	}
	return payload.Addresses, nil
}

type friendStatusResponse struct {
	Status string `json:"status"`
}

func friendPath(playerID int64) string {
	return "/v1/friends/" + strconv.FormatInt(playerID, 10)
}
