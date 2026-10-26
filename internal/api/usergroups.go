package api

import (
	"errors"
	"net/http"

	"github.com/BorisGB2001/RuConnect/internal/store"
)

func (s *Server) adminListUserGroups(w http.ResponseWriter, r *http.Request, u *store.User) {
	org, ok := s.listOrg(w, r, u)
	if !ok {
		return
	}
	list, err := s.store.ListUserGroups(r.Context(), org)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeList(w, len(list), list)
}

// adminSaveUserGroup creates (POST) or updates (PUT .../{guid}) a user group.
func (s *Server) adminSaveUserGroup(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Name         string   `json:"name"`
		Note         string   `json:"note"`
		DeviceGroups []string `json:"device_groups"`
		OrgGUID      string   `json:"org_guid"` // on create, server staff only
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	var org int64
	if guid := r.PathValue("guid"); guid != "" {
		if _, ok := s.scopedUserGroup(w, r, u, guid); !ok {
			return
		}
	} else {
		var ok bool
		if org, ok = s.targetOrg(w, r, u, req.OrgGUID); !ok {
			return
		}
	}
	g, err := s.store.SaveUserGroup(r.Context(), org, r.PathValue("guid"), req.Name, req.Note, req.DeviceGroups)
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "A user group with this name already exists")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Not found")
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusOK, g)
	}
}

func (s *Server) adminDeleteUserGroup(w http.ResponseWriter, r *http.Request, u *store.User) {
	if _, ok := s.scopedUserGroup(w, r, u, r.PathValue("guid")); !ok {
		return
	}
	if err := s.store.DeleteUserGroup(r.Context(), r.PathValue("guid")); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
