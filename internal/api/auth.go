package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ruconnect/ruconnect/internal/store"
	"github.com/ruconnect/ruconnect/internal/totp"
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
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name"`
	Avatar      string         `json:"avatar"`
	Email       string         `json:"email"`
	Note        string         `json:"note"`
	Status      int            `json:"status"`
	IsAdmin     bool           `json:"is_admin"`
	Info        map[string]any `json:"info"`
}

func toPayload(u *store.User) userPayload {
	return userPayload{
		Name:        u.Name,
		DisplayName: u.DisplayName,
		Email:       u.Email,
		Note:        u.Note,
		Status:      u.Status,
		IsAdmin:     u.IsAdmin,
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
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	ip := s.clientIP(r)
	key := ip + "|" + strings.ToLower(strings.TrimSpace(req.Username))
	if s.logins.Blocked(key) {
		writeError(w, http.StatusTooManyRequests, "Too many failed attempts, try again later")
		return
	}

	switch req.Type {
	case "", "account":
		u, err := s.store.UserByName(r.Context(), req.Username)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.writeStoreError(w, err)
			return
		}
		ok := u != nil && u.CheckPassword(req.Password)
		if !ok {
			if lu := s.ldapLogin(r, u, req.Username, req.Password); lu != nil {
				u, ok = lu, true
			}
		}
		if !ok {
			s.logins.Fail(key)
			s.log.Warn("login failed", "user", req.Username, "ip", ip)
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
			s.logins.Fail(key)
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
		if err := s.store.AssignDeviceUserIfUnset(r.Context(), deviceID, u.ID); err != nil {
			s.log.Warn("assign device", "id", deviceID, "err", err)
		}
	}
	s.log.Info("login", "user", u.Name, "method", method, "device", deviceID, "type", di.Type, "ip", s.clientIP(r))
	return tok, nil
}

// loginLimiter blocks a key (ip|user) for a while after repeated failures.
type loginLimiter struct {
	mu    sync.Mutex
	fails map[string]*failState
}

type failState struct {
	count int
	first time.Time
	until time.Time
}

const (
	maxLoginFails   = 5
	loginFailWindow = 5 * time.Minute
	loginBlockTime  = 5 * time.Minute
)

func newLoginLimiter() *loginLimiter { return &loginLimiter{fails: map[string]*failState{}} }

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
	if len(l.fails) > 10000 {
		for k, f := range l.fails {
			if n.Sub(f.first) > loginFailWindow && n.After(f.until) {
				delete(l.fails, k)
			}
		}
	}
	f := l.fails[key]
	if f == nil || n.Sub(f.first) > loginFailWindow {
		f = &failState{first: n}
		l.fails[key] = f
	}
	f.count++
	if f.count >= maxLoginFails {
		f.until = n.Add(loginBlockTime)
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
