//go:build js

package ggscale

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
)

// dialWebSocket gets a new one-time ticket and sends no headers, because a
// browser cannot set them on a WebSocket. CreateTicket refreshes the session
// and retries a 401 itself. The browser does not show the handshake status,
// so a rejected dial is not retried here.
func (c *Client) dialWebSocket(ctx context.Context, wsURL, _, _ string) (*websocket.Conn, *http.Response, error) {
	ticket, err := c.Realtime.CreateTicket(ctx)
	if err != nil {
		return nil, nil, err
	}
	return websocket.Dial(ctx, realtimeTicketURL(wsURL, ticket.Ticket), nil)
}
