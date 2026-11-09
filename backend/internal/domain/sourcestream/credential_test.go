package sourcestream

// 验证凭据加密、连接隔离、密文篡改与缺失密钥时拒绝明文降级。
import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialCipher(t *testing.T) {
	_, err := NewCredentialCipher("invalid")
	require.ErrorIs(t, err, ErrCredentialKey)
	c, err := NewCredentialCipher(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	require.NoError(t, err)
	want := Credential{Kind: "basic", Username: "operator", Secret: "private-test-password"}
	sealed, err := c.Seal(10, want)
	require.NoError(t, err)
	require.NotContains(t, string(sealed), want.Secret)
	got, err := c.Open(10, sealed)
	require.NoError(t, err)
	require.Equal(t, want, got)
	_, err = c.Open(11, sealed)
	require.ErrorIs(t, err, ErrCredential)
	sealed[len(sealed)-1] ^= 1
	_, err = c.Open(10, sealed)
	require.ErrorIs(t, err, ErrCredential)
	_, err = c.Open(10, []byte("short"))
	require.ErrorIs(t, err, ErrCredential)
	for _, item := range []Credential{{Kind: "unknown"}, {Kind: "basic"}, {Kind: "certificate"}} {
		_, err = c.Seal(10, item)
		require.ErrorIs(t, err, ErrCredential)
	}
	_, err = c.Seal(0, want)
	require.ErrorIs(t, err, ErrCredential)
	sealed, err = c.Seal(10, Credential{Kind: "certificate", CertificateFileID: 20, PrivateKeyFileID: 21, CAFileID: 22})
	require.NoError(t, err)
	got, err = c.Open(10, sealed)
	require.NoError(t, err)
	require.EqualValues(t, 20, got.CertificateFileID)
	var missing *CredentialCipher
	_, err = missing.Seal(10, want)
	require.ErrorIs(t, err, ErrCredentialKey)
	_, err = missing.Open(10, sealed)
	require.ErrorIs(t, err, ErrCredentialKey)
}

func TestCredentialNamedValuesStayEncryptedAndScoped(t *testing.T) {
	c, err := NewCredentialCipher(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)))
	require.NoError(t, err)
	for _, kind := range []string{"custom", "http", "basic", "bearer"} {
		credential := Credential{Kind: kind, Values: map[string]string{"apiSecret": "test-private-value", "signingKey": "test-hmac-value"}}
		if kind == "basic" {
			credential.Username = "user"
			credential.Secret = "password"
		}
		if kind == "bearer" {
			credential.Secret = "bearer-token"
		}
		raw, err := c.Seal(10, credential)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "test-private-value")
		opened, err := c.Open(10, raw)
		require.NoError(t, err)
		require.Equal(t, credential, opened)
		_, err = c.Open(11, raw)
		require.ErrorIs(t, err, ErrCredential)
	}
	for _, credential := range []Credential{
		{Kind: "custom"}, {Kind: "custom", Secret: "invalid", Values: map[string]string{"key": "value"}},
		{Kind: "custom", Values: map[string]string{"": "value"}},
		{Kind: "custom", Values: map[string]string{"key": ""}},
		{Kind: "custom", Values: map[string]string{"key": strings.Repeat("x", 64*1024+1)}},
		{Kind: "certificate", CertificateFileID: 1, PrivateKeyFileID: 2, Values: map[string]string{"key": "value"}},
		{Kind: "oauth2", OAuth: &OAuthCredential{}, Values: map[string]string{"key": "value"}},
	} {
		_, err := c.Seal(10, credential)
		require.ErrorIs(t, err, ErrCredential)
	}
}
