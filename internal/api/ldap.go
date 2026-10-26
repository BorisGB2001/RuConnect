package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/BorisGB2001/RuConnect/internal/ldapauth"
	"github.com/BorisGB2001/RuConnect/internal/store"
)

func (s *Server) ldapConfig() ldapauth.Config {
	l := s.cfg.LDAP
	return ldapauth.Config{
		URL: l.URL, StartTLS: l.StartTLS, InsecureSkipVerify: l.InsecureSkipVerify,
		BindDN: l.BindDN, BindPassword: l.BindPassword, BaseDN: l.BaseDN,
		UserFilter: l.UserFilter, NameAttr: l.NameAttr,
		AdminGroupDN: l.AdminGroupDN, RequiredGroup: l.RequiredGroup,
	}
}

// ldapLogin authenticates against the directory and returns the matching
// local user, creating or updating it. local is the existing account with
// the requested name, if any. Accounts that have a local password are never
// taken over by a directory entry with the same name.
func (s *Server) ldapLogin(r *http.Request, local *store.User, username, password string) *store.User {
	cfg := s.ldapConfig()
	if !cfg.Enabled() || (local != nil && !s.ldapMayUse(r, local)) {
		return nil
	}
	id, err := ldapauth.Authenticate(cfg, username, password)
	if err != nil {
		if !errors.Is(err, ldapauth.ErrInvalidCredentials) {
			s.log.Error("ldap", "err", err)
		}
		return nil
	}
	ctx := r.Context()
	u := local
	if u == nil || !strings.EqualFold(u.Name, id.Name) {
		u, err = s.store.UserByName(ctx, id.Name)
	}
	isAdmin := id.IsAdmin
	switch {
	case errors.Is(err, store.ErrNotFound):
		u, err = s.store.CreateUser(ctx, store.UserInput{Name: id.Name, DisplayName: id.DisplayName, Email: id.Email,
			Status: store.UserNormal, IsAdmin: isAdmin})
		if err == nil {
			s.log.Info("user created from LDAP", "user", id.Name, "dn", id.DN)
		}
	case err == nil && !s.ldapMayUse(r, u):
		s.log.Warn("ldap login refused: a local or OIDC account with this name exists", "user", u.Name)
		return nil
	case err == nil:
		if s.cfg.LDAP.AdminGroupDN == "" {
			isAdmin = u.IsAdmin // admin rights are managed in the panel
		}
		u, err = s.store.UpdateUser(ctx, u.GUID, store.UserInput{Name: u.Name, DisplayName: id.DisplayName, Email: id.Email,
			Note: u.Note, Status: u.Status, IsAdmin: isAdmin})
	}
	if err != nil {
		s.log.Error("ldap user sync", "user", id.Name, "err", err)
		return nil
	}
	if err := s.store.SetAuthSource(ctx, u.ID, "ldap"); err != nil {
		s.log.Error("ldap user sync", "user", id.Name, "err", err)
		return nil
	}
	return u
}

// ldapMayUse reports whether a directory login may sign in as the existing
// account u: accounts created by LDAP, or ones an administrator created
// without a password for LDAP use. Accounts with a local password or an
// OIDC identity are never taken over by a directory entry of the same name.
func (s *Server) ldapMayUse(r *http.Request, u *store.User) bool {
	src, err := s.store.AuthSource(r.Context(), u.ID)
	if err != nil {
		return false
	}
	if src == "ldap" {
		return true
	}
	if src != "" || u.PasswordHash != "" {
		return false
	}
	linked, err := s.store.HasOIDCIdentity(r.Context(), u.ID)
	return err == nil && !linked
}
