package ggscale

import (
	"context"
	"net/http"
)

// HealthService exposes the unauthenticated liveness endpoint.
type HealthService struct{ transport Transport }

// Health reports server liveness and build identity.
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// Get reads the unauthenticated server health endpoint.
func (s *HealthService) Get(ctx context.Context) (*Health, error) {
	var health Health
	err := s.transport.Call(ctx, &Request{
		OperationID: "healthz",
		Method:      http.MethodGet,
		Path:        "/v1/healthz",
	}, &health)
	if err != nil {
		return nil, err
	}
	return &health, nil
}
