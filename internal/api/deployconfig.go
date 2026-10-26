package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/BorisGB2001/RuConnect/internal/store"
)

// clientConfigString encodes server settings the way the client's
// "Export/Import server config" does (and "rustdesk --config <string>"
// accepts): reversed URL-safe base64 of {"host","relay","api","key"}.
func clientConfigString(host, relay, api, key string) string {
	b, _ := json.Marshal(map[string]string{"host": host, "relay": relay, "api": api, "key": key})
	enc := []rune(base64.URLEncoding.EncodeToString(b))
	for i, j := 0, len(enc)-1; i < j; i, j = i+1, j-1 {
		enc[i], enc[j] = enc[j], enc[i]
	}
	return string(enc)
}

// adminDeployConfig returns ready-to-use client settings for a site
// (?server=<slug>, default site otherwise).
func (s *Server) adminDeployConfig(w http.ResponseWriter, r *http.Request, u *store.User) {
	slug := r.URL.Query().Get("server")
	if slug == "" {
		slug = store.DefaultServerSlug
	}
	v, err := s.store.ServerBySlug(r.Context(), slug)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	host, key := s.rendezvous(v)
	relay := v.RelayServer
	if relay == "" && v.IsDefault {
		relay = s.cfg.RelayServer
	}
	api := s.publicBase(r, "")
	if !v.IsDefault {
		api += "/s/" + v.Slug
	}
	cfg := clientConfigString(host, relay, api, key)
	out := map[string]any{
		"server":        v.Slug,
		"id_server":     host,
		"relay_server":  relay,
		"api_server":    api,
		"key":           key,
		"config_string": cfg,
		"cli":           "rustdesk --config " + cfg,
	}
	// The installer-name form cannot carry a URL, so it only fits clients that
	// can derive the API address (default site on port 21114) and keys without '/'.
	if v.IsDefault && host != "" && !strings.ContainsAny(key, "/\\:*?\"<>|") {
		name := "rustdesk-host=" + host
		if key != "" {
			name += ",key=" + key
		}
		out["installer_name"] = name + ",.exe"
	}
	writeJSON(w, http.StatusOK, out)
}
