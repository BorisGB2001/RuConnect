package api

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/BorisGB2001/RuConnect/internal/store"
)

// Organizations (tenants).
//
// Users, devices, groups, strategies and shared address books belong to an
// organization or to none. Panel staff of an organization see and change
// only their organization's records; staff without one ("server staff")
// see everything, and server administrators manage organizations, sites and
// login providers. Out-of-scope records answer 404, as if they did not exist.
//
// Clients and panel users reach an organization through its domains: the
// request's Host decides which organization a newly reporting device joins
// and which users may sign in there.

func isServerAdmin(u *store.User) bool { return u.IsAdmin && u.OrgID == 0 }

// inScope reports whether u may see a record of organization org.
func inScope(u *store.User, org int64) bool { return u.OrgID == 0 || u.OrgID == org }

// listOrg is the organization filter for panel lists: an organization's
// staff see their own; server staff see everything or filter with
// ?org=<guid> (or ?org=none).
func (s *Server) listOrg(w http.ResponseWriter, r *http.Request, u *store.User) (int64, bool) {
	if u.OrgID != 0 {
		return u.OrgID, true
	}
	switch g := r.URL.Query().Get("org"); g {
	case "":
		return 0, true
	case "none":
		return store.NoOrg, true
	default:
		o, err := s.store.OrganizationByGUID(r.Context(), g)
		if err != nil {
			s.writeStoreError(w, err)
			return 0, false
		}
		return o.ID, true
	}
}

// targetOrg is the organization a record created by u goes to: u's own, or,
// for server staff, the one named by orgGUID ("" for none).
func (s *Server) targetOrg(w http.ResponseWriter, r *http.Request, u *store.User, orgGUID string) (int64, bool) {
	if u.OrgID != 0 {
		return u.OrgID, true
	}
	if orgGUID == "" {
		return 0, true
	}
	o, err := s.store.OrganizationByGUID(r.Context(), orgGUID)
	if err != nil {
		s.writeStoreError(w, err)
		return 0, false
	}
	return o.ID, true
}

func notFound(w http.ResponseWriter) { writeError(w, http.StatusNotFound, "Not found") }

// requestHost is the host name the client used, without port.
func requestHost(r *http.Request) string {
	h := r.Host
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return store.NormalizeDomain(strings.Trim(h, "[]"))
}

// hostOrg returns the organization whose domain the request came to (0: none).
func (s *Server) hostOrg(r *http.Request) int64 {
	o, err := s.store.OrganizationByDomain(r.Context(), requestHost(r))
	if err != nil {
		return 0
	}
	return o.ID
}

// mayUseHost reports whether u may sign in through the request's domain:
// an organization's domains admit its own members and server staff.
func (s *Server) mayUseHost(r *http.Request, u *store.User) bool {
	org := s.hostOrg(r)
	return org == 0 || u.OrgID == 0 || u.OrgID == org
}

// Panel API for organizations (server administrators).

type orgRequest struct {
	Name    string   `json:"name"`
	Note    string   `json:"note"`
	Domains []string `json:"domains"`
}

func (s *Server) adminListOrgs(w http.ResponseWriter, r *http.Request, u *store.User) {
	if u.OrgID != 0 {
		o, err := s.store.OrganizationByID(r.Context(), u.OrgID)
		if err != nil {
			s.writeStoreError(w, err)
			return
		}
		writeList(w, 1, []*store.Organization{o})
		return
	}
	list, err := s.store.ListOrganizations(r.Context())
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeList(w, len(list), list)
}

func (s *Server) adminSaveOrg(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req orgRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	o, err := s.store.SaveOrganization(r.Context(), r.PathValue("guid"),
		store.OrganizationInput{Name: clip(req.Name, maxField), Note: clip(req.Note, maxText), Domains: req.Domains})
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "The name or one of the domains is already used by another organization")
	case errors.Is(err, store.ErrNotFound):
		notFound(w)
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.log.Info("organization saved", "org", o.Name, "by", u.Name)
		writeJSON(w, http.StatusOK, o)
	}
}

