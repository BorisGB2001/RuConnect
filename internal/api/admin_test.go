package api_test

import (
	"net/http"
	"testing"
)

func TestAdminRequiresAdmin(t *testing.T) {
	e := newEnv(t)
	_, tok := e.userWithToken("plain")
	for _, p := range []string{"/api/admin/users", "/api/admin/devices", "/api/admin/groups", "/api/admin/address-books", "/api/admin/audit/conns"} {
		expectError(t, p+" as user", e.get(p, tok), http.StatusForbidden)
		expectError(t, p+" anonymous", e.get(p, ""), http.StatusUnauthorized)
	}
}

func TestAdminUserCRUD(t *testing.T) {
	e := newEnv(t)
	r := e.post("/api/admin/users", e.admin, map[string]any{"name": "kate", "password": "kate-password", "display_name": "Kate K", "email": "k@example.com"})
	if r.Status != http.StatusOK {
		t.Fatalf("create: %s", r)
	}
	u := r.obj(t)
	guid := u["guid"].(string)
	if u["display_name"] != "Kate K" || numStr(u["status"]) != "1" || u["is_admin"] != false {
		t.Errorf("created user = %v", u)
	}
	if _, leak := u["password_hash"]; leak {
		t.Errorf("password hash leaked: %v", u)
	}
	expectError(t, "duplicate", e.post("/api/admin/users", e.admin, map[string]any{"name": "KATE", "password": "kate-password"}), http.StatusConflict)
	expectError(t, "short password", e.post("/api/admin/users", e.admin, map[string]any{"name": "short", "password": "x"}), http.StatusBadRequest)
	expectError(t, "no name", e.post("/api/admin/users", e.admin, map[string]any{"password": "long-enough"}), 0)

	total, items := listData(t, e.get("/api/admin/users", e.admin))
	if total != 2 || findBy(items, "name", "kate") == nil {
		t.Errorf("list = %v", items)
	}
	_, items = listData(t, e.get("/api/admin/users?q=kat", e.admin))
	if len(items) != 1 {
		t.Errorf("search = %v", items)
	}

	r = e.put("/api/admin/users/"+guid, e.admin, map[string]any{"name": "kate", "display_name": "Kate Two", "password": "new-password"})
	if r.Status != http.StatusOK || r.obj(t)["display_name"] != "Kate Two" {
		t.Fatalf("update: %s", r)
	}
	e.login("kate", "new-password")
	expectError(t, "old password", e.post("/api/login", "", loginBody("kate", "kate-password")), 0)

	if r := e.del("/api/admin/users/"+guid, e.admin, nil); r.Status != http.StatusOK {
		t.Fatalf("delete: %s", r)
	}
	expectError(t, "delete again", e.del("/api/admin/users/"+guid, e.admin, nil), http.StatusNotFound)
	if total, _ := listData(t, e.get("/api/admin/users", e.admin)); total != 1 {
		t.Errorf("total after delete = %d", total)
	}
}

func TestAdminLastAdminProtected(t *testing.T) {
	e := newEnv(t)
	_, items := listData(t, e.get("/api/admin/users", e.admin))
	adminGUID := findBy(items, "name", adminName)["guid"].(string)

	expectError(t, "delete last admin", e.del("/api/admin/users/"+adminGUID, e.admin, nil), http.StatusBadRequest)
	expectError(t, "demote last admin", e.put("/api/admin/users/"+adminGUID, e.admin, map[string]any{"name": adminName, "is_admin": false}), http.StatusBadRequest)
	expectError(t, "disable last admin", e.put("/api/admin/users/"+adminGUID, e.admin, map[string]any{"name": adminName, "is_admin": true, "status": 0}), http.StatusBadRequest)
	if r := e.post("/api/currentUser", e.admin, nil); r.Status != http.StatusOK || r.obj(t)["is_admin"] != true {
		t.Fatalf("admin changed despite refusals: %s", r)
	}

	// With a second active admin the first may be demoted.
	e.createUser("admin2", "admin2-password", true)
	if r := e.put("/api/admin/users/"+adminGUID, e.admin, map[string]any{"name": adminName, "is_admin": false}); r.Status != http.StatusOK {
		t.Fatalf("demote with another admin: %s", r)
	}
	// admin2 is now the only admin and cannot delete itself.
	tok2 := e.login("admin2", "admin2-password")
	_, items = listData(t, e.get("/api/admin/users", tok2))
	a2 := findBy(items, "name", "admin2")["guid"].(string)
	expectError(t, "delete the only remaining admin", e.del("/api/admin/users/"+a2, tok2, nil), http.StatusBadRequest)
}

