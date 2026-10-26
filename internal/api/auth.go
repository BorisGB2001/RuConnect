package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/BorisGB2001/RuConnect/internal/store"
	"github.com/BorisGB2001/RuConnect/internal/totp"
)

type deviceInfo struct {
	OS   string `json:"os"`
	Type string `json:"type"`
	Name string `json:"name"`
}

type loginRequest struct {
	Username         string     `json:"username"`
	Password         string     `json:"password"`
	ID               string     `json:"id"`
	UUID             string     `json:"uuid"`
	AutoLogin        bool       `json:"autoLogin"`
	Type             string     `json:"type"`
	VerificationCode string     `json:"verificationCode"`
	TFACode          string     `json:"tfaCode"`
	Secret           string     `json:"secret"`
	DeviceInfo       deviceInfo `json:"deviceInfo"`
}

// userPayload is the client's UserPayload shape.
type userPayload struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar"`
	Email       string `json:"email"`
	Note        string `json:"note"`
	Status      int    `json:"status"`
	IsAdmin     bool   `json:"is_admin"`
	Role        string `json:"role,omitempty"` // RuConnect panel role; ignored by the client
	// RuConnect organization; ignored by the client.
	OrgGUID string         `json:"org_guid,omitempty"`
	OrgName string         `json:"org_name,omitempty"`
	Info    map[string]any `json:"info"`
}

func toPayload(u *store.User) userPayload {
	return userPayload{
		Name:        u.Name,
		DisplayName: u.DisplayName,
		Email:       u.Email,
		Note:        u.Note,
		Status:      u.Status,
		IsAdmin:     u.IsAdmin,
		Role:        u.Role,
		OrgGUID:     u.OrgGUID,
		OrgName:     u.OrgName,
		Info: map[string]any{
			"email_verification":       false,
			"email_alarm_notification": false,
			"login_device_whitelist":   []any{},
			"other":                    map[string]string{},
		},
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := readJSON(r, &req); err != nil || len(req.Username) > maxField || len(req.Password) > maxField ||
		len(req.ID) > maxID || len(req.UUID) > maxUUID {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	ip := s.clientIP(r)
	name := strings.ToLower(strings.TrimSpace(req.Username))
	// Failures are limited per client and per account, so neither one IP
	// nor many IPs can keep guessing one account's password or TOTP code.
	key, acct := ip+"|"+name, "user|"+name
	if s.logins.Blocked(key) || s.accountLogins.Blocked(acct) {
		writeError(w, http.StatusTooManyRequests, "Too many failed attempts, try again later")
		return
	}
	fail := func() {
		s.logins.Fail(key)
		s.accountLogins.Fail(acct)
	}

	switch req.Type {
	case "", "account":
		u, err := s.store.UserByName(r.Context(), req.Username)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.writeStoreError(w, err)
			return
		}
		ok := u != nil && u.CheckPassword(req.Password)
		if u == nil {
			store.DummyPasswordCheck(req.Password) // same timing as for existing users
		}
		if !ok {
			if lu := s.ldapLogin(r, u, req.Username, req.Password); lu != nil {
				u, ok = lu, true
			}
		}
		if !ok {
			fail()
			s.log.Warn("login failed", "user", clip(req.Username, 64), "ip", ip)
			writeError(w, http.StatusUnauthorized, "Wrong username or password")
			return
		}
		if !s.mayUseHost(r, u) {
			// Members of other organizations do not exist on this domain.
			fail()
			s.log.Warn("login refused on another organization's domain", "user", u.Name, "host", requestHost(r), "ip", ip)
			writeError(w, http.StatusUnauthorized, "Wrong username or password")
			return
		}
		if u.Status != store.UserNormal {
			writeError(w, http.StatusForbidden, "User is disabled")
			return
		}
		if u.TOTPEnabled {
			writeJSON(w, http.StatusOK, map[string]any{
				"type":     "email_check",
				"tfa_type": "tfa_check",
				"secret":   s.tfa.Start(u.ID),
				"user":     map[string]string{"name": u.Name},
			})
			return
		}
		s.issueToken(w, r, u, req)

	case "email_code":
		uid, ok := s.tfa.Lookup(req.Secret)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Verification session expired, please log in again")
			return
		}
		u, err := s.store.UserByID(r.Context(), uid)
		if err != nil {
			s.writeStoreError(w, err)
			return
		}
		code := req.TFACode
		if code == "" {
			code = req.VerificationCode
		}
		used := fmt.Sprintf("%d|%s", u.ID, strings.TrimSpace(code))
		if !strings.EqualFold(u.Name, strings.TrimSpace(req.Username)) || s.totpUsed.Has(used) ||
			!totp.Verify(u.TOTPSecret, code, time.Now()) {
			fail()
			if s.tfa.Fail(req.Secret) {
				writeError(w, http.StatusUnauthorized, "Too many wrong codes, please log in again")
				return
			}
			writeError(w, http.StatusUnauthorized, "Wrong verification code")
			return
		}
		s.tfa.Done(req.Secret)
		s.totpUsed.Add(used)
		if u.Status != store.UserNormal {
			writeError(w, http.StatusForbidden, "User is disabled")
			return
		}
		s.issueToken(w, r, u, req)

	default:
		writeError(w, http.StatusBadRequest, "Unsupported login type")
	}
}

