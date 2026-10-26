// Package totp implements RFC 6238 time-based one-time passwords (SHA-1, 6 digits, 30 s).
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const period = 30

var enc = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewSecret returns a random base32 secret suitable for authenticator apps.
func NewSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return enc.EncodeToString(b)
}

// Code returns the code for the given secret at time t.
func Code(secret string, t time.Time) (string, error) {
	key, err := enc.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	return code(key, uint64(t.Unix()/period)), nil
}

func code(key []byte, counter uint64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1000000)
}

// Verify checks a code, accepting one period of clock skew in either direction.
func Verify(secret, c string, t time.Time) bool {
	c = strings.TrimSpace(c)
	if len(c) != 6 {
		return false
	}
	key, err := enc.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return false
	}
	now := t.Unix() / period
	for d := int64(-1); d <= 1; d++ {
		if subtle.ConstantTimeCompare([]byte(code(key, uint64(now+d))), []byte(c)) == 1 {
			return true
		}
	}
	return false
}

// URI returns an otpauth:// URI for QR codes.
func URI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{"secret": {secret}, "issuer": {issuer}, "digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/" + label + "?" + q.Encode()
}
