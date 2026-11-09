package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"path/filepath"
)

const (
	masterKeySize  = 32
	ciphertextV1   = byte(1)
	masterKeyName  = "master.key"
	accessTokenAAD = "tier0-fleet-agent/access-token/v1"
)

var ErrCiphertextInvalid = errors.New("fleet secret ciphertext is invalid")

type Protector struct {
	aead cipher.AEAD
}

func Open(dataDir string) (*Protector, error) {
	key, err := loadOrCreatePlatformKey(filepath.Join(dataDir, masterKeyName))
	if err != nil {
		return nil, fmt.Errorf("load Fleet Agent master key: %w", err)
	}
	if len(key) != masterKeySize {
		return nil, fmt.Errorf("load Fleet Agent master key: unexpected key length %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create Fleet Agent secret cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create Fleet Agent secret AEAD: %w", err)
	}
	return &Protector{aead: aead}, nil
}

func (p *Protector) EncryptAccessToken(token string) ([]byte, error) {
	if token == "" {
		return nil, nil
	}
	if p == nil || p.aead == nil {
		return nil, errors.New("Fleet Agent secret protector is unavailable")
	}
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate Fleet Agent secret nonce: %w", err)
	}
	out := make([]byte, 1, 1+len(nonce)+len(token)+p.aead.Overhead())
	out[0] = ciphertextV1
	out = append(out, nonce...)
	out = p.aead.Seal(out, nonce, []byte(token), []byte(accessTokenAAD))
	return out, nil
}

func (p *Protector) DecryptAccessToken(ciphertext []byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", nil
	}
	if p == nil || p.aead == nil {
		return "", errors.New("Fleet Agent secret protector is unavailable")
	}
	nonceSize := p.aead.NonceSize()
	if len(ciphertext) < 1+nonceSize+p.aead.Overhead() || ciphertext[0] != ciphertextV1 {
		return "", ErrCiphertextInvalid
	}
	nonce := ciphertext[1 : 1+nonceSize]
	plaintext, err := p.aead.Open(nil, nonce, ciphertext[1+nonceSize:], []byte(accessTokenAAD))
	if err != nil {
		return "", ErrCiphertextInvalid
	}
	return string(plaintext), nil
}

func generateMasterKey() ([]byte, error) {
	key := make([]byte, masterKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	return key, nil
}
