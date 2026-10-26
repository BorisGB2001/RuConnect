package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ruconnect/ruconnect/internal/store"
)

// Audit records are posted by the controlled device without user auth.
// Retries carry the same nonce, so duplicates are dropped here.

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
		if err := s.store.SetConnNoteBySession(ctx, str(req["id"]), str(req["session_id"]), str(req["note"])); err != nil {
			s.log.Warn("audit note", "err", err)
		}
		writeEmpty(w)
		return
	}
	if nonce := str(req["nonce"]); nonce != "" && s.nonces.Seen("conn|"+nonce) {
		writeEmpty(w)
		return
	}
	e := store.ConnEvent{
		DeviceID:     str(req["id"]),
		DeviceUUID:   str(req["uuid"]),
		ConnID:       num(req["conn_id"]),
		SessionID:    str(req["session_id"]),
		Action:       str(req["action"]),
		IP:           str(req["ip"]),
		ConnAuditRef: str(req["conn_audit_ref"]),
		ConnType:     -1,
	}
	if e.DeviceID == "" {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if peer, ok := req["peer"].([]any); ok {
		if len(peer) > 0 {
			e.PeerID = str(peer[0])
		}
		if len(peer) > 1 {
			e.PeerName = str(peer[1])
		}
		e.ConnType = int(num(req["type"]))
		e.PrimaryAuth = int(num(req["primary_auth"]))
		e.TwoFactor = int(num(req["two_factor"]))
	}
	if err := s.store.RecordConnEvent(ctx, e); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeEmpty(w)
}

func (s *Server) handleAuditFile(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if nonce := str(req["nonce"]); nonce != "" && s.nonces.Seen("file|"+nonce) {
		writeEmpty(w)
		return
	}
	a := store.FileAudit{
		DeviceID: str(req["id"]),
		PeerID:   str(req["peer_id"]),
		ConnID:   num(req["conn_id"]),
		Type:     int(num(req["type"])),
		Path:     str(req["path"]),
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
		a.PeerIP, a.PeerName, a.Num = info.IP, info.Name, info.Num
		if len(info.Files) > 0 {
			a.Files = string(info.Files)
		}
	}
	if err := s.store.AddFileAudit(r.Context(), a); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeEmpty(w)
}

func (s *Server) handleAuditAlarm(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if nonce := str(req["nonce"]); nonce != "" && s.nonces.Seen("alarm|"+nonce) {
		writeEmpty(w)
		return
	}
	a := store.AlarmAudit{
		DeviceID: str(req["id"]),
		ConnID:   num(req["conn_id"]),
		Typ:      int(num(req["typ"])),
		Info:     str(req["info"]),
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
	s.log.Warn("security alarm", "device", a.DeviceID, "typ", a.Typ, "ip", a.IP)
	writeEmpty(w)
}

// handleAuditConnActive returns, as a JSON string, the audit GUID of a session
// the caller opened, so the client can attach a note at the end.
func (s *Server) handleAuditConnActive(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	guid, err := s.store.ConnGUIDBySession(r.Context(), q.Get("id"), q.Get("session_id"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, guid)
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
	if err := s.store.SetConnNote(r.Context(), req.GUID, note); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeEmpty(w)
}
