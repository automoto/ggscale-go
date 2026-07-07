package ggscale

import (
	"context"
	"errors"
	"net/http"
	"strconv"
)

// ServerService exposes the /v1/server/* endpoints intended for
// server-tier workloads (game-servers, matchmakers) that authenticate
// with a secret API key. No player session is attached. Reach it via
// Client.Server.
type ServerService struct {
	transport Transport
	apiKey    string
}

// PlayerVerifyResult is returned by VerifySession on a valid token. The
// fields mirror what the server emits — Email is omitted when the
// account has no address (anonymous players).
type PlayerVerifyResult struct {
	PlayerID   int64  `json:"player_id"`
	ExternalID string `json:"external_id"`
	Email      string `json:"email,omitempty"`
}

type playerVerifyRequestBody struct {
	SessionToken string `json:"session_token"`
}

// VerifySession asks ggscale to validate a player's session token on
// behalf of a game-server. The caller's secret API key authenticates
// the request; the token is the player's JWT obtained from one of the
// /v1/auth/* flows.
//
// Every server-side failure mode (expired token, tampered signature,
// disabled player, wrong tenant/project, malformed body) collapses to a
// single opaque 401 — callers should treat any *Error matching
// ErrUnauthorized as "session not valid" without trying to distinguish
// further.
func (s *ServerService) VerifySession(ctx context.Context, sessionToken string) (*PlayerVerifyResult, error) {
	if sessionToken == "" {
		return nil, errors.New("ggscale: session token is required")
	}
	var res PlayerVerifyResult
	err := s.transport.Call(ctx, &Request{
		Method: http.MethodPost,
		Path:   "/v1/server/player-sessions/verify",
		APIKey: s.apiKey,
		Body:   playerVerifyRequestBody{SessionToken: sessionToken},
	}, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// PlayerRemoteAddrs reads the remote addresses a player published for
// direct connectivity (see AccountService.SetRemoteAddrs). Requires a
// secret API key whose project the player belongs to; returns
// ErrNotFound when the player is unknown or has no linked account.
func (s *ServerService) PlayerRemoteAddrs(ctx context.Context, playerID int64) ([]RemoteAddr, error) {
	var payload remoteAddrsPayload
	err := s.transport.Call(ctx, &Request{
		Method: http.MethodGet,
		Path:   "/v1/server/players/" + strconv.FormatInt(playerID, 10) + "/remote-addrs",
		APIKey: s.apiKey,
	}, &payload)
	if err != nil {
		return nil, err
	}
	return payload.Addresses, nil
}
