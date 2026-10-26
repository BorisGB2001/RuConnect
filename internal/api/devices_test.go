package api_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestHeartbeatSysinfoHandshake(t *testing.T) {
	e := newEnv(t)
	const id = "123456789"

	hb := e.heartbeat(id, nil)
	if _, ok := hb["sysinfo"]; !ok {
		t.Fatalf("unknown device: heartbeat must request sysinfo, got %v", hb)
	}

	r := e.sysinfo(id, "bob-pc", nil)
	if r.Status != http.StatusOK || string(r.Body) != "SYSINFO_UPDATED" {
		t.Fatalf("sysinfo: want body exactly SYSINFO_UPDATED, got %s", r)
	}

	hb = e.heartbeat(id, nil)
	if _, ok := hb["sysinfo"]; ok {
		t.Fatalf("known device: heartbeat must not request sysinfo, got %v", hb)
	}
	if _, ok := hb["disconnect"]; ok {
		t.Fatalf("unexpected disconnect: %v", hb)
	}

	// A different uuid for the same id (reinstall / clone) triggers a re-upload.
	r = e.post("/api/heartbeat", "", map[string]any{"id": id, "uuid": "b3RoZXI=", "ver": 1004030})
	if _, ok := r.obj(t)["sysinfo"]; !ok {
		t.Fatalf("uuid change: want sysinfo, got %s", r)
	}
}

func TestSysinfoMissingUUID(t *testing.T) {
	e := newEnv(t)
	r := e.post("/api/sysinfo", "", map[string]any{"id": "1", "hostname": "x"})
	if string(r.Body) == "SYSINFO_UPDATED" {
		t.Fatalf("sysinfo without uuid must not succeed: %s", r)
	}
}

func TestHeartbeatDisconnect(t *testing.T) {
	e := newEnv(t)
	const id = "222333444"
	e.registerDevice(id, "pc")
	e.heartbeat(id, []int{5, 6})

	r := e.post("/api/admin/devices/"+id+"/disconnect", e.admin, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("disconnect: %s", r)
	}
	if n := numStr(r.obj(t)["sessions"]); n != "2" {
		t.Errorf("sessions = %s, want 2", n)
	}

	hb := e.heartbeat(id, []int{5, 6})
	got := []string{}
	for _, v := range hb["disconnect"].([]any) {
		got = append(got, numStr(v))
	}
	if !reflect.DeepEqual(got, []string{"5", "6"}) {
		t.Fatalf("disconnect = %v, want [5 6]", got)
	}
	hb = e.heartbeat(id, []int{5, 6})
	if _, ok := hb["disconnect"]; ok {
		t.Fatalf("disconnect must be delivered once, got again: %v", hb)
	}
}

func TestSysinfoPresets(t *testing.T) {
	e := newEnv(t)
	e.createUser("carol", "carol-password", false)
	e.createUser("dan", "dan-password", false)
	e.createGroup("Office")
	ab := e.createSharedAB("Team AB", "")

	const id = "555666777"
	r := e.sysinfo(id, "front-pc", map[string]any{
		"preset-user-name":             "carol",
		"preset-device-group-name":     "Office",
		"preset-address-book-name":     "Team AB",
		"preset-address-book-tag":      "deployed",
		"preset-address-book-alias":    "Front desk",
		"preset-address-book-note":     "lobby",
		"preset-address-book-password": "pw1",
		"preset-note":                  "device note",
	})
	if string(r.Body) != "SYSINFO_UPDATED" {
		t.Fatalf("sysinfo: %s", r)
	}

	_, items := listData(t, e.get("/api/admin/devices", e.admin))
	d := findBy(items, "id", id)
	if d == nil {
		t.Fatalf("device %s not listed", id)
	}
	if d["user_name"] != "carol" || d["group_name"] != "Office" || d["note"] != "device note" {
		t.Errorf("presets not applied: user=%v group=%v note=%v", d["user_name"], d["group_name"], d["note"])
	}

	_, peers := listData(t, e.post("/api/ab/peers?current=1&pageSize=100&ab="+ab, e.admin, nil))
	p := findBy(peers, "id", id)
	if p == nil {
		t.Fatalf("device not added to the shared book: %v", peers)
	}
	if p["alias"] != "Front desk" || p["note"] != "lobby" || p["password"] != "pw1" || p["hostname"] != "front-pc" ||
		p["platform"] != "Windows" || !reflect.DeepEqual(strList(p["tags"]), []string{"deployed"}) {
		t.Errorf("peer = %v", p)
	}
	var tags []map[string]any
	e.post("/api/ab/tags/"+ab, e.admin, nil).decode(t, &tags)
	if findBy(tags, "name", "deployed") == nil {
		t.Errorf("preset tag not created: %v", tags)
	}

	// Later uploads must not override assignments an admin may have changed.
	e.sysinfo(id, "front-pc", map[string]any{"preset-user-name": "dan"})
	_, items = listData(t, e.get("/api/admin/devices", e.admin))
	if d := findBy(items, "id", id); d["user_name"] != "carol" {
		t.Errorf("preset re-applied over existing assignment: user=%v", d["user_name"])
	}

	// Unknown preset targets are ignored; sysinfo still succeeds.
	if r := e.sysinfo("888", "x", map[string]any{"preset-user-name": "nobody", "preset-address-book-name": "Nope"}); string(r.Body) != "SYSINFO_UPDATED" {
		t.Errorf("sysinfo with unknown presets: %s", r)
	}
}

