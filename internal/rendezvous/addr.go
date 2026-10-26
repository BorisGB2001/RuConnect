package rendezvous

import (
	"encoding/binary"
	"math/big"
	"net/netip"
	"time"
)

// RustDesk passes socket addresses between peers in a lightly scrambled form
// ("AddrMangle"): IPv4 addresses are mixed with a timestamp, IPv6 ones are
// sent as 16 address bytes plus a little-endian port.

func encodeAddr(ap netip.AddrPort) []byte {
	ip := ap.Addr().Unmap()
	if ip.Is6() {
		b := ip.As16()
		return binary.LittleEndian.AppendUint16(b[:], ap.Port())
	}
	tm := new(big.Int).SetUint64(uint64(uint32(time.Now().UnixMicro())))
	v4 := ip.As4()
	ipn := new(big.Int).SetUint64(uint64(binary.LittleEndian.Uint32(v4[:])))
	v := new(big.Int).Add(ipn, tm)
	v.Lsh(v, 49)
	v.Or(v, new(big.Int).Lsh(tm, 17))
	low := new(big.Int).And(tm, big.NewInt(0xFFFF))
	v.Or(v, low.Add(low, big.NewInt(int64(ap.Port()))))
	be := v.FillBytes(make([]byte, 16))
	le := make([]byte, 16)
	for i := range be {
		le[i] = be[15-i]
	}
	n := 16
	for n > 0 && le[n-1] == 0 {
		n--
	}
	return le[:n]
}

func decodeAddr(b []byte) (netip.AddrPort, bool) {
	if len(b) > 16 {
		if len(b) != 18 {
			return netip.AddrPort{}, false
		}
		ip := netip.AddrFrom16([16]byte(b[:16]))
		return netip.AddrPortFrom(ip, binary.LittleEndian.Uint16(b[16:])), true
	}
	be := make([]byte, 16)
	for i := range b {
		be[15-i] = b[i]
	}
	v := new(big.Int).SetBytes(be)
	mask32 := new(big.Int).SetUint64(0xFFFFFFFF)
	tm := new(big.Int).And(new(big.Int).Rsh(v, 17), mask32)
	ipn := new(big.Int).Sub(new(big.Int).Rsh(v, 49), tm)
	ipv := uint32(new(big.Int).And(ipn, mask32).Uint64())
	port := new(big.Int).And(v, big.NewInt(0xFFFFFF))
	port.Sub(port, new(big.Int).And(tm, big.NewInt(0xFFFF)))
	var ip [4]byte
	binary.LittleEndian.PutUint32(ip[:], ipv)
	return netip.AddrPortFrom(netip.AddrFrom4(ip), uint16(port.Int64())), true
}