func (s *Server) adminDeleteOrg(w http.ResponseWriter, r *http.Request, u *store.User) {
	err := s.store.DeleteOrganization(r.Context(), r.PathValue("guid"))
	if errors.Is(err, store.ErrNotEmpty) {
		writeError(w, http.StatusBadRequest, "Move or delete the organization's users and devices first")
		return
	}
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	s.log.Info("organization deleted", "guid", r.PathValue("guid"), "by", u.Name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Scoped lookups: records outside the panel user's organization are 404.

func (s *Server) scopedDevice(w http.ResponseWriter, r *http.Request, u *store.User, id string) (*store.Device, bool) {
	d, err := s.store.GetDevice(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || err == nil && !inScope(u, d.OrgID) {
		notFound(w)
		return nil, false
	}
	if err != nil {
		s.writeStoreError(w, err)
		return nil, false
	}
	return d, true
}

func (s *Server) scopedGroup(w http.ResponseWriter, r *http.Request, u *store.User, guid string) (*store.DeviceGroup, bool) {
	g, err := s.store.DeviceGroupByGUID(r.Context(), guid)
	if errors.Is(err, store.ErrNotFound) || err == nil && !inScope(u, g.OrgID) {
		notFound(w)
		return nil, false
	}
	if err != nil {
		s.writeStoreError(w, err)
		return nil, false
	}
	return g, true
}

func (s *Server) scopedUserGroup(w http.ResponseWriter, r *http.Request, u *store.User, guid string) (*store.UserGroup, bool) {
	g, err := s.store.UserGroupByGUID(r.Context(), guid)
	if errors.Is(err, store.ErrNotFound) || err == nil && !inScope(u, g.OrgID) {
		notFound(w)
		return nil, false
	}
	if err != nil {
		s.writeStoreError(w, err)
		return nil, false
	}
	return g, true
}

func (s *Server) scopedStrategy(w http.ResponseWriter, r *http.Request, u *store.User, guid string) (*store.Strategy, bool) {
	v, err := s.store.StrategyByGUID(r.Context(), guid)
	if errors.Is(err, store.ErrNotFound) || err == nil && !inScope(u, v.OrgID) {
		notFound(w)
		return nil, false
	}
	if err != nil {
		s.writeStoreError(w, err)
		return nil, false
	}
	return v, true
}

// adminSetDeviceOrg moves a device to another organization (server staff).
func (s *Server) adminSetDeviceOrg(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		OrgGUID string `json:"org_guid"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if !isServerAdmin(u) {
		writeError(w, http.StatusForbidden, "Only server administrators move devices between organizations")
		return
	}
	org, ok := s.targetOrg(w, r, u, req.OrgGUID)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := s.store.SetDeviceOrganization(r.Context(), id, org); err != nil {
		s.writeStoreError(w, err)
		return
	}
	s.log.Info("device moved", "device", id, "org", req.OrgGUID, "by", u.Name)
	d, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, adminDevice{Device: d, Online: d.LastOnlineAt >= s.onlineSince()})
}

// adminSetUserOrg moves a user to another organization (server staff). The
// user is signed out, since their access changes.
func (s *Server) adminSetUserOrg(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		OrgGUID string `json:"org_guid"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if !isServerAdmin(u) {
		writeError(w, http.StatusForbidden, "Only server administrators move users between organizations")
		return
	}
	target, ok := s.scopedUser(w, r, u, r.PathValue("guid"))
	if !ok {
		return
	}
	org, ok := s.targetOrg(w, r, u, req.OrgGUID)
	if !ok {
		return
	}
	if target.IsAdmin && target.OrgID == 0 && org != 0 && !s.otherAdminsExist(w, r, target) {
		return
	}
	if err := s.store.SetUserOrganization(r.Context(), target.ID, org); err != nil {
		s.writeStoreError(w, err)
		return
	}
	if err := s.store.DeleteUserTokens(r.Context(), target.ID, ""); err != nil {
		s.writeStoreError(w, err)
		return
	}
	s.log.Info("user moved", "user", target.Name, "org", req.OrgGUID, "by", u.Name)
	nu, _ := s.store.UserByGUID(r.Context(), target.GUID)
	writeJSON(w, http.StatusOK, nu)
}

// orgAPIServer is the API address clients of an organization use: the
// configured API server with the organization's domain as host, else the
// scheme and port of the current request.
func (s *Server) orgAPIServer(r *http.Request, domain string) string {
	if s.cfg.APIServer != "" {
		if u, err := url.Parse(s.cfg.APIServer); err == nil && u.Host != "" {
			if port := u.Port(); port != "" {
				u.Host = net.JoinHostPort(domain, port)
			} else {
				u.Host = domain
			}
			return strings.TrimRight(u.String(), "/")
		}
	}
	scheme := "http"
	if r.TLS != nil || s.cfg.TrustProxy && r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := domain
	if _, port, err := net.SplitHostPort(r.Host); err == nil {
		host = net.JoinHostPort(domain, port)
	}
	return scheme + "://" + host
}

// claimOrg puts a device that has no organization yet into the one whose
// domain it reports to.
func (s *Server) claimOrg(r *http.Request, deviceID string) {
	if org := s.hostOrg(r); org != 0 {
		if err := s.store.ClaimDeviceOrganization(r.Context(), deviceID, org); err != nil {
			s.log.Warn("device organization", "id", deviceID, "err", err)
		}
	}
}

// handleTLSAsk lets a TLS front end (Caddy on_demand_tls "ask") obtain
// certificates only for organization domains and the configured API host,
// so new organizations get HTTPS without editing the proxy configuration.
func (s *Server) handleTLSAsk(w http.ResponseWriter, r *http.Request) {
	d := store.NormalizeDomain(r.URL.Query().Get("domain"))
	if d == "" {
		writeError(w, http.StatusBadRequest, "domain is required")
		return
	}
	if u, err := url.Parse(s.cfg.APIServer); err == nil && strings.EqualFold(u.Hostname(), d) ||
		store.NormalizeDomain(s.cfg.Domain) == d {
		writeText(w, "ok")
		return
	}
	if _, err := s.store.OrganizationByDomain(r.Context(), d); err == nil {
		writeText(w, "ok")
		return
	}
	notFound(w)
}
