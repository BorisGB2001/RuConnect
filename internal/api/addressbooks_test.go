package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func (e *env) personalAB(tok string) string {
	e.t.Helper()
	r := e.post("/api/ab/personal", tok, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("ab/personal: %s", r)
	}
	g, _ := r.obj(e.t)["guid"].(string)
	if g == "" {
		e.t.Fatalf("ab/personal: no guid (client would fall back to legacy mode): %s", r)
	}
	return g
}

func (e *env) abPeers(tok, ab string) []map[string]any {
	e.t.Helper()
	_, peers := listData(e.t, e.post("/api/ab/peers?current=1&pageSize=100&ab="+ab, tok, nil))
	return peers
}

func (e *env) abTags(tok, ab string) []map[string]any {
	e.t.Helper()
	r := e.post("/api/ab/tags/"+ab, tok, nil)
	if r.Status != http.StatusOK {
		e.t.Fatalf("tags: %s", r)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(r.Body), []byte("[")) {
		e.t.Fatalf("tags must be a bare JSON array, got %s", r)
	}
	var tags []map[string]any
	r.decode(e.t, &tags)
	return tags
}

func TestABPersonalAndSettings(t *testing.T) {
	e := newEnv(t)
	g1 := e.personalAB(e.admin)
	if g2 := e.personalAB(e.admin); g2 != g1 {
		t.Errorf("personal guid changed: %s -> %s", g1, g2)
	}
	_, tok := e.userWithToken("frank")
	if g3 := e.personalAB(tok); g3 == g1 {
		t.Errorf("two users share a personal book")
	}

	r := e.post("/api/ab/settings", e.admin, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("settings: %s", r)
	}
	m := r.obj(t)
	if n, ok := m["max_peer_one_ab"].(json.Number); !ok || n.String() != "0" {
		t.Errorf("max_peer_one_ab = %#v, want int 0", m["max_peer_one_ab"])
	}
}

// The Flutter client posts these with no body and Content-Length: 0.
func TestABEmptyBodyPosts(t *testing.T) {
	e := newEnv(t)
	personal := e.personalAB(e.admin)
	shared := e.createSharedAB("Team AB", "")
	for _, path := range []string{
		"/api/ab/personal",
		"/api/ab/settings",
		"/api/ab/shared/profiles?current=1&pageSize=100",
		"/api/ab/peers?current=1&pageSize=100&ab=" + personal,
		"/api/ab/peers?current=1&pageSize=100&ab=" + shared,
		"/api/ab/tags/" + personal,
		"/api/currentUser",
	} {
		req, _ := http.NewRequest(http.MethodPost, e.srv.URL+path, http.NoBody)
		req.ContentLength = 0
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+e.admin)
		res, err := e.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s with empty body: HTTP %d %s", path, res.StatusCode, b)
			continue
		}
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			t.Errorf("%s: not JSON: %s", path, b)
		}
		if m, ok := v.(map[string]any); ok {
			if _, bad := m["error"]; bad {
				t.Errorf("%s: error %s", path, b)
			}
		}
	}
}

