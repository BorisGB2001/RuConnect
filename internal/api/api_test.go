package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ruconnect/ruconnect/internal/store"
	"github.com/ruconnect/ruconnect/internal/totp"
)

// checkUserPayload pins the UserPayload shape (doc 4.1).
func checkUserPayload(t *testing.T, u map[string]any, name string) {
	t.Helper()
	if u["name"] != name {
		t.Errorf("user.name = %v, want %q", u["name"], name)
	}
	st, ok := u["status"].(json.Number)
	if !ok || st.String() != "1" {
		t.Errorf("user.status = %#v, want the integer 1", u["status"])
	}
	if _, ok := u["is_admin"].(bool); !ok {
		t.Errorf("user.is_admin = %#v, want bool", u["is_admin"])
	}
	info, ok := u["info"].(map[string]any)
	if !ok {
		t.Fatalf("user.info = %#v, want an object (required by the Rust client)", u["info"])
	}
	if _, ok := info["login_device_whitelist"].([]any); !ok {
		t.Errorf("info.login_device_whitelist = %#v, want array", info["login_device_whitelist"])
	}
	if _, ok := info["other"].(map[string]any); !ok {
		t.Errorf("info.other = %#v, want object", info["other"])
	}
	if _, ok := info["email_verification"].(bool); !ok {
		t.Errorf("info.email_verification = %#v, want bool", info["email_verification"])
	}
}

func TestLoginSuccessShape(t *testing.T) {
	e := newEnv(t)
	r := e.post("/api/login", "", loginBody(adminName, adminPass))
	if r.Status != http.StatusOK {
		t.Fatalf("login: %s", r)
	}
	m := r.obj(t)
	if _, bad := m["error"]; bad {
		t.Fatalf("success response must not contain error: %s", r)
	}
	if m["type"] != "access_token" {
		t.Errorf("type = %v, want access_token", m["type"])
	}
	if tok, _ := m["access_token"].(string); tok == "" {
		t.Errorf("access_token missing: %s", r)
	}
	u, ok := m["user"].(map[string]any)
	if !ok {
		t.Fatalf("user missing: %s", r)
	}
	checkUserPayload(t, u, adminName)
	if u["is_admin"] != true {
		t.Errorf("admin user is_admin = %v", u["is_admin"])
	}
}

func TestLoginWrongPassword(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct{ user, pass string }{{adminName, "nope"}, {"ghost", "whatever"}} {
		r := e.post("/api/login", "", loginBody(c.user, c.pass))
		expectError(t, "login "+c.user, r, 0)
		if _, ok := r.obj(t)["access_token"]; ok {
			t.Errorf("failed login returned an access_token: %s", r)
		}
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t)
	e.createUser("other", "other-password", false)
	for i := 0; i < 5; i++ {
		r := e.post("/api/login", "", loginBody(adminName, "wrong"))
		if r.Status == http.StatusTooManyRequests {
			t.Fatalf("attempt %d already rate limited", i+1)
		}
		expectError(t, "wrong password", r, 0)
	}
	// Blocked now, even with the right password.
	r := e.post("/api/login", "", loginBody(adminName, adminPass))
	expectError(t, "after 5 failures", r, http.StatusTooManyRequests)
	// The limit is per user (and ip): another account is unaffected.
	e.login("other", "other-password")
}

func TestLoginDisabledUser(t *testing.T) {
	e := newEnv(t)
	if _, err := e.st.CreateUser(context.Background(), store.UserInput{
		Name: "off", Password: "off-password", Status: store.UserDisabled,
	}); err != nil {
		t.Fatal(err)
	}
	r := e.post("/api/login", "", loginBody("off", "off-password"))
	expectError(t, "disabled login", r, 0)
	if _, ok := r.obj(t)["access_token"]; ok {
		t.Errorf("disabled user got a token: %s", r)
	}

	// Disabling a logged-in user invalidates the token.
	guid, tok := e.userWithToken("soon-off")
	if r := e.post("/api/currentUser", tok, nil); r.Status != http.StatusOK {
		t.Fatalf("currentUser: %s", r)
	}
	if r := e.put("/api/admin/users/"+guid, e.admin, map[string]any{"name": "soon-off", "status": 0}); r.Status != http.StatusOK {
		t.Fatalf("disable: %s", r)
	}
	expectError(t, "currentUser after disable", e.post("/api/currentUser", tok, nil), http.StatusUnauthorized)
}

