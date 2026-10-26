package api

import (
	"errors"
	"net/http"

	"github.com/BorisGB2001/RuConnect/internal/store"
)

func (s *Server) adminListUserGroups(w http.ResponseWriter, r *http.Request, u *store.User) {
	list, err := s.store.ListUserGroups(r.Context())
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
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	g, err := s.store.SaveUserGroup(r.Context(), r.PathValue("guid"), req.Name, req.Note, req.DeviceGroups)
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
	if err := s.store.DeleteUserGroup(r.Context(), r.PathValue("guid")); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
