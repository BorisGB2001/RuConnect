package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ruconnect/ruconnect/internal/oidc"
	"github.com/ruconnect/ruconnect/internal/store"
)

// OIDC login for the stock client:
//
//  1. GET  /api/login-options      lists providers ("common-oidc/[...]").
//  2. POST /api/oidc/auth          {op, id, uuid, deviceInfo} -> {code, url}; the client opens url.
//  3. GET  /api/oidc/callback      the identity provider redirects the browser here.
//  4. GET  /api/oidc/auth-query    the client polls with code until the login completes.

const oidcPending = "No authed oidc is found" // exact text the client polls on

type oidcFlow struct {
	code, state, verifier, nonce string
	providerID                   int64
	redirectURI                  string
	deviceID, uuid               string
	device                       deviceInfo
	expires                      time.Time
	done                         bool
	userID                       int64
	err                          string
}

type oidcFlows struct {
	mu      sync.Mutex
	byCode  map[string]*oidcFlow
	byState map[string]*oidcFlow
}

func newOIDCFlows() *oidcFlows {
	return &oidcFlows{byCode: map[string]*oidcFlow{}, byState: map[string]*oidcFlow{}}
}

func (f *oidcFlows) add(fl *oidcFlow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := time.Now()
	for k, v := range f.byCode {
		if n.After(v.expires) {
			delete(f.byCode, k)
			delete(f.byState, v.state)
		}
	}
	f.byCode[fl.code] = fl
	f.byState[fl.state] = fl
}

func (f *oidcFlows) get(byState bool, key string) *oidcFlow {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.byCode
	if byState {
		m = f.byState
	}
	fl := m[key]
	if fl == nil || time.Now().After(fl.expires) {
		return nil
	}
	return fl
}

func (f *oidcFlows) finish(fl *oidcFlow, userID int64, errMsg string) {
	f.mu.Lock()
	fl.done, fl.userID, fl.err = true, userID, errMsg
	f.mu.Unlock()
}

func (f *oidcFlows) remove(fl *oidcFlow) {
	f.mu.Lock()
	delete(f.byCode, fl.code)
	delete(f.byState, fl.state)
	f.mu.Unlock()
}

func (s *Server) handleLoginOptions(w http.ResponseWriter, r *http.Request) {
	providers, err := s.store.ListOIDCProviders(r.Context(), true)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	opts := []string{}
	if len(providers) > 0 {
		list := make([]map[string]string, 0, len(providers))
		for _, p := range providers {
			list = append(list, map[string]string{"name": p.Name, "icon": p.Icon})
		}
		b, _ := json.Marshal(list)
		opts = append(opts, "common-oidc/"+string(b))
	}
	writeJSON(w, http.StatusOK, opts)
}

var sitePrefixRe = regexp.MustCompile(`/s/[^/]+/?$`)

// publicBase returns the externally visible base URL of this server, used for
// the OIDC redirect URI. Site prefixes are dropped so one redirect URI serves
// every site.
func (s *Server) publicBase(r *http.Request, apiDomain string) string {
	base := s.cfg.APIServer
	if base == "" {
		base = apiDomain
	}
	if base == "" {
		scheme := "http"
		if r.TLS != nil || (s.cfg.TrustProxy && r.Header.Get("X-Forwarded-Proto") == "https") {
			scheme = "https"
		}
		base = scheme + "://" + r.Host
	}
	base = strings.TrimRight(base, "/")
	return strings.TrimRight(sitePrefixRe.ReplaceAllString(base, ""), "/")
}

