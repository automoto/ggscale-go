package ggscale

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStdNetTransportDefaultClientUsesContextAndStageTimeouts(t *testing.T) {
	t.Parallel()

	transport := &StdNetTransport{}
	client := transport.client()
	assert.Zero(t, client.Timeout, "the per-call context owns the overall timeout")
	httpTransport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Equal(t, defaultTLSHandshakeTimeout, httpTransport.TLSHandshakeTimeout)
	assert.Equal(t, defaultResponseHeaderTimeout, httpTransport.ResponseHeaderTimeout)
}

func TestStdNetTransportCustomClientWithoutTransportKeepsStageTimeouts(t *testing.T) {
	t.Parallel()

	transport := &StdNetTransport{Client: &http.Client{Timeout: 45 * time.Second}}
	client := transport.client()
	assert.Equal(t, 45*time.Second, client.Timeout)
	httpTransport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Equal(t, defaultTLSHandshakeTimeout, httpTransport.TLSHandshakeTimeout)
	assert.Equal(t, defaultResponseHeaderTimeout, httpTransport.ResponseHeaderTimeout)
}

func TestWithCallTimeoutPreservesCallerDeadlineByDefault(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer parentCancel()
	parentDeadline, ok := parent.Deadline()
	require.True(t, ok)

	ctx, cancel := withCallTimeout(parent, 0)
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	assert.Equal(t, parentDeadline, deadline)
}

func TestWithCallTimeoutAddsFallbackOnlyWithoutCallerDeadline(t *testing.T) {
	started := time.Now()
	ctx, cancel := withCallTimeout(context.Background(), 0)
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	assert.WithinDuration(t, started.Add(defaultTimeout), deadline, time.Second)
}

func TestDefaultResponseBodyCapLeavesRoomForStorageOverrides(t *testing.T) {
	assert.Equal(t, int64(64<<20), (&StdNetTransport{}).maxBodyBytes())
}
