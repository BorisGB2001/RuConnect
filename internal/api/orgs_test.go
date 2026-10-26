package api_test

import (
	"net/http"
	"strings"
	"testing"
)

// orgEnv sets up two organizations, each with a domain, an administrator
// and a device that reported through the organization's domain.
type orgEnv struct {
	*env
	orgA, orgB     string // guids
	adminA, adminB string // tokens
}

func newOrgEnv(t *testing.T) *orgEnv {
	e := &orgEnv{env: newEnv(t)}
	mkOrg := func(name, domain string) string {
		r := e.post("/api/admin/organizations", e.admin, map[string]any{"name": name, "domains": []string{domain}})
		if r.Status != http.StatusOK {
			t.Fatalf("create org %s: %s", name, r)
		}
		return r.obj(t)["guid"].(string)
	}
	e.orgA, e.orgB = mkOrg("Alpha", "rd.alpha.example"), mkOrg("Beta", "RD.Beta.Example.")
	mkAdmin := func(name, org, host string) string {
		r := e.post("/api/admin/users", e.admin, map[string]any{"name": name, "password": name + "-password", "is_admin": true, "org_guid": org})
		if r.Status != http.StatusOK || r.obj(t)["org_guid"] != org {
			t.Fatalf("create %s: %s", name, r)
		}
		r = e.doHost("POST", "/api/login", "", host, loginBody(name, name+"-password"))
		if r.Status != http.StatusOK {
			t.Fatalf("login %s: %s", name, r)
		}
		return r.obj(t)["access_token"].(string)
	}
	e.adminA = mkAdmin("alice", e.orgA, "rd.alpha.example")
	e.adminB = mkAdmin("bob", e.orgB, "rd.beta.example:21114")
	for id, host := range map[string]string{"111000111": "rd.alpha.example", "222000222": "rd.beta.example", "333000333": ""} {
		r := e.doHost("POST", "/api/sysinfo", "", host, map[string]any{"id": id, "uuid": testUUID, "hostname": "pc-" + id})
		if string(r.Body) != "SYSINFO_UPDATED" {
			t.Fatalf("sysinfo %s: %s", id, r)
		}
	}
	return e
}

func ids(t *testing.T, r resp, key string) map[string]bool {
	t.Helper()
	if r.Status != http.StatusOK {
		t.Fatalf("list: %s", r)
	}
	_, items := listData(t, r)
	out := map[string]bool{}
	for _, it := range items {
		out[it[key].(string)] = true
	}
	return out
}

func TestOrgIsolation(t *testing.T) {
	e := newOrgEnv(t)

	// Devices joined the organization of the domain they reported to.
	if got := ids(t, e.get("/api/admin/devices", e.adminA), "id"); len(got) != 1 || !got["111000111"] {
		t.Fatalf("alpha devices: %v", got)
	}
	if got := ids(t, e.get("/api/admin/devices", e.admin), "id"); len(got) != 3 {
		t.Fatalf("server admin devices: %v", got)
	}
	if got := ids(t, e.get("/api/admin/devices?org="+e.orgB, e.admin), "id"); len(got) != 1 || !got["222000222"] {
		t.Fatalf("filtered devices: %v", got)
	}
	if got := ids(t, e.get("/api/admin/devices?org=none", e.admin), "id"); len(got) != 1 || !got["333000333"] {
		t.Fatalf("devices without organization: %v", got)
	}
	// An organization administrator cannot widen the filter.
	if got := ids(t, e.get("/api/admin/devices?org="+e.orgB, e.adminA), "id"); len(got) != 1 || !got["111000111"] {
		t.Fatalf("alpha with beta filter: %v", got)
	}

	// Other organizations' records do not exist for alpha's administrator.
	for _, c := range []struct{ method, path string }{
		{"PUT", "/api/admin/devices/222000222"},
		{"DELETE", "/api/admin/devices/222000222"},
		{"POST", "/api/admin/devices/222000222/disconnect"},
		{"PUT", "/api/admin/devices/333000333"},
	} {
		if r := e.do(c.method, c.path, e.adminA, map[string]any{"note": "x"}); r.Status != http.StatusNotFound {
			t.Errorf("%s %s: %s", c.method, c.path, r)
		}
	}
	users := ids(t, e.get("/api/admin/users", e.adminA), "name")
	if len(users) != 1 || !users["alice"] {
		t.Fatalf("alpha users: %v", users)
	}
	_, bobItems := listData(t, e.get("/api/admin/users?q=bob", e.admin))
	bobGUID := bobItems[0]["guid"].(string)
	if r := e.put("/api/admin/users/"+bobGUID, e.adminA, map[string]any{"name": "bob", "status": 0}); r.Status != http.StatusNotFound {
		t.Fatalf("alpha edits beta's admin: %s", r)
	}
	if r := e.del("/api/admin/users/"+bobGUID, e.adminA, nil); r.Status != http.StatusNotFound {
		t.Fatalf("alpha deletes beta's admin: %s", r)
	}

	// Users created by alpha's administrator land in alpha, whatever they ask.
	r := e.post("/api/admin/users", e.adminA, map[string]any{"name": "carol", "password": "carol-password", "org_guid": e.orgB})
	if r.Status != http.StatusOK || r.obj(t)["org_guid"] != e.orgA {
		t.Fatalf("create carol: %s", r)
	}
	// A member of alpha cannot sign in through beta's domain.
	if r := e.doHost("POST", "/api/login", "", "rd.beta.example", loginBody("carol", "carol-password")); r.Status != http.StatusUnauthorized {
		t.Fatalf("carol on beta's domain: %s", r)
	}
	// Server-level configuration is out of reach.
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/admin/organizations"},
		{"GET", "/api/admin/servers"},
		{"GET", "/api/admin/oidc"},
		{"PUT", "/api/admin/devices/111000111/organization"},
	} {
		if r := e.do(c.method, c.path, e.adminA, map[string]any{"name": "x"}); r.Status != http.StatusForbidden {
			t.Errorf("%s %s: %s", c.method, c.path, r)
		}
	}
	if orgs := ids(t, e.get("/api/admin/organizations", e.adminA), "name"); len(orgs) != 1 || !orgs["Alpha"] {
		t.Fatalf("alpha sees organizations %v", orgs)
	}
}

