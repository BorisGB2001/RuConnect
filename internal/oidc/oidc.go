// Package oidc is a minimal OpenID Connect relying party: discovery,
// authorization code flow with PKCE, token exchange and the userinfo endpoint.
//
// Identity is taken from the userinfo endpoint (fetched over TLS with the
// access token just obtained from the token endpoint), so ID token signature
// verification is not required.
package oidc

import (
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
	"sync"
	"time"
)

type Discovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

// Claims are the userinfo fields RuConnect uses.
type Claims struct {
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	EmailVerified     *bool  `json:"email_verified"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Nickname          string `json:"nickname"`
	Login             string `json:"login"`
}

type Client struct {
	HTTP  *http.Client
	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	d   *Discovery
	exp time.Time
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 15 * time.Second}, cache: map[string]cached{}}
}

// Discover fetches (and caches for an hour) the provider metadata.
func (c *Client) Discover(ctx context.Context, issuer string) (*Discovery, error) {
	issuer = strings.TrimRight(issuer, "/")
	c.mu.Lock()
	if e, ok := c.cache[issuer]; ok && time.Now().Before(e.exp) {
		c.mu.Unlock()
		return e.d, nil
	}
	c.mu.Unlock()
	var d Discovery
	if err := c.getJSON(ctx, issuer+"/.well-known/openid-configuration", "", &d); err != nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}
	if d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" || d.UserinfoEndpoint == "" {
		return nil, errors.New("discovery: provider metadata lacks authorization, token or userinfo endpoint")
	}
	c.mu.Lock()
	c.cache[issuer] = cached{d: &d, exp: time.Now().Add(time.Hour)}
	c.mu.Unlock()
	return &d, nil
}

// NewPKCE returns a random code verifier and its S256 challenge.
func NewPKCE() (verifier, challenge string) {
	verifier = RandomString(32)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func RandomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func AuthURL(d *Discovery, clientID, redirectURI, scopes, state, nonce, challenge string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {scopes},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	sep := "?"
	if strings.Contains(d.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	return d.AuthorizationEndpoint + sep + q.Encode()
}

// Exchange trades an authorization code for an access token.
func (c *Client) Exchange(ctx context.Context, d *Discovery, clientID, clientSecret, redirectURI, code, verifier string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if clientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(clientSecret))
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tr struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("token endpoint: HTTP %d: %.200s", resp.StatusCode, body)
	}
	if tr.Error != "" || tr.AccessToken == "" {
		return "", fmt.Errorf("token endpoint: %s %s", tr.Error, tr.Description)
	}
	return tr.AccessToken, nil
}

func (c *Client) UserInfo(ctx context.Context, d *Discovery, accessToken string) (*Claims, error) {
	var cl Claims
	if err := c.getJSON(ctx, d.UserinfoEndpoint, accessToken, &cl); err != nil {
		return nil, fmt.Errorf("userinfo: %w", err)
	}
	if cl.Subject == "" {
		return nil, errors.New("userinfo: missing sub")
	}
	return &cl, nil
}

func (c *Client) getJSON(ctx context.Context, u, bearer string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %.200s", resp.StatusCode, body)
	}
	return json.Unmarshal(body, v)
}
