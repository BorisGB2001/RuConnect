package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ruconnect/ruconnect/internal/store"
)

type heartbeatRequest struct {
	ID         string `json:"id"`
	UUID       string `json:"uuid"`
	Ver        int64  `json:"ver"`
	Conns      []int  `json:"conns"`
	ModifiedAt int64  `json:"modified_at"`
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req heartbeatRequest
	if err := readJSON(r, &req); err != nil || req.ID == "" || len(req.ID) > maxID || len(req.UUID) > maxUUID {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if len(req.Conns) > maxConns {
		req.Conns = req.Conns[:maxConns]
	}
	known, err := s.store.Heartbeat(r.Context(), req.ID, req.UUID, s.clientIP(r), len(req.Conns))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	resp := map[string]any{}
	if !known {
		// Unknown device or wrong uuid: ask for sysinfo and keep no state.
		resp["sysinfo"] = true
		writeJSON(w, http.StatusOK, resp)
		return
	}
	{
		if err := s.store.SetDeviceServer(r.Context(), req.ID, site(r).ID); err != nil {
			s.log.Warn("device server", "id", req.ID, "err", err)
		}
		// Push the device's strategy when the client's copy is out of date.
		st, err := s.store.EffectiveStrategy(r.Context(), req.ID)
		switch {
		case err != nil:
			s.log.Warn("strategy", "id", req.ID, "err", err)
		case st != nil && st.ModifiedAt != req.ModifiedAt:
			resp["modified_at"] = st.ModifiedAt
			resp["strategy"] = map[string]any{"config_options": st.Options, "extra": map[string]string{}}
		case st == nil && req.ModifiedAt != 0:
			resp["modified_at"] = 0
		}
	}
	if d := s.sessions.update(req.ID, req.Conns); len(d) > 0 {
		resp["disconnect"] = d
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSysinfo(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	id, uuid := str(req["id"]), str(req["uuid"])
	if id == "" || uuid == "" || len(id) > maxID || len(uuid) > maxUUID {
		writeText(w, "ID_NOT_FOUND")
		return
	}
	ctx := r.Context()
	_, err := s.store.UpsertSysInfo(ctx, store.SysInfo{
		ID:       id,
		UUID:     uuid,
		Hostname: clip(str(req["hostname"]), maxField),
		Username: clip(str(req["username"]), maxField),
		OS:       clip(str(req["os"]), maxField),
		CPU:      clip(str(req["cpu"]), maxField),
		Memory:   clip(str(req["memory"]), 64),
		Version:  clip(str(req["version"]), 64),
		IP:       s.clientIP(r),
	})
	if errors.Is(err, store.ErrConflict) {
		// Another machine already owns this id. Any reply other than
		// SYSINFO_UPDATED / ID_NOT_FOUND makes the client retry in 120 s.
		s.log.Warn("sysinfo rejected: device id is bound to another machine", "id", id, "ip", s.clientIP(r))
		writeText(w, "UUID_MISMATCH")
		return
	}
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	if err := s.store.SetDeviceServer(ctx, id, site(r).ID); err != nil {
		s.log.Warn("device server", "id", id, "err", err)
	}
	s.applyPresets(r, id, presets{
		UserName:        str(req["preset-user-name"]),
		StrategyName:    str(req["preset-strategy-name"]),
		DeviceGroupName: str(req["preset-device-group-name"]),
		Note:            str(req["preset-note"]),
		ABName:          str(req["preset-address-book-name"]),
		ABTag:           str(req["preset-address-book-tag"]),
		ABAlias:         str(req["preset-address-book-alias"]),
		ABPassword:      str(req["preset-address-book-password"]),
		ABNote:          str(req["preset-address-book-note"]),
	}, false)
	writeText(w, "SYSINFO_UPDATED")
}

// presets are deployment-time assignments, sent by the client in sysinfo
// (from its preset-* options) or by "rustdesk --assign" via /api/devices/cli.
type presets struct {
	UserName, DeviceGroupName, Note, StrategyName string
	ABName, ABTag, ABAlias, ABPassword, ABNote    string
	DeviceUsername, DeviceName                    string
}

// applyPresets assigns a device to a user, group and shared address book.
// Unless force is set, existing assignments are kept, so an administrator's
// later changes in the panel are not overwritten on every client restart.
func (s *Server) applyPresets(r *http.Request, id string, p presets, force bool) []string {
	ctx := r.Context()
	var problems []string
	d, err := s.store.GetDevice(ctx, id)
	if err != nil {
		return []string{"device not found"}
	}
	var up store.DeviceUpdate
	if p.UserName != "" && (force || d.UserID == nil) {
		if u, err := s.store.UserByName(ctx, p.UserName); err == nil {
			up.UserID = &u.ID
		} else {
			problems = append(problems, "user "+p.UserName+" not found")
		}
	}
	if p.DeviceGroupName != "" && (force || d.GroupID == nil) {
		if g, err := s.store.DeviceGroupByName(ctx, p.DeviceGroupName); err == nil {
			up.GroupID = &g.ID
		} else {
			problems = append(problems, "device group "+p.DeviceGroupName+" not found")
		}
	}
	if p.StrategyName != "" {
		if st, err := s.store.StrategyByName(ctx, p.StrategyName); err != nil {
			problems = append(problems, "strategy "+p.StrategyName+" not found")
		} else if devStrategy, _, err := s.store.StrategyNames(ctx, id); err == nil && (force || devStrategy == "") {
			if err := s.store.SetDeviceStrategy(ctx, id, st.ID); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}
	if p.Note != "" && (force || d.Note == "") {
		up.Note = &p.Note
	}
	if p.DeviceUsername != "" {
		up.Username = &p.DeviceUsername
	}
	if p.DeviceName != "" {
		up.Hostname = &p.DeviceName
	}
	if err := s.store.UpdateDevice(ctx, id, up); err != nil {
		problems = append(problems, err.Error())
	}
	if p.ABName != "" {
		ab, err := s.store.SharedABByName(ctx, p.ABName)
		switch {
		case err != nil:
			problems = append(problems, "address book "+p.ABName+" not found")
		default:
			exists, err := s.store.HasABPeer(ctx, ab.ID, id)
			if err == nil && (force || !exists) {
				peer := &store.ABPeer{
					ID: id, Alias: p.ABAlias, Password: p.ABPassword, Note: p.ABNote,
					Hostname: d.Hostname, Username: d.Username, Platform: platformOf(d.OS),
				}
				if p.ABTag != "" {
					peer.Tags = []string{p.ABTag}
					if err := s.store.EnsureABTag(ctx, ab.ID, store.ABTag{Name: p.ABTag, Color: defaultTagColor}); err != nil {
						problems = append(problems, err.Error())
					}
				}
				err = s.store.AddABPeer(ctx, ab.ID, peer)
			}
			if err != nil {
				problems = append(problems, err.Error())
			}
		}
	}
	if len(problems) > 0 {
		s.log.Warn("device presets", "id", id, "problems", strings.Join(problems, "; "))
	}
	return problems
}

// defaultTagColor is the client's default tag color (opaque grey-blue, ARGB).
const defaultTagColor = 0xFF607D8B

// platformOf derives the client's platform name from a sysinfo os string
// such as "windows / Windows 10 Pro - 19045".
func platformOf(osInfo string) string {
	o := strings.ToLower(strings.TrimSpace(strings.SplitN(osInfo, " / ", 2)[0]))
	switch {
	case strings.Contains(o, "windows"):
		return "Windows"
	case strings.Contains(o, "mac"):
		return "Mac OS"
	case strings.Contains(o, "android"):
		return "Android"
	case o == "":
		return ""
	default:
		return "Linux"
	}
}

func (s *Server) handleSysinfoVer(w http.ResponseWriter, r *http.Request) {
	writeText(w, "")
}

func (s *Server) handleSwitchGrant(w http.ResponseWriter, r *http.Request) {
	// Switch-sides grants need hbbs cooperation, which the open-source hbbs
	// does not have; decline so the client does not retry.
	writeJSON(w, http.StatusOK, map[string]any{"accepted": false})
}

// handleDevicesCLI serves "rustdesk --assign --token <admin token> ...".
func (s *Server) handleDevicesCLI(w http.ResponseWriter, r *http.Request, admin *store.User) {
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	id := s.normalizePeerID(r, str(req["id"]))
	d, err := s.store.GetDevice(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Device "+id+" is not registered yet; make sure the client uses this server and try again")
		return
	}
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	if uuid := str(req["uuid"]); d.UUID != "" && uuid != "" && uuid != d.UUID {
		writeError(w, http.StatusConflict, "Device uuid mismatch")
		return
	}
	problems := s.applyPresets(r, id, presets{
		UserName:        str(req["user_name"]),
		StrategyName:    str(req["strategy_name"]),
		DeviceGroupName: str(req["device_group_name"]),
		Note:            str(req["note"]),
		ABName:          str(req["address_book_name"]),
		ABTag:           str(req["address_book_tag"]),
		ABAlias:         str(req["address_book_alias"]),
		ABPassword:      str(req["address_book_password"]),
		ABNote:          str(req["address_book_note"]),
		DeviceUsername:  str(req["device_username"]),
		DeviceName:      str(req["device_name"]),
	}, true)
	if len(problems) > 0 {
		writeText(w, "Partially done: "+strings.Join(problems, "; "))
		return
	}
	s.log.Info("device assigned", "id", id, "by", admin.Name)
	writeEmpty(w)
}

// handleDevicesDeploy serves "rustdesk --deploy --token <admin token> [--id <id>]".
func (s *Server) handleDevicesDeploy(w http.ResponseWriter, r *http.Request) {
	u, _, err := s.authUser(r)
	if err != nil || !u.IsAdmin {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"result": "INVALID_TOKEN", "error": "Admin token required"})
		return
	}
	var req struct {
		ID   string `json:"id"`
		UUID string `json:"uuid"`
		PK   string `json:"pk"`
	}
	if err := readJSON(r, &req); err != nil || req.ID == "" || req.UUID == "" {
		writeJSON(w, http.StatusOK, map[string]string{"result": "INVALID_INPUT"})
		return
	}
	d, err := s.store.GetDevice(r.Context(), req.ID)
	switch {
	case err == nil && d.UUID != req.UUID:
		writeJSON(w, http.StatusOK, map[string]string{"result": "ID_TAKEN"})
		return
	case errors.Is(err, store.ErrNotFound):
		if _, err := s.store.UpsertSysInfo(r.Context(), store.SysInfo{ID: req.ID, UUID: req.UUID, IP: s.clientIP(r)}); err != nil {
			s.writeStoreError(w, err)
			return
		}
		if err := s.store.SetDeviceServer(r.Context(), req.ID, site(r).ID); err != nil {
			s.writeStoreError(w, err)
			return
		}
	case err != nil:
		s.writeStoreError(w, err)
		return
	}
	s.log.Info("device deployed", "id", req.ID, "by", u.Name)
	writeJSON(w, http.StatusOK, map[string]string{"result": "OK"})
}

func (s *Server) handleRecord(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "Session recording upload is not supported yet")
}
