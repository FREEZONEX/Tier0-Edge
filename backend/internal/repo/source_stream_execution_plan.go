package repo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"backend/internal/contextx"
	"backend/internal/streamexec"
)

const programExecutionPlanContract = "enterprise-stream-execution/v2"
const programExecutionPlanLegacyContract = "enterprise-stream-execution/v1"
const programExecutionPlanField = "_executionPlan"

// ProgramAuthorityRef stores a refreshable identity, never its permission set.
// An API Key is its own principal; its owner must not substitute for that Key.
type ProgramAuthorityRef struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id,string"`
}

func (a ProgramAuthorityRef) valid() bool {
	return a.ID > 0 && (a.Kind == "user" || a.Kind == "api_key")
}

// ProgramExecutionPlan is private immutable publication provenance. Its digest
// binds the complete release, including Source revisions and UNS bindings.
// It is not a grant and does not prove the principal still has permission.
type ProgramExecutionPlan struct {
	Contract  string                  `json:"contract"`
	StreamID  int64                   `json:"streamId,string"`
	VersionID int64                   `json:"versionId,string"`
	Authority ProgramAuthorityRef     `json:"authority"`
	Sources   []ProgramSourceContract `json:"sources,omitempty"`
	Digest    string                  `json:"digest"`
}

func freezeProgramExecutionPlan(ctx context.Context, version *StreamVersion, sources []Source, builders ...ProgramRequestPolicyBuilder) error {
	if len(builders) > 1 {
		return ErrInvalidArgument
	}
	subject, ok := contextx.SubjectFrom(ctx)
	// These callers have no reusable local publication authority. Keep their
	// own execution boundaries; App delegation must require a stored plan.
	if !ok || subject.FleetTask != nil || subject.MCPRestricted || subject.APIKeyAppID != "" || subject.APIKeyUsageType == APIKeyUsageAppData {
		return nil
	}
	var authority ProgramAuthorityRef
	switch subject.AuthType {
	case "session":
		if subject.UserID <= 0 || subject.APIKeyID != 0 {
			return ErrInvalidArgument
		}
		authority = ProgramAuthorityRef{Kind: "user", ID: subject.UserID}
	case "apiKey":
		if subject.UserID <= 0 || subject.APIKeyID <= 0 {
			return ErrInvalidArgument
		}
		authority = ProgramAuthorityRef{Kind: "api_key", ID: subject.APIKeyID}
	default:
		// Legacy repository callers cannot manufacture a session from a user ID.
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(version.ProgramManifest), &fields) != nil || fields == nil || fields[programExecutionPlanField] != nil {
		return ErrInvalidArgument
	}
	contracts, err := freezeProgramSourceContracts(*version, sources)
	if err != nil {
		return err
	}
	if len(builders) == 1 && builders[0] != nil {
		fields[programRequestPoliciesField], err = freezeProgramRequestPolicies(*version, sources, contracts, builders[0])
		if err != nil {
			return err
		}
	}
	plan := ProgramExecutionPlan{Contract: programExecutionPlanContract, StreamID: version.StreamID, VersionID: version.ID, Authority: authority, Sources: contracts}
	plan.Digest, err = programExecutionDigest(*version, fields, plan)
	if err != nil {
		return err
	}
	fields[programExecutionPlanField], err = json.Marshal(plan)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(fields)
	if err != nil || len(raw) > streamexec.MaxJSONBytes {
		return ErrInvalidArgument
	}
	version.ProgramManifest = string(raw)
	return nil
}

