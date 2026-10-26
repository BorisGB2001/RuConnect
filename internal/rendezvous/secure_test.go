package rendezvous

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"golang.org/x/crypto/nacl/box"
	"google.golang.org/protobuf/proto"

	"github.com/BorisGB2001/RuConnect/internal/rendezvous/pb"
)

// Wire vectors pinned by the client's hbb_common (src/tcp.rs tests).
func TestWireVectors(t *testing.T) {
	var key [32]byte
	for i := range key {
		key[i] = 0x11
	}
	c := &cipherState{send: key}
	if got := hex.EncodeToString(c.seal([]byte("hello"))); got != "3b768f827fdcc4af555b9e42533be6611e2c333481" {
		t.Fatalf("v0 frame %s", got)
	}
	ini, resp := bytes.Repeat([]byte{0x22}, 32), bytes.Repeat([]byte{0x33}, 32)
	check := func(adv uint32, wantSend, wantRecv string) {
		t.Helper()
		send := splitKey(key[:], kxSplitInit, adv, 1, ini, resp)
		recv := splitKey(key[:], kxSplitResp, adv, 1, ini, resp)
		if hex.EncodeToString(send[:]) != wantSend || hex.EncodeToString(recv[:]) != wantRecv {
			t.Fatalf("split keys for advertised=%d: %x %x", adv, send, recv)
		}
	}
	check(1, "c189ec6e1935c0751cfc85a0f075405cae507d7925bb19687caf56cbb54e0ecf",
		"8a62648e9195cb10ea900c0a24d2a166c1e982347a2dc6833c3e756bf5715d36")
	check(3, "25b0778ae600f9731c7fa457d1cc5d40071b0aa3d34c90b53e1d5344b0e7123a",
		"5bbde0ac6a708d4cd1ff807125d79cb6030bde62f7a66b6cc1a79e31bd657756")
}

// clientKX plays the client's side of the exchange as hbb_common does.
func clientKX(t *testing.T, pk ed25519.PublicKey, hello *pb.KeyExchange, version uint32) (*pb.KeyExchange, *cipherState) {
	t.Helper()
	signed := hello.Keys[0]
	if !ed25519.Verify(pk, signed[64:], signed[:64]) {
		t.Fatal("bad signature")
	}
	theirPK := signed[64:]
	if theirPK[31]&0x80 != 0 {
		sp := hello.SignedParams
		if !ed25519.Verify(pk, sp[64:], sp[:64]) || !bytes.HasPrefix(sp[64:], []byte(kxParamsDomain)) {
			t.Fatal("bad params signature")
		}
		var params pb.KxParams
		if err := proto.Unmarshal(sp[64+len(kxParamsDomain):], &params); err != nil ||
			!bytes.Equal(params.Pk, theirPK) || params.Version != hello.Version {
			t.Fatal("params mismatch")
		}
	}
	ourPub, ourPriv, _ := box.GenerateKey(rand.Reader)
	var sym [32]byte
	rand.Read(sym[:])
	var their [32]byte
	copy(their[:], theirPK)
	var zero [24]byte
	sealed := box.Seal(nil, sym[:], &zero, &their, ourPriv)
	c := &cipherState{}
	if version == 0 {
		c.send, c.recv = sym, sym
	} else {
		c.send = splitKey(sym[:], kxSplitInit, hello.Version, version, ourPub[:], theirPK)
		c.recv = splitKey(sym[:], kxSplitResp, hello.Version, version, ourPub[:], theirPK)
	}
	return &pb.KeyExchange{Keys: [][]byte{ourPub[:], sealed}, Version: version}, c
}

func TestHandshake(t *testing.T) {
	pk, sk, _ := ed25519.GenerateKey(rand.Reader)
	for _, v := range []uint32{0, 1} {
		h, err := newServerHello(sk)
		if err != nil {
			t.Fatal(err)
		}
		reply, cli := clientKX(t, pk, h.msg.GetKeyExchange(), v)
		srv, err := h.finish(reply)
		if err != nil {
			t.Fatalf("v%d: %v", v, err)
		}
		for i := 0; i < 3; i++ {
			got, err := srv.open(cli.seal([]byte("ping")))
			if err != nil || string(got) != "ping" {
				t.Fatalf("v%d client->server: %q %v", v, got, err)
			}
			got, err = cli.open(srv.seal([]byte("pong")))
			if err != nil || string(got) != "pong" {
				t.Fatalf("v%d server->client: %q %v", v, got, err)
			}
		}
	}
	h, _ := newServerHello(sk)
	reply, _ := clientKX(t, pk, h.msg.GetKeyExchange(), 1)
	reply.Version = 2
	if _, err := h.finish(reply); err == nil {
		t.Fatal("unsupported version accepted")
	}
	reply.Version = 1
	reply.Keys[1][0] ^= 1
	if _, err := h.finish(reply); err == nil {
		t.Fatal("tampered key accepted")
	}
}

func TestFrames(t *testing.T) {
	for _, n := range []int{0, 1, 0x3F, 0x40, 0x3FFF, 0x4000, 0x3FFFFF, 0x400000} {
		var buf bytes.Buffer
		payload := bytes.Repeat([]byte{7}, n)
		if err := writeFrame(&buf, payload); err != nil {
			t.Fatal(err)
		}
		got, err := readFrame(bufio.NewReader(&buf), 1<<23)
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("n=%d: len %d %v", n, len(got), err)
		}
	}
	var buf bytes.Buffer
	writeFrame(&buf, make([]byte, 100))
	if _, err := readFrame(bufio.NewReader(&buf), 99); err != errFrameTooBig {
		t.Fatalf("limit not enforced: %v", err)
	}
}
