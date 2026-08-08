package ggscale

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// GameSessionsService exposes the /v1/game-session endpoints for
// player-hosted (listen-server) games: create a session, share its
// join code, join, and heartbeat to keep the peer roster fresh. Reach
// it via Client.GameSessions.
type GameSessionsService struct {
	c *Client
}

// GameSessionAddr is a peer's public endpoint as exchanged through the
// session roster.
type GameSessionAddr struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

// GameSessionPeer is one member of a session's roster.
type GameSessionPeer struct {
	PlayerID    int64           `json:"player_id"`
	XUID        string          `json:"xuid,omitempty"`
	DisplayName string          `json:"display_name,omitempty"`
	Addr        GameSessionAddr `json:"addr"`
}

// GameSession is the state of a session as returned by Create, Get,
// and Join. JoinCode is the short human-shareable code other players
// resolve via Resolve.
type GameSession struct {
	SessionID string            `json:"session_id"`
	JoinCode  string            `json:"join_code"`
	State     string            `json:"state"`
	ExpiresAt time.Time         `json:"expires_at"`
	Peers     []GameSessionPeer `json:"peers"`
}

// PublicGameSession is one row in the public server browser.
type PublicGameSession struct {
	SessionID       string          `json:"session_id"`
	TitleID         string          `json:"title_id,omitempty"`
	Props           json.RawMessage `json:"props"`
	PlayerCount     int             `json:"player_count"`
	MaxPlayers      int             `json:"max_players"`
	HostPlayerID    int64           `json:"host_player_id"`
	HostDisplayName string          `json:"host_display_name,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

// GameSessionListOptions filters and paginates the public server browser.
type GameSessionListOptions struct {
	TitleID string
	Limit   int
	Cursor  string
}

// GameSessionPage is one cursor page of public game sessions.
type GameSessionPage struct {
	Items      []PublicGameSession `json:"items"`
	NextCursor string              `json:"next_cursor"`
}

// GameSessionCreate is the input to GameSessions.Create. PublicAddr is
// required (the host's reachable endpoint); MaxPlayers defaults
// server-side to 2 and is capped at 64. Private sessions are only
// visible to the host, members, and invitees.
type GameSessionCreate struct {
	TitleID    string          `json:"title_id,omitempty"`
	PublicAddr GameSessionAddr `json:"public_addr"`
	Props      json.RawMessage `json:"props,omitempty"`
	MaxPlayers int             `json:"max_players,omitempty"`
	Private    bool            `json:"private,omitempty"`
}

type gameSessionJoinBody struct {
	PublicAddr GameSessionAddr `json:"public_addr"`
}

type gameSessionHeartbeatBody struct {
	QoS *json.RawMessage `json:"qos,omitempty"`
}

type gameSessionHeartbeatResponse struct {
	OK    bool              `json:"ok"`
	Peers []GameSessionPeer `json:"peers"`
}

// Create opens a new session hosted by the calling player and returns
// it with the caller as first peer. Returns ErrRateLimited when the
// project's open-session cap is reached.
func (g *GameSessionsService) Create(ctx context.Context, req GameSessionCreate) (*GameSession, error) {
	var sess GameSession
	err := g.c.callProtected(ctx, &Request{
		OperationID: "createGameSession",
		Method:      http.MethodPost,
		Path:        "/v1/game-session",
		Body:        req,
	}, &sess)
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// Get returns a session by id, including its peer roster. Only the
// host, members, and invitees can see it; anyone else gets
// ErrNotFound.
func (g *GameSessionsService) Get(ctx context.Context, sessionID string) (*GameSession, error) {
	var sess GameSession
	err := g.c.callProtected(ctx, &Request{
		OperationID: "getGameSession",
		Method:      http.MethodGet,
		Path:        "/v1/game-session/" + url.PathEscape(sessionID),
	}, &sess)
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// Resolve turns a shareable join code into a session id for Join.
// Private sessions resolve only for the host, members, and invitees.
func (g *GameSessionsService) Resolve(ctx context.Context, joinCode string) (string, error) {
	q := url.Values{}
	q.Set("joinCode", joinCode)
	var res struct {
		SessionID string `json:"session_id"`
	}
	err := g.c.callProtected(ctx, &Request{
		OperationID: "resolveGameSession",
		Method:      http.MethodGet,
		Path:        "/v1/game-session",
		Query:       q,
	}, &res)
	if err != nil {
		return "", err
	}
	return res.SessionID, nil
}

// Join adds the calling player to the session, publishing addr to the
// other peers, and returns the refreshed session. A full session
// returns ErrConflict; an ended or expired one returns *Error with
// Status 410.
func (g *GameSessionsService) Join(ctx context.Context, sessionID string, addr GameSessionAddr) (*GameSession, error) {
	var sess GameSession
	err := g.c.callProtected(ctx, &Request{
		OperationID: "joinGameSession",
		Method:      http.MethodPost,
		Path:        "/v1/game-session/" + url.PathEscape(sessionID) + "/join",
		Body:        gameSessionJoinBody{PublicAddr: addr},
	}, &sess)
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// Heartbeat marks the calling player live in the session and returns
// the current roster with stale peers pruned. qos optionally updates
// the caller's connection-quality blob; pass nil to leave the stored
// value untouched. Call roughly every 30 seconds while in the session.
func (g *GameSessionsService) Heartbeat(ctx context.Context, sessionID string, qos json.RawMessage) ([]GameSessionPeer, error) {
	body := gameSessionHeartbeatBody{}
	if qos != nil {
		body.QoS = &qos
	}
	var res gameSessionHeartbeatResponse
	err := g.c.callProtected(ctx, &Request{
		OperationID: "heartbeatGameSession",
		Method:      http.MethodPost,
		Path:        "/v1/game-session/" + url.PathEscape(sessionID) + "/heartbeat",
		Body:        body,
	}, &res)
	if err != nil {
		return nil, err
	}
	return res.Peers, nil
}

// Leave removes the calling player from the session. When the host
// leaves, the session ends for everyone.
func (g *GameSessionsService) Leave(ctx context.Context, sessionID string) error {
	return g.c.callProtected(ctx, &Request{
		OperationID: "leaveGameSession",
		Method:      http.MethodDelete,
		Path:        "/v1/game-session/" + url.PathEscape(sessionID),
	}, nil)
}

// List returns open, public, non-full sessions for the server browser.
func (g *GameSessionsService) List(ctx context.Context, opts GameSessionListOptions) (*GameSessionPage, error) {
	query := url.Values{}
	if opts.TitleID != "" {
		query.Set("title_id", opts.TitleID)
	}
	if opts.Limit > 0 {
		query.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		query.Set("cursor", opts.Cursor)
	}
	var page GameSessionPage
	err := g.c.callProtected(ctx, &Request{
		OperationID: "listGameSessions",
		Method:      http.MethodGet,
		Path:        "/v1/game-sessions",
		Query:       query,
	}, &page)
	if err != nil {
		return nil, err
	}
	return &page, nil
}

// All iterates public game sessions across every cursor page.
func (g *GameSessionsService) All(ctx context.Context, opts GameSessionListOptions) iter.Seq2[PublicGameSession, error] {
	return cursorSequence(opts.Cursor, func(cursor string) ([]PublicGameSession, string, error) {
		opts.Cursor = cursor
		page, err := g.List(ctx, opts)
		if err != nil {
			return nil, "", err
		}
		return page.Items, page.NextCursor, nil
	})
}
