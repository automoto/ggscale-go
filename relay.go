package ggscale

import (
	"context"
	"net/http"
)

// RelayService exposes the /v1/relay/credentials endpoint. Reach it via
// Client.Relay.
type RelayService struct {
	c *Client
}

// Credentials are TURN-REST credentials minted by the server. Pass them
// to your TURN client (e.g. pion/turn) to authenticate against the relay.
type Credentials struct {
	Username string   `json:"username"`
	Password string   `json:"password"`
	TTL      int64    `json:"ttl"`
	Realm    string   `json:"realm"`
	URLs     []string `json:"urls,omitempty"`
}

// GetCredentials returns a fresh TURN credential pair scoped to the
// current player. Requires a player session.
func (r *RelayService) GetCredentials(ctx context.Context) (*Credentials, error) {
	var creds Credentials
	err := r.c.callProtected(ctx, &Request{
		Method: http.MethodPost,
		Path:   "/v1/relay/credentials",
	}, &creds)
	if err != nil {
		return nil, err
	}
	return &creds, nil
}
