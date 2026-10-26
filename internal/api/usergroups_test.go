package api_test

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
)

func peerIDs(t *testing.T, e *env, tok string) string {
	t.Helper()
	r := e.get("/api/peers?current=1&pageSize=100&accessible=&status=1", tok)
	var l struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	r.decode(t, &l)
	var ids []string
	for _, d := range l.Data {
		ids = append(ids, d.ID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func TestUserGroupAccess(t *testing.T) {
	e := newEnv(t)
	office := e.createGroup("Office")
	e.createGroup("Lab")
	e.registerDevice("100", "office-pc")
	e.registerDevice("200", "lab-pc")
	e.put("/api/admin/devices/100", e.admin, map[string]any{"group_guid": office})

	aliceGUID, aliceTok := e.userWithToken("alice")
	bobGUID, _ := e.userWithToken("bob")
	if got := peerIDs(t, e, aliceTok); got != "" {
		t.Fatalf("no access expected, got %q", got)
	}

	r := e.post("/api/admin/user-groups", e.admin, map[string]any{"name": "Support", "device_groups": []string{office}})
	if r.Status != http.StatusOK {
		t.Fatalf("create user group: %s", r)
	}
	ug := r.obj(t)["guid"].(string)
	for _, g := range []string{aliceGUID, bobGUID} {
		if r := e.put("/api/admin/users/"+g, e.admin, map[string]any{"name": map[string]string{aliceGUID: "alice", bobGUID: "bob"}[g],
			"status": 1, "group_guid": ug}); r.Status != http.StatusOK || r.obj(t)["group_name"] != "Support" {
			t.Fatalf("assign user: %s", r)
		}
	}
	if got := peerIDs(t, e, aliceTok); got != "100" {
		t.Fatalf("group access: got %q, want 100", got)
	}
	// Group members see each other in the users list.
	r = e.get("/api/users?current=1&pageSize=100&accessible=&status=1", aliceTok)
	if total := fmt.Sprint(r.obj(t)["total"]); total != "2" {
		t.Fatalf("users visible to alice: %s", r)
	}
	// The device group shows up in the device-group tab.
	r = e.get("/api/device-group/accessible?current=1&pageSize=100", aliceTok)
	if !strings.Contains(string(r.Body), `"Office"`) || strings.Contains(string(r.Body), `"Lab"`) {
		t.Fatalf("accessible groups: %s", r)
	}
	// Revoking the grant removes access.
	e.put("/api/admin/user-groups/"+ug, e.admin, map[string]any{"name": "Support", "device_groups": []string{}})
	if got := peerIDs(t, e, aliceTok); got != "" {
		t.Fatalf("after revoke: %q", got)
	}
	if r := e.post("/api/admin/user-groups", e.admin, map[string]any{"name": "Bad", "device_groups": []string{"nope"}}); r.Status != http.StatusBadRequest {
		t.Fatalf("unknown device group accepted: %s", r)
	}
}