func TestABSharedProfilesAndRules(t *testing.T) {
	e := newEnv(t)
	team := e.createSharedAB("Team AB", "default-pw")
	e.createSharedAB("Secret AB", "")
	e.personalAB(e.admin)

	readerGUID, reader := e.userWithToken("reader")
	writerGUID, writer := e.userWithToken("writer")
	_, outsider := e.userWithToken("outsider")
	e.personalAB(reader)
	e.setABRule(team, readerGUID, 1)
	e.setABRule(team, writerGUID, 2)

	noPersonal := func(who string, items []map[string]any) {
		t.Helper()
		for _, it := range items {
			if it["name"] == "My address book" {
				t.Errorf("%s: shared profiles must not include the personal book: %v", who, items)
			}
		}
	}

	total, items := listData(t, e.post("/api/ab/shared/profiles?current=1&pageSize=100", e.admin, nil))
	noPersonal("admin", items)
	if total != 2 {
		t.Errorf("admin sees %d shared books, want 2", total)
	}

	total, items = listData(t, e.post("/api/ab/shared/profiles?current=1&pageSize=100", reader, nil))
	noPersonal("reader", items)
	if total != 1 || len(items) != 1 {
		t.Fatalf("reader profiles = %v, want only Team AB", items)
	}
	p := items[0]
	if p["guid"] != team || p["name"] != "Team AB" || numStr(p["rule"]) != "1" || p["owner"] != adminName {
		t.Errorf("profile = %v", p)
	}
	if info, _ := p["info"].(map[string]any); info == nil || info["password"] != "default-pw" {
		t.Errorf("profile info.password = %v, want default-pw", p["info"])
	}
	_, items = listData(t, e.post("/api/ab/shared/profiles", writer, nil))
	if len(items) != 1 || numStr(items[0]["rule"]) != "2" {
		t.Errorf("writer profiles = %v", items)
	}

	total, _ = listData(t, e.post("/api/ab/shared/profiles", outsider, nil))
	if total != 0 {
		t.Errorf("outsider sees %d books, want 0", total)
	}
	expectError(t, "outsider peers", e.post("/api/ab/peers?ab="+team, outsider, nil), 0)
	expectError(t, "outsider peer/add", e.post("/api/ab/peer/add/"+team, outsider, map[string]any{"id": "1"}), 0)

	// Read-only: list works, writes are forbidden.
	if peers := e.abPeers(reader, team); len(peers) != 0 {
		t.Errorf("peers = %v", peers)
	}
	e.abTags(reader, team)
	expectError(t, "reader peer/add", e.post("/api/ab/peer/add/"+team, reader, map[string]any{"id": "1"}), http.StatusForbidden)
	expectError(t, "reader tag/add", e.post("/api/ab/tag/add/"+team, reader, map[string]any{"name": "x", "color": 1}), http.StatusForbidden)
	expectError(t, "reader peer delete", e.del("/api/ab/peer/"+team, reader, []string{"1"}), http.StatusForbidden)

	// Read/write user can add.
	expectEmpty(t, "writer peer/add", e.post("/api/ab/peer/add/"+team, writer, map[string]any{"id": "123", "tags": []string{}}))
	if peers := e.abPeers(reader, team); len(peers) != 1 {
		t.Errorf("reader does not see writer's peer: %v", peers)
	}

	// Personal books of other users are invisible, even to admins.
	readerPersonal := e.personalAB(reader)
	expectError(t, "admin reads reader's personal book", e.post("/api/ab/peers?ab="+readerPersonal, e.admin, nil), 0)
	expectError(t, "writer adds to reader's personal book", e.post("/api/ab/peer/add/"+readerPersonal, writer, map[string]any{"id": "1"}), 0)

	// Revoking the rule hides the book again.
	e.setABRule(team, readerGUID, 0)
	if total, _ := listData(t, e.post("/api/ab/shared/profiles", reader, nil)); total != 0 {
		t.Errorf("revoked reader still sees %d books", total)
	}
}

