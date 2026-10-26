package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ruconnect/ruconnect/internal/store"
)

func TestStrategyPushedViaHeartbeat(t *testing.T) {
	ts, st := oidcTestServer(t)
	ctx := context.Background()
	st.CreateUser(ctx, store.UserInput{Name: "admin", Password: "adminpass123", Status: store.UserNormal, IsAdmin: true})
	_, body := siteDo(t, "POST", ts.URL+"/api/login", "", `{"username":"admin","password":"adminpass123"}`)
	var login struct {
		Token string `json:"access_token"`
	}
	json.Unmarshal([]byte(body), &login)
	tok := login.Token

	hb := func(modified int64) map[string]any {
		b, _ := json.Marshal(map[string]any{"id": "555", "uuid": "u5", "modified_at": modified})
		_, out := siteDo(t, "POST", ts.URL+"/api/heartbeat", "", string(b))
		var m map[string]any
		d := json.NewDecoder(strings.NewReader(out))
		d.UseNumber()
		d.Decode(&m)
		return m
	}
	siteDo(t, "POST", ts.URL+"/api/sysinfo", "", `{"id":"555","uuid":"u5","hostname":"pc"}`)
	if r := hb(0); r["strategy"] != nil {
		t.Fatalf("no strategy expected: %v", r)
	}

	code, b := siteDo(t, "POST", ts.URL+"/api/admin/strategies", tok,
		`{"name":"Locked","options":{"enable-file-transfer":"N","approve-mode":"click"},"is_default":true}`)
	if code != 200 {
		t.Fatalf("create: %d %s", code, b)
	}
	var created store.Strategy
	json.Unmarshal([]byte(b), &created)

	r := hb(0)
	sm, _ := r["strategy"].(map[string]any)
	opts, _ := sm["config_options"].(map[string]any)
	if opts["enable-file-transfer"] != "N" || opts["approve-mode"] != "click" {
		t.Fatalf("default strategy not pushed: %v", r)
	}
	mod, _ := r["modified_at"].(json.Number).Int64()
	if mod != created.ModifiedAt {
		t.Fatalf("modified_at %d != %d", mod, created.ModifiedAt)
	}
	if r := hb(mod); r["strategy"] != nil {
		t.Fatalf("up-to-date client got the strategy again: %v", r)
	}

	// A device-specific strategy overrides the default one.
	_, b = siteDo(t, "POST", ts.URL+"/api/admin/strategies", tok, `{"name":"Open","options":{"enable-file-transfer":"Y"}}`)
	var open store.Strategy
	json.Unmarshal([]byte(b), &open)
	if code, b := siteDo(t, "PUT", ts.URL+"/api/admin/devices/555/strategy", tok, `{"strategy_guid":"`+open.GUID+`"}`); code != 200 {
		t.Fatalf("assign: %d %s", code, b)
	}
	r = hb(mod)
	sm, _ = r["strategy"].(map[string]any)
	if o, _ := sm["config_options"].(map[string]any); o["enable-file-transfer"] != "Y" {
		t.Fatalf("device strategy not pushed: %v", r)
	}

	// Removing every strategy resets the client's timestamp.
	siteDo(t, "DELETE", ts.URL+"/api/admin/strategies/"+open.GUID, tok, "")
	siteDo(t, "DELETE", ts.URL+"/api/admin/strategies/"+created.GUID, tok, "")
	r = hb(123)
	if m, _ := r["modified_at"].(json.Number).Int64(); m != 0 || r["strategy"] != nil {
		t.Fatalf("after delete: %v", r)
	}
}
