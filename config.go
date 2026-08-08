package ggscale

import (
	"context"
	"encoding/json"
	"net/http"
)

// ConfigService exposes project remote config. It requires only the project-
// pinned API key, so callers may use it before player login.
type ConfigService struct {
	transport Transport
	apiKey    string
}

// RemoteConfig is one config fetch. Values is nil when NotModified is true.
type RemoteConfig struct {
	Values       map[string]json.RawMessage
	ETag         string
	CacheControl string
	NotModified  bool
}

// Get fetches the project's arbitrary JSON config. Pass a prior ETag to make
// a conditional request; a 304 is returned as NotModified without an error.
func (s *ConfigService) Get(ctx context.Context, etag string) (*RemoteConfig, error) {
	values := map[string]json.RawMessage{}
	metadata := &ResponseMetadata{}
	err := s.transport.Call(ctx, &Request{
		OperationID:      "getRemoteConfig",
		Method:           http.MethodGet,
		Path:             "/v1/config",
		APIKey:           s.apiKey,
		IfNoneMatch:      etag,
		AllowNotModified: true,
		Response:         metadata,
	}, &values)
	if err != nil {
		return nil, err
	}
	if metadata.NotModified {
		values = nil
		if metadata.ETag == "" {
			metadata.ETag = etag
		}
	}
	return &RemoteConfig{
		Values:       values,
		ETag:         metadata.ETag,
		CacheControl: metadata.CacheControl,
		NotModified:  metadata.NotModified,
	}, nil
}
