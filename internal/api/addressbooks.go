package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/BorisGB2001/RuConnect/internal/store"
)

func (s *Server) handleABPersonal(w http.ResponseWriter, r *http.Request, u *store.User) {
	ab, err := s.store.PersonalAB(r.Context(), u.ID)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"guid": ab.GUID})
}

func (s *Server) handleABSettings(w http.ResponseWriter, r *http.Request, u *store.User) {
	writeJSON(w, http.StatusOK, map[string]int{"max_peer_one_ab": 0})
}

type abProfile struct {
	GUID  string         `json:"guid"`
	Name  string         `json:"name"`
	Owner string         `json:"owner"`
	Note  string         `json:"note"`
	Rule  int            `json:"rule"`
	Info  map[string]any `json:"info"`
}

func (s *Server) handleABSharedProfiles(w http.ResponseWriter, r *http.Request, u *store.User) {
	books, total, err := s.store.SharedABsForUser(r.Context(), u, pageOf(r))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	out := make([]abProfile, 0, len(books))
	for _, b := range books {
		info := map[string]any{}
		if b.Password != "" {
			info["password"] = b.Password
		}
		out = append(out, abProfile{GUID: b.GUID, Name: b.Name, Owner: b.Owner, Note: b.Note, Rule: b.Rule, Info: info})
	}
	writeList(w, total, out)
}

// book loads the address book named by guid for u, requiring at least minRule.
func (s *Server) book(w http.ResponseWriter, r *http.Request, u *store.User, guid string, minRule int) (*store.AddressBook, bool) {
	ab, err := s.store.ABForUser(r.Context(), guid, u)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Address book not found")
		return nil, false
	}
	if err != nil {
		s.writeStoreError(w, err)
		return nil, false
	}
	if ab.Rule < minRule {
		writeError(w, http.StatusForbidden, "No permission")
		return nil, false
	}
	return ab, true
}

func (s *Server) handleABPeers(w http.ResponseWriter, r *http.Request, u *store.User) {
	ab, ok := s.book(w, r, u, r.URL.Query().Get("ab"), store.RuleRead)
	if !ok {
		return
	}
	peers, total, err := s.store.ListABPeers(r.Context(), ab.ID, pageOf(r))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	ids := make([]string, 0, len(peers))
	for _, p := range peers {
		ids = append(ids, p.ID)
	}
	pr, err := s.newPeerResolver(r, ids)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	for _, p := range peers {
		p.ID = pr.address(p.ID)
		// Personal books keep the client's password hash, shared books a plain password.
		if ab.Personal {
			p.Password = ""
		} else {
			p.Hash = ""
		}
	}
	writeList(w, total, peers)
}

func (s *Server) handleABTags(w http.ResponseWriter, r *http.Request, u *store.User) {
	ab, ok := s.book(w, r, u, r.PathValue("guid"), store.RuleRead)
	if !ok {
		return
	}
	tags, err := s.store.ListABTags(r.Context(), ab.ID)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) handleABPeerAdd(w http.ResponseWriter, r *http.Request, u *store.User) {
	ab, ok := s.book(w, r, u, r.PathValue("guid"), store.RuleWrite)
	if !ok {
		return
	}
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	p := &store.ABPeer{ID: s.normalizePeerID(r, strings.TrimSpace(str(req["id"])))}
	if p.ID == "" {
		writeError(w, http.StatusBadRequest, "Peer id is required")
		return
	}
	cols := peerColumns(req, ab.Personal)
	p.Alias, p.Hash, p.Password = cols["alias"], cols["hash"], cols["password"]
	p.Username, p.Hostname, p.Platform, p.Note = cols["username"], cols["hostname"], cols["platform"], cols["note"]
	p.ForceAlwaysRelay, p.RdpPort, p.RdpUsername, p.LoginName = cols["force_always_relay"], cols["rdp_port"], cols["rdp_username"], cols["login_name"]
	if t, ok := cols["tags"]; ok {
		json.Unmarshal([]byte(t), &p.Tags)
	}
	if err := s.store.AddABPeer(r.Context(), ab.ID, p); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeEmpty(w)
}