func TestLoginTOTP(t *testing.T) {
	e := newEnv(t)
	guid := e.createUser("bob", "bob-password", false)
	r := e.post("/api/admin/users/"+guid+"/totp", e.admin, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("enable totp: %s", r)
	}
	secret, _ := r.obj(t)["secret"].(string)
	if secret == "" {
		t.Fatalf("no secret: %s", r)
	}

	step1 := func() string {
		t.Helper()
		r := e.post("/api/login", "", loginBody("bob", "bob-password"))
		if r.Status != http.StatusOK {
			t.Fatalf("step 1: %s", r)
		}
		m := r.obj(t)
		if m["type"] != "email_check" || m["tfa_type"] != "tfa_check" {
			t.Fatalf("step 1: want type email_check + tfa_type tfa_check, got %s", r)
		}
		if _, ok := m["access_token"].(string); ok && m["access_token"] != "" {
			t.Fatalf("step 1 must not issue a token: %s", r)
		}
		s, _ := m["secret"].(string)
		if s == "" {
			t.Fatalf("step 1: no secret: %s", r)
		}
		if u, _ := m["user"].(map[string]any); u == nil || u["name"] != "bob" {
			t.Fatalf("step 1: user.name missing: %s", r)
		}
		return s
	}
	step2 := func(challenge, code string) resp {
		return e.post("/api/login", "", map[string]any{
			"username": "bob", "verificationCode": code, "tfaCode": code, "secret": challenge,
			"id": "", "uuid": testUUID, "autoLogin": true, "type": "email_code",
			"deviceInfo": map[string]string{"os": "linux", "type": "client", "name": "h"},
		})
	}

	challenge := step1()
	wrong := "000000"
	if totp.Verify(secret, wrong, time.Now()) {
		wrong = "111111"
	}
	expectError(t, "wrong totp code", step2(challenge, wrong), 0)

	good, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r = step2(challenge, good)
	if r.Status != http.StatusOK {
		t.Fatalf("step 2: %s", r)
	}
	m := r.obj(t)
	tok, _ := m["access_token"].(string)
	if m["type"] != "access_token" || tok == "" {
		t.Fatalf("step 2: %s", r)
	}
	checkUserPayload(t, m["user"].(map[string]any), "bob")
	if r := e.post("/api/currentUser", tok, nil); r.Status != http.StatusOK {
		t.Fatalf("token from TOTP login does not work: %s", r)
	}
	// The challenge is single-use.
	expectError(t, "reused challenge", step2(challenge, good), 0)

	// Email-style second step (no tfaCode, only verificationCode) also works with the TOTP code.
	// A used code cannot be replayed, so take the next period's code (accepted within the skew window).
	next, _ := totp.Code(secret, time.Now().Add(30*time.Second))
	challenge = step1()
	r = e.post("/api/login", "", map[string]any{
		"username": "bob", "verificationCode": next, "secret": challenge, "type": "email_code",
	})
	if r.Status != http.StatusOK || r.obj(t)["type"] != "access_token" {
		t.Fatalf("verificationCode only: %s", r)
	}
}

func TestLogoutInvalidatesToken(t *testing.T) {
	e := newEnv(t)
	tok := e.login(adminName, adminPass)
	if r := e.post("/api/logout", tok, map[string]string{"id": "1", "uuid": testUUID}); r.Status != http.StatusOK {
		t.Fatalf("logout: %s", r)
	}
	expectError(t, "currentUser after logout", e.post("/api/currentUser", tok, nil), http.StatusUnauthorized)
	// The other session stays valid.
	if r := e.post("/api/currentUser", e.admin, nil); r.Status != http.StatusOK {
		t.Fatalf("other token: %s", r)
	}
}

func TestCurrentUserTopLevelPayload(t *testing.T) {
	e := newEnv(t)
	r := e.post("/api/currentUser", e.admin, map[string]string{"id": "123", "uuid": testUUID})
	if r.Status != http.StatusOK {
		t.Fatalf("currentUser: %s", r)
	}
	m := r.obj(t)
	if _, wrapped := m["user"]; wrapped {
		t.Errorf("UserPayload must be at the top level, not wrapped: %s", r)
	}
	if _, bad := m["error"]; bad {
		t.Errorf("unexpected error key: %s", r)
	}
	checkUserPayload(t, m, adminName)
}

