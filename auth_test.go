package ggscale

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTransport captures the last Request it received and returns the
// canned out body. Tests use it to assert AuthService and Authenticator
// build the right Request.
type fakeTransport struct {
	gotReq    *Request
	respond   func(*Request) (any, error)
	callCount int
}

func (f *fakeTransport) Call(ctx context.Context, req *Request, out any) error {
	f.callCount++
	f.gotReq = req
	resp, err := f.respond(req)
	if err != nil {
		return err
	}
	if out == nil || resp == nil {
		return nil
	}
	buf, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return json.Unmarshal(buf, out)
}

func cannedSession() map[string]any {
	return map[string]any{
		"access_token":  "jwt.access.token",
		"refresh_token": "opaque-refresh-hex",
		"end_user_id":   int64(42),
		"expires_at":    time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestEmailPasswordAuth_Authenticate_calls_login_with_credentials(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return cannedSession(), nil },
	}
	a := NewEmailPasswordAuth(ft, "ggs_key", "demo@example.com", "hunter2hunter2")

	sess, err := a.Authenticate(context.Background())
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/auth/login", ft.gotReq.Path)
	assert.Equal(t, "ggs_key", ft.gotReq.APIKey)
	assert.Empty(t, ft.gotReq.SessionToken)

	body, ok := ft.gotReq.Body.(loginRequest)
	require.True(t, ok)
	assert.Equal(t, "demo@example.com", body.Email)
	assert.Equal(t, "hunter2hunter2", body.Password)

	assert.Equal(t, "jwt.access.token", sess.AccessToken)
	assert.Equal(t, "opaque-refresh-hex", sess.RefreshToken)
	assert.Equal(t, int64(42), sess.EndUserID)
}

func TestEmailPasswordAuth_Authenticate_propagates_unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid credentials"))
	}))
	defer srv.Close()

	a := NewEmailPasswordAuth(&StdNetTransport{BaseURL: srv.URL}, "k", "x", "y")
	_, err := a.Authenticate(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnauthorized))
}

func TestCustomTokenAuth_Authenticate_calls_custom_token(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return cannedSession(), nil },
	}
	a := NewCustomTokenAuth(ft, "ggs_key", "tenant-signed-jwt")

	sess, err := a.Authenticate(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "/v1/auth/custom-token", ft.gotReq.Path)
	body, ok := ft.gotReq.Body.(customTokenRequest)
	require.True(t, ok)
	assert.Equal(t, "tenant-signed-jwt", body.Token)
	assert.Equal(t, int64(42), sess.EndUserID)
}

func TestOfflineAuth_returns_synthetic_session_with_no_transport(t *testing.T) {
	a := NewOfflineAuth()

	sess1, err := a.Authenticate(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, sess1.AccessToken)
	assert.Empty(t, sess1.RefreshToken)
	assert.NotZero(t, sess1.EndUserID)
	assert.True(t, sess1.ExpiresAt.After(time.Now().Add(50*365*24*time.Hour)))

	// Stable identity across calls on the same OfflineAuth instance.
	sess2, err := a.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, sess1.EndUserID, sess2.EndUserID)
	assert.Equal(t, sess1.AccessToken, sess2.AccessToken)
}

func TestOfflineAuth_two_instances_get_distinct_ids(t *testing.T) {
	a := NewOfflineAuth()
	b := NewOfflineAuth()
	sa, _ := a.Authenticate(context.Background())
	sb, _ := b.Authenticate(context.Background())
	assert.NotEqual(t, sa.EndUserID, sb.EndUserID)
}

func TestAuthService_Signup_posts_to_signup_endpoint(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return nil, nil },
	}
	svc := &AuthService{transport: ft, apiKey: "k"}

	err := svc.Signup(context.Background(), "demo@example.com", "hunter2hunter2")
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, ft.gotReq.Method)
	assert.Equal(t, "/v1/auth/signup", ft.gotReq.Path)
	assert.Equal(t, "k", ft.gotReq.APIKey)
}

func TestAuthService_Verify_returns_result(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{"end_user_id": int64(7), "verified": true}, nil
		},
	}
	svc := &AuthService{transport: ft, apiKey: "k"}

	res, err := svc.Verify(context.Background(), "verify-token")
	require.NoError(t, err)
	assert.Equal(t, int64(7), res.EndUserID)
	assert.True(t, res.Verified)

	body, ok := ft.gotReq.Body.(verifyRequest)
	require.True(t, ok)
	assert.Equal(t, "verify-token", body.Token)
}

func TestAuthService_Refresh_rotates_token(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) {
			return map[string]any{
				"access_token":  "new.jwt",
				"refresh_token": "rotated-refresh",
				"end_user_id":   int64(42),
				"expires_at":    time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
			}, nil
		},
	}
	svc := &AuthService{transport: ft, apiKey: "k"}

	sess, err := svc.Refresh(context.Background(), "old-refresh")
	require.NoError(t, err)
	assert.Equal(t, "new.jwt", sess.AccessToken)
	assert.Equal(t, "rotated-refresh", sess.RefreshToken)

	body, ok := ft.gotReq.Body.(refreshRequest)
	require.True(t, ok)
	assert.Equal(t, "old-refresh", body.RefreshToken)
}

func TestAuthService_Logout_204(t *testing.T) {
	ft := &fakeTransport{
		respond: func(*Request) (any, error) { return nil, nil },
	}
	svc := &AuthService{transport: ft, apiKey: "k"}

	err := svc.Logout(context.Background(), "refresh")
	require.NoError(t, err)
	assert.Equal(t, "/v1/auth/logout", ft.gotReq.Path)
	body, ok := ft.gotReq.Body.(refreshRequest)
	require.True(t, ok)
	assert.Equal(t, "refresh", body.RefreshToken)
}
