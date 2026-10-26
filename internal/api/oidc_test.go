package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ruconnect/ruconnect/internal/config"
	"github.com/ruconnect/ruconnect/internal/store"
)

// fakeIdP is a minimal OpenID provider issuing one fixed identity.
func fakeIdP(t *testing.T, sub, email, username string) *httptest.Server {
	t.Helper()
	return fakeIdPWith(t, sub, email, username, true)
}

func fakeIdPWith(t *testing.T, sub, email, username string, verified any) *httptest.Server {
	t.Helper()
	var idp *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 idp.URL,
			"authorization_endpoint": idp.URL + "/authorize",
			"token_endpoint":         idp.URL + "/token",
			"userinfo_endpoint":      idp.URL + "/userinfo",
		})
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
			http.Error(w, "pkce required", 400)
			return
		}
		http.Redirect(w, r, q.Get("redirect_uri")+"?code=authcode&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("code") != "authcode" || r.Form.Get("code_verifier") == "" {
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"access_token": "at-1", "token_type": "Bearer"})
	})
	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at-1" {
			http.Error(w, "unauthorized", 401)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"sub": sub, "email": email, "email_verified": verified,
			"preferred_username": username, "name": "OIDC User"})
	})
	idp = httptest.NewServer(mux)
	t.Cleanup(idp.Close)
	return idp
}

func oidcTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(config.Config{TokenTTL: 3600e9}, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, st
}

func oidcJSON(t *testing.T, method, u, body string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(method, u, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("%s %s: not a JSON object: %s", method, u, b)
	}
	return out
}

func runOIDCLogin(t *testing.T, ts *httptest.Server) map[string]any {
	t.Helper()
	start := oidcJSON(t, "POST", ts.URL+"/api/oidc/auth",
		`{"op":"acme","id":"123","uuid":"dXVpZA==","deviceInfo":{"os":"linux","type":"client","name":"pc"},"apiDomain":"`+ts.URL+`/s/default"}`)
	code, _ := start["code"].(string)
	authURL, _ := start["url"].(string)
	if code == "" || authURL == "" {
		t.Fatalf("oidc/auth: %v", start)
	}
	if !strings.Contains(authURL, url.QueryEscape(ts.URL+"/api/oidc/callback")) {
		t.Fatalf("redirect_uri should drop the site prefix: %s", authURL)
	}
	query := ts.URL + "/api/oidc/auth-query?code=" + url.QueryEscape(code) + "&id=123&uuid=" + url.QueryEscape("dXVpZA==")
	if r := oidcJSON(t, "GET", query, ""); r["error"] != oidcPending {
		t.Fatalf("before callback: %v", r)
	}
	// Browser: authorize -> redirect -> callback (follows redirects). The
	// callback only shows a confirmation page; nothing is signed in yet.
	resp, err := http.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(page), "Sign in to RustDesk?") {
		t.Fatalf("no confirmation page: %d %.200s", resp.StatusCode, page)
	}
	if r := oidcJSON(t, "GET", query, ""); r["error"] != oidcPending {
		t.Fatalf("login completed without confirmation: %v", r)
	}
	// The user confirms: the form posts state and code back.
	cb := resp.Request.URL
	form := url.Values{"state": {cb.Query().Get("state")}, "code": {cb.Query().Get("code")}}
	resp, err = http.PostForm(ts.URL+"/api/oidc/callback", form)
	if err != nil {
		t.Fatal(err)
	}
	page, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("confirm page: %d %.80s", resp.StatusCode, page)
	return oidcJSON(t, "GET", query, "")
}

func TestOIDCLoginAutoCreate(t *testing.T) {
	ts, st := oidcTestServer(t)
	idp := fakeIdP(t, "sub-1", "Jane@Example.com", "jane")
	if _, err := st.CreateOIDCProvider(context.Background(), store.OIDCProviderInput{
		Name: "acme", Issuer: idp.URL, ClientID: "ruconnect", ClientSecret: "s3cret", AutoCreate: true, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	var opts []string
	resp, _ := http.Get(ts.URL + "/api/login-options")
	json.NewDecoder(resp.Body).Decode(&opts)
	resp.Body.Close()
	if len(opts) != 1 || !strings.HasPrefix(opts[0], `common-oidc/[{"icon":"","name":"acme"}]`) {
		t.Fatalf("login-options: %v", opts)
	}

	res := runOIDCLogin(t, ts)
	if res["type"] != "access_token" || res["access_token"] == "" {
		t.Fatalf("auth-query result: %v", res)
	}
	user, _ := res["user"].(map[string]any)
	if user["name"] != "jane" {
		t.Fatalf("user: %v", user)
	}
	if _, ok := user["info"].(map[string]any); !ok {
		t.Fatal("user.info must be an object, the client ignores the response otherwise")
	}
	// Second login maps to the same account through the stored identity.
	res = runOIDCLogin(t, ts)
	if u, _ := res["user"].(map[string]any); u["name"] != "jane" {
		t.Fatalf("second login: %v", res)
	}
	n, _ := st.CountUsers(context.Background())
	if n != 1 {
		t.Fatalf("users = %d, want 1", n)
	}
}

func TestOIDCLoginNoAccount(t *testing.T) {
	ts, st := oidcTestServer(t)
	idp := fakeIdP(t, "sub-2", "bob@example.com", "bob")
	st.CreateOIDCProvider(context.Background(), store.OIDCProviderInput{Name: "acme", Issuer: idp.URL, ClientID: "c", Enabled: true})
	res := runOIDCLogin(t, ts)
	msg, _ := res["error"].(string)
	if !strings.Contains(msg, "no RuConnect account") {
		t.Fatalf("want a final error, got %v", res)
	}

	// An existing user with the same verified email is linked.
	if _, err := st.CreateUser(context.Background(), store.UserInput{Name: "robert", Email: "BOB@example.com", Status: store.UserNormal}); err != nil {
		t.Fatal(err)
	}
	res = runOIDCLogin(t, ts)
	if u, _ := res["user"].(map[string]any); u["name"] != "robert" {
		t.Fatalf("link by email: %v", res)
	}
}

// Accounts are not linked by an unverified (or unflagged) email, and
// administrators are never linked automatically.
func TestOIDCLinkingRestrictions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		verified any
		admin    bool
	}{
		{"unflagged email", nil, false},
		{"unverified email", false, false},
		{"admin account", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts, st := oidcTestServer(t)
			idp := fakeIdPWith(t, "sub-x", "boss@example.com", "intruder", tc.verified)
			st.CreateOIDCProvider(context.Background(), store.OIDCProviderInput{Name: "acme", Issuer: idp.URL, ClientID: "c", Enabled: true})
			st.CreateUser(context.Background(), store.UserInput{Name: "boss", Email: "boss@example.com", Status: store.UserNormal, IsAdmin: tc.admin})
			res := runOIDCLogin(t, ts)
			if u, _ := res["user"].(map[string]any); u != nil {
				t.Fatalf("signed in as %v", u["name"])
			}
		})
	}
}
