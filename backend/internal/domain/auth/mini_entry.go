package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"backend/internal/contextx"
	respx "backend/internal/httpx"
	"backend/internal/infra/cache"
	"github.com/golang-jwt/jwt/v5"
)

const miniEntryTTL = 60 * time.Second

var miniTicketPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

type MiniEntryGrant struct {
	Token     string `json:"token"`
	ProjectID int64  `json:"projectId"`
	AppID     int64  `json:"appId"`
}

func miniEntryKey(ticket string) string {
	sum := sha256.Sum256([]byte(ticket))
	return "auth:mini-entry:" + hex.EncodeToString(sum[:])
}

// ValidateMiniEntrySession only accepts the existing session JWT, never API keys
// or delegated OAuth credentials. It preserves the original expiry and rechecks
// the account status at both issuance and redemption. Default-password reminders
// remain optional, matching Enterprise web sign-in.
func (s *Service) ValidateMiniEntrySession(ctx context.Context, token string) (contextx.Subject, time.Time, error) {
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(_ *jwt.Token) (interface{}, error) { return s.jwtSecret, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid {
		return contextx.Subject{}, time.Time{}, respx.NewHTTPError(401, "invalid session")
	}
	expire, err := claims.GetExpirationTime()
	if err != nil || expire == nil {
		return contextx.Subject{}, time.Time{}, respx.NewHTTPError(401, "invalid session")
	}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://localhost/", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	subject, err := s.AuthenticateRequest(request)
	if err != nil || subject.AuthType != "session" {
		return contextx.Subject{}, time.Time{}, respx.NewHTTPError(401, "invalid session")
	}
	_, _, _, _, err = s.CurrentUser(ctx, subject.UserID)
	if err != nil {
		return contextx.Subject{}, time.Time{}, err
	}
	return subject, expire.Time, nil
}

// CreateMiniEntry stores a single-use reference, not a new long-lived session.
// App authorization is checked by the caller with the normal Launchpad evaluator.
func (s *Service) CreateMiniEntry(ctx context.Context, client *cache.Client, grant MiniEntryGrant) (string, error) {
	if grant.ProjectID <= 0 || grant.AppID <= 0 {
		return "", respx.NewHTTPError(400, "invalid app")
	}
	subject, _, err := s.ValidateMiniEntrySession(ctx, grant.Token)
	if err != nil {
		return "", err
	}
	allowed, err := client.SetNX(ctx, "auth:mini-entry-rate:"+strconv.FormatInt(subject.UserID, 10), "1", time.Second)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", respx.NewHTTPError(429, "try again shortly")
	}
	ticket, err := randomURLToken(32)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(grant)
	if err != nil {
		return "", err
	}
	if err = client.SetString(ctx, miniEntryKey(ticket), string(payload), miniEntryTTL); err != nil {
		return "", err
	}
	return ticket, nil
}

func (s *Service) TakeMiniEntry(ctx context.Context, client *cache.Client, ticket string) (MiniEntryGrant, error) {
	var grant MiniEntryGrant
	if !miniTicketPattern.MatchString(ticket) {
		return grant, respx.NewHTTPError(401, "entry expired or already used")
	}
	payload, err := client.TakeString(ctx, miniEntryKey(ticket))
	if err != nil {
		return grant, respx.NewHTTPError(401, "entry expired or already used")
	}
	if json.Unmarshal([]byte(payload), &grant) != nil || grant.ProjectID <= 0 || grant.AppID <= 0 || grant.Token == "" {
		return MiniEntryGrant{}, respx.NewHTTPError(401, "invalid entry")
	}
	return grant, nil
}
