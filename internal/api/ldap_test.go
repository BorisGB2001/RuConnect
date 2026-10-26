package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ruconnect/ruconnect/internal/config"
	"github.com/ruconnect/ruconnect/internal/store"
)

// See internal/ldapauth/ldapauth_test.go for the directory layout.
func TestLDAPLogin(t *testing.T) {
	url := os.Getenv("RUCONNECT_TEST_LDAP_URL")
	if url == "" {
		t.Skip("RUCONNECT_TEST_LDAP_URL not set")
	}
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cfg := config.Config{TokenTTL: 3600e9, LDAP: config.LDAP{
		URL:          url,
		BindDN:       "cn=svc,ou=staff,ou=users,dc=example,dc=com",
		BindPassword: "secret123",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(&(objectClass=posixAccount)(|(uid=%s)(cn=%s)))",
		AdminGroupDN: "ou=rdadmins,ou=groups,dc=example,dc=com",
	}}
	ts := httptest.NewServer(New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil).Handler())
	defer ts.Close()

	code, body := siteDo(t, "POST", ts.URL+"/api/login", "", `{"username":"alice","password":"secret123","type":"account"}`)
	var res struct {
		Type  string `json:"type"`
		Token string `json:"access_token"`
		User  struct {
			Name    string `json:"name"`
			IsAdmin bool   `json:"is_admin"`
		} `json:"user"`
	}
	json.Unmarshal([]byte(body), &res)
	if code != 200 || res.Type != "access_token" || res.User.Name != "alice" || !res.User.IsAdmin {
		t.Fatalf("ldap login: %d %s", code, body)
	}
	if code, _ := siteDo(t, "POST", ts.URL+"/api/login", "", `{"username":"alice","password":"nope"}`); code != 401 {
		t.Fatalf("wrong ldap password: %d", code)
	}

	// A local account with a password is never taken over by the directory.
	st.CreateUser(context.Background(), store.UserInput{Name: "bob", Password: "localpass1", Status: store.UserNormal})
	if code, _ := siteDo(t, "POST", ts.URL+"/api/login", "", `{"username":"bob","password":"secret123"}`); code != 401 {
		t.Fatalf("directory password accepted for a local account: %d", code)
	}
	if code, _ := siteDo(t, "POST", ts.URL+"/api/login", "", `{"username":"bob","password":"localpass1"}`); code != 200 {
		t.Fatalf("local password rejected: %d", code)
	}
}
