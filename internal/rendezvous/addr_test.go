package rendezvous

import (
	"net/netip"
	"testing"
)

func TestAddrMangle(t *testing.T) {
	for _, s := range []string{"192.168.10.20:55275", "1.2.3.4:1", "255.255.255.255:65535", "0.0.0.1:0", "[2001:db8::1]:21116"} {
		ap := netip.MustParseAddrPort(s)
		got, ok := decodeAddr(encodeAddr(ap))
		if !ok || got != ap {
			t.Fatalf("%s -> %s", s, got)
		}
	}
	// IPv4-mapped IPv6 encodes as IPv4, like the client's try_into_v4.
	if got, _ := decodeAddr(encodeAddr(netip.MustParseAddrPort("[::ffff:10.0.0.1]:80"))); got.String() != "10.0.0.1:80" {
		t.Fatalf("mapped: %s", got)
	}
}

func mustAP(s string) netip.AddrPort { return netip.MustParseAddrPort(s) }
