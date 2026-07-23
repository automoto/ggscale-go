package ggscale

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// StorageService exposes the /v1/storage/objects/* endpoints. Reach
// it via Client.Storage.
type StorageService struct {
	c *Client
}

// Object is a single key/value entry returned by Get and Put. Value is
// the raw JSON the caller stored; unmarshal it into a concrete type with
// json.Unmarshal(obj.Value, &dst).
//
// List does not return values — it returns lightweight
// StorageObjectMetadata. Call Get on a key to read its value.
type Object struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	Version   int64           `json:"version"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// StorageObjectMetadata is one entry in a List page. The server returns
// metadata only — no value — so listings stay cheap regardless of object
// size. SizeBytes is the stored value's size in bytes; read the value
// itself with StorageService.Get.
type StorageObjectMetadata struct {
	Key       string    `json:"key"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	SizeBytes int64     `json:"size_bytes"`
}

// ObjectPage is one page of List results. NextCursor is empty when
// there are no further pages.
type ObjectPage struct {
	Items      []StorageObjectMetadata `json:"items"`
	NextCursor string                  `json:"next_cursor"`
}

// ListOptions configures Storage.List. Limit defaults server-side to
// 50 and is capped at 100; Cursor is the NextCursor from a prior page
// (empty for the first call).
type ListOptions struct {
	KeyPrefix string
	Limit     int
	Cursor    string
}

// PutOption tweaks a Storage.Put call. Use IfMatch to enforce OCC.
type PutOption func(*putConfig)

type putConfig struct {
	ifMatch string
}

// IfMatch makes the Put conditional on the object currently being at
// the given version. The server returns 412 (surfaced as
// errors.Is(err, ErrConflict)) on mismatch.
func IfMatch(version int64) PutOption {
	return func(c *putConfig) {
		c.ifMatch = strconv.FormatInt(version, 10)
	}
}

// Get returns the object stored at key, or wraps ErrNotFound if the
// key has been deleted or never existed.
func (s *StorageService) Get(ctx context.Context, key string) (*Object, error) {
	var obj Object
	err := s.c.callProtected(ctx, &Request{
		Method: http.MethodGet,
		Path:   storagePath(key),
	}, &obj)
	if err != nil {
		return nil, err
	}
	return &obj, nil
}

// Put writes value at key. The server stores value as raw JSON; the
// caller can pass any value json.Marshal can handle (struct, map,
// json.RawMessage, etc.). On a successful write the returned Object
// carries the new Version.
func (s *StorageService) Put(ctx context.Context, key string, value any, opts ...PutOption) (*Object, error) {
	cfg := putConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	var obj Object
	err := s.c.callProtected(ctx, &Request{
		Method:  http.MethodPut,
		Path:    storagePath(key),
		Body:    value,
		IfMatch: cfg.ifMatch,
	}, &obj)
	if err != nil {
		return nil, err
	}
	return &obj, nil
}

// Delete soft-deletes the object at key. A subsequent Get returns
// ErrNotFound.
func (s *StorageService) Delete(ctx context.Context, key string) error {
	return s.c.callProtected(ctx, &Request{
		Method: http.MethodDelete,
		Path:   storagePath(key),
	}, nil)
}

// List paginates through the calling player's objects, oldest first.
// Filter by key prefix via opts.KeyPrefix. Loop until NextCursor is
// empty. Each item is StorageObjectMetadata (key, version, updated_at,
// size_bytes) — not the value; call Get to read a value.
func (s *StorageService) List(ctx context.Context, opts ListOptions) (*ObjectPage, error) {
	q := url.Values{}
	if opts.KeyPrefix != "" {
		q.Set("key_prefix", opts.KeyPrefix)
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		q.Set("cursor", opts.Cursor)
	}
	var page ObjectPage
	err := s.c.callProtected(ctx, &Request{
		Method: http.MethodGet,
		Path:   "/v1/storage/objects",
		Query:  q,
	}, &page)
	if err != nil {
		return nil, err
	}
	return &page, nil
}

func storagePath(key string) string {
	return "/v1/storage/objects/" + url.PathEscape(key)
}
