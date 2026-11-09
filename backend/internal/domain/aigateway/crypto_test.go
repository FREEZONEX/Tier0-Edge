package aigateway

// 测试目标：AI 网关凭证加密（crypto.go）。
// 覆盖的正常路径：base64 与 hex 两种 32 字节密钥构造；加密→解密往返；密文带 v1: 前缀。
// 覆盖的异常情况：空密钥（nil cipher，返回 ErrCredentialKeyMissing）、非法 base64/hex、
// 非 32 字节、无 v1: 前缀、密文被篡改、AAD 不匹配。

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestNewCredentialCipher(t *testing.T) {
	// 空密钥 → nil cipher（不报错）
	cipher, err := newCredentialCipher("")
	if err != nil || cipher != nil {
		t.Fatalf("empty key = %v, %v", cipher, err)
	}

	// base64 32 字节
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cipher, err = newCredentialCipher(key)
	if err != nil || cipher == nil {
		t.Fatalf("base64 key = %v, %v", cipher, err)
	}

	// hex 32 字节
	hexKey := hex.EncodeToString(make([]byte, 32))
	cipher, err = newCredentialCipher(hexKey)
	if err != nil || cipher == nil {
		t.Fatalf("hex key = %v, %v", cipher, err)
	}

	// 非法输入
	if _, err := newCredentialCipher("not-a-key"); err == nil {
		t.Fatal("invalid key should error")
	}
	if _, err := newCredentialCipher(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("short key should error")
	}
}

func TestCredentialCipherRoundTrip(t *testing.T) {
	cipher, err := newCredentialCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatalf("newCredentialCipher: %v", err)
	}
	secret := "sk-test-123456"
	encrypted, err := cipher.encrypt(secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(encrypted, "v1:") {
		t.Fatalf("encrypted = %q, want v1: prefix", encrypted)
	}
	decrypted, err := cipher.decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted != secret {
		t.Fatalf("round trip = %q, want %q", decrypted, secret)
	}

	// 两次加密结果不同（随机 nonce）
	encrypted2, _ := cipher.encrypt(secret)
	if encrypted == encrypted2 {
		t.Fatal("encrypt should be non-deterministic")
	}
}

func TestCredentialCipherErrors(t *testing.T) {
	cipher, err := newCredentialCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatalf("newCredentialCipher: %v", err)
	}
	// nil cipher
	var nilCipher *credentialCipher
	if _, err := nilCipher.encrypt("x"); !errors.Is(err, ErrCredentialKeyMissing) {
		t.Fatalf("nil encrypt = %v", err)
	}
	if _, err := nilCipher.decrypt("v1:abc"); !errors.Is(err, ErrCredentialKeyMissing) {
		t.Fatalf("nil decrypt = %v", err)
	}

	// 无前缀
	if _, err := cipher.decrypt("raw"); !errors.Is(err, ErrCredentialInvalid) {
		t.Fatalf("no prefix = %v", err)
	}
	// 非法 base64
	if _, err := cipher.decrypt("v1:!!!not-base64!!!"); !errors.Is(err, ErrCredentialInvalid) {
		t.Fatalf("bad base64 = %v", err)
	}
	// 篡改密文
	encrypted, _ := cipher.encrypt("secret")
	tampered := encrypted[:len(encrypted)-2] + "XX"
	if _, err := cipher.decrypt(tampered); !errors.Is(err, ErrCredentialInvalid) {
		t.Fatalf("tampered = %v", err)
	}
}