func TestInvalidToken401(t *testing.T) {
	e := newEnv(t)
	cases := []struct{ method, path string }{
		{"POST", "/api/currentUser"},
		{"POST", "/api/ab/personal"},
		{"POST", "/api/ab/shared/profiles"},
		{"GET", "/api/ab"},
		{"GET", "/api/peers"},
		{"GET", "/api/users?accessible=&status=1"},
		{"GET", "/api/device-group/accessible"},
		{"GET", "/api/audit/conn/active?id=1&session_id=1"},
	}
	for _, c := range cases {
		for _, tok := range []string{"", "not-a-token"} {
			r := e.do(c.method, c.path, tok, nil)
			expectError(t, c.method+" "+c.path+" token="+tok, r, http.StatusUnauthorized)
		}
	}
}

func TestLoginOptionsIsArray(t *testing.T) {
	e := newEnv(t)
	r := e.get("/api/login-options", "")
	if r.Status/100 != 2 {
		t.Fatalf("login-options: %s", r)
	}
	var arr []string
	r.decode(t, &arr)
	if arr == nil {
		t.Fatalf("login-options must be a JSON array, got %s", r)
	}
	// HEAD is used as a TLS probe; any HTTP response is fine, it just must not hang or fail.
	if r := e.do(http.MethodHead, "/api/login-options", "", nil); r.Status == 0 {
		t.Fatal("HEAD failed")
	}
}

func TestLoginEmptyAndBadBodies(t *testing.T) {
	e := newEnv(t)
	expectError(t, "garbage body", e.post("/api/login", "", "{not json"), 0)
	expectError(t, "empty body", e.post("/api/login", "", nil), 0)
	expectError(t, "unknown type", e.post("/api/login", "", map[string]any{"username": adminName, "password": adminPass, "type": "sms_code"}), 0)
	expectError(t, "email_code without challenge", e.post("/api/login", "", map[string]any{
		"username": adminName, "verificationCode": "123456", "secret": "bogus", "type": "email_code",
	}), 0)
}

func TestLoginClaimsUnassignedDevice(t *testing.T) {
	e := newEnv(t)
	e.registerDevice("700000001", "pc1")
	_, _ = e.userWithToken("ivy")
	body := loginBody("ivy", "ivy-password")
	body["id"] = "700000001"
	if r := e.post("/api/login", "", body); r.Status != http.StatusOK {
		t.Fatalf("login: %s", r)
	}
	_, items := listData(t, e.get("/api/admin/devices", e.admin))
	d := findBy(items, "id", "700000001")
	if d == nil || d["user_name"] != "ivy" {
		t.Fatalf("device not claimed by ivy: %v", d)
	}
	// A second user logging in on the same device does not steal it.
	e.createUser("jack", "jack-password", false)
	body = loginBody("jack", "jack-password")
	body["id"] = "700000001"
	e.post("/api/login", "", body)
	_, items = listData(t, e.get("/api/admin/devices", e.admin))
	if d := findBy(items, "id", "700000001"); d["user_name"] != "ivy" {
		t.Fatalf("device owner changed to %v", d["user_name"])
	}
}

// A TOTP code accepted once cannot be replayed in another login.
func TestTOTPReplay(t *testing.T) {
	e := newEnv(t)
	guid := e.createUser("carol", "carol-password", false)
	r := e.post("/api/admin/users/"+guid+"/totp", e.admin, nil)
	secret, _ := r.obj(t)["secret"].(string)
	code, _ := totp.Code(secret, time.Now())
	step := func() resp {
		first := e.post("/api/login", "", loginBody("carol", "carol-password")).obj(t)
		body := loginBody("carol", "carol-password")
		body["type"], body["secret"], body["verificationCode"], body["tfaCode"] = "email_code", first["secret"], code, code
		return e.post("/api/login", "", body)
	}
	if r := step(); r.Status != http.StatusOK {
		t.Fatalf("first use: %s", r)
	}
	if r := step(); r.Status == http.StatusOK {
		t.Fatalf("replayed code accepted: %s", r)
	}
}
