package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// auditPost posts a device-side audit record (no Authorization header) and
// checks the client's success rule: 2xx with an empty (whitespace-only) body.
func (e *env) auditPost(typ string, body any) {
	e.t.Helper()
	// Audit posts are only accepted from registered devices; register the
	// sender (sysinfo is idempotent for the same id and uuid).
	var m map[string]any
	switch v := body.(type) {
	case string:
		json.Unmarshal([]byte(v), &m)
	case map[string]any:
		m = v
	}
	if id, _ := m["id"].(string); id != "" {
		if uuid, _ := m["uuid"].(string); uuid != "" {
			e.post("/api/sysinfo", "", map[string]any{"id": id, "uuid": uuid, "hostname": "h-" + id})
		}
	}
	r := e.post("/api/audit/"+typ, "", body)
	if r.Status/100 != 2 || strings.TrimSpace(string(r.Body)) != "" {
		e.t.Errorf("audit/%s: want 2xx with an empty body, got %s", typ, r)
	}
}

func (e *env) connAudits() []map[string]any {
	e.t.Helper()
	_, items := listData(e.t, e.get("/api/admin/audit/conns?current=1&pageSize=100", e.admin))
	return items
}

// rawConn builds a raw JSON body so that u64 session ids are sent as JSON
// numbers exactly like the Rust client does (no float rounding).
func rawConn(id string, connID int, sessionID, nonce, extra string) string {
	s := fmt.Sprintf(`{"id":%q,"uuid":%q,"conn_id":%d,"session_id":%s,"nonce":%q`, id, testUUID, connID, sessionID, nonce)
	if extra != "" {
		s += "," + extra
	}
	return s + "}"
}

func connLifecycle(e *env, id string, connID int, newSession, session string) {
	e.auditPost("conn", rawConn(id, connID, newSession, nonce(), `"ip":"203.0.113.5","action":"new","conn_audit_ref":"ref-1"`))
	e.auditPost("conn", rawConn(id, connID, session, nonce(), `"peer":["999888777","Controller PC"],"type":0,"primary_auth":2,"two_factor":1`))
	e.auditPost("conn", rawConn(id, connID, session, nonce(), `"action":"close"`))
}

func checkFoldedRow(t *testing.T, rows []map[string]any, session string) map[string]any {
	t.Helper()
	if len(rows) != 1 {
		t.Fatalf("want exactly one folded audit row, got %d: %v", len(rows), rows)
	}
	a := rows[0]
	want := map[string]string{
		"device_id": "555000111", "conn_id": "12", "session_id": session, "ip": "203.0.113.5",
		"peer_id": "999888777", "peer_name": "Controller PC", "conn_type": "0", "primary_auth": "2",
		"two_factor": "1", "conn_audit_ref": "ref-1",
	}
	for k, v := range want {
		if got := numStr(a[k]); got != v {
			t.Errorf("%s = %s, want %s", k, got, v)
		}
	}
	if numStr(a["authorized_at"]) == "0" || numStr(a["closed_at"]) == "0" {
		t.Errorf("authorized_at / closed_at not set: %v", a)
	}
	return a
}

func TestAuditConnLifecycle(t *testing.T) {
	e := newEnv(t)
	connLifecycle(e, "555000111", 12, "987654321", "987654321")
	checkFoldedRow(t, e.connAudits(), "987654321")
}

// The stock client posts the "new" record before login, when its
// LoginRequest.session_id is still 0; the authorized/close records carry the
// real session id. They must still fold into one row.
func TestAuditConnNewBeforeSessionKnown(t *testing.T) {
	e := newEnv(t)
	connLifecycle(e, "555000111", 12, "0", "987654321")
	rows := e.connAudits()
	if len(rows) != 1 || numStr(rows[0]["ip"]) != "203.0.113.5" || numStr(rows[0]["closed_at"]) == "0" {
		t.Fatalf("regression: 'new' record with session_id 0 is not folded with the authorized/close records "+
			"(internal/store/audit.go openConn matches session_id only when equal or ''): got %d rows %v", len(rows), rows)
	}
	checkFoldedRow(t, rows, "987654321")
}

func TestAuditConnNonceDedup(t *testing.T) {
	e := newEnv(t)
	n := nonce()
	body := rawConn("555000111", 12, "1", n, `"ip":"203.0.113.5","action":"new"`)
	e.auditPost("conn", body)
	e.auditPost("conn", body) // retried post
	if rows := e.connAudits(); len(rows) != 1 {
		t.Fatalf("retried post with the same nonce was not deduplicated: %d rows", len(rows))
	}
	// A different nonce is a new record (conn ids restart with the client process).
	e.auditPost("conn", rawConn("555000111", 12, "1", nonce(), `"ip":"203.0.113.5","action":"new"`))
	if rows := e.connAudits(); len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
}

