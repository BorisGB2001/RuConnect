package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/BorisGB2001/RuConnect/internal/store"
)

func siteDo(t *testing.T, method, u, token, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, u, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestMultiServerPeerAddresses(t *testing.T) {
	ts, st := oidcTestServer(t)
	ctx := context.Background()
	if _, err := st.CreateUser(ctx, store.UserInput{Name: "admin", Password: "adminpass123", Status: store.UserNormal, IsAdmin: true}); err != nil {
		t.Fatal(err)
	}
	_, body := siteDo(t, "POST", ts.URL+"/api/login", "", `{"username":"admin","password":"adminpass123"}`)
	var login struct {
		Token string `json:"access_token"`
	}
	json.Unmarshal([]byte(body), &login)
	tok := login.Token

	def, _ := st.ServerBySlug(ctx, store.DefaultServerSlug)
	if _, err := st.UpdateServer(ctx, def.GUID, store.ServerInput{Name: "HQ", IDServer: "hq.example.com", Key: "HQKEY="}); err != nil {
		t.Fatal(err)
	}
	if code, b := siteDo(t, "POST", ts.URL+"/api/admin/servers", tok,
		`{"name":"Branch","slug":"branch","id_server":"br.example.com:21116","key":"BR+KEY/="}`); code != 200 {
		t.Fatalf("create server: %d %s", code, b)
	}
	if code, _ := siteDo(t, "POST", ts.URL+"/api/admin/servers", tok, `{"name":"Bad","slug":"Bad Slug!"}`); code != 400 {
		t.Fatalf("invalid slug accepted: %d", code)
	}

	siteDo(t, "POST", ts.URL+"/api/sysinfo", "", `{"id":"111","uuid":"u1","hostname":"hq-pc"}`)
	siteDo(t, "POST", ts.URL+"/s/branch/api/sysinfo", "", `{"id":"222","uuid":"u2","hostname":"br-pc"}`)

	ids := func(prefix string) []string {
		_, b := siteDo(t, "GET", ts.URL+prefix+"/api/peers?current=1&pageSize=100&accessible=&status=1", tok, "")
		var l struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		json.Unmarshal([]byte(b), &l)
		var out []string
		for _, d := range l.Data {
			out = append(out, d.ID)
		}
		return out
	}
	if got := strings.Join(ids(""), ","); got != "111,222@br.example.com:21116?key=BR+KEY/=" {
		t.Fatalf("default site view: %s", got)
	}
	if got := strings.Join(ids("/s/branch"), ","); got != "111@hq.example.com?key=HQKEY=,222" {
		t.Fatalf("branch site view: %s", got)
	}

	// Address book entries are stored canonically and rewritten per site.
	_, b := siteDo(t, "POST", ts.URL+"/api/ab/personal", tok, "")
	var ab struct {
		GUID string `json:"guid"`
	}
	json.Unmarshal([]byte(b), &ab)
	if code, b := siteDo(t, "POST", ts.URL+"/api/ab/peer/add/"+ab.GUID, tok, `{"id":"222@br.example.com:21116?key=BR+KEY/="}`); code != 200 || b != "" {
		t.Fatalf("add: %d %q", code, b)
	}
	if ok, _ := st.HasABPeer(ctx, mustPersonal(t, st).ID, "222"); !ok {
		t.Fatal("peer id was not normalized to 222")
	}
	_, b = siteDo(t, "POST", ts.URL+"/s/branch/api/ab/peers?ab="+ab.GUID, tok, "")
	if !strings.Contains(b, `"id":"222"`) {
		t.Fatalf("branch view of own device should be plain: %s", b)
	}
	if code, b := siteDo(t, "DELETE", ts.URL+"/api/ab/peer/"+ab.GUID, tok, `["222@br.example.com:21116?key=BR+KEY/="]`); code != 200 || b != "" {
		t.Fatalf("delete: %d %q", code, b)
	}
	if ok, _ := st.HasABPeer(ctx, mustPersonal(t, st).ID, "222"); ok {
		t.Fatal("delete by rewritten id did not remove the peer")
	}

	if code, _ := siteDo(t, "POST", ts.URL+"/s/unknown/api/heartbeat", "", `{"id":"1","uuid":"x"}`); code != 404 {
		t.Fatalf("unknown site: %d", code)
	}
	if code, _ := siteDo(t, "DELETE", ts.URL+"/api/admin/servers/"+def.GUID, tok, ""); code != 400 {
		t.Fatalf("default server deleted: %d", code)
	}
}

func mustPersonal(t *testing.T, st *store.Store) *store.AddressBook {
	t.Helper()
	u, err := st.UserByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	ab, err := st.PersonalAB(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return ab
}

func TestProbeTCP(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if r := probeTCP(l.Addr().String(), 21116); !r.OK {
		t.Fatalf("listening port reported down: %+v", r)
	}
	addr := l.Addr().String()
	l.Close()
	if r := probeTCP(addr, 21116); r.OK || r.Error == "" {
		t.Fatalf("closed port reported up: %+v", r)
	}
	if r := probeTCP("", 21116); r.OK {
		t.Fatal("empty host reported up")
	}
	if r := probeTCP("127.0.0.1", 9); r.Address != "127.0.0.1:9" {
		t.Fatalf("default port not applied: %+v", r)
	}
}
