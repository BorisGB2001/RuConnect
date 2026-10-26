package rendezvous

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/nacl/box"
	"golang.org/x/crypto/nacl/secretbox"
	"google.golang.org/protobuf/proto"

	"github.com/BorisGB2001/RuConnect/internal/rendezvous/pb"
)

// Secure TCP between a client and the rendezvous server.
//
// The server opens with KeyExchange{keys: [sign(ephemeral X25519 public key)]},
// signed with the server's long-term Ed25519 key the clients pin as "Key".
// The client answers KeyExchange{keys: [its ephemeral public key,
// box(symmetric key)]} and from then on every frame longer than one byte is a
// secretbox under a per-direction counter nonce.
//
// Version 1 (advertised in KeyExchange.version) derives one key per direction
// from the exchanged key and the handshake transcript. The server marks the
// signed public key with bit 255 (ignored by X25519) and signs KxParams next
// to it, so a client cannot be downgraded to version 0 by stripping fields.

const (
	kxVersionLatest = 1
	kxParamsDomain  = "rdkx-params"
	kxSplitContext  = "rdkx-spl"
	kxSplitInit     = 1
	kxSplitResp     = 2
)

// cipherState encrypts one connection. Counters start at zero and are
// incremented before use, matching the client.
type cipherState struct {
	send, recv       [32]byte
	sendSeq, recvSeq uint64
}

func nonceFor(seq uint64) *[24]byte {
	var n [24]byte
	binary.LittleEndian.PutUint64(n[:8], seq)
	return &n
}

func (c *cipherState) seal(msg []byte) []byte {
	c.sendSeq++
	return secretbox.Seal(nil, msg, nonceFor(c.sendSeq), &c.send)
}

var errDecrypt = errors.New("decryption error")

func (c *cipherState) open(frame []byte) ([]byte, error) {
	if len(frame) <= 1 { // keep-alive bytes are never encrypted
		return frame, nil
	}
	c.recvSeq++
	out, ok := secretbox.Open(nil, frame, nonceFor(c.recvSeq), &c.recv)
	if !ok {
		return nil, errDecrypt
	}
	return out, nil
}

// serverHello is the server's side of one key exchange.
type serverHello struct {
	msg       *pb.RendezvousMessage
	pub, priv *[32]byte
	signedPub []byte // the public key bytes as signed (bit 255 set for v1)
	version   uint32
}

func newServerHello(sk ed25519.PrivateKey) (*serverHello, error) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	h := &serverHello{pub: pub, priv: priv, version: kxVersionLatest}
	h.signedPub = append([]byte(nil), pub[:]...)
	h.signedPub[31] |= 0x80
	params, err := proto.Marshal(&pb.KxParams{Pk: h.signedPub, Version: h.version})
	if err != nil {
		return nil, err
	}
	h.msg = &pb.RendezvousMessage{Union: &pb.RendezvousMessage_KeyExchange{KeyExchange: &pb.KeyExchange{
		Keys:         [][]byte{signCombined(sk, h.signedPub)},
		Version:      h.version,
		SignedParams: signCombined(sk, append([]byte(kxParamsDomain), params...)),
	}}}
	return h, nil
}

// signCombined returns signature||message, libsodium's crypto_sign format.
func signCombined(sk ed25519.PrivateKey, msg []byte) []byte {
	return append(ed25519.Sign(sk, msg), msg...)
}

var errHandshake = errors.New("handshake failed")

// finish processes the client's KeyExchange reply.
func (h *serverHello) finish(ex *pb.KeyExchange) (*cipherState, error) {
	if ex == nil || len(ex.Keys) != 2 || len(ex.Keys[0]) != 32 {
		return nil, errHandshake
	}
	var theirPub [32]byte
	copy(theirPub[:], ex.Keys[0])
	var zero [24]byte
	key, ok := box.Open(nil, ex.Keys[1], &zero, &theirPub, h.priv)
	if !ok || len(key) != 32 {
		return nil, errHandshake
	}
	c := &cipherState{}
	switch picked := ex.Version; {
	case picked == 0:
		copy(c.send[:], key)
		c.recv = c.send
	case picked == 1 && picked <= h.version:
		// The client sealed the key, so it is the initiator.
		c.recv = splitKey(key, kxSplitInit, h.version, picked, ex.Keys[0], h.signedPub)
		c.send = splitKey(key, kxSplitResp, h.version, picked, ex.Keys[0], h.signedPub)
	default:
		return nil, errHandshake
	}
	return c, nil
}

func splitKey(key []byte, direction byte, advertised, picked uint32, initiatorPK, responderPK []byte) [32]byte {
	hh, _ := blake2b.New(32, key)
	hh.Write([]byte(kxSplitContext))
	hh.Write([]byte{direction})
	hh.Write(binary.LittleEndian.AppendUint32(nil, advertised))
	hh.Write(binary.LittleEndian.AppendUint32(nil, picked))
	hh.Write(initiatorPK)
	hh.Write(responderPK)
	var out [32]byte
	copy(out[:], hh.Sum(nil))
	return out
}
