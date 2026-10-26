package api

import (
	"errors"
	"net/http"

	"github.com/BorisGB2001/RuConnect/internal/store"
)

// Panel API for strategies: named client option sets pushed via heartbeat.

func (s *Server) adminListStrategies(w http.ResponseWriter, r *http.Request, u *store.User) {
	list, err := s.store.ListStrategies(r.Context())
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeList(w, len(list), list)
}

func strategyInput(w http.ResponseWriter, r *http.Request) (store.StrategyInput, bool) {
	var req struct {
		Name      string            `json:"name"`
		Note      string            `json:"note"`
		Options   map[string]string `json:"options"`
		IsDefault bool              `json:"is_default"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request: options must be an object of string values")
		return store.StrategyInput{}, false
	}
	return store.StrategyInput{Name: req.Name, Note: req.Note, Options: req.Options, IsDefault: req.IsDefault}, true
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
	in, ok := strategyInput(w, r)
	if !ok {
		return
	}
	v, err := s.store.CreateStrategy(r.Context(), in)
	s.writeStrategyResult(w, v, err)
}

func (s *Server) adminUpdateStrategy(w http.ResponseWriter, r *http.Request, u *store.User) {
	in, ok := strategyInput(w, r)
	if !ok {
		return
	}
	v, err := s.store.UpdateStrategy(r.Context(), r.PathValue("guid"), in)
	s.writeStrategyResult(w, v, err)
}

func (s *Server) adminDeleteStrategy(w http.ResponseWriter, r *http.Request, u *store.User) {
	if err := s.store.DeleteStrategy(r.Context(), r.PathValue("guid")); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// strategyID resolves an optional strategy guid ("" clears the assignment).
func (s *Server) strategyID(w http.ResponseWriter, r *http.Request, guid string) (int64, bool) {
	if guid == "" {
		return 0, true
	}
	st, err := s.store.StrategyByGUID(r.Context(), guid)
	if err != nil {
		s.writeStoreError(w, err)
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
	id, ok := s.strategyID(w, r, req.StrategyGUID)
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
	id, ok := s.strategyID(w, r, req.StrategyGUID)
	if !ok {
		return
	}
	if err := s.store.SetGroupStrategy(r.Context(), r.PathValue("guid"), id); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
