//go:build js

package ggscale

import (
	"context"
	"encoding/json"
	"errors"
)

const realtimeOperationID = "realtimeWebSocket"

type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type RealtimeClient struct{}

func (c *Client) DialRealtime(context.Context) (*RealtimeClient, error) {
	return nil, errors.New("ggscale: realtime websocket is unavailable in browser builds")
}

func (*RealtimeClient) ReadMessage(context.Context) (Message, error) {
	return Message{}, ErrConnectionClosed
}

func (*RealtimeClient) Close() error { return nil }
