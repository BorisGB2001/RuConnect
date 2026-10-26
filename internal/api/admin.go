package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ruconnect/ruconnect/internal/store"
	"github.com/ruconnect/ruconnect/internal/totp"
)

// Panel API. Every route requires an administrator token obtained via /api/login.
func (s *Server) adminRoutes(mux *http.ServeMux) {
	h := func(pattern string, fn userHandler) { mux.HandleFunc(pattern, s.withAdmin(fn)) }

	h("GET /api/admin/overview", s.adminOverview)
	h("PUT /api/admin/password", s.adminChangePassword)
	h("POST /api/admin/tokens", s.adminCreateToken)
	h("GET /api/admin/deploy-config", s.adminDeployConfig)

	h("GET /api/admin/users", s.adminListUsers)
	h("POST /api/admin/users", s.adminCreateUser)
	h("PUT /api/admin/users/{guid}", s.adminUpdateUser)
	h("DELETE /api/admin/users/{guid}", s.adminDeleteUser)
	h("POST /api/admin/users/{guid}/totp", s.adminEnableTOTP)
	h("DELETE /api/admin/users/{guid}/totp", s.adminDisableTOTP)

	h("GET /api/admin/devices", s.adminListDevices)
	h("PUT /api/admin/devices/{id}", s.adminUpdateDevice)
	h("DELETE /api/admin/devices/{id}", s.adminDeleteDevice)
	h("POST /api/admin/devices/{id}/disconnect", s.adminDisconnectDevice)

	h("GET /api/admin/groups", s.adminListGroups)
	h("POST /api/admin/groups", s.adminCreateGroup)
	h("PUT /api/admin/groups/{guid}", s.adminUpdateGroup)
	h("DELETE /api/admin/groups/{guid}", s.adminDeleteGroup)

	h("GET /api/admin/address-books", s.adminListABs)
	h("POST /api/admin/address-books", s.adminCreateAB)
	h("PUT /api/admin/address-books/{guid}", s.adminUpdateAB)
	h("DELETE /api/admin/address-books/{guid}", s.adminDeleteAB)
	h("GET /api/admin/address-books/{guid}/rules", s.adminListABRules)
	h("PUT /api/admin/address-books/{guid}/rules", s.adminSetABRule)
	h("GET /api/admin/address-books/{guid}/peers", s.adminListABPeers)

	h("GET /api/admin/servers", s.adminListServers)
	h("POST /api/admin/servers", s.adminCreateServer)
	h("PUT /api/admin/servers/{guid}", s.adminUpdateServer)
	h("DELETE /api/admin/servers/{guid}", s.adminDeleteServer)
	h("GET /api/admin/servers/{guid}/check", s.adminCheckServer)

	h("GET /api/admin/user-groups", s.adminListUserGroups)
	h("POST /api/admin/user-groups", s.adminSaveUserGroup)
	h("PUT /api/admin/user-groups/{guid}", s.adminSaveUserGroup)
	h("DELETE /api/admin/user-groups/{guid}", s.adminDeleteUserGroup)

	h("GET /api/admin/strategies", s.adminListStrategies)
	h("POST /api/admin/strategies", s.adminCreateStrategy)
	h("PUT /api/admin/strategies/{guid}", s.adminUpdateStrategy)
	h("DELETE /api/admin/strategies/{guid}", s.adminDeleteStrategy)
	h("PUT /api/admin/devices/{id}/strategy", s.adminSetDeviceStrategy)
	h("PUT /api/admin/groups/{guid}/strategy", s.adminSetGroupStrategy)

	h("GET /api/admin/oidc", s.adminListOIDC)
	h("POST /api/admin/oidc", s.adminCreateOIDC)
	h("PUT /api/admin/oidc/{guid}", s.adminUpdateOIDC)
	h("DELETE /api/admin/oidc/{guid}", s.adminDeleteOIDC)

	h("GET /api/admin/audit/conns", s.adminAuditConns)
	h("GET /api/admin/audit/files", s.adminAuditFiles)
	h("GET /api/admin/audit/alarms", s.adminAuditAlarms)
}

