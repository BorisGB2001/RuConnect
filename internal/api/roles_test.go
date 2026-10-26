package api_test

import (
	"net/http"
	"testing"
)

func TestPanelRoles(t *testing.T) {
	e := newEnv(t)
	e.registerDevice("100200400", "pc")
	mk := func(name, role string) string {
		e.createUser(name, name+"-password", false)
		r := e.get("/api/admin/users?q="+name, e.admin)
		_, items := listData(t, r)
		guid := items[0]["guid"].(string)
		if r := e.put("/api/admin/users/"+guid, e.admin, map[string]any{"name": name, "status": 1, "role": role}); r.Status != http.StatusOK {
			t.Fatalf("set role %s: %s", role, r)
		}
		return e.login(name, name+"-password")
	}
	auditor, helpdesk := mk("aud", "auditor"), mk("help", "helpdesk")
	_, plain := e.userWithToken("plain")

	cases := []struct {
		who, method, path string
		body              any
		want              int
	}{
		{auditor, "GET", "/api/admin/devices", nil, 200},
		{auditor, "GET", "/api/admin/audit/conns", nil, 200},
		{auditor, "PUT", "/api/admin/devices/100200400", map[string]any{"note": "x"}, 403},
		{auditor, "GET", "/api/admin/tokens", nil, 403},
		{auditor, "GET", "/api/admin/oidc", nil, 403},
		{helpdesk, "PUT", "/api/admin/devices/100200400", map[string]any{"note": "x"}, 200},
		{helpdesk, "POST", "/api/admin/address-books", map[string]any{"name": "Team"}, 200},
		{helpdesk, "POST", "/api/admin/users", map[string]any{"name": "z", "password": "zzzzzzzz"}, 403},
		{helpdesk, "POST", "/api/admin/servers", map[string]any{"name": "B", "slug": "b"}, 403},
		{helpdesk, "POST", "/api/admin/tokens", map[string]any{"days": 1}, 403},
		{plain, "GET", "/api/admin/devices", nil, 403},
	}
	for _, c := range cases {
		r := e.do(c.method, c.path, c.who, c.body)
		if r.Status != c.want {
			t.Errorf("%s %s: got %s, want %d", c.method, c.path, r, c.want)
		}
	}
	if r := e.put("/api/admin/users/x", e.admin, map[string]any{"name": "x", "role": "root"}); r.Status != http.StatusBadRequest {
		t.Fatalf("invalid role accepted: %s", r)
	}
	// The role is reported to the panel.
	if r := e.post("/api/currentUser", helpdesk, nil); r.obj(t)["role"] != "helpdesk" {
		t.Fatalf("currentUser: %s", r)
	}
}