func (s *Server) handleOIDCAuth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Op         string     `json:"op"`
		ID         string     `json:"id"`
		UUID       string     `json:"uuid"`
		DeviceInfo deviceInfo `json:"deviceInfo"`
		APIDomain  string     `json:"apiDomain"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	p, err := s.store.OIDCProviderByName(r.Context(), req.Op)
	if err != nil || !p.Enabled {
		writeError(w, http.StatusBadRequest, "Unknown login provider "+req.Op)
		return
	}
	d, err := s.oidc.Discover(r.Context(), p.Issuer)
	if err != nil {
		s.log.Error("oidc discovery", "provider", p.Name, "err", err)
		writeError(w, http.StatusBadGateway, "Login provider is unavailable")
		return
	}
	verifier, challenge := oidc.NewPKCE()
	fl := &oidcFlow{
		code:        oidc.RandomString(24),
		state:       oidc.RandomString(24),
		verifier:    verifier,
		nonce:       oidc.RandomString(16),
		providerID:  p.ID,
		redirectURI: s.publicBase(r, req.APIDomain) + "/api/oidc/callback",
		deviceID:    req.ID,
		uuid:        req.UUID,
		device:      req.DeviceInfo,
		expires:     time.Now().Add(10 * time.Minute),
	}
	s.oidcFlows.add(fl)
	writeJSON(w, http.StatusOK, map[string]string{
		"code": fl.code,
		"url":  oidc.AuthURL(d, p.ClientID, fl.redirectURI, p.Scopes, fl.state, fl.nonce, challenge),
	})
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	fl := s.oidcFlows.get(true, q.Get("state"))
	if fl == nil {
		oidcPage(w, http.StatusBadRequest, "Login session expired", "Start the login again from RustDesk.")
		return
	}
	if fl.done {
		oidcPage(w, http.StatusOK, "Already done", "You can close this window.")
		return
	}
	if e := q.Get("error"); e != "" {
		msg := "Login was cancelled or denied by the provider (" + e + ")."
		s.oidcFlows.finish(fl, 0, msg)
		oidcPage(w, http.StatusBadRequest, "Login failed", msg)
		return
	}
	u, err := s.oidcResolveUser(r, fl, q.Get("code"))
	if err != nil {
		s.log.Warn("oidc login failed", "err", err)
		s.oidcFlows.finish(fl, 0, err.Error())
		oidcPage(w, http.StatusForbidden, "Login failed", err.Error())
		return
	}
	s.oidcFlows.finish(fl, u.ID, "")
	oidcPage(w, http.StatusOK, "Signed in as "+u.Name, "You can close this window and return to RustDesk.")
}

var nameCleanRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (s *Server) oidcResolveUser(r *http.Request, fl *oidcFlow, code string) (*store.User, error) {
	ctx := r.Context()
	p, err := s.store.OIDCProviderByID(ctx, fl.providerID)
	if err != nil {
		return nil, errors.New("login provider no longer exists")
	}
	d, err := s.oidc.Discover(ctx, p.Issuer)
	if err != nil {
		return nil, errors.New("login provider is unavailable")
	}
	at, err := s.oidc.Exchange(ctx, d, p.ClientID, p.ClientSecret, fl.redirectURI, code, fl.verifier)
	if err != nil {
		s.log.Error("oidc exchange", "provider", p.Name, "err", err)
		return nil, errors.New("could not complete login with the provider")
	}
	cl, err := s.oidc.UserInfo(ctx, d, at)
	if err != nil {
		s.log.Error("oidc userinfo", "provider", p.Name, "err", err)
		return nil, errors.New("could not read your profile from the provider")
	}

	u, err := s.store.OIDCUser(ctx, p.ID, cl.Subject)
	if errors.Is(err, store.ErrNotFound) && cl.Email != "" && (cl.EmailVerified == nil || *cl.EmailVerified) {
		// Link an existing account by verified email.
		if u, err = s.store.UserByEmail(ctx, cl.Email); err == nil {
			err = s.store.LinkOIDCUser(ctx, p.ID, cl.Subject, u.ID)
		}
	}
	if errors.Is(err, store.ErrNotFound) && p.AutoCreate {
		u, err = s.oidcCreateUser(r, cl)
		if err == nil {
			err = s.store.LinkOIDCUser(ctx, p.ID, cl.Subject, u.ID)
		}
	}
	if errors.Is(err, store.ErrNotFound) {
		who := cl.Email
		if who == "" {
			who = cl.PreferredUsername
		}
		return nil, fmt.Errorf("no RuConnect account for %s; ask an administrator to create one", who)
	}
	if err != nil {
		return nil, err
	}
	if u.Status != store.UserNormal {
		return nil, errors.New("your account is disabled")
	}
	return u, nil
}

func (s *Server) oidcCreateUser(r *http.Request, cl *oidc.Claims) (*store.User, error) {
	base := cl.PreferredUsername
	for _, c := range []string{cl.Login, cl.Nickname, strings.Split(cl.Email, "@")[0]} {
		if base == "" {
			base = c
		}
	}
	base = strings.Trim(nameCleanRe.ReplaceAllString(base, "-"), "-")
	if base == "" {
		base = "user"
	}
	email := ""
	if cl.EmailVerified == nil || *cl.EmailVerified {
		email = cl.Email
	}
	for i := 0; i < 20; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s-%d", base, i+1)
		}
		u, err := s.store.CreateUser(r.Context(), store.UserInput{
			Name: name, DisplayName: cl.Name, Email: email, Status: store.UserNormal,
		})
		if errors.Is(err, store.ErrConflict) {
			continue
		}
		if err == nil {
			s.log.Info("user created from OIDC", "user", name)
		}
		return u, err
	}
	return nil, errors.New("could not pick a unique user name")
}

func (s *Server) handleOIDCQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	fl := s.oidcFlows.get(false, q.Get("code"))
	if fl == nil {
		writeError(w, http.StatusOK, "Login session expired, please try again")
		return
	}
	if fl.deviceID != q.Get("id") || fl.uuid != q.Get("uuid") {
		writeError(w, http.StatusOK, oidcPending)
		return
	}
	s.oidcFlows.mu.Lock()
	done, uid, msg := fl.done, fl.userID, fl.err
	s.oidcFlows.mu.Unlock()
	if !done {
		writeError(w, http.StatusOK, oidcPending)
		return
	}
	s.oidcFlows.remove(fl)
	if msg != "" {
		writeError(w, http.StatusOK, msg)
		return
	}
	u, err := s.store.UserByID(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusOK, "Account not found")
		return
	}
	tok, err := s.loginToken(r, u, fl.deviceID, fl.uuid, fl.device, "oidc")
	if err != nil {
		writeError(w, http.StatusOK, "Internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"type": "access_token", "access_token": tok, "user": toPayload(u)})
}

func oidcPage(w http.ResponseWriter, status int, title, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>RuConnect</title><style>body{font-family:system-ui,sans-serif;display:grid;place-items:center;min-height:90vh;margin:0 16px;
background:#f6f7f9;color:#1d2330}@media(prefers-color-scheme:dark){body{background:#14171c;color:#e6e8ec}}
main{max-width:420px;text-align:center}h1{font-size:1.4rem}</style></head><body><main><h1>%s</h1><p>%s</p></main></body></html>`,
		html.EscapeString(title), html.EscapeString(msg))
}

