package ggscale

import (
	"context"
	"net/http"
	"net/url"
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

// RelayOption configures a relay credential request.
type RelayOption func(*relayOptions)

type relayOptions struct {
	matchID string
}

// WithMatch scopes the credential request to a match: the server verifies the
// caller is in that match's roster before issuing (403 otherwise). Use it
// after matchmaking so relay credential requests are authorized against a real
// match. Unqualified requests remain valid for non-matchmade peer-to-peer.
func WithMatch(matchID string) RelayOption {
	return func(o *relayOptions) { o.matchID = matchID }
}

// GetCredentials returns a fresh TURN credential pair scoped to the current
// player. Requires a player session. Pass WithMatch to additionally prove
// membership of a match.
func (r *RelayService) GetCredentials(ctx context.Context, opts ...RelayOption) (*Credentials, error) {
	var o relayOptions
	for _, fn := range opts {
		fn(&o)
	}
	req := &Request{
		Method: http.MethodPost,
		Path:   "/v1/relay/credentials",
	}
	if o.matchID != "" {
		req.Query = url.Values{"match_id": {o.matchID}}
	}
	var creds Credentials
	if err := r.c.callProtected(ctx, req, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}
