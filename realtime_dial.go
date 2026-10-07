//go:build !js

package ggscale

import (
	"context"
	"net/http"

	"github.com/coder/websocket"
)

// dialWebSocket sends the API key and session token as headers.
func (c *Client) dialWebSocket(ctx context.Context, wsURL, requestID, accessToken string) (*websocket.Conn, *http.Response, error) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+c.apiKey)
	headers.Set("X-Session-Token", accessToken)
	headers.Set("X-Request-Id", requestID)
	ua := userAgent
	var httpClient *http.Client
	if transport, ok := c.transport.(*StdNetTransport); ok {
		httpClient = transport.client()
		if transport.UserAgent != "" {
			ua = transport.UserAgent
		}
	}
	headers.Set("User-Agent", ua)

	return websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: httpClient,
		HTTPHeader: headers,
	})
}