func TestAuditControllerNote(t *testing.T) {
	e := newEnv(t)
	connLifecycle(e, "555000111", 12, "987654321", "987654321")
	// Sent by the controlling client: remote peer id, session id, note; no uuid/conn_id/nonce/auth.
	e.auditPost("conn", `{"id":"555000111","session_id":987654321,"note":"in-session note"}`)
	rows := e.connAudits()
	if len(rows) != 1 {
		t.Fatalf("note created a row: %v", rows)
	}
	if rows[0]["note"] != "in-session note" {
		t.Errorf("note = %v", rows[0]["note"])
	}
}

func TestAuditConnActiveAndNote(t *testing.T) {
	e := newEnv(t)
	connLifecycle(e, "555000111", 12, "987654321", "987654321")
	guid := e.connAudits()[0]["guid"].(string)
	tok := e.controllerToken("ctrl", "999888777")

	r := e.get("/api/audit/conn/active?id=555000111&session_id=987654321&conn_type=0", tok)
	if r.Status != http.StatusOK {
		t.Fatalf("active: %s", r)
	}
	var got any
	r.decode(t, &got)
	if s, ok := got.(string); !ok || s != guid {
		t.Fatalf("active must return the guid as a JSON string, got %s", r)
	}

	r = e.get("/api/audit/conn/active?id=555000111&session_id=1&conn_type=0", tok)
	var none any
	r.decode(t, &none)
	if r.Status != http.StatusOK || (none != nil && none != "") {
		t.Errorf("unknown session: want 200 with \"\" or null (keep polling), got %s", r)
	}

	r = e.put("/api/audit", tok, map[string]any{"guid": guid, "note": "end of session"})
	if r.Status != http.StatusOK {
		t.Fatalf("PUT /api/audit: %s", r)
	}
	if n := e.connAudits()[0]["note"]; n != "end of session" {
		t.Errorf("note = %v", n)
	}
	expectError(t, "PUT unknown guid", e.put("/api/audit", tok, map[string]any{"guid": "nope", "note": "x"}), 0)
	expectError(t, "PUT without token", e.put("/api/audit", "", map[string]any{"guid": guid, "note": "x"}), http.StatusUnauthorized)
}

func TestAuditBigSessionID(t *testing.T) {
	e := newEnv(t)
	const big = "18446744073709551615" // u64 max, > 2^63
	connLifecycle(e, "555000111", 12, big, big)
	rows := e.connAudits()
	a := checkFoldedRow(t, rows, big)
	if s, ok := a["session_id"].(string); !ok || s != big {
		t.Errorf("session_id must round-trip as the decimal string %s, got %#v", big, a["session_id"])
	}

	tok := e.controllerToken("ctrl", "999888777")
	var guid any
	e.get("/api/audit/conn/active?id=555000111&session_id="+big+"&conn_type=0", tok).decode(t, &guid)
	if guid != a["guid"] {
		t.Errorf("active with big session id = %v, want %v", guid, a["guid"])
	}
	e.auditPost("conn", `{"id":"555000111","session_id":`+big+`,"note":"big note"}`)
	if n := e.connAudits()[0]["note"]; n != "big note" {
		t.Errorf("note by big session id = %v", n)
	}

	// 2^63 exactly, distinct from 2^63-1.
	connLifecycle(e, "555000222", 1, "9223372036854775808", "9223372036854775808")
	connLifecycle(e, "555000222", 2, "9223372036854775807", "9223372036854775807")
	seen := map[string]bool{}
	for _, r := range e.connAudits() {
		seen[numStr(r["session_id"])] = true
	}
	if !seen["9223372036854775808"] || !seen["9223372036854775807"] {
		t.Errorf("session ids around 2^63 not preserved: %v", seen)
	}
}

