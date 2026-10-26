package rendezvous

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id_ed25519")
	sk, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := LoadOrCreateKey(path)
	if err != nil || !again.Equal(sk) {
		t.Fatalf("reload: %v", err)
	}
	pub, _ := os.ReadFile(path + ".pub")
	if string(pub) != PublicKeyString(sk) {
		t.Fatal("public key file mismatch")
	}
	os.WriteFile(path, []byte("garbage"), 0o600)
	if _, err := LoadOrCreateKey(path); err == nil {
		t.Fatal("corrupt key accepted")
	}
}

// The demo hbbs key pair must load unchanged.
func TestHbbsKeyCompat(t *testing.T) {
	src := os.Getenv("RUCONNECT_TEST_HBBS_KEY")
	if src == "" {
		t.Skip("RUCONNECT_TEST_HBBS_KEY not set")
	}
	data, _ := os.ReadFile(src)
	pub, _ := os.ReadFile(src + ".pub")
	path := filepath.Join(t.TempDir(), "id_ed25519")
	os.WriteFile(path, data, 0o600)
	sk, err := LoadOrCreateKey(path)
	if err != nil || PublicKeyString(sk) != string(pub) {
		t.Fatalf("hbbs key: %v %s vs %s", err, PublicKeyString(sk), pub)
	}
}