func (s *Server) onlineSince() int64 { return time.Now().Add(-s.cfg.OnlineTimeout).Unix() }

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request, u *store.User) {
	ctx := r.Context()
	users, err := s.store.CountUsers(ctx)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	total, online, err := s.store.CountDevices(ctx, s.onlineSince())
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":        Version,
		"users":          users,
		"devices":        total,
		"devices_online": online,
		"server": map[string]string{
			"id_server":    s.cfg.IDServer,
			"relay_server": s.cfg.RelayServer,
			"api_server":   s.cfg.APIServer,
			"key":          s.publicKey(),
		},
	})
}

func (s *Server) adminChangePassword(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if !u.CheckPassword(req.Old) {
		writeError(w, http.StatusForbidden, "Wrong current password")
		return
	}
	if len(req.New) < 8 {
		writeError(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}
	if err := s.store.SetPassword(r.Context(), u.ID, req.New); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// adminCreateToken issues a long-lived token for "rustdesk --assign/--deploy".
func (s *Server) adminCreateToken(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Days int `json:"days"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if req.Days <= 0 || req.Days > 3650 {
		req.Days = 365
	}
	tok, err := s.store.CreateToken(r.Context(), store.Token{UserID: u.ID, DeviceInfo: `{"type":"cli"}`, IP: s.clientIP(r)},
		time.Duration(req.Days)*24*time.Hour)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": tok, "days": req.Days})
}

type userRequest struct {
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name"`
	Email       string  `json:"email"`
	Note        string  `json:"note"`
	Password    string  `json:"password"`
	Status      *int    `json:"status"`
	IsAdmin     bool    `json:"is_admin"`
	GroupGUID   *string `json:"group_guid"` // user group; "" removes the user from it
}

// applyUserGroup sets the user's group when the request names one.
func (s *Server) applyUserGroup(w http.ResponseWriter, r *http.Request, u *store.User, groupGUID *string) bool {
	if groupGUID == nil {
		return true
	}
	var gid int64
	if *groupGUID != "" {
		g, err := s.store.UserGroupByGUID(r.Context(), *groupGUID)
		if err != nil {
			s.writeStoreError(w, err)
			return false
		}
		gid = g.ID
	}
	if err := s.store.SetUserGroup(r.Context(), u.ID, gid); err != nil {
		s.writeStoreError(w, err)
		return false
	}
	return true
}

func (q userRequest) input() store.UserInput {
	st := store.UserNormal
	if q.Status != nil {
		st = *q.Status
	}
	return store.UserInput{Name: q.Name, DisplayName: q.DisplayName, Email: q.Email, Note: q.Note,
		Password: q.Password, Status: st, IsAdmin: q.IsAdmin}
}

func (s *Server) adminListUsers(w http.ResponseWriter, r *http.Request, u *store.User) {
	users, total, err := s.store.ListUsers(r.Context(), store.UserFilter{Search: r.URL.Query().Get("q")}, pageOf(r))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeList(w, total, users)
}

func (s *Server) adminCreateUser(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req userRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}
	nu, err := s.store.CreateUser(r.Context(), req.input())
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "A user with this name already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.applyUserGroup(w, r, nu, req.GroupGUID) {
		return
	}
	nu, _ = s.store.UserByGUID(r.Context(), nu.GUID)
	s.log.Info("user created", "user", nu.Name, "by", u.Name)
	writeJSON(w, http.StatusOK, nu)
}

func (s *Server) adminUpdateUser(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req userRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if req.Password != "" && len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "Password must be at least 8 characters")
		return
	}
	target, err := s.store.UserByGUID(r.Context(), r.PathValue("guid"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	in := req.input()
	if target.IsAdmin && (!in.IsAdmin || in.Status != store.UserNormal) && !s.otherAdminsExist(w, r, target.ID) {
		return
	}
	nu, err := s.store.UpdateUser(r.Context(), target.GUID, in)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "A user with this name already exists")
		return
	}
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	if !s.applyUserGroup(w, r, nu, req.GroupGUID) {
		return
	}
	nu, _ = s.store.UserByGUID(r.Context(), nu.GUID)
	writeJSON(w, http.StatusOK, nu)
}

// otherAdminsExist refuses changes that would leave the server without an administrator.
func (s *Server) otherAdminsExist(w http.ResponseWriter, r *http.Request, exclude int64) bool {
	n, err := s.store.CountActiveAdmins(r.Context(), exclude)
	if err != nil {
		s.writeStoreError(w, err)
		return false
	}
	if n == 0 {
		writeError(w, http.StatusBadRequest, "At least one active administrator must remain")
		return false
	}
	return true
}