func TestAdminDeviceUpdateDelete(t *testing.T) {
	e := newEnv(t)
	e.registerDevice("600000001", "pc")
	ug := e.createUser("liam", "liam-password", false)
	gg := e.createGroup("Office")

	r := e.put("/api/admin/devices/600000001", e.admin, map[string]any{"note": "n1", "user_guid": ug, "group_guid": gg})
	if r.Status != http.StatusOK {
		t.Fatalf("update: %s", r)
	}
	d := r.obj(t)
	if d["note"] != "n1" || d["user_name"] != "liam" || d["group_name"] != "Office" {
		t.Errorf("updated device = %v", d)
	}
	if _, ok := d["online"].(bool); !ok {
		t.Errorf("online flag missing: %v", d)
	}
	// Only the note changes; assignments stay.
	r = e.put("/api/admin/devices/600000001", e.admin, map[string]any{"note": "n2"})
	if d := r.obj(t); d["note"] != "n2" || d["user_name"] != "liam" {
		t.Errorf("partial update = %v", d)
	}
	// Empty guid clears.
	r = e.put("/api/admin/devices/600000001", e.admin, map[string]any{"user_guid": "", "group_guid": ""})
	if d := r.obj(t); d["user_name"] != "" || d["group_name"] != "" {
		t.Errorf("clear = %v", d)
	}
	expectError(t, "unknown user", e.put("/api/admin/devices/600000001", e.admin, map[string]any{"user_guid": "nope"}), http.StatusNotFound)
	expectError(t, "unknown device", e.put("/api/admin/devices/999", e.admin, map[string]any{"note": "x"}), http.StatusNotFound)

	// Filters.
	e.put("/api/admin/devices/600000001", e.admin, map[string]any{"group_guid": gg})
	e.registerDevice("600000002", "other")
	if total, _ := listData(t, e.get("/api/admin/devices?group="+gg, e.admin)); total != 1 {
		t.Errorf("group filter total = %d", total)
	}
	if total, _ := listData(t, e.get("/api/admin/devices?q=other", e.admin)); total != 1 {
		t.Errorf("search total = %d", total)
	}

	if r := e.del("/api/admin/devices/600000001", e.admin, nil); r.Status != http.StatusOK {
		t.Fatalf("delete: %s", r)
	}
	expectError(t, "delete again", e.del("/api/admin/devices/600000001", e.admin, nil), http.StatusNotFound)
	if total, _ := listData(t, e.get("/api/admin/devices", e.admin)); total != 1 {
		t.Errorf("total after delete = %d", total)
	}
	// A deleted device asks for sysinfo again.
	if hb := e.heartbeat("600000001", nil); hb["sysinfo"] == nil {
		t.Errorf("heartbeat of deleted device = %v", hb)
	}
}

func TestAdminGroupCRUD(t *testing.T) {
	e := newEnv(t)
	g := e.createGroup("Office")
	expectError(t, "duplicate (case-insensitive)", e.post("/api/admin/groups", e.admin, map[string]any{"name": "office"}), http.StatusConflict)
	expectError(t, "empty name", e.post("/api/admin/groups", e.admin, map[string]any{"name": ""}), http.StatusBadRequest)

	r := e.put("/api/admin/groups/"+g, e.admin, map[string]any{"name": "HQ", "note": "main"})
	if r.Status != http.StatusOK || r.obj(t)["name"] != "HQ" {
		t.Fatalf("update: %s", r)
	}
	e.registerDevice("610000001", "pc")
	e.put("/api/admin/devices/610000001", e.admin, map[string]any{"group_guid": g})
	_, items := listData(t, e.get("/api/admin/groups", e.admin))
	if len(items) != 1 || items[0]["name"] != "HQ" || numStr(items[0]["devices"]) != "1" {
		t.Errorf("groups = %v", items)
	}

	if r := e.del("/api/admin/groups/"+g, e.admin, nil); r.Status != http.StatusOK {
		t.Fatalf("delete: %s", r)
	}
	expectError(t, "delete again", e.del("/api/admin/groups/"+g, e.admin, nil), http.StatusNotFound)
	_, devs := listData(t, e.get("/api/admin/devices", e.admin))
	if d := findBy(devs, "id", "610000001"); d == nil || d["group_name"] != "" {
		t.Errorf("device keeps a deleted group: %v", d)
	}
}