func TestSysinfoPresetKeepsTagColor(t *testing.T) {
	e := newEnv(t)
	ab := e.createSharedAB("Team AB", "")
	expectEmpty(t, "tag/add", e.post("/api/ab/tag/add/"+ab, e.admin, map[string]any{"name": "deployed", "color": 4280391411}))
	e.sysinfo("900000001", "pc", map[string]any{"preset-address-book-name": "Team AB", "preset-address-book-tag": "deployed"})
	var tags []map[string]any
	e.post("/api/ab/tags/"+ab, e.admin, nil).decode(t, &tags)
	tg := findBy(tags, "name", "deployed")
	if tg == nil {
		t.Fatalf("tag missing: %v", tags)
	}
	if c := numStr(tg["color"]); c != "4280391411" {
		t.Fatalf("regression: deploying a device with preset-address-book-tag resets the existing tag's color "+
			"(internal/api/devices.go applyPresets -> store.AddABTag upserts color): color = %s, want 4280391411", c)
	}
}

func TestDevicesCLI(t *testing.T) {
	e := newEnv(t)
	const id = "321321321"
	e.registerDevice(id, "pc")
	e.createUser("dave", "dave-password", false)
	e.createGroup("Lab")
	ab := e.createSharedAB("Team AB", "")
	// An earlier assignment is overridden by --assign.
	e.createUser("prev", "prev-password", false)
	e.sysinfo(id, "pc", map[string]any{"preset-user-name": "prev"})

	r := e.post("/api/devices/cli", e.admin, map[string]any{
		"id": id, "uuid": testUUID,
		"user_name": "dave", "device_group_name": "Lab",
		"address_book_name": "Team AB", "address_book_tag": "cli", "address_book_alias": "Lab PC",
		"device_name": "renamed-pc",
	})
	if r.Status != http.StatusOK || len(r.Body) != 0 {
		t.Fatalf("cli: want 200 with an empty body (client prints Done!), got %s", r)
	}
	_, items := listData(t, e.get("/api/admin/devices", e.admin))
	d := findBy(items, "id", id)
	if d["user_name"] != "dave" || d["group_name"] != "Lab" || d["hostname"] != "renamed-pc" {
		t.Errorf("cli assignment not applied: %v", d)
	}
	_, peers := listData(t, e.post("/api/ab/peers?ab="+ab, e.admin, nil))
	if p := findBy(peers, "id", id); p == nil || p["alias"] != "Lab PC" {
		t.Errorf("cli did not add the peer to the book: %v", peers)
	}

	// Non-admin token is refused.
	_, tok := e.userWithToken("eve")
	expectError(t, "cli as non-admin", e.post("/api/devices/cli", tok, map[string]any{"id": id, "uuid": testUUID, "user_name": "eve"}), http.StatusForbidden)
	// No token.
	expectError(t, "cli without token", e.post("/api/devices/cli", "", map[string]any{"id": id, "uuid": testUUID}), http.StatusUnauthorized)

	// Unknown device and unknown user: the body is printed, so it must be non-empty.
	if r := e.post("/api/devices/cli", e.admin, map[string]any{"id": "000", "uuid": testUUID, "user_name": "dave"}); len(r.Body) == 0 {
		t.Errorf("unknown device: want a message, got %s", r)
	}
	if r := e.post("/api/devices/cli", e.admin, map[string]any{"id": id, "uuid": testUUID, "user_name": "nobody"}); len(r.Body) == 0 {
		t.Errorf("unknown user: want a message, got %s", r)
	}
	// uuid mismatch is refused.
	if r := e.post("/api/devices/cli", e.admin, map[string]any{"id": id, "uuid": "d3Jvbmc=", "user_name": "dave"}); len(r.Body) == 0 || r.Status == http.StatusOK {
		t.Errorf("uuid mismatch: %s", r)
	}
}

