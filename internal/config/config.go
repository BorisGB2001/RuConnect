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
	// LDAP enables directory logins when URL and BaseDN are set.
	LDAP LDAP
	// Rendezvous configures the built-in ID and relay servers.
	Rendezvous Rendezvous
}

// Rendezvous configures the built-in RustDesk-compatible ID server (hbbs
// replacement: TCP+UDP Port, TCP Port-1 for NAT tests and online queries) and
// relay server (hbbr replacement: TCP RelayPort).
type Rendezvous struct {
	Enabled      bool
	Port         int
	RelayPort    int
	RelayEnabled bool
	// PrivateKeyFile holds the server key in hbbs format; the public key is
	// written next to it with a .pub suffix.
	PrivateKeyFile string
	// RelayServers are handed to clients for relayed sessions; empty means
	// RelayServer, else the ID server host.
	RelayServers []string
	// AlwaysRelay makes every session go through the relay.
	AlwaysRelay bool
}

type LDAP struct {
	URL, BindDN, BindPassword, BaseDN, UserFilter, NameAttr string
	AdminGroupDN, RequiredGroup                             string
	StartTLS, InsecureSkipVerify                            bool
}

func Load() Config {
	return Config{
		Listen:             env("RUCONNECT_LISTEN", ":21114"),
		DB:                 env("RUCONNECT_DB", "data/ruconnect.db"),
		AdminUser:          env("RUCONNECT_ADMIN_USER", "admin"),
		AdminPassword:      os.Getenv("RUCONNECT_ADMIN_PASSWORD"),
		KeyFile:            os.Getenv("RUCONNECT_KEY_FILE"),
		IDServer:           os.Getenv("RUCONNECT_ID_SERVER"),
		RelayServer:        os.Getenv("RUCONNECT_RELAY_SERVER"),
		APIServer:          os.Getenv("RUCONNECT_API_SERVER"),
		TokenTTL:           time.Duration(envInt("RUCONNECT_TOKEN_TTL_HOURS", 24*30)) * time.Hour,
		OnlineTimeout:      time.Duration(envInt("RUCONNECT_ONLINE_TIMEOUT_SECONDS", 60)) * time.Second,
		TrustProxy:         envBool("RUCONNECT_TRUST_PROXY", false),
		AuditRetentionDays: envInt("RUCONNECT_AUDIT_RETENTION_DAYS", 0),
		LDAP: LDAP{
			URL:                os.Getenv("RUCONNECT_LDAP_URL"),
			BindDN:             os.Getenv("RUCONNECT_LDAP_BIND_DN"),
			BindPassword:       os.Getenv("RUCONNECT_LDAP_BIND_PASSWORD"),
			BaseDN:             os.Getenv("RUCONNECT_LDAP_BASE_DN"),
			UserFilter:         os.Getenv("RUCONNECT_LDAP_USER_FILTER"),
			NameAttr:           os.Getenv("RUCONNECT_LDAP_NAME_ATTR"),
			AdminGroupDN:       os.Getenv("RUCONNECT_LDAP_ADMIN_GROUP"),
			RequiredGroup:      os.Getenv("RUCONNECT_LDAP_REQUIRED_GROUP"),
			StartTLS:           envBool("RUCONNECT_LDAP_STARTTLS", false),
			InsecureSkipVerify: envBool("RUCONNECT_LDAP_INSECURE_SKIP_VERIFY", false),
		},
		Rendezvous: Rendezvous{
			Enabled:        envBool("RUCONNECT_RENDEZVOUS", true),
			Port:           envInt("RUCONNECT_RENDEZVOUS_PORT", 21116),
			RelayEnabled:   envBool("RUCONNECT_RELAY", true),
			RelayPort:      envInt("RUCONNECT_RELAY_PORT", 21117),
			PrivateKeyFile: env("RUCONNECT_PRIVATE_KEY_FILE", "data/id_ed25519"),
			RelayServers:   envList("RUCONNECT_RELAY_SERVERS"),
			AlwaysRelay:    envBool("RUCONNECT_ALWAYS_RELAY", false),
		},
	}
}

func envList(key string) []string {
	var out []string
	for _, v := range strings.Split(os.Getenv(key), ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
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
