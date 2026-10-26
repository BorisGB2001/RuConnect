// Package ldapauth authenticates users against LDAP / Active Directory.
package ldapauth

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

type Config struct {
	URL                string // ldap://host:389 or ldaps://host:636
	StartTLS           bool
	InsecureSkipVerify bool
	BindDN             string // service account used to search for users
	BindPassword       string
	BaseDN             string
	// UserFilter is an LDAP filter with %s for the escaped login name.
	UserFilter    string
	NameAttr      string // login name attribute, e.g. uid or sAMAccountName
	DisplayAttr   string
	EmailAttr     string
	AdminGroupDN  string // members of this group become RuConnect administrators
	RequiredGroup string // when set, only members of this group may log in
}

func (c Config) Enabled() bool { return c.URL != "" && c.BaseDN != "" }

// Defaults fill attributes suitable for both OpenLDAP and Active Directory.
func (c *Config) defaults() {
	if c.UserFilter == "" {
		c.UserFilter = "(&(|(objectClass=person)(objectClass=inetOrgPerson))(|(uid=%s)(sAMAccountName=%s)(mail=%s)))"
	}
	if c.DisplayAttr == "" {
		c.DisplayAttr = "displayName"
	}
	if c.EmailAttr == "" {
		c.EmailAttr = "mail"
	}
}

// Identity is the directory entry of an authenticated user.
type Identity struct {
	DN          string
	Name        string
	DisplayName string
	Email       string
	Groups      []string
	IsAdmin     bool
}

var ErrInvalidCredentials = errors.New("invalid credentials")

// Authenticate verifies username/password and returns the user's identity.
func Authenticate(cfg Config, username, password string) (*Identity, error) {
	cfg.defaults()
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		// An empty password would be an anonymous bind, which many servers accept.
		return nil, ErrInvalidCredentials
	}
	conn, err := dial(cfg)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
			return nil, fmt.Errorf("service bind: %w", err)
		}
	}
	esc := ldap.EscapeFilter(username)
	filter := strings.ReplaceAll(cfg.UserFilter, "%s", esc)
	attrs := []string{"dn", cfg.DisplayAttr, cfg.EmailAttr, "memberOf", "uid", "sAMAccountName", "cn"}
	if cfg.NameAttr != "" {
		attrs = append(attrs, cfg.NameAttr)
	}
	res, err := conn.Search(ldap.NewSearchRequest(cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		2, 10, false, filter, attrs, nil))
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if len(res.Entries) != 1 {
		return nil, ErrInvalidCredentials
	}
	e := res.Entries[0]
	if err := conn.Bind(e.DN, password); err != nil {
		var le *ldap.Error
		if errors.As(err, &le) && le.ResultCode == ldap.LDAPResultInvalidCredentials {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("user bind: %w", err)
	}

	id := &Identity{
		DN:          e.DN,
		DisplayName: e.GetAttributeValue(cfg.DisplayAttr),
		Email:       e.GetAttributeValue(cfg.EmailAttr),
		Groups:      e.GetAttributeValues("memberOf"),
	}
	for _, a := range []string{cfg.NameAttr, "sAMAccountName", "uid", "cn"} {
		if a != "" && id.Name == "" {
			id.Name = e.GetAttributeValue(a)
		}
	}
	if id.Name == "" {
		id.Name = username
	}
	if cfg.RequiredGroup != "" && !hasGroup(id.Groups, cfg.RequiredGroup) {
		return nil, ErrInvalidCredentials
	}
	id.IsAdmin = cfg.AdminGroupDN != "" && hasGroup(id.Groups, cfg.AdminGroupDN)
	return id, nil
}

func hasGroup(groups []string, dn string) bool {
	for _, g := range groups {
		if strings.EqualFold(strings.ReplaceAll(g, " ", ""), strings.ReplaceAll(dn, " ", "")) {
			return true
		}
	}
	return false
}

func dial(cfg Config) (*ldap.Conn, error) {
	tlsCfg := &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify} //nolint:gosec // opt-in for self-signed directories
	conn, err := ldap.DialURL(cfg.URL, ldap.DialWithTLSConfig(tlsCfg))
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	conn.SetTimeout(10 * time.Second)
	if cfg.StartTLS {
		if err := conn.StartTLS(tlsCfg); err != nil {
			conn.Close()
			return nil, fmt.Errorf("starttls: %w", err)
		}
	}
	return conn, nil
}