// Panel API for OIDC providers.

func (s *Server) adminListOIDC(w http.ResponseWriter, r *http.Request, u *store.User) {
	list, err := s.store.ListOIDCProviders(r.Context(), false)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	for _, p := range list {
		p.ClientSecret = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total":        len(list),
		"data":         list,
		"redirect_uri": s.publicBase(r, "") + "/api/oidc/callback",
	})
}

func (s *Server) oidcInput(w http.ResponseWriter, r *http.Request) (store.OIDCProviderInput, bool) {
	var req struct {
		Name         string `json:"name"`
		Issuer       string `json:"issuer"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		Scopes       string `json:"scopes"`
		Icon         string `json:"icon"`
		AutoCreate   bool   `json:"auto_create"`
		Enabled      *bool  `json:"enabled"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return store.OIDCProviderInput{}, false
	}
	enabled := req.Enabled == nil || *req.Enabled
	if u, err := url.Parse(req.Issuer); err != nil || u.Host == "" {
		writeError(w, http.StatusBadRequest, "Issuer must be an http(s) URL")
		return store.OIDCProviderInput{}, false
	}
	return store.OIDCProviderInput{Name: req.Name, Issuer: req.Issuer, ClientID: req.ClientID, ClientSecret: req.ClientSecret,
		Scopes: req.Scopes, Icon: req.Icon, AutoCreate: req.AutoCreate, Enabled: enabled}, true
}

func (s *Server) writeOIDCResult(w http.ResponseWriter, p *store.OIDCProvider, err error) {
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "A provider with this name already exists")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Not found")
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		p.ClientSecret = ""
		writeJSON(w, http.StatusOK, p)
	}
}

func (s *Server) adminCreateOIDC(w http.ResponseWriter, r *http.Request, u *store.User) {
	in, ok := s.oidcInput(w, r)
	if !ok {
		return
	}
	p, err := s.store.CreateOIDCProvider(r.Context(), in)
	s.writeOIDCResult(w, p, err)
}

func (s *Server) adminUpdateOIDC(w http.ResponseWriter, r *http.Request, u *store.User) {
	in, ok := s.oidcInput(w, r)
	if !ok {
		return
	}
	p, err := s.store.UpdateOIDCProvider(r.Context(), r.PathValue("guid"), in)
	s.writeOIDCResult(w, p, err)
}

func (s *Server) adminDeleteOIDC(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := s.store.DeleteOIDCProvider(r.Context(), r.PathValue("guid")); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