func TestOrgGroupsStrategiesAndBooks(t *testing.T) {
	e := newOrgEnv(t)
	// Names are unique per organization.
	gA := e.post("/api/admin/groups", e.adminA, map[string]any{"name": "Office"})
	gB := e.post("/api/admin/groups", e.adminB, map[string]any{"name": "Office"})
	if gA.Status != http.StatusOK || gB.Status != http.StatusOK {
		t.Fatalf("groups: %s %s", gA, gB)
	}
	if r := e.post("/api/admin/groups", e.adminA, map[string]any{"name": "office"}); r.Status != http.StatusConflict {
		t.Fatalf("duplicate group: %s", r)
	}
	groupB := gB.obj(t)["guid"].(string)
	if r := e.put("/api/admin/devices/111000111", e.adminA, map[string]any{"group_guid": groupB}); r.Status != http.StatusNotFound {
		t.Fatalf("alpha device into beta group: %s", r)
	}
	if got := ids(t, e.get("/api/admin/groups", e.adminA), "guid"); len(got) != 1 || got[groupB] {
		t.Fatalf("alpha groups: %v", got)
	}
	// A server administrator cannot mix organizations either.
	if r := e.put("/api/admin/devices/111000111", e.admin, map[string]any{"group_guid": groupB}); r.Status != http.StatusBadRequest {
		t.Fatalf("server admin mixes: %s", r)
	}

	// Each organization has its own default strategy.
	r := e.post("/api/admin/strategies", e.adminA, map[string]any{"name": "Locked", "is_default": true, "options": map[string]string{"allow-remote-config-modification": "N"}})
	if r.Status != http.StatusOK {
		t.Fatalf("strategy: %s", r)
	}
	if r := e.post("/api/admin/strategies", e.adminB, map[string]any{"name": "Locked", "is_default": true}); r.Status != http.StatusOK {
		t.Fatalf("beta strategy: %s", r)
	}
	hbA := e.post("/api/heartbeat", "", map[string]any{"id": "111000111", "uuid": testUUID, "ver": 1})
	hbN := e.post("/api/heartbeat", "", map[string]any{"id": "333000333", "uuid": testUUID, "ver": 1})
	if _, ok := hbA.obj(t)["strategy"]; !ok {
		t.Fatalf("alpha device got no strategy: %s", hbA)
	}
	if _, ok := hbN.obj(t)["strategy"]; ok {
		t.Fatalf("device without organization got alpha's strategy: %s", hbN)
	}
	if got := ids(t, e.get("/api/admin/strategies", e.adminB), "name"); len(got) != 1 {
		t.Fatalf("beta strategies: %v", got)
	}

	// Shared address books.
	abB := e.post("/api/admin/address-books", e.adminB, map[string]any{"name": "Team"})
	if abB.Status != http.StatusOK {
		t.Fatalf("beta book: %s", abB)
	}
	if r := e.post("/api/admin/address-books", e.adminA, map[string]any{"name": "Team"}); r.Status != http.StatusOK {
		t.Fatalf("alpha book with the same name: %s", r)
	}
	bookB := abB.obj(t)["guid"].(string)
	for _, c := range []struct{ method, path string }{
		{"PUT", "/api/admin/address-books/" + bookB},
		{"DELETE", "/api/admin/address-books/" + bookB},
		{"GET", "/api/admin/address-books/" + bookB + "/peers"},
		{"GET", "/api/admin/address-books/" + bookB + "/rules"},
	} {
		if r := e.do(c.method, c.path, e.adminA, map[string]any{"name": "x"}); r.Status != http.StatusNotFound {
			t.Errorf("%s %s: %s", c.method, c.path, r)
		}
	}
	// The client does not list beta's book to alpha's administrator.
	_, profiles := listData(t, e.post("/api/ab/shared/profiles", e.adminA, nil))
	for _, p := range profiles {
		if p["guid"] == bookB {
			t.Fatal("alpha's admin sees beta's shared book in the client")
		}
	}
	// Accessible devices in the client stay within the organization.
	if got := ids(t, e.get("/api/peers", e.adminA), "id"); len(got) != 1 || !got["111000111"] {
		t.Fatalf("alpha client peers: %v", got)
	}
	if got := ids(t, e.get("/api/users", e.adminA), "name"); got["bob"] || got["admin"] {
		t.Fatalf("alpha client users: %v", got)
	}
}