// Every action endpoint must answer HTTP 200 with a zero-byte body: anything
// else (including "{}" or "null") is reported as an error by the client.
func TestABActionEndpointsEmptyBody(t *testing.T) {
	for _, kind := range []string{"personal", "shared"} {
		t.Run(kind, func(t *testing.T) {
			e := newEnv(t)
			ab := e.createSharedAB("Team AB", "")
			if kind == "personal" {
				ab = e.personalAB(e.admin)
			}
			tok := e.admin

			expectEmpty(t, "tag/add work", e.post("/api/ab/tag/add/"+ab, tok, map[string]any{"name": "work", "color": 4280391411}))
			expectEmpty(t, "tag/add home", e.post("/api/ab/tag/add/"+ab, tok, map[string]any{"name": "home", "color": 1}))
			expectEmpty(t, "peer/add 100", e.post("/api/ab/peer/add/"+ab, tok, map[string]any{"id": "100", "alias": "", "tags": []string{"work"}}))
			expectEmpty(t, "peer/add 200", e.post("/api/ab/peer/add/"+ab, tok, map[string]any{
				"id": "200", "username": "u", "hostname": "h", "platform": "Linux", "alias": "two",
				"tags": []string{"work", "home"}, "forceAlwaysRelay": "true", "rdpPort": "3389",
				"rdpUsername": "ru", "loginName": "ln", "note": "n", "device_group_name": "", "same_server": false,
			}))
			expectEmpty(t, "peer/update alias", e.put("/api/ab/peer/update/"+ab, tok, map[string]any{"id": "100", "alias": "Server"}))
			expectEmpty(t, "peer/update tags", e.put("/api/ab/peer/update/"+ab, tok, map[string]any{"id": "100", "tags": []string{"work", "home"}}))
			expectEmpty(t, "peer/update note", e.put("/api/ab/peer/update/"+ab, tok, map[string]any{"id": "100", "note": "rack 4"}))
			expectEmpty(t, "tag/update", e.put("/api/ab/tag/update/"+ab, tok, map[string]any{"name": "work", "color": 5}))
			expectEmpty(t, "tag/rename", e.put("/api/ab/tag/rename/"+ab, tok, map[string]any{"old": "work", "new": "office"}))

			peers := e.abPeers(tok, ab)
			p100, p200 := findBy(peers, "id", "100"), findBy(peers, "id", "200")
			if p100 == nil || p200 == nil {
				t.Fatalf("peers = %v", peers)
			}
			if p100["alias"] != "Server" || p100["note"] != "rack 4" {
				t.Errorf("partial updates lost a field: %v", p100)
			}
			if got := strList(p100["tags"]); !reflect.DeepEqual(got, []string{"office", "home"}) {
				t.Errorf("peer 100 tags after rename = %v", got)
			}
			if got := strList(p200["tags"]); !reflect.DeepEqual(got, []string{"office", "home"}) {
				t.Errorf("peer 200 tags after rename = %v", got)
			}
			if p200["forceAlwaysRelay"] != "true" || p200["rdpPort"] != "3389" || p200["username"] != "u" ||
				p200["hostname"] != "h" || p200["platform"] != "Linux" || p200["loginName"] != "ln" || p200["rdpUsername"] != "ru" {
				t.Errorf("peer 200 fields = %v", p200)
			}

			tags := e.abTags(tok, ab)
			if len(tags) != 2 || findBy(tags, "name", "work") != nil {
				t.Errorf("tags after rename = %v", tags)
			}
			if o := findBy(tags, "name", "office"); o == nil || numStr(o["color"]) != "5" {
				t.Errorf("renamed tag lost its color: %v", tags)
			}

			expectEmpty(t, "DELETE tag", e.del("/api/ab/tag/"+ab, tok, []string{"home"}))
			peers = e.abPeers(tok, ab)
			for _, p := range peers {
				if got := strList(p["tags"]); !reflect.DeepEqual(got, []string{"office"}) {
					t.Errorf("peer %v tags after delete = %v", p["id"], got)
				}
			}
			if tags := e.abTags(tok, ab); len(tags) != 1 {
				t.Errorf("tags after delete = %v", tags)
			}

			expectEmpty(t, "DELETE peer", e.del("/api/ab/peer/"+ab, tok, []string{"200", "not-there"}))
			if peers := e.abPeers(tok, ab); len(peers) != 1 || peers[0]["id"] != "100" {
				t.Errorf("peers after delete = %v", peers)
			}

			// Errors are JSON with an error key, never an empty 200.
			expectError(t, "update unknown peer", e.put("/api/ab/peer/update/"+ab, tok, map[string]any{"id": "nope", "alias": "x"}), 0)
			expectError(t, "update unknown tag", e.put("/api/ab/tag/update/"+ab, tok, map[string]any{"name": "nope", "color": 1}), 0)
			expectError(t, "rename unknown tag", e.put("/api/ab/tag/rename/"+ab, tok, map[string]any{"old": "nope", "new": "x"}), 0)
			expectError(t, "peer/add without id", e.post("/api/ab/peer/add/"+ab, tok, map[string]any{"alias": "x"}), 0)
			expectError(t, "action on unknown book", e.post("/api/ab/peer/add/00000000-0000-4000-8000-000000000000", tok, map[string]any{"id": "1"}), 0)
		})
	}
}

