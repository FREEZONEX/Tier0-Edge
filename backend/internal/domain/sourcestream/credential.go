package sourcestream

import (
	"backend/internal/domain/unsmeta"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
)

var ErrCredentialKey = errors.New("SourceStream.credentialKey")
var ErrCredential = errors.New("SourceStream.credentialInvalid")

// Credential 不出现在 Source 文档、运行快照或导出结果中。
type Credential struct {
	OAuth             *OAuthCredential  `json:"oauth,omitempty"`
	Kind              string            `json:"kind"`
	Username          string            `json:"username,omitempty"`
	Secret            string            `json:"secret,omitempty"`
	Values            map[string]string `json:"values,omitempty"`
	CertificateFileID int64             `json:"certificateFileId,omitempty"`
	PrivateKeyFileID  int64             `json:"privateKeyFileId,omitempty"`
	CAFileID          int64             `json:"caFileId,omitempty"`
}

type CredentialCipher struct {
	aead           cipher.AEAD
	fingerprintKey []byte
}

func NewCredentialCipher(encodedKey string) (*CredentialCipher, error) {
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return nil, ErrCredentialKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrCredentialKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrCredentialKey
	}
	fingerprint := hmac.New(sha256.New, key)
	_, _ = fingerprint.Write([]byte("source-credential-intent/1"))
	return &CredentialCipher{aead: aead, fingerprintKey: fingerprint.Sum(nil)}, nil
}

func (s *Service) CredentialIntentDigest(raw []byte) (string, error) {
	if s.cipher == nil || len(s.cipher.fingerprintKey) == 0 {
		return "", ErrCredentialKey
	}
	hash := hmac.New(sha256.New, s.cipher.fingerprintKey)
	_, _ = hash.Write(raw)
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func (c *CredentialCipher) Seal(sourceID int64, credential Credential) ([]byte, error) {
	if sourceID <= 0 {
		return nil, ErrCredential
	}
	return c.SealSource(strconv.FormatInt(sourceID, 10), credential)
}
func (c *CredentialCipher) SealSource(sourceID string, credential Credential) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, ErrCredentialKey
	}
	if !unsmeta.ValidID(sourceID) {
		return nil, ErrCredential
	}
	if credential.Kind != "basic" && credential.Kind != "certificate" && credential.Kind != "oauth2" && credential.Kind != "bearer" && credential.Kind != "custom" && credential.Kind != "http" {
		return nil, ErrCredential
	}
	if len(credential.Values) > 128 || len(credential.Values) > 0 && credential.Kind != "custom" && credential.Kind != "http" && credential.Kind != "basic" && credential.Kind != "bearer" {
		return nil, ErrCredential
	}
	for key, value := range credential.Values {
		if strings.TrimSpace(key) == "" || len(key) > 128 || strings.ContainsAny(key, "\x00\r\n") || value == "" || len(value) > 64*1024 {
			return nil, ErrCredential
		}
	}
	if credential.Kind == "custom" || credential.Kind == "http" {
		if len(credential.Values) == 0 || credential.Secret != "" || credential.Username != "" || credential.OAuth != nil || credential.CertificateFileID != 0 || credential.PrivateKeyFileID != 0 || credential.CAFileID != 0 {
			return nil, ErrCredential
		}
	}
	if credential.Kind == "oauth2" && credential.OAuth == nil {
		return nil, ErrCredential
	}
	if credential.Kind == "bearer" && (strings.TrimSpace(credential.Secret) == "" || strings.ContainsAny(credential.Secret, "\r\n")) {
		return nil, ErrCredential
	}
	if credential.Kind == "basic" && (credential.Username == "" || credential.Secret == "") {
		return nil, ErrCredential
	}
	if credential.Kind == "certificate" && (credential.CertificateFileID <= 0 || credential.PrivateKeyFileID <= 0) {
		return nil, ErrCredential
	}
	if len(credential.Username) > 1024 || len(credential.Secret) > 64*1024 {
		return nil, ErrCredential
	}
	plain, err := json.Marshal(credential)
	if err != nil || len(plain) > 128*1024 {
		return nil, ErrCredential
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plain, []byte("uns-source:"+sourceID)), nil
}
func (c *CredentialCipher) Open(sourceID int64, sealed []byte) (Credential, error) {
	return c.OpenSource(strconv.FormatInt(sourceID, 10), sealed)
}
func (c *CredentialCipher) OpenSource(sourceID string, sealed []byte) (Credential, error) {
	if !unsmeta.ValidID(sourceID) {
		return Credential{}, ErrCredential
	}
	if c == nil || c.aead == nil {
		return Credential{}, ErrCredentialKey
	}
	size := c.aead.NonceSize()
	if len(sealed) < size+c.aead.Overhead() {
		return Credential{}, ErrCredential
	}
	plain, err := c.aead.Open(nil, sealed[:size], sealed[size:], []byte("uns-source:"+sourceID))
	if err != nil {
		return Credential{}, ErrCredential
	}
	var result Credential
	if err = json.Unmarshal(plain, &result); err != nil {
		return Credential{}, ErrCredential
	}
	return result, nil
}
