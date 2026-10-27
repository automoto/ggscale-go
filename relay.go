package ggscale

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// RelayService exposes the /v1/relay/credentials endpoint. Reach it via
// Client.Relay.
type RelayService struct {
	c *Client

	// after replaces time.After in tests.
	after func(time.Duration) <-chan time.Time
}

// Credentials are TURN-REST credentials minted by the server. Pass them
// to your TURN client (e.g. pion/turn) to authenticate against the relay.
type Credentials struct {
	Username string   `json:"username"`
	Password string   `json:"password"`
	TTL      int64    `json:"ttl"`
	Realm    string   `json:"realm"`
	URLs     []string `json:"urls,omitempty"`
	STUNURLs []string `json:"stun_urls,omitempty"`
	// Region is the region the URLs were chosen from, or "" when they span
	// regions (no region requested, or none eligible in it).
	Region string `json:"region"`
}

// RelayOption configures a relay credential request.
type RelayOption func(*relayOptions)

type relayOptions struct {
	matchID string
	region  string
}

// WithMatch scopes the credential request to a match: the server verifies the
// caller is in that match's roster before issuing (403 otherwise). Use it
// after matchmaking so relay credential requests are authorized against a real
// match. Unqualified requests remain valid for non-matchmade peer-to-peer.
func WithMatch(matchID string) RelayOption {
	return func(o *relayOptions) { o.matchID = matchID }
}

// WithRegion asks for relays in region (for example "us-east"), matched
// case-insensitively. When the region has no eligible relay, the server
// returns relays from every region and sets Credentials.Region to "".
func WithRegion(region string) RelayOption {
	return func(o *relayOptions) { o.region = region }
}

// GetCredentials returns a fresh TURN credential pair scoped to the current
// player. Requires a player session. Pass WithMatch to additionally prove
// membership of a match, and WithRegion to prefer nearby relays.
//
// When no relay is eligible the server answers 503 with Retry-After; the
// request is retried within the client's RetryPolicy and the context deadline.
// Minting credentials changes no server state, so the retry is safe.
func (r *RelayService) GetCredentials(ctx context.Context, opts ...RelayOption) (*Credentials, error) {
	var o relayOptions
	for _, fn := range opts {
		fn(&o)
	}
	req := &Request{
		OperationID: "issueRelayCredentials",
		Method:      http.MethodPost,
		Path:        "/v1/relay/credentials",
		ReplaySafe:  true,
	}
	query := url.Values{}
	if o.matchID != "" {
		query.Set("match_id", o.matchID)
	}
	if o.region != "" {
		query.Set("region", o.region)
	}
	if len(query) > 0 {
		req.Query = query
	}
	var creds Credentials
	if err := r.c.callProtected(ctx, req, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

const (
	relayRetryMin = time.Second
	relayRetryMax = 30 * time.Second
)

// KeepFresh fetches credentials now and again at 80% of each TTL, passing every
// new set to onCredentials until ctx is done. Update the ICE servers of your
// peer connections there: the relay refuses an allocation Refresh signed with
// an expired credential, so a session that keeps its first credentials loses
// its relay allocation within minutes. Renewed credentials keep controlling the
// same allocation.
//
// Errors that can pass (no eligible relay, rate limits, network failures) are
// retried with backoff, honoring Retry-After. Any other error is returned. On
// cancellation KeepFresh returns ctx.Err().
func (r *RelayService) KeepFresh(ctx context.Context, onCredentials func(*Credentials), opts ...RelayOption) error {
	backoff := relayRetryMin
	for {
		creds, err := r.GetCredentials(ctx, opts...)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		var wait time.Duration
		switch {
		case err == nil:
			onCredentials(creds)
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			backoff = relayRetryMin
			wait = max(time.Duration(creds.TTL)*time.Second*8/10, relayRetryMin)
		case relayRetryable(err):
			wait = backoff
			var apiErr *Error
			if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
				wait = apiErr.RetryAfter
			}
			backoff = min(backoff*2, relayRetryMax)
		default:
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.wait(wait):
		}
	}
}

func (r *RelayService) wait(d time.Duration) <-chan time.Time {
	if r.after != nil {
		return r.after(d)
	}
	return time.After(d)
}

// relayRetryable reports whether a failed credential request can succeed later:
// a transport failure, or a 408, 429 or 5xx response.
func relayRetryable(err error) bool {
	var reqErr *RequestError
	if errors.As(err, &reqErr) {
		return reqErr.Kind != FailureEncode && reqErr.Kind != FailureDecode
	}
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusRequestTimeout || apiErr.Status == http.StatusTooManyRequests ||
			apiErr.Status >= http.StatusInternalServerError
	}
	return false
}
