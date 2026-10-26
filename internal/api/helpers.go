package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/BorisGB2001/RuConnect/internal/store"
)

// Request size limits. Device and login requests are small; only the legacy
// address book (one JSON blob per user) needs a large body.
const (
	maxBody      = 256 << 10
	maxLargeBody = 8 << 20

	maxID    = 64  // RustDesk device id
	maxUUID  = 128 // base64 machine uid
	maxConns = 64  // connection ids per heartbeat
	maxField = 256 // hostnames, user names, peer names, ...
	maxText  = 4096
)

// clip truncates s to at most n bytes.
func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

type userHandler func(w http.ResponseWriter, r *http.Request, u *store.User)

// readJSON decodes the request body into v. An empty body is not an error:
// the client sends several POSTs without a body.
func readJSON(r *http.Request, v any) error { return readJSONLimit(r, v, maxBody) }

func readJSONLimit(r *http.Request, v any, limit int64) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err == nil && int64(len(b)) > limit {
		return errors.New("request body too large")
	}
	if err != nil {
		return err
	}
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeEmpty writes 200 with an empty body, which is what the client treats as
// success for address book actions and audit posts. "{}" would be an error.
func writeEmpty(w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)
}

func writeText(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(s))
}

type list[T any] struct {
	Total int `json:"total"`
	Data  []T `json:"data"`
}

func writeList[T any](w http.ResponseWriter, total int, data []T) {
	if data == nil {
		data = []T{}
	}
	writeJSON(w, http.StatusOK, list[T]{Total: total, Data: data})
}

// writeStoreError maps store errors to HTTP responses.
func (s *Server) writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "Already exists")
	case errors.Is(err, store.ErrFull):
		writeError(w, http.StatusBadRequest, "The address book is full")
	default:
		s.log.Error("store", "err", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func (s *Server) authUser(r *http.Request) (*store.User, *store.Token, error) {
	return s.store.UserByToken(r.Context(), bearerToken(r))
}

func (s *Server) withUser(h userHandler) http.HandlerFunc { return s.withToken(h, false) }

// withDeploy is withAdmin that also accepts deployment tokens.
func (s *Server) withDeploy(h userHandler) http.HandlerFunc {
	return s.withToken(func(w http.ResponseWriter, r *http.Request, u *store.User) {
		if !u.IsAdmin {
			writeError(w, http.StatusForbidden, "Admin required!")
			return
		}
		h(w, r, u)
	}, true)
}

func (s *Server) withToken(h userHandler, allowDeploy bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, tok, err := s.authUser(r)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "Invalid token")
			return
		}
		if err != nil {
			s.writeStoreError(w, err)
			return
		}
		if tok.Kind == store.TokenDeploy && !allowDeploy {
			writeError(w, http.StatusForbidden, "Deployment tokens only work for rustdesk --assign and --deploy")
			return
		}
		h(w, r, u)
	}
}

// Panel permission levels.
type perm int

const (
	permView    perm = iota // auditors, helpdesk and administrators
	permOperate             // helpdesk and administrators
	permAdmin               // administrators only
)

// routePerm maps a panel route pattern to the permission it needs.
// Reads are open to every panel role, except configuration that holds
// secrets or grants access (tokens, login providers). Helpdesk staff may
// change devices and address books. Everything else is admin-only.
func routePerm(pattern string) perm {
	method, path, _ := strings.Cut(pattern, " ")
	switch {
	case strings.HasPrefix(path, "/api/admin/tokens"), strings.HasPrefix(path, "/api/admin/oidc"):
		return permAdmin
	case method == http.MethodGet, path == "/api/admin/password":
		return permView
	case strings.HasPrefix(path, "/api/admin/devices/"), strings.HasPrefix(path, "/api/admin/address-books"):
		return permOperate
	default:
		return permAdmin
	}
}

func userPerm(u *store.User) (perm, bool) {
	switch {
	case u.IsAdmin:
		return permAdmin, true
	case u.Role == store.RoleHelpdesk:
		return permOperate, true
	case u.Role == store.RoleAuditor:
		return permView, true
	}
	return 0, false
}

func (s *Server) withPerm(need perm, h userHandler) http.HandlerFunc {
	return s.withUser(func(w http.ResponseWriter, r *http.Request, u *store.User) {
		if have, ok := userPerm(u); !ok || have < need {
			writeError(w, http.StatusForbidden, "Admin required!")
			return
		}
		h(w, r, u)
	})
}

func pageOf(r *http.Request) store.Page {
	q := r.URL.Query()
	cur, _ := strconv.Atoi(q.Get("current"))
	size, _ := strconv.Atoi(q.Get("pageSize"))
	return store.Page{Current: cur, PageSize: size}
}

func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		// Proxies append the address they saw, so only the right-most entry
		// was written by our proxy; earlier ones come from the client.
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return clip(strings.TrimSpace(parts[len(parts)-1]), 64)
		}
		if xr := r.Header.Get("X-Real-IP"); xr != "" {
			return strings.TrimSpace(xr)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// str converts a decoded JSON value to a string ("" for null).
func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

// num converts a decoded JSON value to an int64 (0 when not a number).
func num(v any) int64 {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		if f, err := t.Float64(); err == nil {
			return int64(f)
		}
	case string:
		i, _ := strconv.ParseInt(t, 10, 64)
		return i
	}
	return 0
}
