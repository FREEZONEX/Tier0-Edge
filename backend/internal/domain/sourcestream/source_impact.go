package sourcestream

import (
	"backend/internal/contextx"
	"backend/internal/repo"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"gorm.io/gorm"
	"strconv"
	"time"
)

type impactScopeKey struct{}
type sourceImpactScope struct {
	verify func(context.Context, *gorm.DB, int64, string) error
}

func (s *Service) impactCandidate(action string, input any) (string, error) {
	if action != "update" {
		return "", nil
	}
	def, raw, err := parseDocument[SourceDefinition](input)
	if err != nil {
		return "", err
	}
	if err = def.Validate(); err != nil {
		return "", err
	}
	return raw, nil
}
func (s *Service) impactProof(ctx context.Context, r *repo.SourceStreamRepo, id, revision int64, action, candidate string) (string, []map[string]any, int64, error) {
	current, err := r.Source(ctx, id)
	if err != nil {
		return "", nil, 0, err
	}
	if current.Revision != revision {
		return "", nil, 0, repo.ErrConflict
	}
	refs, err := r.SourceImpactReferences(ctx, id)
	if err != nil {
		return "", nil, 0, err
	}
	s.impactOnce.Do(func() { s.impactKey = make([]byte, 32); _, s.impactErr = rand.Read(s.impactKey) })
	if s.impactErr != nil {
		return "", nil, 0, ErrUnavailable
	}
	bucket := time.Now().Unix() / 300
	payload, _ := json.Marshal([]any{id, revision, action, candidate, refs, bucket})
	hash := hmac.New(sha256.New, s.impactKey)
	_, _ = hash.Write(payload)
	return "si1_" + base64.RawURLEncoding.EncodeToString(hash.Sum(nil)), refs, (bucket + 1) * 300000, nil
}
func (s *Service) BindSourceImpact(ctx context.Context, id, revision int64, action string, input any) (context.Context, error) {
	candidate, err := s.impactCandidate(action, input)
	if err != nil {
		return ctx, err
	}
	scope := sourceImpactScope{verify: func(ctx context.Context, tx *gorm.DB, currentID int64, confirmed string) error {
		if currentID != id {
			return repo.ErrConflict
		}
		token, refs, _, err := s.impactProof(ctx, repo.NewSourceStreamRepo(tx), id, revision, action, candidate)
		if err != nil {
			return err
		}
		if (len(refs) > 0 || confirmed != "") && !hmac.Equal([]byte(token), []byte(confirmed)) {
			return repo.ErrConflict
		}
		return nil
	}}
	return context.WithValue(ctx, impactScopeKey{}, scope), nil
}
func (s *Service) InspectSourceImpact(ctx context.Context, id, revision int64, action string, input any, offset, size int, previous string) (map[string]any, error) {
	if action != "update" && action != "delete" && action != "credentials" && action != "enable" && action != "disable" {
		return nil, invalid("action", "unsupportedCapability")
	}
	if offset < 0 || size < 0 || size > 200 {
		return nil, repo.ErrInvalidArgument
	}
	if size == 0 {
		size = 100
	}
	candidate, err := s.impactCandidate(action, input)
	if err != nil {
		return nil, err
	}
	token, refs, expiry, err := s.impactProof(ctx, repo.NewSourceStreamRepo(ctx), id, revision, action, candidate)
	if err != nil {
		return nil, err
	}
	if (offset > 0 && previous == "") || previous != "" && !hmac.Equal([]byte(token), []byte(previous)) {
		return nil, repo.ErrConflict
	}
	subject, _ := contextx.SubjectFrom(ctx)
	visible := []map[string]any{}
	restricted := 0
	counts := map[string]int{"draft": 0, "activeRelease": 0, "historicalRelease": 0}
	for _, ref := range refs {
		key := map[string]string{"draft": "draft", "active_release": "activeRelease", "historical_release": "historicalRelease"}[ref["kind"].(string)]
		counts[key]++
		if contextx.HasResource(subject, "uns.stream") {
			visible = append(visible, ref)
		} else {
			restricted++
		}
	}
	if offset > len(visible) {
		return nil, repo.ErrInvalidArgument
	}
	end := min(offset+size, len(visible))
	next := 0
	if end < len(visible) {
		next = end
	}
	return map[string]any{"impactRevision": token, "sourceID": strconv.FormatInt(id, 10), "sourceRevision": revision, "action": action, "complete": 1, "counts": counts, "restrictedCount": restricted, "references": visible[offset:end], "nextOffset": next, "expiresAt": expiry}, nil
}
