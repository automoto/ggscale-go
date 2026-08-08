package ggscale

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"time"
)

// Authenticator establishes a session with ggscale. Implementations
// either call /v1/auth/* (EmailPasswordAuth, CustomTokenAuth) or
// return a synthetic local session (OfflineAuth).
type Authenticator interface {
	Authenticate(ctx context.Context) (*Session, error)
}

// Session is the result of a successful Authenticate call. The
// AccessToken is sent on protected requests as X-Session-Token; the
// RefreshToken is used to mint a new AccessToken when the old one
// expires (empty for OfflineAuth, which does not refresh).
type Session struct {
	AccessToken  string
	RefreshToken string
	PlayerID     int64
	ExpiresAt    time.Time
}

// AuthService exposes the /v1/auth/* operations that are not
// authentication strategies — signup, email verification, refresh,
// and logout. Reach it via Client.Auth.
type AuthService struct {
	c         *Client
	transport Transport
	apiKey    string
}

// VerifyResult is returned by AuthService.Verify after a successful
// email-verification round trip.
type VerifyResult struct {
	PlayerID int64 `json:"player_id"`
	Verified bool  `json:"verified"`
}

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type customTokenRequest struct {
	Token string `json:"token"`
}

type steamAuthRequest struct {
	Ticket string `json:"ticket"`
}

type linkEmailRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type resetPasswordConfirmRequest struct {
	Email       string `json:"email"`
	Code        string `json:"code"`
	NewPassword string `json:"new_password"`
}

type verifyRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type sessionResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	PlayerID     int64     `json:"player_id"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (r *sessionResponse) toSession() *Session {
	return &Session{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		PlayerID:     r.PlayerID,
		ExpiresAt:    r.ExpiresAt,
	}
}

// Signup registers a new player. The server sends a verification
// email and returns 202; call Verify with the code from the email
// before the player can log in.
func (a *AuthService) Signup(ctx context.Context, email, password string) error {
	return a.transport.Call(ctx, &Request{
		OperationID: "authSignup",
		Method:      http.MethodPost,
		Path:        "/v1/auth/signup",
		APIKey:      a.apiKey,
		Body:        signupRequest{Email: email, Password: password},
	}, nil)
}

// Verify completes email verification using the code mailed to the
// player during signup.
func (a *AuthService) Verify(ctx context.Context, email, code string) (*VerifyResult, error) {
	var res VerifyResult
	err := a.transport.Call(ctx, &Request{
		OperationID: "authVerify",
		Method:      http.MethodPost,
		Path:        "/v1/auth/verify",
		APIKey:      a.apiKey,
		Body:        verifyRequest{Email: email, Code: code},
	}, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// Refresh exchanges a refresh token for a new session. The previous
// refresh token is revoked server-side.
func (a *AuthService) Refresh(ctx context.Context, refreshToken string) (*Session, error) {
	var resp sessionResponse
	err := a.transport.Call(ctx, &Request{
		OperationID: "authRefresh",
		Method:      http.MethodPost,
		Path:        "/v1/auth/refresh",
		APIKey:      a.apiKey,
		Body:        refreshRequest{RefreshToken: refreshToken},
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.toSession(), nil
}

// Logout revokes the given refresh token.
func (a *AuthService) Logout(ctx context.Context, refreshToken string) error {
	err := a.transport.Call(ctx, &Request{
		OperationID: "authLogout",
		Method:      http.MethodPost,
		Path:        "/v1/auth/logout",
		APIKey:      a.apiKey,
		Body:        refreshRequest{RefreshToken: refreshToken},
	}, nil)
	if err != nil {
		return err
	}
	// Logging out another device's token must not disturb the current player.
	// When the revoked token is the installed session, clear it and notify the
	// persistence callback so no revoked credential survives a restart.
	if a.c != nil {
		current := a.c.Session()
		if current != nil && current.RefreshToken == refreshToken {
			a.c.SetSession(nil)
		}
	}
	return nil
}

// ResendVerification sends a fresh verification code for an email signup or
// linked email identity.
func (a *AuthService) ResendVerification(ctx context.Context, email string) error {
	return a.transport.Call(ctx, &Request{
		OperationID: "resendVerification",
		Method:      http.MethodPost,
		Path:        "/v1/auth/verify/resend",
		APIKey:      a.apiKey,
		Body: struct {
			Email string `json:"email"`
		}{Email: email},
	}, nil)
}

// RequestPasswordReset emails a reset code. The endpoint deliberately does
// not reveal whether the address exists.
func (a *AuthService) RequestPasswordReset(ctx context.Context, email string) error {
	return a.transport.Call(ctx, &Request{
		OperationID: "requestPasswordReset",
		Method:      http.MethodPost,
		Path:        "/v1/auth/password-reset",
		APIKey:      a.apiKey,
		Body: struct {
			Email string `json:"email"`
		}{Email: email},
	}, nil)
}

// ConfirmPasswordReset consumes a reset code and installs a new password.
func (a *AuthService) ConfirmPasswordReset(ctx context.Context, email, code, newPassword string) error {
	return a.transport.Call(ctx, &Request{
		OperationID: "confirmPasswordReset",
		Method:      http.MethodPost,
		Path:        "/v1/auth/password-reset/confirm",
		APIKey:      a.apiKey,
		Body: resetPasswordConfirmRequest{
			Email: email, Code: code, NewPassword: newPassword,
		},
	}, nil)
}

// LinkEmail upgrades the current player to email/password sign-in without
// changing its player ID or data.
func (a *AuthService) LinkEmail(ctx context.Context, email, password string) error {
	return a.c.callProtected(ctx, &Request{
		OperationID: "linkEmail",
		Method:      http.MethodPost,
		Path:        "/v1/auth/link",
		Body:        linkEmailRequest{Email: email, Password: password},
	}, nil)
}

// LinkSteam attaches a verified Steam session-ticket identity to the current
// player without changing its player ID or data.
func (a *AuthService) LinkSteam(ctx context.Context, ticket string) error {
	return a.c.callProtected(ctx, &Request{
		OperationID: "linkSteam",
		Method:      http.MethodPost,
		Path:        "/v1/auth/link/steam",
		Body:        steamAuthRequest{Ticket: ticket},
	}, nil)
}

// ChangePassword changes the current player's password and revokes all
// refresh tokens. The current access token remains valid until it expires.
func (a *AuthService) ChangePassword(ctx context.Context, currentPassword, newPassword string) error {
	return a.c.callProtected(ctx, &Request{
		OperationID: "changePassword",
		Method:      http.MethodPost,
		Path:        "/v1/auth/password",
		Body: changePasswordRequest{
			CurrentPassword: currentPassword, NewPassword: newPassword,
		},
	}, nil)
}

// Disable deactivates the current player and clears the local session after
// the server revokes it. Password may be empty for anonymous players.
func (a *AuthService) Disable(ctx context.Context, password string) error {
	err := a.c.callProtected(ctx, &Request{
		OperationID: "disablePlayer",
		Method:      http.MethodPost,
		Path:        "/v1/auth/disable",
		Body: struct {
			Password string `json:"password,omitempty"`
		}{Password: password},
	}, nil)
	if err != nil {
		return err
	}
	a.c.SetSession(nil)
	return nil
}

// EmailPasswordAuth authenticates via POST /v1/auth/login.
type EmailPasswordAuth struct {
	transport Transport
	apiKey    string
	email     string
	password  string
}

// NewEmailPasswordAuth builds an Authenticator that exchanges
// (email, password) for a session via POST /v1/auth/login.
func NewEmailPasswordAuth(t Transport, apiKey, email, password string) *EmailPasswordAuth {
	return &EmailPasswordAuth{transport: t, apiKey: apiKey, email: email, password: password}
}

// Authenticate implements Authenticator.
func (a *EmailPasswordAuth) Authenticate(ctx context.Context) (*Session, error) {
	var resp sessionResponse
	err := a.transport.Call(ctx, &Request{
		OperationID: "authLogin",
		Method:      http.MethodPost,
		Path:        "/v1/auth/login",
		APIKey:      a.apiKey,
		Body:        loginRequest{Email: a.email, Password: a.password},
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.toSession(), nil
}

// CustomTokenAuth authenticates via POST /v1/auth/custom-token. The
// supplied token is an HS256-signed JWT minted by the tenant carrying
// an external_id claim; ggscale verifies it and issues its own session.
type CustomTokenAuth struct {
	transport Transport
	apiKey    string
	token     string
}

// NewCustomTokenAuth builds an Authenticator that exchanges a tenant-
// signed JWT for a ggscale session.
func NewCustomTokenAuth(t Transport, apiKey, signedToken string) *CustomTokenAuth {
	return &CustomTokenAuth{transport: t, apiKey: apiKey, token: signedToken}
}

// Authenticate implements Authenticator.
func (a *CustomTokenAuth) Authenticate(ctx context.Context) (*Session, error) {
	var resp sessionResponse
	err := a.transport.Call(ctx, &Request{
		OperationID: "authCustomToken",
		Method:      http.MethodPost,
		Path:        "/v1/auth/custom-token",
		APIKey:      a.apiKey,
		Body:        customTokenRequest{Token: a.token},
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.toSession(), nil
}

// SteamAuth authenticates with a Steamworks session ticket.
type SteamAuth struct {
	transport Transport
	apiKey    string
	ticket    string
}

// NewSteamAuth builds an authenticator for a Steamworks session ticket.
func NewSteamAuth(t Transport, apiKey, ticket string) *SteamAuth {
	return &SteamAuth{transport: t, apiKey: apiKey, ticket: ticket}
}

// Authenticate exchanges the Steamworks ticket for a ggscale player session.
func (a *SteamAuth) Authenticate(ctx context.Context) (*Session, error) {
	var resp sessionResponse
	err := a.transport.Call(ctx, &Request{
		OperationID: "authSteam",
		Method:      http.MethodPost,
		Path:        "/v1/auth/steam",
		APIKey:      a.apiKey,
		Body:        steamAuthRequest{Ticket: a.ticket},
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp.toSession(), nil
}

// OfflineAuth returns a synthetic local session and never calls the
// API. Intended for LAN parties and self-hosted installs without a
// central directory. The PlayerID is a per-process random int64;
// persistence is out of scope.
type OfflineAuth struct {
	session *Session
}

// NewOfflineAuth builds an Authenticator that issues a synthetic
// session derived from crypto/rand. The session has an empty refresh
// token (OfflineAuth never refreshes) and an effectively-infinite
// expiry.
func NewOfflineAuth() *OfflineAuth {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read on a healthy OS does not fail; if it
		// does, there's nothing useful to do here.
		panic("ggscale: crypto/rand failed: " + err.Error())
	}
	// Mask the sign bit so PlayerID is always positive.
	id := int64(binary.BigEndian.Uint64(b[:]) & 0x7fffffffffffffff)

	var tokBytes [16]byte
	if _, err := rand.Read(tokBytes[:]); err != nil {
		panic("ggscale: crypto/rand failed: " + err.Error())
	}
	return &OfflineAuth{
		session: &Session{
			AccessToken: "offline-" + hex.EncodeToString(tokBytes[:]),
			PlayerID:    id,
			ExpiresAt:   time.Now().Add(100 * 365 * 24 * time.Hour),
		},
	}
}

// Authenticate implements Authenticator. Returns the session generated
// at construction; repeated calls return the same identity.
func (a *OfflineAuth) Authenticate(_ context.Context) (*Session, error) {
	return a.session, nil
}

// Compile-time interface checks.
var (
	_ Authenticator = (*EmailPasswordAuth)(nil)
	_ Authenticator = (*CustomTokenAuth)(nil)
	_ Authenticator = (*SteamAuth)(nil)
	_ Authenticator = (*OfflineAuth)(nil)
	_ Authenticator = (*AnonymousAuth)(nil)
)
