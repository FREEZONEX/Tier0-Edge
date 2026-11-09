package sourcestream

import (
	"context"
	"encoding/json"
	"testing"

	"backend/internal/repo"

	"github.com/stretchr/testify/require"
)

func TestSingleModelAdmissionRejectsSelectorsBeforeExecution(t *testing.T) {
	s := New(nil, nil, "")
	for _, field := range []string{"sdkVersion", "configVersion", "supportedVersions", "supportedSDKVersions", "compatEngine", "release", "unknown"} {
		doc := map[string]any{"name": "Current", "script": "module.exports=api=>{};", "timeoutMs": 1000, field: "tier0-stream/v2"}
		_, err := s.SaveStream(context.Background(), 0, 0, doc)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrUnavailable)
		_, _, err = s.Validate(context.Background(), doc, 0, false)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrUnavailable)
	}
	for _, raw := range []string{
		`{"name":"Old","model":"script","script":{"code":"return input;","timeoutMs":1000}}`,
		`{"name":"Old","model":"publish","sourceId":1,"script":{"code":"return input;","timeoutMs":1000}}`,
	} {
		var def StreamDefinition
		require.Error(t, json.Unmarshal([]byte(raw), &def))
	}
}

func TestSingleModelDocumentStrictDecodeBeforeCustomUnmarshal(t *testing.T) {
	for _, field := range []string{"supportedSDKVersions", "compatEngine", "release", "versionId", "createdAt", "activatedAt"} {
		for _, value := range []any{nil, 7, "old-engine", []string{"old"}} {
			doc := map[string]any{"name": "Current", "script": "module.exports=api=>{};", "timeoutMs": 1000, field: value}
			raw, err := json.Marshal(doc)
			require.NoError(t, err)
			var definition StreamDefinition
			require.Error(t, json.Unmarshal(raw, &definition), field)
			_, _, err = parseDocument[StreamDefinition](doc)
			require.Error(t, err, "outer strict decoding must not be bypassed by UnmarshalJSON: %s", field)
		}
	}
	_, _, err := parseDocument[StreamDefinition](map[string]any{
		"name": "Current", "script": "module.exports=api=>{};", "timeoutMs": 1000,
		"extensions": map[string]any{"business": map[string]any{"release": 7}},
	})
	require.NoError(t, err, "explicit opaque extensions remain part of the current document")
}

func TestSingleModelHistoricalReleaseRemainsReadOnly(t *testing.T) {
	version := repo.StreamVersion{
		ID: 11, StreamID: 7,
		Document:        `{"sdkVersion":"tier0-stream/v2","name":"Historical","script":"original bytes"}`,
		ProgramManifest: `{"sdkVersion":"tier0-stream/v2","sources":[],"topics":[],"entries":[],"_unsBindings":[{"private":true}]}`,
	}
	before := version
	view, err := programVersionView(version)
	require.NoError(t, err)
	manifest, err := json.Marshal(view["manifest"])
	require.NoError(t, err)
	require.NotContains(t, string(manifest), "_unsBindings")
	require.Contains(t, string(manifest), "sdkVersion")
	require.Equal(t, before, version)
	_, err = version.ProgramV2Manifest()
	require.Error(t, err, "history projection must not make the release executable")
}

func TestSingleModelSourceRetainsProtocolAndRejectsSelectors(t *testing.T) {
	base := map[string]any{"name": "MQTT", "protocol": "mqtt", "endpoint": "mqtt://broker:1883", "runtimeLocation": "local", "mqtt": map[string]any{"clientId": "test"}}
	s := New(nil, nil, "")
	require.NoError(t, s.ValidateSource(base))
	for _, field := range []string{"sdkVersion", "configVersion", "supportedVersions", "unknown"} {
		base[field] = nil
		require.Error(t, s.ValidateSource(base))
		delete(base, field)
	}
}

// Historical manifests remain inspectable, while strict execution admission
// still rejects version selectors and malformed retained state.
func TestSingleModelHistoricalStreamRemainsVisible(t *testing.T) {
	s := New(nil, nil, "")
	for _, manifest := range []string{
		`{"sdkVersion":"tier0-stream/v2","sources":[],"topics":[],"entries":[{"id":"read","kind":"request"}]}`,
		`{"sources":[],"topics":[],"entries":[]}`,
		`{invalid`,
	} {
		item := repo.Stream{ID: 7, Name: "Historical", Model: "program", Status: "enabled", Revision: 3, Document: `{"name":"Historical","script":"original bytes"}`, DraftManifest: manifest}
		before := item
		view, err := s.executionView(context.Background(), item)
		require.NoError(t, err)
		require.Equal(t, "Historical", view["name"])
		require.Equal(t, "needs-redeploy", view["executionState"])
		require.Equal(t, "needs-redeploy", view["deploymentState"])
		require.Nil(t, view["manifest"])
		require.Equal(t, before, item)
		_, err = item.ProgramV2DraftManifest()
		require.Error(t, err, "display must not relax execution admission")
	}
	item := repo.Stream{ID: 8, Model: "program", Status: "disabled", DraftManifest: `{"sources":[],"topics":[],"entries":[{"id":"read","kind":"request"}]}`}
	view, err := s.executionView(context.Background(), item)
	require.NoError(t, err)
	require.Equal(t, "disabled", view["executionState"])
}
