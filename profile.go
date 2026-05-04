package ggscale

import (
	"context"
	"net/http"
	"time"
)

// ProfileService exposes the /v1/profile endpoints. Reach it via
// Client.Profile.
type ProfileService struct {
	c *Client
}

// Profile is the calling end-user's profile. EmailVerifiedAt is nil
// until the user confirms their email; the SDK distinguishes
// "unverified" from a zero time so callers can branch on a real
// nil check.
type Profile struct {
	ID              int64      `json:"id"`
	ProjectID       int64      `json:"project_id"`
	ExternalID      string     `json:"external_id"`
	Email           string     `json:"email,omitempty"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// ProfilePatch is the body of a PATCH /v1/profile call. Email is a
// pointer so the SDK can distinguish "leave alone" from "set to
// empty"; the server currently rejects empty patches with 400.
type ProfilePatch struct {
	Email *string `json:"email,omitempty"`
}

// Get returns the calling end-user's profile.
func (p *ProfileService) Get(ctx context.Context) (*Profile, error) {
	var prof Profile
	err := p.c.callProtected(ctx, &Request{
		Method: http.MethodGet,
		Path:   "/v1/profile",
	}, &prof)
	if err != nil {
		return nil, err
	}
	return &prof, nil
}

// Update applies a patch to the profile. Setting Email triggers a
// verification round-trip server-side (clears EmailVerifiedAt and
// mails a fresh verification token). The server returns 202; this
// method returns nil on success.
func (p *ProfileService) Update(ctx context.Context, patch ProfilePatch) error {
	return p.c.callProtected(ctx, &Request{
		Method: http.MethodPatch,
		Path:   "/v1/profile",
		Body:   patch,
	}, nil)
}