func TestDevicesDeploy(t *testing.T) {
	e := newEnv(t)
	result := func(r resp) string {
		t.Helper()
		v, _ := r.obj(t)["result"].(string)
		return v
	}
	deploy := func(tok string, body map[string]any) resp { return e.post("/api/devices/deploy", tok, body) }

	if got := result(deploy(e.admin, map[string]any{"id": "424242", "uuid": "dXVpZDE=", "pk": "cGs="})); got != "OK" {
		t.Errorf("new id: result = %q, want OK", got)
	}
	if got := result(deploy(e.admin, map[string]any{"id": "424242", "uuid": "dXVpZDE=", "pk": "cGs="})); got != "OK" {
		t.Errorf("same device redeploy: result = %q, want OK", got)
	}
	if got := result(deploy(e.admin, map[string]any{"id": "424242", "uuid": "dXVpZDI=", "pk": "cGs="})); got != "ID_TAKEN" {
		t.Errorf("other uuid: result = %q, want ID_TAKEN", got)
	}
	if got := result(deploy(e.admin, map[string]any{"id": "", "uuid": "dXVpZDE="})); got != "INVALID_INPUT" {
		t.Errorf("no id: result = %q, want INVALID_INPUT", got)
	}
	if got := result(deploy(e.admin, map[string]any{"id": "1"})); got != "INVALID_INPUT" {
		t.Errorf("no uuid: result = %q, want INVALID_INPUT", got)
	}
	_, tok := e.userWithToken("eve")
	if got := result(deploy(tok, map[string]any{"id": "5", "uuid": "dXVpZDE="})); got == "OK" {
		t.Errorf("non-admin deploy succeeded")
	}
	if got := result(deploy("", map[string]any{"id": "5", "uuid": "dXVpZDE="})); got == "OK" {
		t.Errorf("deploy without token succeeded")
	}
	// The deployed device is registered.
	_, items := listData(t, e.get("/api/admin/devices", e.admin))
	if findBy(items, "id", "424242") == nil {
		t.Errorf("deployed device not registered")
	}
}

func TestGroupsTab(t *testing.T) {
	e := newEnv(t)
	gOffice := e.createGroup("Office")
	e.createGroup("Empty")
	erinGUID, erin := e.userWithToken("erin")
	if _, err := e.st.CreateUser(t.Context(), storeDisabled("frozen")); err != nil {
		t.Fatal(err)
	}

	e.registerDevice("100000001", "erin-pc")
	e.registerDevice("100000002", "other-pc")
	if r := e.put("/api/admin/devices/100000001", e.admin, map[string]any{"user_guid": erinGUID, "group_guid": gOffice}); r.Status != http.StatusOK {
		t.Fatalf("assign: %s", r)
	}

	// Device groups.
	total, groups := listData(t, e.get("/api/device-group/accessible?current=1&pageSize=100", erin))
	if total != 1 || len(groups) != 1 || groups[0]["name"] != "Office" {
		t.Errorf("erin groups = %d %v, want [Office]", total, groups)
	}
	total, _ = listData(t, e.get("/api/device-group/accessible?current=1&pageSize=100", e.admin))
	if total != 2 {
		t.Errorf("admin groups total = %d, want 2", total)
	}

	// Users.
	total, users := listData(t, e.get("/api/users?current=1&pageSize=100&accessible=&status=1", erin))
	if total != 1 || users[0]["name"] != "erin" {
		t.Errorf("erin users = %v", users)
	}
	total, users = listData(t, e.get("/api/users?current=1&pageSize=100&accessible=&status=1", e.admin))
	if total != 2 || findBy(users, "name", "frozen") != nil || findBy(users, "name", "erin") == nil {
		t.Errorf("admin users (status=1) = %v", users)
	}
	for _, u := range users {
		if numStr(u["status"]) != "1" {
			t.Errorf("status=1 filter returned %v", u)
		}
	}

	// Peers.
	total, peers := listData(t, e.get("/api/peers?current=1&pageSize=100&accessible=&status=1", erin))
	if total != 1 || len(peers) != 1 {
		t.Fatalf("erin peers = %v, want only her device", peers)
	}
	p := peers[0]
	info, ok := p["info"].(map[string]any)
	if !ok {
		t.Fatalf("peer.info must be an object: %v", p)
	}
	if p["id"] != "100000001" || p["user_name"] != "erin" || p["device_group_name"] != "Office" {
		t.Errorf("peer = %v", p)
	}
	if info["device_name"] != "erin-pc" {
		t.Errorf("info.device_name = %v, want the sysinfo hostname erin-pc", info["device_name"])
	}
	if info["username"] != "bob" || !strings.HasPrefix(info["os"].(string), "windows / ") {
		t.Errorf("info = %v", info)
	}
	total, _ = listData(t, e.get("/api/peers?current=1&pageSize=100&accessible=&status=1", e.admin))
	if total != 2 {
		t.Errorf("admin peers total = %d, want 2", total)
	}
	// Pagination: page 2 of size 1.
	total, page := listData(t, e.get("/api/peers?current=2&pageSize=1&accessible=&status=1", e.admin))
	if total != 2 || len(page) != 1 || page[0]["id"] != "100000002" {
		t.Errorf("page 2 = %d %v", total, page)
	}
}