func (s *Server) adminDeleteUser(w http.ResponseWriter, r *http.Request, u *store.User) {
	target, err := s.store.UserByGUID(r.Context(), r.PathValue("guid"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	if target.IsAdmin && !s.otherAdminsExist(w, r, target.ID) {
		return
	}
	if err := s.store.DeleteUser(r.Context(), target.GUID); err != nil {
		s.writeStoreError(w, err)
		return
	}
	s.log.Info("user deleted", "user", target.Name, "by", u.Name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminEnableTOTP(w http.ResponseWriter, r *http.Request, u *store.User) {
	target, err := s.store.UserByGUID(r.Context(), r.PathValue("guid"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	secret := totp.NewSecret()
	if err := s.store.SetTOTPSecret(r.Context(), target.ID, secret); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": secret, "uri": totp.URI("RuConnect", target.Name, secret)})
}

func (s *Server) adminDisableTOTP(w http.ResponseWriter, r *http.Request, u *store.User) {
	target, err := s.store.UserByGUID(r.Context(), r.PathValue("guid"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	if err := s.store.SetTOTPSecret(r.Context(), target.ID, ""); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type adminDevice struct {
	*store.Device
	Online bool `json:"online"`
}

func (s *Server) adminListDevices(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	f := store.DeviceFilter{Search: q.Get("q")}
	if g := q.Get("group"); g != "" {
		grp, err := s.store.DeviceGroupByGUID(r.Context(), g)
		if err != nil {
			s.writeStoreError(w, err)
			return
		}
		f.GroupID = grp.ID
	}
	if ug := q.Get("user"); ug != "" {
		usr, err := s.store.UserByGUID(r.Context(), ug)
		if err != nil {
			s.writeStoreError(w, err)
			return
		}
		f.UserID = usr.ID
	}
	since := s.onlineSince()
	if q.Get("online") == "1" {
		f.OnlineSince = since
	}
	devices, total, err := s.store.ListDevices(r.Context(), f, pageOf(r))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	out := make([]adminDevice, 0, len(devices))
	for _, d := range devices {
		out = append(out, adminDevice{Device: d, Online: d.LastOnlineAt >= since})
	}
	writeList(w, total, out)
}

func (s *Server) adminUpdateDevice(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Note      *string `json:"note"`
		UserGUID  *string `json:"user_guid"`
		GroupGUID *string `json:"group_guid"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	up := store.DeviceUpdate{Note: req.Note}
	if req.UserGUID != nil {
		var id int64
		if *req.UserGUID != "" {
			usr, err := s.store.UserByGUID(r.Context(), *req.UserGUID)
			if err != nil {
				s.writeStoreError(w, err)
				return
			}
			id = usr.ID
		}
		up.UserID = &id
	}
	if req.GroupGUID != nil {
		var id int64
		if *req.GroupGUID != "" {
			grp, err := s.store.DeviceGroupByGUID(r.Context(), *req.GroupGUID)
			if err != nil {
				s.writeStoreError(w, err)
				return
			}
			id = grp.ID
		}
		up.GroupID = &id
	}
	if err := s.store.UpdateDevice(r.Context(), r.PathValue("id"), up); err != nil {
		s.writeStoreError(w, err)
		return
	}
	d, err := s.store.GetDevice(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, adminDevice{Device: d, Online: d.LastOnlineAt >= s.onlineSince()})
}

func (s *Server) adminDeleteDevice(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := s.store.DeleteDevice(r.Context(), r.PathValue("id")); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminDisconnectDevice(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("id")
	n := s.sessions.closeAll(id)
	s.log.Info("disconnect requested", "device", id, "sessions", n, "by", u.Name)
	writeJSON(w, http.StatusOK, map[string]int{"sessions": n})
}

type groupRequest struct {
	Name string `json:"name"`
	Note string `json:"note"`
}

func (s *Server) adminListGroups(w http.ResponseWriter, r *http.Request, u *store.User) {
	groups, total, err := s.store.ListDeviceGroups(r.Context(), 0, pageOf(r))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeList(w, total, groups)
}

func (s *Server) adminCreateGroup(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req groupRequest
	if err := readJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "Group name is required")
		return
	}
	g, err := s.store.CreateDeviceGroup(r.Context(), req.Name, req.Note)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) adminUpdateGroup(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req groupRequest
	if err := readJSON(r, &req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "Group name is required")
		return
	}
	g, err := s.store.UpdateDeviceGroup(r.Context(), r.PathValue("guid"), req.Name, req.Note)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) adminDeleteGroup(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := s.store.DeleteDeviceGroup(r.Context(), r.PathValue("guid")); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type abRequest struct {
	Name      string `json:"name"`
	Note      string `json:"note"`
	Password  string `json:"password"`
	OwnerGUID string `json:"owner_guid"`
}

func (s *Server) abOwner(w http.ResponseWriter, r *http.Request, u *store.User, guid string) (int64, bool) {
	if guid == "" {
		return u.ID, true
	}
	owner, err := s.store.UserByGUID(r.Context(), guid)
	if err != nil {
		s.writeStoreError(w, err)
		return 0, false
	}
	return owner.ID, true
}

func (s *Server) adminListABs(w http.ResponseWriter, r *http.Request, u *store.User) {
	books, total, err := s.store.SharedABsForUser(r.Context(), nil, pageOf(r))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeList(w, total, books)
}

func (s *Server) adminCreateAB(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req abRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	owner, ok := s.abOwner(w, r, u, req.OwnerGUID)
	if !ok {
		return
	}
	ab, err := s.store.CreateSharedAB(r.Context(), req.Name, owner, req.Note, req.Password)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "An address book with this name already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ab)
}

func (s *Server) adminUpdateAB(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req abRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	cur, err := s.store.ABByGUID(r.Context(), r.PathValue("guid"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	owner := cur.OwnerID
	if req.OwnerGUID != "" {
		var ok bool
		if owner, ok = s.abOwner(w, r, u, req.OwnerGUID); !ok {
			return
		}
	}
	ab, err := s.store.UpdateSharedAB(r.Context(), cur.GUID, req.Name, owner, req.Note, req.Password)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ab)
}

func (s *Server) adminDeleteAB(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := s.store.DeleteAB(r.Context(), r.PathValue("guid")); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminListABRules(w http.ResponseWriter, r *http.Request, u *store.User) {
	ab, err := s.store.ABByGUID(r.Context(), r.PathValue("guid"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	rules, err := s.store.ListABRules(r.Context(), ab.ID)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) adminSetABRule(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		UserGUID string `json:"user_guid"`
		Rule     int    `json:"rule"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	ab, err := s.store.ABByGUID(r.Context(), r.PathValue("guid"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	if ab.Personal {
		writeError(w, http.StatusBadRequest, "Personal address books cannot be shared")
		return
	}
	target, err := s.store.UserByGUID(r.Context(), req.UserGUID)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	if err := s.store.SetABRule(r.Context(), ab.ID, target.ID, req.Rule); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminListABPeers(w http.ResponseWriter, r *http.Request, u *store.User) {
	ab, err := s.store.ABByGUID(r.Context(), r.PathValue("guid"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	peers, total, err := s.store.ListABPeers(r.Context(), ab.ID, pageOf(r))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	for _, p := range peers {
		p.Hash, p.Password = "", ""
	}
	writeList(w, total, peers)
}

func auditFilter(r *http.Request) store.AuditFilter {
	q := r.URL.Query()
	from, _ := strconv.ParseInt(q.Get("from"), 10, 64)
	to, _ := strconv.ParseInt(q.Get("to"), 10, 64)
	return store.AuditFilter{DeviceID: q.Get("device_id"), PeerID: q.Get("peer_id"), From: from, To: to}
}

func (s *Server) adminAuditConns(w http.ResponseWriter, r *http.Request, u *store.User) {
	if wantCSV(r) {
		s.exportConns(w, r)
		return
	}
	items, total, err := s.store.ListConnAudits(r.Context(), auditFilter(r), pageOf(r))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeList(w, total, items)
}

func (s *Server) adminAuditFiles(w http.ResponseWriter, r *http.Request, u *store.User) {
	if wantCSV(r) {
		s.exportFiles(w, r)
		return
	}
	items, total, err := s.store.ListFileAudits(r.Context(), auditFilter(r), pageOf(r))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeList(w, total, items)
}

func (s *Server) adminAuditAlarms(w http.ResponseWriter, r *http.Request, u *store.User) {
	if wantCSV(r) {
		s.exportAlarms(w, r)
		return
	}
	items, total, err := s.store.ListAlarmAudits(r.Context(), auditFilter(r), pageOf(r))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeList(w, total, items)
}
