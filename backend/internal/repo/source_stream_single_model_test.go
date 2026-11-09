package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"backend/internal/contextx"
	"backend/internal/operationcontract"
	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

func TestProgramRequestPolicyRejectsMalformedTargets(t *testing.T) {
	manifest := streamexec.ProgramManifest{Entries: []streamexec.ProgramEntry{{ID: "run", Kind: "request", Request: &operationcontract.Resource{Code: "target.run", SideEffect: "read"}}}}
	contract := ProgramSourceContract{SourceID: 1, ConfigRevision: 1, Protocol: "opcua", ConfigDigest: strings.Repeat("a", 64), Resources: []ProgramResourceContract{{Ref: "opcua:value", Digest: strings.Repeat("b", 64)}}}
	target := ProgramRequestTarget{Kind: "source", Action: "read", Effect: "read", SourceID: 1, ConfigRevision: 1, ResourceRef: "opcua:value", ConfigDigest: contract.ConfigDigest, ResourceDigest: contract.Resources[0].Digest}
	var err error
	target.Digest, err = programRequestTargetDigest(target)
	require.NoError(t, err)
	policy := ProgramRequestPolicy{EntryID: "run", Code: "target.run", Effect: "read", Class: "device", Targets: []ProgramRequestTarget{target}}
	encode := func(item ProgramRequestPolicy) []byte {
		raw, err := json.Marshal([]ProgramRequestPolicy{item})
		require.NoError(t, err)
		return raw
	}
	_, err = decodeProgramRequestPolicies(encode(policy), manifest, []ProgramSourceContract{contract}, nil, 11)
	require.NoError(t, err)
	for name, mutate := range map[string]func(*ProgramRequestPolicy){
		"entry":                     func(p *ProgramRequestPolicy) { p.EntryID = "other" },
		"code":                      func(p *ProgramRequestPolicy) { p.Code = "other.code" },
		"effect":                    func(p *ProgramRequestPolicy) { p.Effect = "write" },
		"upper bound":               func(p *ProgramRequestPolicy) { p.EffectUpperBound = true },
		"class":                     func(p *ProgramRequestPolicy) { p.Class = "compute" },
		"omitted source capability": func(p *ProgramRequestPolicy) { p.Targets = []ProgramRequestTarget{}; p.Class = "compute" },
		"null targets":              func(p *ProgramRequestPolicy) { p.Targets = nil },
		"duplicate target":          func(p *ProgramRequestPolicy) { p.Targets = append(p.Targets, p.Targets[0]) },
		"unknown failure":           func(p *ProgramRequestPolicy) { p.UnsupportedReason = "ignore" },
		"unsupported with targets":  func(p *ProgramRequestPolicy) { p.UnsupportedReason = "unsupported-resource" },
		"source":                    func(p *ProgramRequestPolicy) { p.Targets[0].SourceID = 2 },
		"source revision":           func(p *ProgramRequestPolicy) { p.Targets[0].ConfigRevision++ },
		"resource":                  func(p *ProgramRequestPolicy) { p.Targets[0].ResourceRef = "opcua:other" },
		"source digest":             func(p *ProgramRequestPolicy) { p.Targets[0].ConfigDigest = strings.Repeat("c", 64) },
		"resource digest":           func(p *ProgramRequestPolicy) { p.Targets[0].ResourceDigest = strings.Repeat("c", 64) },
		"unbound field":             func(p *ProgramRequestPolicy) { p.Targets[0].CollectionID = "other" },
		"write capability":          func(p *ProgramRequestPolicy) { p.Targets[0].Action, p.Targets[0].Effect = "write", "write" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := policy
			changed.Targets = append([]ProgramRequestTarget{}, policy.Targets...)
			mutate(&changed)
			// Recompute hashes to exercise semantic checks, not just corruption detection.
			for i := range changed.Targets {
				changed.Targets[i].Digest, err = programRequestTargetDigest(changed.Targets[i])
				require.NoError(t, err)
			}
			_, err = decodeProgramRequestPolicies(encode(changed), manifest, []ProgramSourceContract{contract}, nil, 11)
			require.ErrorIs(t, err, ErrInvalidArgument)
		})
	}
	for _, raw := range [][]byte{[]byte(`null`), []byte(`[]`), bytes.Replace(encode(policy), []byte(`"entryId"`), []byte(`"EntryId"`), 1), bytes.Replace(encode(policy), []byte(`"class":"device"`), []byte(`"class":"device","unknown":true`), 1)} {
		_, err = decodeProgramRequestPolicies(raw, manifest, []ProgramSourceContract{contract}, nil, 11)
		require.ErrorIs(t, err, ErrInvalidArgument)
	}
	// Persisted Enterprise protocol names differ from Cloud's HTTP labels.
	// Generic HTTP has no discovered native resource, but is still an exact
	// Source-bound write capability, never an empty compute capability.
	manifest.Entries[0].Request.SideEffect = "write"
	policy.Effect = "write"
	policy.Targets[0].Action, policy.Targets[0].Effect = "invoke", "write"
	policy.Targets[0].ResourceRef, policy.Targets[0].ResourceDigest = "http:request", contract.ConfigDigest
	policy.Targets[0].Digest, err = programRequestTargetDigest(policy.Targets[0])
	require.NoError(t, err)
	contract.Resources = []ProgramResourceContract{}
	for _, protocol := range []string{"http-api", "oauth2", "http", "oauth", "opcua"} {
		contract.Protocol = protocol
		_, err = decodeProgramRequestPolicies(encode(policy), manifest, []ProgramSourceContract{contract}, nil, 11)
		if protocol == "http-api" || protocol == "oauth2" {
			require.NoError(t, err, protocol)
		} else {
			require.ErrorIs(t, err, ErrInvalidArgument, protocol)
		}
	}
}