func validateProgramExecutionPlan(version StreamVersion, fields map[string]json.RawMessage) (ProgramExecutionPlan, error) {
	var plan ProgramExecutionPlan
	raw, exists := fields[programExecutionPlanField]
	if !exists {
		return plan, ErrNotFound
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&plan) != nil || decoder.Decode(new(any)) != io.EOF || (plan.Contract != programExecutionPlanContract && plan.Contract != programExecutionPlanLegacyContract) ||
		!plan.Authority.valid() || plan.StreamID <= 0 || plan.StreamID != version.StreamID || plan.VersionID <= 0 || plan.VersionID != version.ID {
		return plan, ErrInvalidArgument
	}
	if plan.Contract == programExecutionPlanLegacyContract {
		if plan.Sources != nil {
			return plan, ErrInvalidArgument
		}
	} else if err := validateProgramSourceContracts(version, plan.Sources); err != nil {
		return plan, err
	}
	// Reject case variants, omitted/null fields and extra fields even though the
	// standard struct decoder accepts some of them. Stored JSONB has unique keys.
	encoded, err := json.Marshal(plan)
	if err != nil || !sameProgramDocument(string(raw), string(encoded)) {
		return plan, ErrInvalidArgument
	}
	digest, err := programExecutionDigest(version, fields, plan)
	if err != nil || plan.Digest != digest {
		return plan, ErrInvalidArgument
	}
	return plan, nil
}

// ProgramExecutionPlan never fills missing history from a draft or current actor.
func (v StreamVersion) ProgramExecutionPlan() (ProgramExecutionPlan, error) {
	if _, err := v.ProgramV2Manifest(); err != nil {
		return ProgramExecutionPlan{}, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(v.ProgramManifest), &fields) != nil {
		return ProgramExecutionPlan{}, ErrInvalidArgument
	}
	return validateProgramExecutionPlan(v, fields)
}

func programExecutionDigest(version StreamVersion, fields map[string]json.RawMessage, plan ProgramExecutionPlan) (string, error) {
	if version.ID <= 0 || version.StreamID <= 0 || !plan.Authority.valid() {
		return "", ErrInvalidArgument
	}
	manifest := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		if key != programExecutionPlanField {
			manifest[key] = value
		}
	}
	values := []any{plan.Contract, version.StreamID, version.ID, version.SourceRevision, plan.Authority,
		json.RawMessage(version.Document), json.RawMessage(version.SourceManifest), manifest}
	if plan.Contract != programExecutionPlanLegacyContract {
		values = append(values, plan.Sources)
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "", ErrInvalidArgument
	}
	canonical, err := canonicalProgramJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// PostgreSQL JSONB normalizes whitespace, key order and decimal/exponent forms.
// Keep numbers exact (including schema bounds above 2^53), without expanding an
// exponent into a potentially enormous string or converting through float64.
func canonicalProgramJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrInvalidArgument
	}
	var normalize func(any) (any, error)
	normalize = func(value any) (any, error) {
		switch item := value.(type) {
		case json.Number:
			coefficient := string(item)
			negative := strings.HasPrefix(coefficient, "-")
			coefficient = strings.TrimPrefix(coefficient, "-")
			var power int64
			if at := strings.IndexAny(coefficient, "eE"); at >= 0 {
				var err error
				power, err = strconv.ParseInt(coefficient[at+1:], 10, 32)
				if err != nil {
					return nil, ErrInvalidArgument
				}
				coefficient = coefficient[:at]
			}
			if at := strings.IndexByte(coefficient, '.'); at >= 0 {
				power -= int64(len(coefficient) - at - 1)
				coefficient = coefficient[:at] + coefficient[at+1:]
			}
			coefficient = strings.TrimLeft(coefficient, "0")
			if coefficient == "" {
				return json.Number("0"), nil
			}
			trimmed := strings.TrimRight(coefficient, "0")
			power += int64(len(coefficient) - len(trimmed))
			if negative {
				trimmed = "-" + trimmed
			}
			return json.Number(trimmed + "e" + strconv.FormatInt(power, 10)), nil
		case []any:
			for i := range item {
				var err error
				item[i], err = normalize(item[i])
				if err != nil {
					return nil, err
				}
			}
		case map[string]any:
			for key := range item {
				var err error
				item[key], err = normalize(item[key])
				if err != nil {
					return nil, err
				}
			}
		}
		return value, nil
	}
	value, err := normalize(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
