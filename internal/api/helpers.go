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

	"github.com/ruconnect/ruconnect/internal/store"
)

const maxBody = 8 << 20

type userHandler func(w http.ResponseWriter, r *http.Request, u *store.User)

// readJSON decodes the request body into v. An empty body is not an error:
// the client sends several POSTs without a body.
func readJSON(r *http.Request, v any) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
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

func (s *Server) withUser(h userHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _, err := s.authUser(r)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "Invalid token")
			return
		}
		if err != nil {
			s.writeStoreError(w, err)
			return
		}
		h(w, r, u)
	}
}

func (s *Server) withAdmin(h userHandler) http.HandlerFunc {
	return s.withUser(func(w http.ResponseWriter, r *http.Request, u *store.User) {
		if !u.IsAdmin {
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
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
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