func TestProgramExecutionPlanLegacySnapshot(t *testing.T) {
	// Literal v1 fixture: its digest must survive v2 rollout without rebuilding
	// missing Source facts from today's catalog or the current caller.
	version := StreamVersion{ID: 11, StreamID: 7, Document: `{"script":"x"}`, SourceManifest: "[]",
		ProgramManifest: `{"sources":[],"topics":[],"entries":[{"id":"run","kind":"request"}],"_executionPlan":{"contract":"enterprise-stream-execution/v1","streamId":"7","versionId":"11","authority":{"kind":"user","id":"1"},"digest":"d5e2aeae9e6b4e5206b1c61b1856335391f668d9d3d4e059900f47bc4ef1bb05"}}`}
	plan, err := version.ProgramExecutionPlan()
	require.NoError(t, err)
	require.Equal(t, programExecutionPlanLegacyContract, plan.Contract)
	require.Nil(t, plan.Sources)
	require.NoError(t, version.ValidateProgramExecutionSource(Source{ID: 123}))
	require.NoError(t, version.validateExecutionSources(nil))
	_, _, err = version.ProgramRequestPolicy("run")
	require.ErrorIs(t, err, ErrNotFound, "legacy publications cannot acquire targets from current state")
	version.Document = `{"script":"changed"}`
	_, err = version.ProgramExecutionPlan()
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestProgramExecutionSourceContractIdentity(t *testing.T) {
	source := Source{ID: 23, ConfigRevision: 1, Protocol: "modbus-tcp",
		Document:  `{"name":"Device","description":"Display","protocol":"modbus-tcp","endpoint":"tcp://localhost:502"}`,
		Resources: `[{"id":"modbus:value","name":"Value","models":["publish","subscribe"],"schema":{"maximum":9007199254740993},"metadata":{"unitId":7,"address":0,"quantity":2,"writable":true,"metadataVerifiedAt":1000}}]`}
	base, err := programSourceContract(source, []string{"modbus:value"})
	require.NoError(t, err)
	plan := ProgramExecutionPlan{Contract: programExecutionPlanContract, Sources: []ProgramSourceContract{base}}
	for _, tc := range []struct{ name, from, to string }{
		{"label", `"Value"`, `"Renamed"`},
		{"discovery time", `"metadataVerifiedAt":1000`, `"metadataVerifiedAt":2000`},
		{"model order", `["publish","subscribe"]`, `["subscribe","publish"]`},
		{"number representation", `9007199254740993`, `9007199254740993.0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := source
			changed.Resources = strings.Replace(source.Resources, tc.from, tc.to, 1)
			require.NoError(t, plan.validateSource(changed))
		})
	}
	renamed := source
	renamed.Document = strings.ReplaceAll(source.Document, `"Device"`, `"Renamed"`)
	renamed.Document = strings.ReplaceAll(renamed.Document, `"Display"`, `"Changed description"`)
	require.NoError(t, plan.validateSource(renamed))
	for _, tc := range []struct{ name, from, to string }{
		{"address", `"address":0`, `"address":1`},
		{"unit", `"unitId":7`, `"unitId":8`},
		{"writable", `"writable":true`, `"writable":false`},
		{"models", `["publish","subscribe"]`, `["publish"]`},
		{"exact schema bound", `9007199254740993`, `9007199254740992`},
		{"missing resource", `"modbus:value"`, `"modbus:other"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := source
			changed.Resources = strings.Replace(source.Resources, tc.from, tc.to, 1)
			require.ErrorIs(t, plan.validateSource(changed), ErrConflict)
		})
	}
	changed := source
	changed.Document = strings.Replace(source.Document, "localhost:502", "localhost:503", 1)
	require.ErrorIs(t, plan.validateSource(changed), ErrConflict)
	changed = source
	changed.ConfigRevision++
	require.ErrorIs(t, plan.validateSource(changed), ErrConflict)
	changed = source
	changed.DeletedAt = 1
	require.ErrorIs(t, plan.validateSource(changed), ErrConflict)
	for _, catalog := range []string{`null`, `[]`, `[{"id":"modbus:value","models":[]}]`, `[{"id":"modbus:value","models":["publish"],"metadata":[]}]`, source.Resources[:len(source.Resources)-1] + "," + source.Resources[1:]} {
		changed = source
		changed.Resources = catalog
		require.ErrorIs(t, plan.validateSource(changed), ErrConflict)
	}
	// Adding only a discovery timestamp to a resource without metadata is also
	// display-only; absent, null and empty metadata have no physical facts.
	var empty ProgramSourceContract
	for i, metadata := range []string{"", `,"metadata":null`, `,"metadata":{}`, `,"metadata":{"metadataVerifiedAt":1}`} {
		changed = source
		changed.Resources = `[{"id":"modbus:value","models":["publish"]` + metadata + `}]`
		actual, err := programSourceContract(changed, []string{"modbus:value"})
		require.NoError(t, err)
		if i == 0 {
			empty = actual
		} else {
			require.Equal(t, empty, actual)
		}
	}
}

