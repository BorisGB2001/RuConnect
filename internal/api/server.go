// Package api implements the RustDesk client HTTP API and the RuConnect panel API.
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ruconnect/ruconnect/internal/config"
	"github.com/ruconnect/ruconnect/internal/oidc"
	"github.com/ruconnect/ruconnect/internal/store"
)

// Version is the server version, set at build time via -ldflags.
var Version = "dev"

type Server struct {
	cfg   config.Config
	store *store.Store
	log   *slog.Logger
	web   fs.FS

	oidc      *oidc.Client
	oidcFlows *oidcFlows

	nonces   *ttlSet
	logins   *loginLimiter
	tfa      *tfaChallenges
	sessions *liveSessions
}

func New(cfg config.Config, st *store.Store, log *slog.Logger, web fs.FS) *Server {
	if _, err := st.EnsureDefaultServer(context.Background(), cfg.IDServer, cfg.RelayServer); err != nil {
		log.Error("create default server", "err", err)
	}
	return &Server{
		cfg:       cfg,
		store:     st,
		log:       log,
		web:       web,
		oidc:      oidc.NewClient(),
		oidcFlows: newOIDCFlows(),
		nonces:    newTTLSet(10 * time.Minute),
		logins:    newLoginLimiter(),
		tfa:       newTFAChallenges(),
		sessions:  newLiveSessions(),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health check for Docker.
	mux.HandleFunc("GET /healthz", s.handleHealth)

	// Authentication.
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("POST /api/currentUser", s.withUser(s.handleCurrentUser))
	mux.HandleFunc("GET /api/login-options", s.handleLoginOptions)
	mux.HandleFunc("POST /api/oidc/auth", s.handleOIDCAuth)
	mux.HandleFunc("GET /api/oidc/auth-query", s.handleOIDCQuery)
	mux.HandleFunc("GET /api/oidc/callback", s.handleOIDCCallback)

	// Device sync (no user auth; devices identify themselves by id + uuid).
	mux.HandleFunc("POST /api/heartbeat", s.handleHeartbeat)
	mux.HandleFunc("POST /api/sysinfo", s.handleSysinfo)
	mux.HandleFunc("POST /api/sysinfo_ver", s.handleSysinfoVer)
	mux.HandleFunc("POST /api/switch-grant", s.handleSwitchGrant)

	// Deployment tooling (admin token).
	mux.HandleFunc("POST /api/devices/cli", s.withAdmin(s.handleDevicesCLI))
	mux.HandleFunc("POST /api/devices/deploy", s.handleDevicesDeploy)

	// Audit.
	mux.HandleFunc("POST /api/audit/conn", s.handleAuditConn)
	mux.HandleFunc("POST /api/audit/file", s.handleAuditFile)
	mux.HandleFunc("POST /api/audit/alarm", s.handleAuditAlarm)
	mux.HandleFunc("GET /api/audit/conn/active", s.withUser(s.handleAuditConnActive))
	mux.HandleFunc("PUT /api/audit", s.withUser(s.handleAuditNote))
	mux.HandleFunc("POST /api/record", s.handleRecord)

	// Address books, new (multi-book) mode.
	mux.HandleFunc("POST /api/ab/personal", s.withUser(s.handleABPersonal))
	mux.HandleFunc("POST /api/ab/settings", s.withUser(s.handleABSettings))
	mux.HandleFunc("POST /api/ab/shared/profiles", s.withUser(s.handleABSharedProfiles))
	mux.HandleFunc("POST /api/ab/peers", s.withUser(s.handleABPeers))
	mux.HandleFunc("POST /api/ab/tags/{guid}", s.withUser(s.handleABTags))
	mux.HandleFunc("POST /api/ab/peer/add/{guid}", s.withUser(s.handleABPeerAdd))
	mux.HandleFunc("PUT /api/ab/peer/update/{guid}", s.withUser(s.handleABPeerUpdate))
	mux.HandleFunc("DELETE /api/ab/peer/{guid}", s.withUser(s.handleABPeerDelete))
	mux.HandleFunc("POST /api/ab/tag/add/{guid}", s.withUser(s.handleABTagAdd))
	mux.HandleFunc("PUT /api/ab/tag/rename/{guid}", s.withUser(s.handleABTagRename))
	mux.HandleFunc("PUT /api/ab/tag/update/{guid}", s.withUser(s.handleABTagUpdate))
	mux.HandleFunc("DELETE /api/ab/tag/{guid}", s.withUser(s.handleABTagDelete))

	// Address books, legacy (single blob) mode.
	mux.HandleFunc("GET /api/ab", s.withUser(s.handleLegacyABGet))
	mux.HandleFunc("POST /api/ab", s.withUser(s.handleLegacyABSet))
	mux.HandleFunc("POST /api/ab/get", s.withUser(s.handleLegacyABGetSciter))

	// Accessible devices tab.
	mux.HandleFunc("GET /api/device-group/accessible", s.withUser(s.handleAccessibleGroups))
	mux.HandleFunc("GET /api/users", s.withUser(s.handleAccessibleUsers))
	mux.HandleFunc("GET /api/peers", s.withUser(s.handleAccessiblePeers))

	s.adminRoutes(mux)

	// Web panel.
	if s.web != nil {
		mux.Handle("GET /", s.panelHandler())
	}

	return s.middleware(s.siteRouter(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		http.Error(w, "db: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok"))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", "method", r.Method, "path", r.URL.Path, "err", rec)
				writeError(sw, http.StatusInternalServerError, "Internal server error")
			}
			// Heartbeats arrive every few seconds per device; keep them out of the info log.
			level := slog.LevelInfo
			if r.URL.Path == "/api/heartbeat" || r.URL.Path == "/healthz" || !strings.HasPrefix(r.URL.Path, "/api/") {
				level = slog.LevelDebug
			}
			s.log.Log(r.Context(), level, "http", "method", r.Method, "path", r.URL.Path, "status", sw.status,
				"ip", s.clientIP(r), "dur", time.Since(start).Round(time.Millisecond))
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(sw, r)
	})
}

