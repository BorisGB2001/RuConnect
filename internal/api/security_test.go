package api_test

import (
	"net/http"
	"strings"
	"testing"
)

// Regression tests for the security review findings.

func TestSysinfoCannotTakeOverDevice(t *testing.T) {
	e := newEnv(t)
	e.registerDevice("100200300", "real-pc")
	r := e.sysinfo("100200300", "evil", map[string]any{"uuid": "EVIL", "preset-user-name": adminName})
	if string(r.Body) == "SYSINFO_UPDATED" {
		t.Fatalf("takeover accepted: %s", r)
	}
	// The real device keeps working and its record is untouched.
	if hb := e.heartbeat("100200300", nil); hb["sysinfo"] != nil {
		t.Fatalf("real device lost its registration: %v", hb)
	}
	r = e.get("/api/admin/devices?q=100200300", e.admin)
	if !strings.Contains(string(r.Body), `"hostname":"real-pc"`) || strings.Contains(string(r.Body), `"user_name":"admin"`) {
		t.Fatalf("device record changed: %s", r)
	}
}

func TestUnknownHeartbeatKeepsNoState(t *testing.T) {
	e := newEnv(t)
	e.registerDevice("100200301", "pc")
	e.heartbeat("100200301", []int{7})
	e.post("/api/admin/devices/100200301/disconnect", e.admin, nil)
	// A spoofed heartbeat (wrong uuid) must not consume the queued disconnect.
	r := e.post("/api/heartbeat", "", map[string]any{"id": "100200301", "uuid": "spoof", "conns": []int{1}})
	if !strings.Contains(string(r.Body), `"sysinfo"`) || strings.Contains(string(r.Body), "disconnect") {
		t.Fatalf("spoofed heartbeat: %s", r)
	}
	if hb := e.heartbeat("100200301", []int{7}); hb["disconnect"] == nil {
		t.Fatalf("disconnect was lost: %v", hb)
	}
}

func TestOversizedRequestsRejected(t *testing.T) {
	e := newEnv(t)
	big := strings.Repeat("a", 300<<10)
	if r := e.post("/api/login", "", map[string]any{"username": big, "password": "x"}); r.Status != http.StatusBadRequest {
		t.Fatalf("oversized login: %s", r)
	}
	if r := e.post("/api/heartbeat", "", map[string]any{"id": strings.Repeat("9", 100), "uuid": "u"}); r.Status != http.StatusBadRequest {
		t.Fatalf("oversized id: %s", r)
	}
}

func TestLoginClaimNeedsMatchingUUID(t *testing.T) {
	e := newEnv(t)
	e.registerDevice("100200302", "pc")
	e.createUser("mallory", "mallory-password", false)
	body := loginBody("mallory", "mallory-password")
	body["id"], body["uuid"] = "100200302", "not-the-device"
	if r := e.post("/api/login", "", body); r.Status != http.StatusOK {
		t.Fatalf("login: %s", r)
	}
	if r := e.get("/api/admin/devices?q=100200302", e.admin); strings.Contains(string(r.Body), `"user_name":"mallory"`) {
		t.Fatalf("device claimed with a wrong uuid: %s", r)
	}
}

func TestDeployTokenScope(t *testing.T) {
	e := newEnv(t)
	r := e.post("/api/admin/tokens", e.admin, map[string]any{"days": 1})
	tok, _ := r.obj(t)["token"].(string)
	if r := e.get("/api/admin/users", tok); r.Status != http.StatusForbidden {
		t.Fatalf("deploy token reached the panel API: %s", r)
	}
	e.registerDevice("100200303", "pc")
	if r := e.post("/api/devices/cli", tok, map[string]any{"id": "100200303", "uuid": testUUID, "note": "x"}); r.Status != http.StatusOK {
		t.Fatalf("deploy token rejected by --assign: %s", r)
	}
	// Listed and revocable.
	r = e.get("/api/admin/tokens", e.admin)
	_, items := listData(t, r)
	if len(items) != 1 {
		t.Fatalf("tokens: %s", r)
	}
	id := items[0]["id"].(string)
	if r := e.del("/api/admin/tokens/"+id, e.admin, nil); r.Status != http.StatusOK {
		t.Fatalf("revoke: %s", r)
	}
	if r := e.post("/api/devices/cli", tok, map[string]any{"id": "100200303", "uuid": testUUID, "note": "y"}); r.Status != http.StatusUnauthorized {
		t.Fatalf("revoked token still works: %s", r)
	}
}

func TestPasswordChangeRevokesOtherSessions(t *testing.T) {
	e := newEnv(t)
	other := e.login(adminName, adminPass)
	if r := e.put("/api/admin/password", e.admin, map[string]any{"old": adminPass, "new": "new-password-1"}); r.Status != http.StatusOK {
		t.Fatalf("change password: %s", r)
	}
	if r := e.post("/api/currentUser", other, nil); r.Status != http.StatusUnauthorized {
		t.Fatalf("old session survived: %s", r)
	}
	if r := e.post("/api/currentUser", e.admin, nil); r.Status != http.StatusOK {
		t.Fatalf("current session was revoked: %s", r)
	}
}

func TestCSVFormulaNeutralized(t *testing.T) {
	e := newEnv(t)
	e.auditPost("conn", rawConn("555000111", 1, "9", nonce(), `"peer":["1","=HYPERLINK(\"http://x\")"],"type":0`))
	r := e.get("/api/admin/audit/conns?format=csv", e.admin)
	if strings.Contains(string(r.Body), ",=HYPERLINK") || !strings.Contains(string(r.Body), "'=HYPERLINK") {
		t.Fatalf("formula not neutralized: %s", r.Body)
	}
}