func TestProgramExecutionPlanRejectsMalformedProvenance(t *testing.T) {
	version := StreamVersion{ID: 11, StreamID: 7, Document: `{"script":"x"}`, SourceManifest: "[]",
		ProgramManifest: `{"sources":[],"topics":[],"entries":[{"id":"run","kind":"request"}]}`}
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, AuthType: "session"})
	require.NoError(t, freezeProgramExecutionPlan(ctx, &version, nil))
	for _, raw := range []string{
		`null`,
		`{}`,
		`{"contract":"unknown","streamId":"7","versionId":"11","authority":{"kind":"user","id":"1"},"digest":"x"}`,
	} {
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(version.ProgramManifest), &fields))
		fields[programExecutionPlanField] = json.RawMessage(raw)
		encoded, err := json.Marshal(fields)
		require.NoError(t, err)
		changed := version
		changed.ProgramManifest = string(encoded)
		_, err = changed.ProgramV2Manifest()
		require.ErrorIs(t, err, ErrInvalidArgument)
	}
	for _, change := range []func(map[string]any){
		func(plan map[string]any) { plan["Contract"] = plan["contract"]; delete(plan, "contract") },
		func(plan map[string]any) { plan["unknown"] = true },
		func(plan map[string]any) { plan["authority"].(map[string]any)["id"] = "0" },
		func(plan map[string]any) { plan["authority"].(map[string]any)["kind"] = "app" },
	} {
		var fields map[string]any
		require.NoError(t, json.Unmarshal([]byte(version.ProgramManifest), &fields))
		change(fields[programExecutionPlanField].(map[string]any))
		encoded, err := json.Marshal(fields)
		require.NoError(t, err)
		changed := version
		changed.ProgramManifest = string(encoded)
		_, err = changed.ProgramV2Manifest()
		require.ErrorIs(t, err, ErrInvalidArgument)
	}
}