func (s *Server) issueToken(w http.ResponseWriter, r *http.Request, u *store.User, req loginRequest) {
	tok, err := s.loginToken(r, u, req.ID, req.UUID, req.DeviceInfo, "password")
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	s.logins.Reset(s.clientIP(r) + "|" + strings.ToLower(u.Name))
	writeJSON(w, http.StatusOK, map[string]any{
		"type":         "access_token",
		"access_token": tok,
		"user":         toPayload(u),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if tok := bearerToken(r); tok != "" {
		if err := s.store.DeleteToken(r.Context(), tok); err != nil {
			s.log.Warn("logout", "err", err)
		}
	}
	writeEmpty(w)
}

func (s *Server) handleCurrentUser(w http.ResponseWriter, r *http.Request, u *store.User) {
	writeJSON(w, http.StatusOK, toPayload(u))
}

// loginToken issues an access token for a successful login from a device.
func (s *Server) loginToken(r *http.Request, u *store.User, deviceID, uuid string, di deviceInfo, method string) (string, error) {
	info, _ := json.Marshal(di)
	tok, err := s.store.CreateToken(r.Context(), store.Token{
		UserID:     u.ID,
		DeviceID:   deviceID,
		DeviceUUID: uuid,
		DeviceInfo: string(info),
		IP:         s.clientIP(r),
	}, s.cfg.TokenTTL)
	if err != nil {
		return "", err
	}
	// A client logging in claims its device for the user, unless an admin assigned it already.
	if deviceID != "" {
		if err := s.store.AssignDeviceUserIfUnset(r.Context(), deviceID, uuid, u.ID); err != nil {
			s.log.Warn("assign device", "id", deviceID, "err", err)
		}
	}
	s.log.Info("login", "user", u.Name, "method", method, "device", deviceID, "type", di.Type, "ip", s.clientIP(r))
	return tok, nil
}

// loginLimiter blocks a key (ip|user) for a while after repeated failures.
type loginLimiter struct {
	mu            sync.Mutex
	fails         map[string]*failState
	max           int
	window, block time.Duration
}

type failState struct {
	count int
	first time.Time
	until time.Time
}

// maxLimiterKeys bounds the limiter's memory under a flood of distinct keys.
const maxLimiterKeys = 100000

func newLoginLimiter(maxFails int, window, block time.Duration) *loginLimiter {
	return &loginLimiter{fails: map[string]*failState{}, max: maxFails, window: window, block: block}
}

func (l *loginLimiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.fails[key]
	return f != nil && time.Now().Before(f.until)
}

func (l *loginLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := time.Now()
	if len(l.fails) > maxLimiterKeys/2 {
		for k, f := range l.fails {
			if n.Sub(f.first) > l.window && n.After(f.until) {
				delete(l.fails, k)
			}
		}
	}
	f := l.fails[key]
	if f == nil && len(l.fails) >= maxLimiterKeys {
		return
	}
	if f == nil || n.Sub(f.first) > l.window {
		f = &failState{first: n}
		l.fails[key] = f
	}
	f.count++
	if f.count >= l.max {
		f.until = n.Add(l.block)
		f.count = 0
		f.first = n
	}
}

func (l *loginLimiter) Reset(key string) {
	l.mu.Lock()
	delete(l.fails, key)
	l.mu.Unlock()
}

// tfaChallenges holds pending second-factor logins keyed by an opaque secret.
type tfaChallenges struct {
	mu    sync.Mutex
	items map[string]*tfaChallenge
}

type tfaChallenge struct {
	userID  int64
	expires time.Time
	fails   int
}

func newTFAChallenges() *tfaChallenges { return &tfaChallenges{items: map[string]*tfaChallenge{}} }

func (t *tfaChallenges) Start(userID int64) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := time.Now()
	for k, c := range t.items {
		if n.After(c.expires) {
			delete(t.items, k)
		}
	}
	secret := store.RandomToken(24)
	t.items[secret] = &tfaChallenge{userID: userID, expires: n.Add(5 * time.Minute)}
	return secret
}

func (t *tfaChallenges) Lookup(secret string) (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.items[secret]
	if c == nil || time.Now().After(c.expires) {
		return 0, false
	}
	return c.userID, true
}

// Fail counts a wrong code and reports whether the challenge was revoked.
func (t *tfaChallenges) Fail(secret string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.items[secret]
	if c == nil {
		return true
	}
	c.fails++
	if c.fails >= 5 {
		delete(t.items, secret)
		return true
	}
	return false
}

func (t *tfaChallenges) Done(secret string) {
	t.mu.Lock()
	delete(t.items, secret)
	t.mu.Unlock()
}
