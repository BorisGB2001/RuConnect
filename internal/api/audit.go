package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ruconnect/ruconnect/internal/store"
)

// Audit records are posted by the controlled device without user auth.
// Retries carry the same nonce, so duplicates are dropped here.

// knownDevice accepts an audit post only from a registered device whose
// uuid matches, so records cannot be forged for other devices. A 4xx makes
// the client drop the record instead of retrying.
func (s *Server) knownDevice(w http.ResponseWriter, r *http.Request, req map[string]any, nonce string) bool {
	id, uuid := str(req["id"]), str(req["uuid"])
	if id == "" || len(id) > maxID || len(uuid) > maxUUID || len(nonce) > 64 {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return false
	}
	ok, err := s.store.DeviceMatches(r.Context(), id, uuid)
	if err != nil {
		s.writeStoreError(w, err)
		return false
	}
	if !ok {
		writeError(w, http.StatusForbidden, "Unknown device")
		return false
	}
	return true
}

func (s *Server) handleAuditConn(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	ctx := r.Context()
	_, hasNote := req["note"]
	_, hasConn := req["conn_id"]
	if hasNote && !hasConn {
		// Note from the controlling side: {id: remote peer id, session_id, note}.
		id := s.normalizePeerID(r, str(req["id"]))
		if err := s.store.SetConnNoteBySession(ctx, id, str(req["session_id"]), clip(str(req["note"]), 1024)); err != nil {
			s.log.Warn("audit note", "err", err)
		}
		writeEmpty(w)
		return
	}
	nonce := str(req["nonce"])
	if !s.knownDevice(w, r, req, nonce) {
		return
	}
	if nonce != "" && s.nonces.Has("conn|"+nonce) {
		writeEmpty(w)
		return
	}
	e := store.ConnEvent{
		DeviceID:     str(req["id"]),
		DeviceUUID:   str(req["uuid"]),
		ConnID:       num(req["conn_id"]),
		SessionID:    str(req["session_id"]),
		Action:       str(req["action"]),
		IP:           clip(str(req["ip"]), 64),
		ConnAuditRef: clip(str(req["conn_audit_ref"]), maxField),
		ConnType:     -1,
	}
	if e.DeviceID == "" {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if peer, ok := req["peer"].([]any); ok {
		if len(peer) > 0 {
			e.PeerID = clip(str(peer[0]), maxID)
		}
		if len(peer) > 1 {
			e.PeerName = clip(str(peer[1]), maxField)
		}
		e.ConnType = int(num(req["type"]))
		e.PrimaryAuth = int(num(req["primary_auth"]))
		e.TwoFactor = int(num(req["two_factor"]))
	}
	if err := s.store.RecordConnEvent(ctx, e); err != nil {
		s.writeStoreError(w, err)
		return
	}
	s.nonces.Add("conn|" + nonce)
	writeEmpty(w)
}

func (s *Server) handleAuditFile(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	nonce := str(req["nonce"])
	if !s.knownDevice(w, r, req, nonce) {
		return
	}
	if nonce != "" && s.nonces.Has("file|"+nonce) {
		writeEmpty(w)
		return
	}
	a := store.FileAudit{
		DeviceID: str(req["id"]),
		PeerID:   clip(str(req["peer_id"]), maxID),
		ConnID:   num(req["conn_id"]),
		Type:     int(num(req["type"])),
		Path:     clip(str(req["path"]), maxText),
		IsFile:   req["is_file"] == true,
	}
	if a.DeviceID == "" {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	// info is a JSON-encoded string: {"ip","name","num","files":[[name,size],...]}.
	var info struct {
		IP    string          `json:"ip"`
		Name  string          `json:"name"`
		Num   int64           `json:"num"`
		Files json.RawMessage `json:"files"`
	}
	if json.Unmarshal([]byte(str(req["info"])), &info) == nil {
		a.PeerIP, a.PeerName, a.Num = clip(info.IP, 64), clip(info.Name, maxField), info.Num
		if len(info.Files) > 0 && len(info.Files) <= maxText {
			a.Files = string(info.Files)
		}
	}
	if err := s.store.AddFileAudit(r.Context(), a); err != nil {
		s.writeStoreError(w, err)
		return
	}
	s.nonces.Add("file|" + nonce)
	writeEmpty(w)
}

func (s *Server) handleAuditAlarm(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	nonce := str(req["nonce"])
	if !s.knownDevice(w, r, req, nonce) {
		return
	}
	if nonce != "" && s.nonces.Has("alarm|"+nonce) {
		writeEmpty(w)
		return
	}
	a := store.AlarmAudit{
		DeviceID: str(req["id"]),
		ConnID:   num(req["conn_id"]),
		Typ:      int(num(req["typ"])),
		Info:     clip(str(req["info"]), maxText),
	}
	if a.DeviceID == "" {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	var info struct {
		IP string `json:"ip"`
	}
	if json.Unmarshal([]byte(a.Info), &info) == nil {
		a.IP = info.IP
	}
	if err := s.store.AddAlarmAudit(r.Context(), a); err != nil {
		s.writeStoreError(w, err)
		return
	}
	s.nonces.Add("alarm|" + nonce)
	s.log.Warn("security alarm", "device", a.DeviceID, "typ", a.Typ, "ip", a.IP)
	writeEmpty(w)
}

// handleAuditConnActive returns, as a JSON string, the audit GUID of a session
// the caller opened, so the client can attach a note at the end.
func (s *Server) handleAuditConnActive(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	guid, err := s.store.ConnGUIDBySession(r.Context(), s.normalizePeerID(r, q.Get("id")), q.Get("session_id"), s.controllerID(r, u))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, guid)
}

// controllerID limits session notes to sessions opened from the device the
// caller logged in on. Administrators may annotate any session ("").
func (s *Server) controllerID(r *http.Request, u *store.User) string {
	if u.IsAdmin {
		return ""
	}
	_, tok, err := s.authUser(r)
	if err != nil || tok.DeviceID == "" {
		return "\x00" // matches nothing
	}
	return tok.DeviceID
}

func (s *Server) handleAuditNote(w http.ResponseWriter, r *http.Request, u *store.User) {
	var req struct {
		GUID string `json:"guid"`
		Note string `json:"note"`
	}
	if err := readJSON(r, &req); err != nil || req.GUID == "" {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	note := strings.TrimSpace(req.Note)
	if len(note) > 1024 {
		note = note[:1024]
	}
	if err := s.store.SetConnNote(r.Context(), req.GUID, note, s.controllerID(r, u)); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeEmpty(w)
}
