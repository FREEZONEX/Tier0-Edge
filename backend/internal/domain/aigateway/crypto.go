package aigateway

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

const credentialAAD = "tier0-ai-provider-config:v1"

type credentialCipher struct {
	aead cipher.AEAD
}

func newCredentialCipher(encodedKey string) (*credentialCipher, error) {
	encodedKey = strings.TrimSpace(encodedKey)
	if encodedKey == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		key, err = hex.DecodeString(encodedKey)
	}
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("AI_GATEWAY_CREDENTIAL_KEY must encode exactly 32 bytes (base64 or hex)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &credentialCipher{aead: aead}, nil
}

func (c *credentialCipher) encrypt(secret string) (string, error) {
	if c == nil {
		return "", ErrCredentialKeyMissing
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(secret), []byte(credentialAAD))
	return "v1:" + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (c *credentialCipher) decrypt(encoded string) (string, error) {
	if c == nil {
		return "", ErrCredentialKeyMissing
	}
	if !strings.HasPrefix(encoded, "v1:") {
		return "", ErrCredentialInvalid
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encoded, "v1:"))
	if err != nil || len(raw) < c.aead.NonceSize() {
		return "", ErrCredentialInvalid
	}
	nonce, ciphertext := raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():]
	plain, err := c.aead.Open(nil, nonce, ciphertext, []byte(credentialAAD))
	if err != nil {
		return "", ErrCredentialInvalid
	}
	return string(plain), nil
}
