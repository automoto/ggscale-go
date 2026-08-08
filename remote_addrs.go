package ggscale

import (
	"context"
	"net/http"
)

// RemoteAddr is one typed connectivity address a player publishes so
// peers (or game servers) can reach them directly: a LAN IP, public IP,
// DNS name, or iroh node address. Scope is derived server-side and
// ignored on writes.
type RemoteAddr struct {
	Type    string `json:"type"`
	Scope   string `json:"scope,omitempty"`
	Address string `json:"address"`
}

type remoteAddrsPayload struct {
	Addresses []RemoteAddr `json:"addresses"`
}

// AccountService exposes the /v1/account/* endpoints operating on the
// calling player's linked account. Reach it via Client.Account.
//
// These endpoints require a linked (non-anonymous) account; the server
// answers 403 (ErrForbidden) for anonymous players.
type AccountService struct {
	c *Client
}

// RemoteAddrs returns the calling player's published remote addresses.
func (a *AccountService) RemoteAddrs(ctx context.Context) ([]RemoteAddr, error) {
	var payload remoteAddrsPayload
	err := a.c.callProtected(ctx, &Request{
		OperationID: "getAccountRemoteAddrs",
		Method:      http.MethodGet,
		Path:        "/v1/account/remote-addrs",
	}, &payload)
	if err != nil {
		return nil, err
	}
	return payload.Addresses, nil
}

// SetRemoteAddrs replaces the calling player's published address set
// (at most one address per type: ip_lan, ip_public, dns, iroh) and
// returns the canonical list as stored, with server-derived scopes.
func (a *AccountService) SetRemoteAddrs(ctx context.Context, addrs []RemoteAddr) ([]RemoteAddr, error) {
	var payload remoteAddrsPayload
	err := a.c.callProtected(ctx, &Request{
		OperationID: "putAccountRemoteAddrs",
		Method:      http.MethodPut,
		Path:        "/v1/account/remote-addrs",
		Body:        remoteAddrsPayload{Addresses: addrs},
	}, &payload)
	if err != nil {
		return nil, err
	}
	return payload.Addresses, nil
}
