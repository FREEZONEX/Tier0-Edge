package tenantpg

import (
	"fmt"
	"net/url"
	"strings"
)

// ParsePostgresDSN 解析 PostgreSQL DSN 为 url.URL
func ParsePostgresDSN(dsn string) (*url.URL, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return nil, fmt.Errorf("unsupported dsn scheme: %q", u.Scheme)
	}
	return u, nil
}

// BuildProjectConnDSN 用 admin 凭据构造连接到 Project Database 的 DSN
func BuildProjectConnDSN(baseDSN, projectDB string) (string, error) {
	u, err := ParsePostgresDSN(baseDSN)
	if err != nil {
		return "", err
	}
	u.Path = "/" + projectDB
	q := u.Query()
	q.Del("dbname") // Explicit target wins over a base URL's query database.
	u.RawQuery = q.Encode()
	// 清理旧的 search_path options，避免指向其他 schema
	cleanSearchPath(u)
	return u.String(), nil
}

// BuildProjectConnDSNWithCreds 用指定凭据构造连接到 Project Database 的 DSN
func BuildProjectConnDSNWithCreds(baseDSN, projectDB, user, password string) (string, error) {
	u, err := ParsePostgresDSN(baseDSN)
	if err != nil {
		return "", err
	}
	u.Path = "/" + projectDB
	u.User = url.UserPassword(user, password)
	// Drivers can prefer query credentials/service files over URL userinfo.
	// This constructor's explicit project identity must be authoritative.
	q := u.Query()
	for _, key := range []string{"user", "password", "dbname", "service", "servicefile", "passfile"} {
		q.Del(key)
	}
	u.RawQuery = q.Encode()
	cleanSearchPath(u)
	return u.String(), nil
}

// BuildAppDatabaseURL 在 project DSN 上追加 search_path，使其指向指定 app schema
func BuildAppDatabaseURL(projectDSN, schemaName string) string {
	return buildAppDatabaseURLWithSearchPath(projectDSN, schemaName, true)
}

func buildAppDatabaseURLWithSearchPath(projectDSN, schemaName string, includePublic bool) string {
	u, err := ParsePostgresDSN(projectDSN)
	if err != nil {
		// 退化：直接拼字符串
		sep := "?"
		if strings.Contains(projectDSN, "?") {
			sep = "&"
		}
		searchPath := schemaName
		if includePublic {
			searchPath = schemaName + ",public"
		}
		return fmt.Sprintf("%s%soptions=-c%%20search_path%%3D%s", projectDSN, sep, url.QueryEscape(searchPath))
	}
	// 确保有 options 参数
	q := u.Query()
	// 追加 search_path；保留其他 options
	existing := q.Get("options")
	searchPath := schemaName
	if includePublic {
		searchPath = schemaName + ",public"
	}
	searchPathOpt := fmt.Sprintf("-c search_path=%s", searchPath)
	if existing != "" {
		q.Set("options", existing+" "+searchPathOpt)
	} else {
		q.Set("options", searchPathOpt)
	}
	// PostgreSQL libpq 要求 options 参数中的空格使用 %20 而非 form-urlencoded 的 +
	u.RawQuery = strings.ReplaceAll(q.Encode(), "+", "%20")
	return u.String()
}

// cleanSearchPath 移除 DSN 中的 search_path options，避免 project 连接被强制指向其他 schema
func cleanSearchPath(u *url.URL) {
	q := u.Query()
	options := q.Get("options")
	if options == "" {
		return
	}
	parts := strings.Fields(options)
	var filtered []string
	for i := 0; i < len(parts); i++ {
		if parts[i] == "-c" || parts[i] == "--set" {
			if i+1 < len(parts) && strings.HasPrefix(parts[i+1], "search_path=") {
				i++ // 跳过 -c search_path=xxx
				continue
			}
		}
		filtered = append(filtered, parts[i])
	}
	if len(filtered) == 0 {
		q.Del("options")
	} else {
		q.Set("options", strings.Join(filtered, " "))
	}
	// PostgreSQL libpq 要求 options 参数中的空格使用 %20 而非 form-urlencoded 的 +
	u.RawQuery = strings.ReplaceAll(q.Encode(), "+", "%20")
}
