// Package config loads RuConnect settings from environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Listen is the HTTP listen address for the client API and the web panel.
	Listen string
	// DB is the SQLite database file path.
	DB string
	// AdminUser and AdminPassword seed the first administrator on an empty database.
	// When AdminPassword is empty a random one is generated and logged once.
	AdminUser     string
	AdminPassword string
	// KeyFile is the hbbs public key (id_ed25519.pub), shown in the panel for client setup.
	KeyFile string
	// IDServer, RelayServer and APIServer are the public addresses shown in the panel.
	IDServer    string
	RelayServer string
	APIServer   string
	// TokenTTL is the lifetime of client and panel access tokens.
	TokenTTL time.Duration
	// OnlineTimeout is how long after the last heartbeat a device counts as online.
	OnlineTimeout time.Duration
	// TrustProxy makes the server take client IPs from X-Forwarded-For / X-Real-IP.
	TrustProxy bool
	// AuditRetentionDays deletes audit records older than this; 0 keeps them forever.
	AuditRetentionDays int
}

func Load() Config {
	return Config{
		Listen:             env("RUCONNECT_LISTEN", ":21114"),
		DB:                 env("RUCONNECT_DB", "data/ruconnect.db"),
		AdminUser:          env("RUCONNECT_ADMIN_USER", "admin"),
		AdminPassword:      os.Getenv("RUCONNECT_ADMIN_PASSWORD"),
		KeyFile:            env("RUCONNECT_KEY_FILE", "data/id_ed25519.pub"),
		IDServer:           os.Getenv("RUCONNECT_ID_SERVER"),
		RelayServer:        os.Getenv("RUCONNECT_RELAY_SERVER"),
		APIServer:          os.Getenv("RUCONNECT_API_SERVER"),
		TokenTTL:           time.Duration(envInt("RUCONNECT_TOKEN_TTL_HOURS", 24*30)) * time.Hour,
		OnlineTimeout:      time.Duration(envInt("RUCONNECT_ONLINE_TIMEOUT_SECONDS", 60)) * time.Second,
		TrustProxy:         envBool("RUCONNECT_TRUST_PROXY", false),
		AuditRetentionDays: envInt("RUCONNECT_AUDIT_RETENTION_DAYS", 0),
	}
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil && v > 0 {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(key))); err == nil {
		return v
	}
	return def
}
