package ggscale

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ReconnectPolicy controls WebSocket reconnects after retryable closures.
// Reconnect is on by default. The server does not replay events sent during
// an outage, so use OnRealtimeReconnect to read the authoritative state again.
// Zero values select five attempts, a random 0-5 second first delay, then
// capped full-jitter exponential backoff. A handshake Retry-After is followed.
type ReconnectPolicy struct {
	// Disabled turns reconnect off: a dropped connection ends ReadMessage.
	Disabled      bool
	MaxAttempts   int
	FirstDelayMax time.Duration
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	Jitter        func(cap time.Duration) time.Duration
}

// RealtimeDialError describes a failed WebSocket opening handshake.
type RealtimeDialError struct {
	Status     int
	RetryAfter time.Duration
	Err        error
}

func (e *RealtimeDialError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("ggscale: websocket handshake status %d: %v", e.Status, e.Err)
	}
	return fmt.Sprintf("ggscale: websocket handshake: %v", e.Err)
}

func (e *RealtimeDialError) Unwrap() error { return e.Err }

// RealtimeError describes a terminal or reconnect-exhausted WebSocket read.
type RealtimeError struct {
	CloseCode int
	Err       error
}

func (e *RealtimeError) Error() string {
	if e.CloseCode >= 0 {
		return fmt.Sprintf("ggscale: realtime connection closed (%d): %v", e.CloseCode, e.Err)
	}
	return fmt.Sprintf("ggscale: realtime connection closed: %v", e.Err)
}

func (e *RealtimeError) Unwrap() error { return e.Err }

// Is makes all terminal realtime failures match ErrConnectionClosed while
// preserving the underlying cause through Unwrap.
func (e *RealtimeError) Is(target error) bool {
	return target == ErrConnectionClosed
}

// ErrConnectionClosed is returned directly for a terminal WebSocket closure;
// it also matches reconnect-exhausted RealtimeError values via errors.Is.
var ErrConnectionClosed = errors.New("ggscale: realtime connection closed")

type realtimeReconnectHook func(context.Context, *Client)
