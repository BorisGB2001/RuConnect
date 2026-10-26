package api

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// Mirrors the client's ServerConfig.decode (flutter/lib/common.dart).
func TestClientConfigStringRoundTrip(t *testing.T) {
	s := clientConfigString("hq.example.com", "relay.example.com", "https://hq.example.com", "AbC+/=")
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	b, err := base64.URLEncoding.DecodeString(string(r))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["host"] != "hq.example.com" || m["relay"] != "relay.example.com" || m["api"] != "https://hq.example.com" || m["key"] != "AbC+/=" {
		t.Fatalf("decoded %v", m)
	}
}
