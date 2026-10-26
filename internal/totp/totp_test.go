package totp

import (
	"encoding/base32"
	"testing"
	"time"
)

// RFC 6238 appendix B, SHA-1 vectors (last 6 digits).
func TestRFCVectors(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	cases := map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	}
	for ts, want := range cases {
		got, err := Code(secret, time.Unix(ts, 0))
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("t=%d: got %s, want %s", ts, got, want)
		}
	}
}

func TestVerifySkew(t *testing.T) {
	s := NewSecret()
	now := time.Unix(1700000000, 0)
	c, _ := Code(s, now.Add(-30*time.Second))
	if !Verify(s, c, now) {
		t.Fatal("previous period code rejected")
	}
	c, _ = Code(s, now.Add(-90*time.Second))
	if Verify(s, c, now) {
		t.Fatal("stale code accepted")
	}
}
