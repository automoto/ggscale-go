package ggscale

import (
	"context"
	"errors"
	"net/http"
)

// EndUsersService exposes the /v1/end-users/* endpoints intended for
// server-tier workloads (game-servers, matchmakers) that authenticate
// with a secret API key. Reach it via Client.EndUsers.
type EndUsersService struct {
	transport Transport
	apiKey    string
}

// EndUserVerifyResult is returned by VerifySession on a valid token. The
// fields mirror what the server emits — Email is omitted when the
// account has no address (anonymous users).
type EndUserVerifyResult struct {
	UserID     int64  `json:"user_id"`
	ExternalID string `json:"external_id"`
	Email      string `json:"email,omitempty"`
}

type endUserVerifyRequestBody struct {
	SessionToken string `json:"session_token"`
}

// VerifySession asks ggscale to validate a player's session token on
// behalf of a game-server. The caller's secret API key authenticates
// the request; the token is the player's JWT obtained from one of the
// /v1/auth/* flows.
//
// Every server-side failure mode (expired token, tampered signature,
// disabled user, wrong tenant/project, malformed body) collapses to a
// single opaque 401 — callers should treat any *Error matching
// ErrUnauthorized as "session not valid" without trying to distinguish
// further.
func (e *EndUsersService) VerifySession(ctx context.Context, sessionToken string) (*EndUserVerifyResult, error) {
	if sessionToken == "" {
		return nil, errors.New("ggscale: session token is required")
	}
	var res EndUserVerifyResult
	err := e.transport.Call(ctx, &Request{
		Method: http.MethodPost,
		Path:   "/v1/end-users/verify",
		APIKey: e.apiKey,
		Body:   endUserVerifyRequestBody{SessionToken: sessionToken},
	}, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}
