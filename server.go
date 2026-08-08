package ggscale

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"net/http"
	"net/url"
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

type serverSubmitScoreRequest struct {
	PlayerID int64           `json:"player_id"`
	Score    int64           `json:"score"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
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
		OperationID: "verifyPlayerSession",
		Method:      http.MethodPost,
		Path:        "/v1/server/player-sessions/verify",
		APIKey:      s.apiKey,
		Body:        playerVerifyRequestBody{SessionToken: sessionToken},
	}, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// SubmitScore posts an authoritative score for playerID. It requires a secret
// server-tier key. A backend that begins with a player session token should
// first call VerifySession and pass the returned PlayerID here.
func (s *ServerService) SubmitScore(ctx context.Context, playerID, leaderboardID, score int64, opts ...ScoreOption) error {
	if playerID <= 0 {
		return errors.New("ggscale: player ID must be greater than zero")
	}
	metadata := submitScoreRequest{Score: score}
	for _, opt := range opts {
		opt(&metadata)
	}
	body := serverSubmitScoreRequest{PlayerID: playerID, Score: score, Metadata: metadata.Metadata}
	return s.transport.Call(ctx, &Request{
		OperationID: "serverSubmitScore",
		Method:      http.MethodPost,
		Path:        "/v1/server/leaderboards/" + strconv.FormatInt(leaderboardID, 10) + "/scores",
		Body:        body,
		APIKey:      s.apiKey,
	}, nil)
}

// PlayerRemoteAddrs reads the remote addresses a player published for
// direct connectivity (see AccountService.SetRemoteAddrs). Requires a
// secret API key whose project the player belongs to; returns
// ErrNotFound when the player is unknown or has no linked account.
func (s *ServerService) PlayerRemoteAddrs(ctx context.Context, playerID int64) ([]RemoteAddr, error) {
	var payload remoteAddrsPayload
	err := s.transport.Call(ctx, &Request{
		OperationID: "serverGetPlayerRemoteAddrs",
		Method:      http.MethodGet,
		Path:        "/v1/server/players/" + strconv.FormatInt(playerID, 10) + "/remote-addrs",
		APIKey:      s.apiKey,
	}, &payload)
	if err != nil {
		return nil, err
	}
	return payload.Addresses, nil
}

// StorageGet reads one player's object using server-tier authorization.
func (s *ServerService) StorageGet(ctx context.Context, playerID int64, key string) (*Object, error) {
	var object Object
	err := s.transport.Call(ctx, &Request{
		OperationID: "serverGetStorageObject",
		Method:      http.MethodGet,
		Path:        serverStoragePath(playerID, key),
		APIKey:      s.apiKey,
	}, &object)
	if err != nil {
		return nil, err
	}
	return &object, nil
}

// StoragePut creates or replaces one player's object. IfMatch enables
// optimistic concurrency in the same way as Storage.Put.
func (s *ServerService) StoragePut(ctx context.Context, playerID int64, key string, value any, opts ...PutOption) (*Object, error) {
	config := putConfig{}
	for _, opt := range opts {
		opt(&config)
	}
	var object Object
	body := value
	if body == nil {
		body = json.RawMessage("null")
	}
	err := s.transport.Call(ctx, &Request{
		OperationID: "serverPutStorageObject",
		Method:      http.MethodPut,
		Path:        serverStoragePath(playerID, key),
		APIKey:      s.apiKey,
		IfMatch:     config.ifMatch,
		Body:        body,
	}, &object)
	if err != nil {
		return nil, err
	}
	return &object, nil
}

// StorageList returns a cursor-paginated metadata-only object list for a
// player using server-tier authorization.
func (s *ServerService) StorageList(ctx context.Context, playerID int64, opts ListOptions) (*ObjectPage, error) {
	query := url.Values{}
	if opts.KeyPrefix != "" {
		query.Set("key_prefix", opts.KeyPrefix)
	}
	if opts.Limit > 0 {
		query.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		query.Set("cursor", opts.Cursor)
	}
	var page ObjectPage
	err := s.transport.Call(ctx, &Request{
		OperationID: "serverListStorageObjects",
		Method:      http.MethodGet,
		Path: "/v1/server/players/" + strconv.FormatInt(playerID, 10) +
			"/storage/objects",
		Query:  query,
		APIKey: s.apiKey,
	}, &page)
	if err != nil {
		return nil, err
	}
	return &page, nil
}

// StorageAll iterates all storage metadata for one player across cursor pages.
func (s *ServerService) StorageAll(ctx context.Context, playerID int64, opts ListOptions) iter.Seq2[StorageObjectMetadata, error] {
	return cursorSequence(opts.Cursor, func(cursor string) ([]StorageObjectMetadata, string, error) {
		opts.Cursor = cursor
		page, err := s.StorageList(ctx, playerID, opts)
		if err != nil {
			return nil, "", err
		}
		return page.Items, page.NextCursor, nil
	})
}

func serverStoragePath(playerID int64, key string) string {
	return "/v1/server/players/" + strconv.FormatInt(playerID, 10) +
		"/storage/objects/" + url.PathEscape(key)
}
