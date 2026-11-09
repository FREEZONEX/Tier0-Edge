package tenantpg

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// DeriveRoleName 返回 project database 的受限 role 名称：rw_<databaseName>
func DeriveRoleName(databaseName string) string {
	return "rw_" + databaseName
}

// DeriveRolePassword 返回 HMAC-SHA256(secret, databaseName) 的 hex 字符串
func DeriveRolePassword(roleSecret, databaseName string) string {
	mac := hmac.New(sha256.New, []byte(roleSecret))
	mac.Write([]byte(databaseName))
	return hex.EncodeToString(mac.Sum(nil))
}

// DeriveCredentials 返回 project database 受限 role 的 (username, password)
func DeriveCredentials(roleSecret, databaseName string) (string, string, error) {
	if err := ValidateIdentifier(databaseName); err != nil {
		return "", "", fmt.Errorf("invalid database name for credential derivation: %w", err)
	}
	roleName := DeriveRoleName(databaseName)
	if len(roleName) > 63 {
		return "", "", fmt.Errorf("derived role name %q exceeds 63-char PG limit", roleName)
	}
	return roleName, DeriveRolePassword(roleSecret, databaseName), nil
}

// ValidateIdentifier 白名单校验 SQL 标识符：仅 [a-z0-9_]，长度 1..63
func ValidateIdentifier(s string) error {
	if len(s) == 0 || len(s) > 63 {
		return fmt.Errorf("length out of range: %d", len(s))
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return fmt.Errorf("unexpected char %q", c)
		}
	}
	return nil
}
