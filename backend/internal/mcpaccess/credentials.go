package mcpaccess

import "strings"

// IsCredentialField is shared by query provenance checks and output redaction.
func IsCredentialField(key string) bool {
	key = strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	switch key {
	case "password", "passwd", "secret", "token", "apikey", "accesskey", "secretkey", "privatekey", "clientsecret", "authorization", "cookie", "setcookie", "accesstoken", "refreshtoken", "idtoken", "credentials", "credentialref", "connectionstring", "jwtsecret":
		return true
	}
	return false
}
