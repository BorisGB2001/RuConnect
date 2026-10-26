package api

import (
	"errors"
	"net/http"

	"github.com/BorisGB2001/RuConnect/internal/store"
)

// Panel API for strategies: named client option sets pushed via heartbeat.

func (s *Server) adminListStrategies(w http.ResponseWriter, r *http.Request, u *store.User) {
	org, ok := s.listOrg(w, r, u)
	if !ok {
		return
	}
	list, err := s.store.ListStrategies(r.Context(), org)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeList(w, len(list), list)
}

func strategyInput(w http.ResponseWriter, r *http.Request) (store.StrategyInput, string, bool) {
	var req struct {
		Name      string            `json:"name"`
		Note      string            `json:"note"`
		Options   map[string]string `json:"options"`
		IsDefault bool              `json:"is_default"`
		OrgGUID   string            `json:"org_guid"` // on create, server staff only
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request: options must be an object of string values")
		return store.StrategyInput{}, "", false
	}
	return store.StrategyInput{Name: req.Name, Note: req.Note, Options: req.Options, IsDefault: req.IsDefault}, req.OrgGUID, true
}

func (s *Server) writeStrategyResult(w http.ResponseWriter, v *store.Strategy, err error) {
	switch {
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "A strategy with this name already exists")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Not found")
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusOK, v)
	}
}

func (s *Server) adminCreateStrategy(w http.ResponseWriter, r *http.Request, u *store.User) {
	in, orgGUID, ok := strategyInput(w, r)
	if !ok {
		return
	}
	if in.OrgID, ok = s.targetOrg(w, r, u, orgGUID); !ok {
		return
	}
	v, err := s.store.CreateStrategy(r.Context(), in)
	s.writeStrategyResult(w, v, err)
}

func (s *Server) adminUpdateStrategy(w http.ResponseWriter, r *http.Request, u *store.User) {
	in, _, ok := strategyInput(w, r)
	if !ok {
		return
	}
	if _, ok := s.scopedStrategy(w, r, u, r.PathValue("guid")); !ok {
		return
	}
	v, err := s.store.UpdateStrategy(r.Context(), r.PathValue("guid"), in)
	s.writeStrategyResult(w, v, err)
}

func (s *Server) adminDeleteStrategy(w http.ResponseWriter, r *http.Request, u *store.User) {
	if _, ok := s.scopedStrategy(w, r, u, r.PathValue("guid")); !ok {
		return
	}
	if err := s.store.DeleteStrategy(r.Context(), r.PathValue("guid")); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// strategyID resolves an optional strategy guid ("" clears the
// assignment); the strategy must belong to organization org.
func (s *Server) strategyID(w http.ResponseWriter, r *http.Request, u *store.User, guid string, org int64) (int64, bool) {
	if guid == "" {
		return 0, true
	}
	st, ok := s.scopedStrategy(w, r, u, guid)
	if !ok {
		return 0, false
	}
	if st.OrgID != org {
		writeError(w, http.StatusBadRequest, "The strategy belongs to another organization")
		return 0, false
	}
	return st.ID, true
}

func (s *Server) adminSetDeviceStrategy(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		StrategyGUID string `json:"strategy_guid"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	dev, ok := s.scopedDevice(w, r, u, r.PathValue("id"))
	if !ok {
		return
	}
	id, ok := s.strategyID(w, r, u, req.StrategyGUID, dev.OrgID)
	if !ok {
		return
	}
	if err := s.store.SetDeviceStrategy(r.Context(), r.PathValue("id"), id); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) adminSetGroupStrategy(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		StrategyGUID string `json:"strategy_guid"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	grp, ok := s.scopedGroup(w, r, u, r.PathValue("guid"))
	if !ok {
		return
	}
	id, ok := s.strategyID(w, r, u, req.StrategyGUID, grp.OrgID)
	if !ok {
		return
	}
	if err := s.store.SetGroupStrategy(r.Context(), r.PathValue("guid"), id); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