func TestAuditFile(t *testing.T) {
	e := newEnv(t)
	n := nonce()
	body := map[string]any{
		"id": "555000111", "uuid": testUUID, "peer_id": "999888777", "conn_id": 12, "type": 1,
		"path": `C:\data`, "is_file": false,
		"info":  `{"ip":"203.0.113.5","name":"Controller PC","num":3,"files":[["a.txt",1024],["b.bin",99]]}`,
		"nonce": n,
	}
	e.auditPost("file", body)
	e.auditPost("file", body) // retry
	_, items := listData(t, e.get("/api/admin/audit/files", e.admin))
	if len(items) != 1 {
		t.Fatalf("file audits = %v, want 1 (nonce dedup)", items)
	}
	a := items[0]
	want := map[string]string{
		"device_id": "555000111", "peer_id": "999888777", "conn_id": "12", "type": "1", "path": `C:\data`,
		"peer_ip": "203.0.113.5", "peer_name": "Controller PC", "num": "3",
		"files": `[["a.txt",1024],["b.bin",99]]`,
	}
	for k, v := range want {
		if got := numStr(a[k]); got != v {
			t.Errorf("%s = %s, want %s", k, got, v)
		}
	}
	if a["is_file"] != false {
		t.Errorf("is_file = %v", a["is_file"])
	}
	// Clipboard transfer: empty path, is_file true.
	e.auditPost("file", map[string]any{"id": "555000111", "uuid": testUUID, "peer_id": "1", "conn_id": 1, "type": 0,
		"path": "", "is_file": true, "info": `{"ip":"1.2.3.4","name":"x","num":1,"files":[["",5]]}`, "nonce": nonce()})
	if _, items := listData(t, e.get("/api/admin/audit/files", e.admin)); len(items) != 2 || items[0]["is_file"] != true {
		t.Errorf("clipboard file audit = %v", items)
	}
}

func TestAuditAlarm(t *testing.T) {
	e := newEnv(t)
	n := nonce()
	body := map[string]any{
		"id": "555000111", "uuid": testUUID, "conn_id": 12, "nonce": n,
		"typ": 2, "info": `{"ip":"203.0.113.5","id":"999","name":"attacker"}`,
	}
	e.auditPost("alarm", body)
	e.auditPost("alarm", body)
	e.auditPost("alarm", map[string]any{"id": "555000111", "uuid": testUUID, "conn_id": 0, "nonce": nonce(),
		"typ": 0, "info": `{"ip":"198.51.100.1"}`, "conn_audit_ref": "r"})
	_, items := listData(t, e.get("/api/admin/audit/alarms", e.admin))
	if len(items) != 2 {
		t.Fatalf("alarms = %v, want 2", items)
	}
	a := findBy(items, "typ", "2")
	if a == nil || a["ip"] != "203.0.113.5" || a["device_id"] != "555000111" {
		t.Errorf("alarm typ 2 = %v", a)
	}
	if b := findBy(items, "typ", "0"); b == nil || b["ip"] != "198.51.100.1" {
		t.Errorf("alarm typ 0 = %v", b)
	}
}

func TestAuditBadRecordsAreDropped4xx(t *testing.T) {
	e := newEnv(t)
	// 4xx (not 5xx/408/429) makes the client drop the record instead of retrying forever.
	for _, typ := range []string{"conn", "file", "alarm"} {
		r := e.post("/api/audit/"+typ, "", map[string]any{"nonce": nonce()})
		if r.Status/100 != 4 || r.Status == http.StatusRequestTimeout || r.Status == http.StatusTooManyRequests {
			t.Errorf("audit/%s without id: want a non-retryable 4xx, got %s", typ, r)
		}
	}
}

// Session notes are limited to the controller that opened the session.
func TestAuditNoteForeignSession(t *testing.T) {
	e := newEnv(t)
	connLifecycle(e, "555000111", 12, "987654321", "987654321")
	guid := e.connAudits()[0]["guid"].(string)
	tok := e.controllerToken("other", "111222333")

	r := e.get("/api/audit/conn/active?id=555000111&session_id=987654321&conn_type=0", tok)
	var got any
	r.decode(t, &got)
	if got != "" {
		t.Fatalf("foreign controller got the session guid: %s", r)
	}
	if r := e.put("/api/audit", tok, map[string]any{"guid": guid, "note": "x"}); r.Status != http.StatusNotFound {
		t.Fatalf("foreign controller set a note: %s", r)
	}
}

// Audit posts from unregistered devices or with a wrong uuid are refused.
func TestAuditRequiresKnownDevice(t *testing.T) {
	e := newEnv(t)
	if r := e.post("/api/audit/conn", "", rawConn("777", 1, "5", nonce(), `"action":"new"`)); r.Status != http.StatusForbidden {
		t.Fatalf("unknown device: %s", r)
	}
	e.registerDevice("777", "pc")
	body := `{"id":"777","uuid":"wrong","conn_id":1,"session_id":5,"nonce":"n1","action":"new"}`
	if r := e.post("/api/audit/conn", "", body); r.Status != http.StatusForbidden {
		t.Fatalf("wrong uuid: %s", r)
	}
	if rows := e.connAudits(); len(rows) != 0 {
		t.Fatalf("forged rows stored: %v", rows)
	}
}
