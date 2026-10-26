package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruconnect/ruconnect/internal/api"
	"github.com/ruconnect/ruconnect/internal/config"
	"github.com/ruconnect/ruconnect/internal/store"
)

const (
	adminName = "admin"
	adminPass = "admin-password"
	testUUID  = "dGVzdC1tYWNoaW5lLXVpZA=="
)

// env is one isolated server instance with a seeded administrator.
type env struct {
	t     *testing.T
	srv   *httptest.Server
	st    *store.Store
	admin string // admin access token
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	cfg := config.Config{
		TokenTTL:      time.Hour,
		OnlineTimeout: time.Minute,
	}
	log := slog.New(slog.DiscardHandler)
	srv := httptest.NewServer(api.New(cfg, st, log, nil).Handler())
	t.Cleanup(func() {
		srv.Close()
		st.Close()
	})
	e := &env{t: t, srv: srv, st: st}
	if _, err := st.CreateUser(context.Background(), store.UserInput{
		Name: adminName, Password: adminPass, Status: store.UserNormal, IsAdmin: true,
	}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	e.admin = e.login(adminName, adminPass)
	return e
}

type resp struct {
	Status int
	Header http.Header
	Body   []byte
}

func (r resp) String() string { return fmt.Sprintf("HTTP %d %q", r.Status, r.Body) }

// obj decodes the body as a JSON object (numbers kept as json.Number).
func (r resp) obj(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(r.Body))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("body is not a JSON object: %v (%s)", err, r)
	}
	if m == nil {
		t.Fatalf("body is JSON null, want object (%s)", r)
	}
	return m
}

// decode decodes the body into v.
func (r resp) decode(t *testing.T, v any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(r.Body))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("decode body: %v (%s)", err, r)
	}
}

// do sends a request. body may be nil (no body), a string or []byte (sent
// verbatim), or any other value (JSON encoded). Like the stock client, every
// request carries Content-Type: application/json.
func (e *env) do(method, path, token string, body any) resp {
	e.t.Helper()
	var rd io.Reader = http.NoBody
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		j, err := json.Marshal(b)
		if err != nil {
			e.t.Fatalf("marshal: %v", err)
		}
		rd = bytes.NewReader(j)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		e.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		e.t.Fatalf("read body: %v", err)
	}
	return resp{Status: res.StatusCode, Header: res.Header, Body: b}
}

func (e *env) post(path, token string, body any) resp {
	return e.do(http.MethodPost, path, token, body)
}
func (e *env) get(path, token string) resp           { return e.do(http.MethodGet, path, token, nil) }
func (e *env) put(path, token string, body any) resp { return e.do(http.MethodPut, path, token, body) }
func (e *env) del(path, token string, body any) resp {
	return e.do(http.MethodDelete, path, token, body)
}

func loginBody(user, pass string) map[string]any {
	return map[string]any{
		"username":   user,
		"password":   pass,
		"id":         "",
		"uuid":       testUUID,
		"autoLogin":  true,
		"type":       "account",
		"deviceInfo": map[string]string{"os": "linux", "type": "client", "name": "test-host"},
	}
}

// login performs an account login and returns the access token.
func (e *env) login(user, pass string) string {
	e.t.Helper()
	r := e.post("/api/login", "", loginBody(user, pass))
	if r.Status != http.StatusOK {
		e.t.Fatalf("login %s: %s", user, r)
	}
	m := r.obj(e.t)
	tok, _ := m["access_token"].(string)
	if m["type"] != "access_token" || tok == "" {
		e.t.Fatalf("login %s: unexpected response %s", user, r)
	}
	return tok
}

// createUser creates a user through the admin API and returns its guid.
func (e *env) createUser(name, pass string, admin bool) string {
	e.t.Helper()
	r := e.post("/api/admin/users", e.admin, map[string]any{"name": name, "password": pass, "is_admin": admin})
	if r.Status != http.StatusOK {
		e.t.Fatalf("create user %s: %s", name, r)
	}
	guid, _ := r.obj(e.t)["guid"].(string)
	if guid == "" {
		e.t.Fatalf("create user %s: no guid in %s", name, r)
	}
	return guid
}

// userWithToken creates a non-admin user and logs it in.
func (e *env) userWithToken(name string) (guid, token string) {
	e.t.Helper()
	pass := name + "-password"
	guid = e.createUser(name, pass, false)
	return guid, e.login(name, pass)
}

// controllerToken creates a user and logs it in from the given device id,
// as the controlling client does.
func (e *env) controllerToken(name, deviceID string) string {
	e.t.Helper()
	pass := name + "-password"
	e.createUser(name, pass, false)
	body := loginBody(name, pass)
	body["id"] = deviceID
	r := e.post("/api/login", "", body)
	if r.Status != http.StatusOK {
		e.t.Fatalf("login %s: %s", name, r)
	}
	return r.obj(e.t)["access_token"].(string)
}