func TestABPasswordHashVisibility(t *testing.T) {
	e := newEnv(t)
	personal := e.personalAB(e.admin)
	shared := e.createSharedAB("Team AB", "")

	expectEmpty(t, "personal add", e.post("/api/ab/peer/add/"+personal, e.admin, map[string]any{"id": "1", "hash": "h1", "password": "p1"}))
	expectEmpty(t, "shared add", e.post("/api/ab/peer/add/"+shared, e.admin, map[string]any{"id": "1", "hash": "h2", "password": "p2"}))

	pp := e.abPeers(e.admin, personal)[0]
	if pp["hash"] != "h1" {
		t.Errorf("personal peer hash = %v, want h1", pp["hash"])
	}
	if v, ok := pp["password"]; ok && v != "" {
		t.Errorf("personal peer exposes password %v", v)
	}
	sp := e.abPeers(e.admin, shared)[0]
	if sp["password"] != "p2" {
		t.Errorf("shared peer password = %v, want p2", sp["password"])
	}
	if v, ok := sp["hash"]; ok && v != "" {
		t.Errorf("shared peer exposes hash %v", v)
	}

	// Updates follow the same rule.
	expectEmpty(t, "personal hash update", e.put("/api/ab/peer/update/"+personal, e.admin, map[string]any{"id": "1", "hash": "h1b"}))
	expectEmpty(t, "shared password update", e.put("/api/ab/peer/update/"+shared, e.admin, map[string]any{"id": "1", "password": "p2b", "hash": "ignored"}))
	if pp := e.abPeers(e.admin, personal)[0]; pp["hash"] != "h1b" {
		t.Errorf("personal hash update: %v", pp)
	}
	if sp := e.abPeers(e.admin, shared)[0]; sp["password"] != "p2b" || (sp["hash"] != nil && sp["hash"] != "") {
		t.Errorf("shared password update: %v", sp)
	}
}

func TestABPeersPaginationAndShape(t *testing.T) {
	e := newEnv(t)
	ab := e.personalAB(e.admin)
	for _, id := range []string{"1", "2", "3"} {
		expectEmpty(t, "add "+id, e.post("/api/ab/peer/add/"+ab, e.admin, map[string]any{"id": id}))
	}
	total, page := listData(t, e.post("/api/ab/peers?current=2&pageSize=2&ab="+ab, e.admin, nil))
	if total != 3 || len(page) != 1 {
		t.Errorf("page 2 = %d %v", total, page)
	}
	p := e.abPeers(e.admin, ab)[0]
	if _, ok := p["tags"].([]any); !ok {
		t.Errorf("tags must be an array even when empty: %v", p)
	}
	if p["forceAlwaysRelay"] != "false" {
		t.Errorf("forceAlwaysRelay must be the string \"false\" by default: %#v", p["forceAlwaysRelay"])
	}
	if tags := e.abTags(e.admin, ab); tags == nil || len(tags) != 0 {
		t.Errorf("empty tags must be [] : %v", tags)
	}
}

func TestLegacyAB(t *testing.T) {
	e := newEnv(t)
	r := e.get("/api/ab", e.admin)
	if r.Status != http.StatusOK || strings.TrimSpace(string(r.Body)) != "null" {
		t.Fatalf("empty legacy book: want literal null, got %s", r)
	}

	data := `{"tags":["work"],"peers":[{"id":"1","username":"","hostname":"","platform":"","alias":"","tags":[],"hash":""}],"tag_colors":"{\"work\":4280391411}"}`
	r = e.post("/api/ab", e.admin, map[string]any{"data": data})
	if r.Status != http.StatusOK {
		t.Fatalf("legacy POST: %s", r)
	}
	if b := strings.TrimSpace(string(r.Body)); b != "" && b != "null" {
		var m map[string]any
		if json.Unmarshal(r.Body, &m) != nil || m["error"] != nil {
			t.Errorf("legacy POST success body must be empty, null, or an object without error: %s", r)
		}
	}

	m := e.get("/api/ab", e.admin).obj(t)
	if m["data"] != data {
		t.Errorf("data = %v, want the stored string", m["data"])
	}
	if _, ok := m["licensed_devices"].(json.Number); !ok {
		t.Errorf("licensed_devices = %#v, want int", m["licensed_devices"])
	}

	// Sciter variant.
	m = e.post("/api/ab/get", e.admin, map[string]any{}).obj(t)
	if m["data"] != data {
		t.Errorf("ab/get data = %v", m["data"])
	}
	if _, ok := m["updated_at"]; !ok {
		t.Errorf("ab/get: no updated_at: %v", m)
	}

	// Per user.
	_, tok := e.userWithToken("legacy")
	if r := e.get("/api/ab", tok); strings.TrimSpace(string(r.Body)) != "null" {
		t.Errorf("other user's legacy book not empty: %s", r)
	}
	expectError(t, "invalid data", e.post("/api/ab", e.admin, map[string]any{"data": "{broken"}), 0)
}