// RunMaintenance periodically removes expired tokens and old audit records.
func (s *Server) RunMaintenance(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if err := s.store.DeleteExpiredTokens(ctx); err != nil {
			s.log.Warn("token cleanup", "err", err)
		}
		if d := s.cfg.AuditRetentionDays; d > 0 {
			if err := s.store.PruneAudit(ctx, time.Now().AddDate(0, 0, -d).Unix()); err != nil {
				s.log.Warn("audit cleanup", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// publicKey returns the hbbs public key, if the key file is readable.
func (s *Server) publicKey() string {
	b, err := os.ReadFile(s.cfg.KeyFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// ttlSet remembers keys for a fixed duration (audit nonce deduplication).
type ttlSet struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]time.Time
	last  time.Time
}

func newTTLSet(ttl time.Duration) *ttlSet {
	return &ttlSet{ttl: ttl, items: map[string]time.Time{}}
}

// Seen records key and reports whether it was already present.
func (t *ttlSet) Seen(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := time.Now()
	if n.Sub(t.last) > time.Minute {
		for k, exp := range t.items {
			if n.After(exp) {
				delete(t.items, k)
			}
		}
		t.last = n
	}
	if exp, ok := t.items[key]; ok && n.Before(exp) {
		return true
	}
	t.items[key] = n.Add(t.ttl)
	return false
}

// liveSessions tracks the connection ids each device reported in its last
// heartbeat and the ones an administrator asked to close.
type liveSessions struct {
	mu         sync.Mutex
	conns      map[string][]int
	disconnect map[string][]int
}

func newLiveSessions() *liveSessions {
	return &liveSessions{conns: map[string][]int{}, disconnect: map[string][]int{}}
}

func (l *liveSessions) update(id string, conns []int) []int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(conns) == 0 {
		delete(l.conns, id)
	} else {
		l.conns[id] = conns
	}
	d := l.disconnect[id]
	delete(l.disconnect, id)
	return d
}

// closeAll queues all live connections of a device for disconnection.
func (l *liveSessions) closeAll(id string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.conns[id]
	if len(c) > 0 {
		l.disconnect[id] = append([]int(nil), c...)
	}
	return len(c)
}
