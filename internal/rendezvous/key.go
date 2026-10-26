package rendezvous

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadOrCreateKey reads the server's Ed25519 key pair from path (and
// path+".pub"), in the format hbbs uses: base64 of libsodium's 64-byte secret
// key and of the 32-byte public key. A missing key is copied from importPath
// (an earlier hbbs key file) when that exists, else generated, so clients
// keep their configured Key.
func LoadOrCreateKey(path, importPath string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && importPath != "" {
		if old, ierr := os.ReadFile(importPath); ierr == nil {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, old, 0o600); err != nil {
				return nil, err
			}
			data, err = old, nil
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		return createKey(path)
	}
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%s: not a base64 Ed25519 secret key", path)
	}
	sk := ed25519.PrivateKey(raw)
	// libsodium keeps the public key in the second half; make sure it matches the seed.
	if !ed25519.NewKeyFromSeed(sk.Seed()).Equal(sk) {
		return nil, fmt.Errorf("%s: corrupt key", path)
	}
	pub := PublicKeyString(sk)
	if old, err := os.ReadFile(path + ".pub"); err != nil || strings.TrimSpace(string(old)) != pub {
		if err := os.WriteFile(path+".pub", []byte(pub), 0o644); err != nil {
			return nil, err
		}
	}
	return sk, nil
}

func createKey(path string) (ed25519.PrivateKey, error) {
	_, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(sk)), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path+".pub", []byte(PublicKeyString(sk)), 0o644); err != nil {
		return nil, err
	}
	return sk, nil
}

// PublicKeyString is the key clients enter in the "Key" field.
func PublicKeyString(sk ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(sk.Public().(ed25519.PublicKey))
}