func TestProgramExecutionDigestPreservesJSONBNumberIdentity(t *testing.T) {
	for _, forms := range [][]string{
		{`1000`, `1e3`, `1.000E+003`, `1000.0`},
		{`0`, `-0.00`, `0e-10`},
		{`-0.0123`, `-123e-4`, `-1.2300e-2`},
		{`9007199254740993`, `9007199254740993.0`, `9.007199254740993e15`},
	} {
		first, err := canonicalProgramJSON([]byte(forms[0]))
		require.NoError(t, err)
		for _, form := range forms[1:] {
			actual, err := canonicalProgramJSON([]byte(form))
			require.NoError(t, err)
			require.Equal(t, first, actual, form)
		}
	}
	a, err := canonicalProgramJSON([]byte(`{"z":[1e3,null,true,"1000"],"n":9007199254740993}`))
	require.NoError(t, err)
	b, err := canonicalProgramJSON([]byte(` { "n":9007199254740993.0, "z":[1000,null,true,"1000"] } `))
	require.NoError(t, err)
	require.Equal(t, a, b)
	c, err := canonicalProgramJSON(bytes.ReplaceAll(b, []byte("9007199254740993"), []byte("9007199254740992")))
	require.NoError(t, err)
	require.NotEqual(t, a, c, "adjacent integers above float64 precision must not share a digest")
	for _, raw := range []string{`1e999999999999999999`, `1 2`, `null false`, `{"a":`} {
		_, err := canonicalProgramJSON([]byte(raw))
		require.ErrorIs(t, err, ErrInvalidArgument)
	}
}

func TestSingleModelFrozenManifestStrictDecodePreservesHistory(t *testing.T) {
	current := `{"sources":[],"topics":[],"entries":[{"id":"read","kind":"request"}],"_unsBindings":[]}`
	version := StreamVersion{ProgramManifest: current}
	manifest, err := version.ProgramV2Manifest()
	require.NoError(t, err)
	require.Equal(t, "read", manifest.Entries[0].ID)
	require.Equal(t, current, version.ProgramManifest)
	for _, raw := range []string{
		`{"sdkVersion":"tier0-stream/v2","sources":[],"topics":[],"entries":[{"id":"read","kind":"request"}]}`,
		`{"configVersion":1,"sources":[],"topics":[],"entries":[{"id":"read","kind":"request"}]}`,
		`{"supportedVersions":[],"sources":[],"topics":[],"entries":[{"id":"read","kind":"request"}]}`,
		`{"other":true,"sources":[],"topics":[],"entries":[{"id":"read","kind":"request"}]}`,
	} {
		version := StreamVersion{ProgramManifest: raw}
		_, err := version.ProgramV2Manifest()
		require.ErrorIs(t, err, ErrInvalidArgument)
		require.Equal(t, raw, version.ProgramManifest)
	}
}

func TestSingleModelRepoDocumentRejectsSelectorsAndTransformBodies(t *testing.T) {
	require.True(t, validProgramV2Document(`{"name":"Current","script":"module.exports=api=>{};","timeoutMs":1000}`))
	for _, raw := range []string{
		`{"sdkVersion":"tier0-stream/v2","script":"x"}`,
		`{"configVersion":null,"script":"x"}`,
		`{"supportedVersions":[],"script":"x"}`,
		`{"supportedSDKVersions":["old"],"script":"x"}`,
		`{"compatEngine":"old","script":"x"}`,
		`{"release":7,"script":"x"}`,
		`{"unknown":true,"script":"x"}`,
		`{"model":"program","script":"x"}`,
		`{"script":"x"} {"script":"y"}`,
		`{"script":{"code":"return input;","timeoutMs":1000}}`,
	} {
		require.False(t, validProgramV2Document(raw), raw)
	}
}