func (e *env) createGroup(name string) string {
	e.t.Helper()
	r := e.post("/api/admin/groups", e.admin, map[string]any{"name": name})
	if r.Status != http.StatusOK {
		e.t.Fatalf("create group %s: %s", name, r)
	}
	return r.obj(e.t)["guid"].(string)
}

func (e *env) createSharedAB(name, password string) string {
	e.t.Helper()
	r := e.post("/api/admin/address-books", e.admin, map[string]any{"name": name, "password": password})
	if r.Status != http.StatusOK {
		e.t.Fatalf("create address book %s: %s", name, r)
	}
	return r.obj(e.t)["guid"].(string)
}

func (e *env) setABRule(abGUID, userGUID string, rule int) {
	e.t.Helper()
	r := e.put("/api/admin/address-books/"+abGUID+"/rules", e.admin, map[string]any{"user_guid": userGUID, "rule": rule})
	if r.Status != http.StatusOK {
		e.t.Fatalf("set rule: %s", r)
	}
}

// sysinfo uploads a sysinfo record for a device; extra keys are merged in.
func (e *env) sysinfo(id, hostname string, extra map[string]any) resp {
	e.t.Helper()
	body := map[string]any{
		"cpu":      "Intel(R) Core(TM) i7, 3.6GHz, 8/4 cores",
		"memory":   "15.6GB",
		"os":       "windows / Windows 10 Pro - 19045",
		"hostname": hostname,
		"username": "bob",
		"version":  "1.4.3",
		"id":       id,
		"uuid":     testUUID,
	}
	for k, v := range extra {
		body[k] = v
	}
	return e.post("/api/sysinfo", "", body)
}

func (e *env) registerDevice(id, hostname string) {
	e.t.Helper()
	if r := e.sysinfo(id, hostname, nil); string(r.Body) != "SYSINFO_UPDATED" {
		e.t.Fatalf("sysinfo %s: %s", id, r)
	}
}

func (e *env) heartbeat(id string, conns []int) map[string]any {
	e.t.Helper()
	body := map[string]any{"id": id, "uuid": testUUID, "ver": 1004030, "modified_at": 0}
	if conns != nil {
		body["conns"] = conns
	}
	r := e.post("/api/heartbeat", "", body)
	if r.Status != http.StatusOK {
		e.t.Fatalf("heartbeat: %s", r)
	}
	return r.obj(e.t)
}

// listData returns total and data of a {"total","data"} envelope.
func listData(t *testing.T, r resp) (int, []map[string]any) {
	t.Helper()
	if r.Status != http.StatusOK {
		t.Fatalf("list: %s", r)
	}
	var env struct {
		Total *json.Number     `json:"total"`
		Data  []map[string]any `json:"data"`
		Error *string          `json:"error"`
	}
	r.decode(t, &env)
	if env.Error != nil {
		t.Fatalf("list: error %q", *env.Error)
	}
	if env.Total == nil {
		t.Fatalf("list: missing total (client would ignore data): %s", r)
	}
	if env.Data == nil {
		t.Fatalf("list: data must be an array, got %s", r)
	}
	n, err := env.Total.Int64()
	if err != nil {
		t.Fatalf("list: total is not an int: %v", err)
	}
	return int(n), env.Data
}

// expectEmpty asserts the client's action-endpoint success: HTTP 200, zero-byte body.
func expectEmpty(t *testing.T, what string, r resp) {
	t.Helper()
	if r.Status != http.StatusOK || len(r.Body) != 0 {
		t.Errorf("%s: want HTTP 200 with an empty body, got %s", what, r)
	}
}

// expectError asserts a JSON {"error": "<non-empty string>"} body and a non-200 status.
func expectError(t *testing.T, what string, r resp, status int) {
	t.Helper()
	if status != 0 && r.Status != status {
		t.Errorf("%s: want HTTP %d, got %s", what, status, r)
	}
	if status == 0 && r.Status == http.StatusOK {
		t.Errorf("%s: want a non-200 status, got %s", what, r)
	}
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Errorf("%s: error body is not JSON: %s", what, r)
		return
	}
	if s, ok := m["error"].(string); !ok || s == "" {
		t.Errorf("%s: want non-empty string error, got %s", what, r)
	}
}

var seq atomic.Int64

func nonce() string { return fmt.Sprintf("nonce-%d-%d", time.Now().UnixNano(), seq.Add(1)) }

func numStr(v any) string {
	switch n := v.(type) {
	case json.Number:
		return n.String()
	case string:
		return n
	case nil:
		return "<nil>"
	default:
		return fmt.Sprint(n)
	}
}

func findBy(items []map[string]any, key, val string) map[string]any {
	for _, it := range items {
		if numStr(it[key]) == val {
			return it
		}
	}
	return nil
}

func strList(v any) []string {
	arr, _ := v.([]any)
	out := []string{}
	for _, x := range arr {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func storeDisabled(name string) store.UserInput {
	return store.UserInput{Name: name, Password: name + "-password", Status: store.UserDisabled}
}
