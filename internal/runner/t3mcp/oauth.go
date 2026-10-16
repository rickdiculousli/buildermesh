package t3mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrRefused is a sign-in the operator (or T3) refused.
var ErrRefused = errors.New("T3 sign-in refused")

// Metadata is the part of a server's OAuth authorization server document
// the login uses.
type Metadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint"`
}

func getJSON(ctx context.Context, hc *http.Client, method, u string, body io.Reader, contentType string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	return doJSON(hc, req, out)
}

func doJSON(hc *http.Client, req *http.Request, out any) error {
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error != "" {
			return fmt.Errorf("%s: %s: %s", req.URL.Path, e.Error, e.Description)
		}
		return fmt.Errorf("%s: HTTP %d: %s", req.URL.Path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.Unmarshal(b, out)
}

// Discover reads server's /.well-known/oauth-authorization-server.
func Discover(ctx context.Context, hc *http.Client, server string) (Metadata, error) {
	var md Metadata
	if err := getJSON(ctx, hc, http.MethodGet, strings.TrimRight(server, "/")+"/.well-known/oauth-authorization-server", nil, "", &md); err != nil {
		return Metadata{}, fmt.Errorf("T3 sign-in discovery: %w", err)
	}
	if md.AuthorizationEndpoint == "" || md.TokenEndpoint == "" || md.RegistrationEndpoint == "" {
		return Metadata{}, errors.New("T3 sign-in discovery: the server names no authorization, token or registration endpoint")
	}
	return md, nil
}

// Register registers a public client (no secret) named name with one
// redirect URI and returns its client id.
func Register(ctx context.Context, hc *http.Client, md Metadata, name, redirect string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"client_name": name, "redirect_uris": []string{redirect}, "grant_types": []string{"authorization_code"},
		"response_types": []string{"code"}, "token_endpoint_auth_method": "none",
	})
	var v struct {
		ClientID string `json:"client_id"`
	}
	if err := getJSON(ctx, hc, http.MethodPost, md.RegistrationEndpoint, bytes.NewReader(body), "application/json", &v); err != nil {
		return "", fmt.Errorf("T3 client registration: %w", err)
	}
	if v.ClientID == "" {
		return "", errors.New("T3 client registration: no client_id")
	}
	return v.ClientID, nil
}

// PKCE is an S256 code verifier and its challenge.
type PKCE struct{ Verifier, Challenge string }

func random(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// NewPKCE makes a 43-character verifier and its S256 challenge.
func NewPKCE() (PKCE, error) {
	v, err := random(32)
	if err != nil {
		return PKCE{}, err
	}
	sum := sha256.Sum256([]byte(v))
	return PKCE{Verifier: v, Challenge: base64.RawURLEncoding.EncodeToString(sum[:])}, nil
}

// NewState is a random OAuth state.
func NewState() (string, error) { return random(16) }

// AuthorizeURL is the page the operator opens to approve the client. It
// names no resource: T3 takes the resource from the origin the browser
// reached it on, which a tunnel may rename.
func AuthorizeURL(md Metadata, clientID, redirect, state string, p PKCE) string {
	q := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {state},
		"code_challenge": {p.Challenge}, "code_challenge_method": {"S256"},
		"scope": {"orchestration:read orchestration:operate"},
	}
	return md.AuthorizationEndpoint + "?" + q.Encode()
}

// ParseRedirect reads the code and iss from the URL the browser was sent
// back to, refusing a state other than state and an OAuth error.
func ParseRedirect(raw, state string) (code, iss string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", fmt.Errorf("redirect URL: %w", err)
	}
	q := u.Query()
	if e := q.Get("error"); e != "" {
		if d := q.Get("error_description"); d != "" {
			e += ": " + d
		}
		return "", "", fmt.Errorf("%w: %s", ErrRefused, e)
	}
	if q.Get("state") != state {
		return "", "", errors.New("redirect URL: state does not match this login (an old link?)")
	}
	if q.Get("code") == "" {
		return "", "", errors.New("redirect URL: no code")
	}
	return q.Get("code"), q.Get("iss"), nil
}

// Exchange trades code for a token at server and reads the session's
// ceiling and expiry from the token's claims. T3 binds a code to the origin
// the browser approved it on, and names that origin as iss; when iss
// differs from server the request carries iss's host, so a code approved
// through a tunnel at localhost still exchanges at 127.0.0.1.
func Exchange(ctx context.Context, hc *http.Client, md Metadata, server, clientID, redirect, code, verifier, iss string, now time.Time) (Token, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect},
		"client_id": {clientID}, "code_verifier": {verifier}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, md.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if iu, err := url.Parse(iss); err == nil && iss != "" && iu.Host != "" && iu.Host != req.URL.Host {
		req.Host = iu.Host
	}
	var v struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	if err := doJSON(hc, req, &v); err != nil {
		return Token{}, fmt.Errorf("T3 token exchange: %w", err)
	}
	if v.AccessToken == "" {
		return Token{}, errors.New("T3 token exchange: no access_token")
	}
	t := Token{Server: strings.TrimRight(server, "/"), Token: v.AccessToken, Scopes: strings.Fields(v.Scope),
		ExpiresAt: now.Add(time.Duration(v.ExpiresIn) * time.Second).UTC().Truncate(time.Second)}
	c := Claims(v.AccessToken)
	if c.Exp > 0 {
		t.ExpiresAt = time.UnixMilli(c.Exp).UTC().Truncate(time.Second)
	}
	if len(t.Scopes) == 0 {
		t.Scopes = c.Scopes
	}
	t.Access = c.Rtc
	if t.Access == "" {
		t.Access = "read-only"
		for _, s := range t.Scopes {
			if s == "orchestration:operate" {
				t.Access = "approval-required" // T3's ceiling for a session without one
			}
		}
	}
	return t, nil
}

// TokenClaims are the claims T3 puts in a session token's first segment.
type TokenClaims struct {
	Scopes []string `json:"scopes"`
	Rtc    string   `json:"rtc"` // runtime-mode ceiling
	Exp    int64    `json:"exp"` // unix ms
}

// Claims decodes a T3 session token's claims; zero when it can't.
func Claims(token string) TokenClaims {
	body, _, _ := strings.Cut(token, ".")
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(body, "="))
	if err != nil {
		return TokenClaims{}
	}
	var c TokenClaims
	_ = json.Unmarshal(b, &c)
	return c
}
