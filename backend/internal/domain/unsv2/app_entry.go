package unsv2

import (
	"encoding/json"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var appEnvironmentName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

func validateAppEntry(entry *AppEntry) error {
	if entry == nil {
		return nil
	}
	if entry.Environment != "" && !appEnvironmentName.MatchString(entry.Environment) {
		return invalid("entry.environment", "expected a deployment environment name")
	}
	for field, value := range map[string]string{"basePath": entry.BasePath, "path": entry.Path} {
		if value == "" && field == "basePath" {
			continue
		}
		decoded, err := url.PathUnescape(value)
		for _, char := range decoded {
			if char <= ' ' || char == 127 {
				return invalid("entry."+field, "whitespace and control characters are not allowed")
			}
		}
		if err != nil || len(value) > 2048 || !strings.HasPrefix(value, "/") || strings.ContainsAny(decoded, "\\?#\r\n\t") || strings.Contains(decoded, "%") || strings.Contains(decoded, "//") {
			return invalid("entry."+field, "expected an absolute path without host, query, fragment or encoded escapes")
		}
		for _, segment := range strings.Split(decoded, "/") {
			if segment == "." || segment == ".." {
				return invalid("entry."+field, "relative path segments are not allowed")
			}
		}
	}
	return nil
}

// The computed launch URL is response metadata, never persisted into AppData.
// Empty environment uses the current browser origin. Named environments resolve
// against a deployment-owned JSON map, not arbitrary process environment keys.
func appLaunch(raw any) map[string]string {
	result := map[string]string{"status": "unconfigured"}
	if raw == nil {
		return result
	}
	bytes, err := json.Marshal(raw)
	var entry AppEntry
	if err != nil || json.Unmarshal(bytes, &entry) != nil || validateAppEntry(&entry) != nil {
		result["status"] = "invalid"
		return result
	}
	base := ""
	if entry.Environment != "" {
		var environments map[string]string
		if json.Unmarshal([]byte(os.Getenv("UNS_APP_BASE_URLS")), &environments) != nil || environments[entry.Environment] == "" {
			result["status"] = "environmentMissing"
			return result
		}
		parsed, err := url.Parse(environments[entry.Environment])
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			result["status"] = "environmentInvalid"
			return result
		}
		base = strings.TrimRight(parsed.String(), "/")
	}
	result["status"] = "ready"
	result["url"] = base + strings.TrimRight(entry.BasePath, "/") + entry.Path
	return result
}