func TestAdminSharedABCRUD(t *testing.T) {
	e := newEnv(t)
	ownerGUID := e.createUser("owner", "owner-password", false)
	r := e.post("/api/admin/address-books", e.admin, map[string]any{"name": "Team AB", "note": "n", "password": "pw", "owner_guid": ownerGUID})
	if r.Status != http.StatusOK {
		t.Fatalf("create: %s", r)
	}
	ab := r.obj(t)
	guid := ab["guid"].(string)
	if ab["owner"] != "owner" || ab["personal"] != false {
		t.Errorf("created = %v", ab)
	}
	expectError(t, "duplicate", e.post("/api/admin/address-books", e.admin, map[string]any{"name": "team ab"}), http.StatusConflict)
	expectError(t, "reserved name", e.post("/api/admin/address-books", e.admin, map[string]any{"name": "My address book"}), http.StatusBadRequest)
	expectError(t, "reserved legacy name", e.post("/api/admin/address-books", e.admin, map[string]any{"name": "Legacy address book"}), http.StatusBadRequest)

	r = e.put("/api/admin/address-books/"+guid, e.admin, map[string]any{"name": "Renamed AB", "note": "n2"})
	if m := r.obj(t); r.Status != http.StatusOK || m["name"] != "Renamed AB" || m["owner"] != "owner" {
		t.Fatalf("update: %s", r)
	}

	// The owner has full control without an explicit rule.
	ownerTok := e.login("owner", "owner-password")
	_, profiles := listData(t, e.post("/api/ab/shared/profiles", ownerTok, nil))
	if len(profiles) != 1 || numStr(profiles[0]["rule"]) != "3" {
		t.Errorf("owner profiles = %v", profiles)
	}

	// Rules.
	ug := e.createUser("member", "member-password", false)
	e.setABRule(guid, ug, 2)
	var rules []map[string]any
	e.get("/api/admin/address-books/"+guid+"/rules", e.admin).decode(t, &rules)
	if len(rules) != 1 || rules[0]["user_name"] != "member" || numStr(rules[0]["rule"]) != "2" {
		t.Errorf("rules = %v", rules)
	}
	e.setABRule(guid, ug, 3)
	e.get("/api/admin/address-books/"+guid+"/rules", e.admin).decode(t, &rules)
	if len(rules) != 1 || numStr(rules[0]["rule"]) != "3" {
		t.Errorf("rules after upgrade = %v", rules)
	}
	expectError(t, "invalid rule", e.put("/api/admin/address-books/"+guid+"/rules", e.admin, map[string]any{"user_guid": ug, "rule": 7}), http.StatusBadRequest)
	e.setABRule(guid, ug, 0)
	e.get("/api/admin/address-books/"+guid+"/rules", e.admin).decode(t, &rules)
	if len(rules) != 0 {
		t.Errorf("rules after revoke = %v", rules)
	}

	// Personal books cannot be shared, edited or deleted from the panel.
	personal := e.personalAB(ownerTok)
	expectError(t, "share personal", e.put("/api/admin/address-books/"+personal+"/rules", e.admin, map[string]any{"user_guid": ug, "rule": 1}), http.StatusBadRequest)
	expectError(t, "delete personal", e.del("/api/admin/address-books/"+personal, e.admin, nil), http.StatusNotFound)
	_, books := listData(t, e.get("/api/admin/address-books", e.admin))
	if len(books) != 1 {
		t.Errorf("admin list must contain only shared books: %v", books)
	}

	// Peers listing hides secrets.
	expectEmpty(t, "add", e.post("/api/ab/peer/add/"+guid, e.admin, map[string]any{"id": "1", "password": "secret"}))
	_, peers := listData(t, e.get("/api/admin/address-books/"+guid+"/peers", e.admin))
	if len(peers) != 1 || (peers[0]["password"] != nil && peers[0]["password"] != "") {
		t.Errorf("admin peers = %v", peers)
	}

	if r := e.del("/api/admin/address-books/"+guid, e.admin, nil); r.Status != http.StatusOK {
		t.Fatalf("delete: %s", r)
	}
	expectError(t, "peers of deleted book", e.post("/api/ab/peers?ab="+guid, e.admin, nil), http.StatusNotFound)
}
