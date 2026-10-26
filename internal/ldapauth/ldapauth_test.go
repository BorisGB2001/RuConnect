package ldapauth

import (
	"errors"
	"os"
	"testing"
)

// Integration test against a real directory. Run with e.g. GLAuth:
//
//	RUCONNECT_TEST_LDAP_URL=ldap://127.0.0.1:3893 go test ./internal/ldapauth
//
// using users svc, alice (group rdadmins) and bob (group staff), all with
// the password "secret123", under dc=example,dc=com.
func testConfig(t *testing.T) Config {
	url := os.Getenv("RUCONNECT_TEST_LDAP_URL")
	if url == "" {
		t.Skip("RUCONNECT_TEST_LDAP_URL not set")
	}
	return Config{
		URL:          url,
		BindDN:       "cn=svc,ou=staff,ou=users,dc=example,dc=com",
		BindPassword: "secret123",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(&(objectClass=posixAccount)(|(uid=%s)(cn=%s)(mail=%s)))",
		AdminGroupDN: "ou=rdadmins,ou=groups,dc=example,dc=com",
	}
}

func TestAuthenticate(t *testing.T) {
	cfg := testConfig(t)
	id, err := Authenticate(cfg, "alice", "secret123")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("identity: %+v", id)
	if id.Name != "alice" || id.Email != "alice@example.com" || !id.IsAdmin {
		t.Fatalf("unexpected identity %+v", id)
	}
	if id, err := Authenticate(cfg, "bob@example.com", "secret123"); err != nil || id.Name != "bob" || id.IsAdmin {
		t.Fatalf("bob by mail: %+v %v", id, err)
	}
	if _, err := Authenticate(cfg, "alice", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := Authenticate(cfg, "alice", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("empty password must not bind anonymously: %v", err)
	}
	if _, err := Authenticate(cfg, "*", "secret123"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("filter injection: %v", err)
	}
	cfg.RequiredGroup = "ou=rdadmins,ou=groups,dc=example,dc=com"
	if _, err := Authenticate(cfg, "bob", "secret123"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("required group not enforced: %v", err)
	}
}
