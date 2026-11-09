package unsv2

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
)

const pipelineCursorMaxBytes = 32 << 10

type pipelineSeekTerm struct {
	sql, direction, cast string
	nullable             bool
}
type pipelineCursor struct {
	Version int       `json:"v"`
	Binding string    `json:"b"`
	Expires int64     `json:"e"`
	Values  []*string `json:"k"`
}

// SetQueryCursorSecret uses a separate HMAC key domain from authentication.
// Rotating the deployment secret intentionally invalidates existing cursors.
func (s *Service) SetQueryCursorSecret(secret string) {
	if secret == "" {
		return
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte("tier0/uns-v2/query-cursor/v2"))
	s.queryCursorKey = h.Sum(nil)
}

func pipelineFingerprint(kind string, q DataQuery) string {
	q.QueryID = ""
	q.Page = nil
	q.ResultMode = ""
	b, _ := json.Marshal(struct {
		Kind       string
		Query      DataQuery
		Projection string
		Scope      *sampleQueryScope
	}{kind, q, q.projection, q.sampleScope})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func pipelinePageMode(q DataQuery, p pipelinePlan) (string, error) {
	mode := "offset"
	if p.direct {
		mode = "cursor"
	}
	if q.Page != nil {
		if q.Page.Mode != "" {
			mode = q.Page.Mode
		}
		if q.Page.Mode == "" && q.Page.Page > 0 {
			mode = "offset"
		}
		if q.Page.Cursor != "" {
			mode = "cursor"
		}
	}
	if mode == "cursor" && !p.direct {
		return "", invalid("page", "cursor requires a pipeline containing only $match and $sort; use offset pagination for transformed or aggregate results")
	}
	return mode, nil
}

func pipelineCursorBinding(ctx context.Context, kind string, q DataQuery, p pipelinePlan) (string, error) {
	var identity struct {
		Database string
		Revision int64
	}
	if err := repo.GetUnsConn(ctx).Raw("SELECT current_database() AS database, revision FROM public.uns_meta WHERE kind='collection' AND id=? AND deleted_at=0", q.CollectionID).Scan(&identity).Error; err != nil {
		return "", err
	}
	subject, _ := contextx.SubjectFrom(ctx)
	b, err := json.Marshal(struct {
		Query                    string
		Database                 string
		Revision                 int64
		Schema                   any
		UserID, APIKeyID         int64
		AppID, GrantID, AuthType string
		Restricted               bool
	}{pipelineFingerprint(kind, q), identity.Database, identity.Revision, p.schema, subject.UserID, subject.APIKeyID, subject.APIKeyAppID, subject.MCPGrantID, subject.AuthType, subject.MCPRestricted})
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

func (s *Service) readPipelineCursor(token, binding string, terms []pipelineSeekTerm) ([]*string, error) {
	bad := func() ([]*string, error) {
		return nil, invalid("page.cursor", "cursor is invalid, expired, or belongs to another query, schema or identity")
	}
	if len(token) > pipelineCursorMaxBytes {
		return bad()
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return bad()
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || base64.RawURLEncoding.EncodeToString(raw) != parts[0] {
		return bad()
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || base64.RawURLEncoding.EncodeToString(sig) != parts[1] {
		return bad()
	}
	h := hmac.New(sha256.New, s.queryCursorKey)
	h.Write(raw)
	if len(s.queryCursorKey) == 0 || !hmac.Equal(h.Sum(nil), sig) {
		return bad()
	}
	var cur pipelineCursor
	if Decode(raw, &cur) != nil || cur.Version != 2 || cur.Binding != binding || cur.Expires <= time.Now().Unix() || len(cur.Values) != len(terms) {
		return bad()
	}
	// Validate types before passing authenticated values back to PostgreSQL.
	for i, v := range cur.Values {
		if v == nil {
			if !terms[i].nullable {
				return bad()
			}
			continue
		}
		switch terms[i].cast {
		case "timestamptz":
			if _, err := time.Parse(time.RFC3339Nano, *v); err != nil {
				return bad()
			}
		case "boolean":
			if *v != "true" && *v != "false" {
				return bad()
			}
		case "numeric":
			if _, ok := new(big.Rat).SetString(*v); !ok {
				return bad()
			}
		}
	}
	return cur.Values, nil
}

func (s *Service) nextPipelineCursor(binding string, values []*string) (string, error) {
	if len(s.queryCursorKey) == 0 {
		return "", problem("INTERNAL_ERROR", "query cursor signing is not configured")
	}
	raw, err := json.Marshal(pipelineCursor{Version: 2, Binding: binding, Expires: time.Now().Add(7 * 24 * time.Hour).Unix(), Values: values})
	if err != nil {
		return "", err
	}
	h := hmac.New(sha256.New, s.queryCursorKey)
	h.Write(raw)
	token := base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	if len(token) > pipelineCursorMaxBytes {
		return "", invalid("page.cursor", "sort values exceed the 32 KiB cursor budget; use shorter sort fields or explicit offset pagination")
	}
	return token, nil
}

func pipelineSeekColumns(terms []pipelineSeekTerm) string {
	values := make([]string, 0, len(terms))
	for _, t := range terms {
		v := "(" + t.sql + ")::text"
		if t.cast == "timestamptz" {
			// PostgreSQL's own microsecond timestamp, never public millisecond JSON.
			v = "to_char(" + t.sql + " AT TIME ZONE 'UTC','YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"')"
		}
		values = append(values, v)
	}
	return "jsonb_build_array(" + strings.Join(values, ",") + ")"
}

func pipelineSeekAfter(terms []pipelineSeekTerm, values []*string) (string, []any) {
	branches := []string{}
	args := []any{}
	for i, t := range terms {
		if values[i] == nil {
			continue
		} // NULLS LAST has no later value at this term.
		parts := []string{}
		for j := 0; j < i; j++ {
			if values[j] == nil {
				parts = append(parts, terms[j].sql+" IS NULL")
			} else {
				parts = append(parts, terms[j].sql+" IS NOT DISTINCT FROM ?::"+terms[j].cast)
				args = append(args, *values[j])
			}
		}
		op := ">"
		if t.direction == "desc" {
			op = "<"
		}
		comparison := t.sql + " " + op + " ?::" + t.cast
		if t.nullable {
			comparison = "(" + comparison + " OR " + t.sql + " IS NULL)"
		}
		parts = append(parts, comparison)
		args = append(args, *values[i])
		branches = append(branches, "("+strings.Join(parts, " AND ")+")")
	}
	if len(branches) == 0 {
		return "FALSE", nil
	}
	clause := "(" + strings.Join(branches, " OR ") + ")"
	// Expose a leading range to the planner for composite native indexes, while
	// the lexicographic predicate resolves ties and mixed subsequent directions.
	if len(terms) > 1 && values[0] != nil && !terms[0].nullable {
		op := ">="
		if terms[0].direction == "desc" {
			op = "<="
		}
		clause = "(" + terms[0].sql + " " + op + " ?::" + terms[0].cast + " AND " + clause + ")"
		args = append([]any{*values[0]}, args...)
	}
	return clause, args
}

func pipelineOffsetNext(offset, size int) int { return offset/size + 2 }