// peerColumns converts a client peer JSON object into ab_peers column values.
func peerColumns(req map[string]any, personal bool) map[string]string {
	cols := map[string]string{}
	for key, col := range store.ABPeerColumns {
		v, ok := req[key]
		if !ok {
			continue
		}
		if personal && key == "password" || !personal && key == "hash" {
			continue
		}
		if key == "tags" {
			tags := []string{}
			if arr, ok := v.([]any); ok {
				for _, t := range arr {
					if ts := str(t); ts != "" {
						tags = append(tags, ts)
					}
				}
			}
			b, _ := json.Marshal(tags)
			cols[col] = string(b)
			continue
		}
		cols[col] = str(v)
	}
	return cols
}

func (s *Server) handleABPeerUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	ab, ok := s.book(w, r, u, r.PathValue("guid"), store.RuleWrite)
	if !ok {
		return
	}
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	id := s.normalizePeerID(r, strings.TrimSpace(str(req["id"])))
	if id == "" {
		writeError(w, http.StatusBadRequest, "Peer id is required")
		return
	}
	if err := s.store.UpdateABPeer(r.Context(), ab.ID, id, peerColumns(req, ab.Personal)); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeEmpty(w)
}

func (s *Server) handleABPeerDelete(w http.ResponseWriter, r *http.Request, u *store.User) {
	ab, ok := s.book(w, r, u, r.PathValue("guid"), store.RuleWrite)
	if !ok {
		return
	}
	var ids []string
	if err := readJSON(r, &ids); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	for i, id := range ids {
		ids[i] = s.normalizePeerID(r, id)
	}
	if err := s.store.DeleteABPeers(r.Context(), ab.ID, ids); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeEmpty(w)
}

func (s *Server) handleABTagAdd(w http.ResponseWriter, r *http.Request, u *store.User) {
	ab, ok := s.book(w, r, u, r.PathValue("guid"), store.RuleWrite)
	if !ok {
		return
	}
	var t store.ABTag
	if err := readJSON(r, &t); err != nil || strings.TrimSpace(t.Name) == "" {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if err := s.store.AddABTag(r.Context(), ab.ID, t); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeEmpty(w)
}

func (s *Server) handleABTagRename(w http.ResponseWriter, r *http.Request, u *store.User) {
	ab, ok := s.book(w, r, u, r.PathValue("guid"), store.RuleWrite)
	if !ok {
		return
	}
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := readJSON(r, &req); err != nil || req.Old == "" || strings.TrimSpace(req.New) == "" {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if err := s.store.RenameABTag(r.Context(), ab.ID, req.Old, req.New); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeEmpty(w)
}

func (s *Server) handleABTagUpdate(w http.ResponseWriter, r *http.Request, u *store.User) {
	ab, ok := s.book(w, r, u, r.PathValue("guid"), store.RuleWrite)
	if !ok {
		return
	}
	var t store.ABTag
	if err := readJSON(r, &t); err != nil || t.Name == "" {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if err := s.store.UpdateABTagColor(r.Context(), ab.ID, t); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeEmpty(w)
}

func (s *Server) handleABTagDelete(w http.ResponseWriter, r *http.Request, u *store.User) {
	ab, ok := s.book(w, r, u, r.PathValue("guid"), store.RuleWrite)
	if !ok {
		return
	}
	var names []string
	if err := readJSON(r, &names); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if err := s.store.DeleteABTags(r.Context(), ab.ID, names); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeEmpty(w)
}

// Legacy address book: one JSON document per user, used by old and Sciter clients.

func (s *Server) handleLegacyABGet(w http.ResponseWriter, r *http.Request, u *store.User) {
	data, _, err := s.store.LegacyAB(r.Context(), u.ID)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	if data == "" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("null"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "licensed_devices": 0})
}

func (s *Server) handleLegacyABSet(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		Data string `json:"data"`
	}
	if err := readJSONLimit(r, &req, maxLargeBody); err != nil || !json.Valid([]byte(req.Data)) {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if err := s.store.SetLegacyAB(r.Context(), u.ID, req.Data); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeEmpty(w)
}

func (s *Server) handleLegacyABGetSciter(w http.ResponseWriter, r *http.Request, u *store.User) {
	data, updated, err := s.store.LegacyAB(r.Context(), u.ID)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	if data == "" {
		data = `{"tags":[],"peers":[]}`
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "updated_at": updated})
}