func TestOrgManagement(t *testing.T) {
	e := newOrgEnv(t)
	// A domain belongs to one organization.
	if r := e.post("/api/admin/organizations", e.admin, map[string]any{"name": "Gamma", "domains": []string{"rd.alpha.example"}}); r.Status != http.StatusConflict {
		t.Fatalf("duplicate domain: %s", r)
	}
	if r := e.post("/api/admin/organizations", e.admin, map[string]any{"name": "Gamma", "domains": []string{"bad domain"}}); r.Status != http.StatusBadRequest {
		t.Fatalf("invalid domain: %s", r)
	}
	// Non-empty organizations cannot be deleted.
	if r := e.del("/api/admin/organizations/"+e.orgA, e.admin, nil); r.Status != http.StatusBadRequest {
		t.Fatalf("delete non-empty: %s", r)
	}
	// Moving a device clears its organization-bound settings.
	r := e.put("/api/admin/devices/111000111/organization", e.admin, map[string]any{"org_guid": e.orgB})
	if r.Status != http.StatusOK || r.obj(t)["org_guid"] != e.orgB {
		t.Fatalf("move device: %s", r)
	}
	if got := ids(t, e.get("/api/admin/devices", e.adminB), "id"); len(got) != 2 {
		t.Fatalf("beta devices after move: %v", got)
	}
	// Overview is scoped too.
	ov := e.get("/api/admin/overview", e.adminA).obj(t)
	if numStr(ov["devices"]) != "0" || ov["server_admin"] != false {
		t.Fatalf("alpha overview: %v", ov)
	}
	if srv := ov["server"].(map[string]any); srv["id_server"] != "rd.alpha.example" {
		t.Fatalf("alpha connection settings: %v", srv)
	}
}

func TestOrgAuditAndTokens(t *testing.T) {
	e := newOrgEnv(t)
	connLifecycle(e.env, "222000222", 12, "987654321", "987654321")
	if n, _ := listData(t, e.get("/api/admin/audit/conns", e.adminA)); n != 0 {
		t.Fatalf("alpha sees beta's sessions: %d", n)
	}
	if n, _ := listData(t, e.get("/api/admin/audit/conns", e.adminB)); n != 1 {
		t.Fatalf("beta's sessions: %d", n)
	}
	if r := e.get("/api/admin/audit/conns?format=csv", e.adminA); r.Status != http.StatusOK || len(r.Body) > 200 {
		t.Fatalf("alpha csv export: %s", r)
	}
	// Deployment tokens are listed and revoked per organization.
	if r := e.post("/api/admin/tokens", e.adminB, map[string]any{"days": 1}); r.Status != http.StatusOK {
		t.Fatalf("beta token: %s", r)
	}
	_, toks := listData(t, e.get("/api/admin/tokens", e.adminB))
	if len(toks) != 1 {
		t.Fatalf("beta tokens: %v", toks)
	}
	if n, _ := listData(t, e.get("/api/admin/tokens", e.adminA)); n != 0 {
		t.Fatalf("alpha sees beta's tokens: %d", n)
	}
	if r := e.del("/api/admin/tokens/"+toks[0]["id"].(string), e.adminA, nil); r.Status != http.StatusNotFound {
		t.Fatalf("alpha revokes beta's token: %s", r)
	}
}

func TestTLSAsk(t *testing.T) {
	e := newOrgEnv(t)
	if r := e.get("/tls/ask?domain=RD.alpha.example", ""); r.Status != http.StatusOK {
		t.Fatalf("org domain: %s", r)
	}
	if r := e.get("/tls/ask?domain=evil.example", ""); r.Status != http.StatusNotFound {
		t.Fatalf("unknown domain: %s", r)
	}
}

func TestOrgDeployConfig(t *testing.T) {
	e := newOrgEnv(t)
	r := e.get("/api/admin/deploy-config", e.adminA).obj(t)
	if r["id_server"] != "rd.alpha.example" || !strings.Contains(r["api_server"].(string), "rd.alpha.example") {
		t.Fatalf("alpha deploy config: %v", r)
	}
	if r := e.get("/api/admin/deploy-config?org="+e.orgB, e.admin).obj(t); r["id_server"] != "rd.beta.example" {
		t.Fatalf("server admin, beta selected: %v", r)
	}
	e.post("/api/admin/groups", e.adminA, map[string]any{"name": "Office"})
	_, groups := listData(t, e.get("/api/admin/groups", e.admin))
	if len(groups) != 1 || groups[0]["org_name"] != "Alpha" {
		t.Fatalf("groups carry their organization: %v", groups)
	}
}
