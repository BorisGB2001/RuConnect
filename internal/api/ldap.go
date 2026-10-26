package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ruconnect/ruconnect/internal/ldapauth"
	"github.com/ruconnect/ruconnect/internal/store"
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
	if !cfg.Enabled() || (local != nil && local.PasswordHash != "") {
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
	case err == nil && u.PasswordHash != "":
		s.log.Warn("ldap login refused: a local account with this name has a password", "user", u.Name)
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
	return u
}
