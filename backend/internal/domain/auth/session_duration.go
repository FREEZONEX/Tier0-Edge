package auth

import (
	"time"
)

// 登录有效期设置存储于 sys_setting(login_session_duration),仅对新签发的登录会话生效。
const (
	SystemSettingKeyLoginSessionDuration = "login_session_duration"
	DefaultLoginSessionDuration          = SessionDuration12h

	SessionDuration12h       = "12h"
	SessionDuration1d        = "1d"
	SessionDuration7d        = "7d"
	SessionDuration30d       = "30d"
	SessionDurationPermanent = "permanent"

	// "永久"以极长有效期实现;账号禁用仍通过每请求的用户状态重查即时失效。
	loginSessionPermanentTTL = 100 * 365 * 24 * time.Hour
)

var loginSessionDurations = map[string]time.Duration{
	SessionDuration12h:       12 * time.Hour,
	SessionDuration1d:        24 * time.Hour,
	SessionDuration7d:        7 * 24 * time.Hour,
	SessionDuration30d:       30 * 24 * time.Hour,
	SessionDurationPermanent: loginSessionPermanentTTL,
}

// IsLoginSessionDuration reports whether value is a supported duration option.
func IsLoginSessionDuration(value string) bool {
	_, ok := loginSessionDurations[value]
	return ok
}

// TTLForLoginSessionDuration maps the stored setting to a token lifetime;
// unknown or blank values fall back to the 12h default.
func TTLForLoginSessionDuration(value string) time.Duration {
	if ttl, ok := loginSessionDurations[value]; ok {
		return ttl
	}
	return loginSessionDurations[DefaultLoginSessionDuration]
}
