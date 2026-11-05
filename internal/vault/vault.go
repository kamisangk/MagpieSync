package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Vault encrypts credentials with AES-256-GCM.
type Vault struct {
	aead cipher.AEAD
}

// New loads the key from MAGPIESYNC_SECRET (base64, 32 bytes) or
// <dataDir>/secret.key, generating the latter on first use.
func New(secret, dataDir string) (*Vault, error) {
	key, err := loadKey(secret, dataDir)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead}, nil
}

func loadKey(secret, dataDir string) ([]byte, error) {
	if secret != "" {
		key, err := base64.StdEncoding.DecodeString(secret)
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("MAGPIESYNC_SECRET must be base64 of exactly 32 bytes")
		}
		return key, nil
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "secret.key")
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(b) != 32 {
			return nil, fmt.Errorf("%s must contain exactly 32 bytes", path)
		}
		return b, nil
	case errors.Is(err, os.ErrNotExist):
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, key, 0o600); err != nil {
			return nil, err
		}
		return key, nil
	default:
		return nil, err
	}
}

// Encrypt binds the ciphertext to aad so it cannot be moved between accounts.
func (v *Vault) Encrypt(plain []byte, aad string) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, plain, []byte(aad)), nil
}

func (v *Vault) Decrypt(data []byte, aad string) ([]byte, error) {
	ns := v.aead.NonceSize()
	if len(data) < ns {
		return nil, errors.New("ciphertext too short")
	}
	return v.aead.Open(nil, data[:ns], data[ns:], []byte(aad))
}
