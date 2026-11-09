package main

import (
	"encoding/json"
	"testing"

	"backend/internal/domain/sourcestream"

	"github.com/stretchr/testify/require"
)

func TestOfflineDraftCandidatePreservesScriptAndOriginal(t *testing.T) {
	raw := []byte(`{"sdkVersion":"tier0-stream/v2","name":"设备读取","script":"module.exports=api=>{api.stream.onRequest({id:'read'},()=>123);};","timeoutMs":1000,"extensions":{"revision":7}}`)
	original := string(raw)
	candidate, err := convertDraft("stream", raw)
	require.NoError(t, err)
	require.Equal(t, original, string(raw))
	var before, after map[string]any
	require.NoError(t, json.Unmarshal(raw, &before))
	require.NoError(t, json.Unmarshal(candidate, &after))
	delete(before, "sdkVersion")
	require.Equal(t, before, after)
	var current sourcestream.StreamDefinition
	require.NoError(t, json.Unmarshal(candidate, &current))
	require.NoError(t, current.ValidateDraft())
	second, err := convertDraft("stream", candidate)
	require.NoError(t, err)
	require.Equal(t, candidate, second)
}

func TestOfflineDraftCandidateRefusesUnknownModelsAndReleaseEnvelopes(t *testing.T) {
	for _, raw := range []string{
		`{"sdkVersion":"unknown","name":"p","script":"x"}`,
		`{"sdkVersion":null,"name":"p","script":"x"}`,
		`{"configVersion":1,"name":"p","script":"x"}`,
		`{"supportedVersions":[],"name":"p","script":"x"}`,
		`{"supportedSDKVersions":["old"],"name":"p","script":"x"}`,
		`{"compatEngine":"old","name":"p","script":"x"}`,
		`{"release":7,"name":"p","script":"x"}`,
		`{"versionId":7,"name":"p","script":"x"}`,
		`{"createdAt":7,"name":"p","script":"x"}`,
		`{"activatedAt":7,"name":"p","script":"x"}`,
		`{"sdkVersion":"tier0-stream/v2","release":7,"name":"p","script":"x"}`,
		`{"name":"p","model":"publish","sourceId":1,"script":{"code":"return input;"}}`,
		`{"release":1,"document":{"name":"p","script":"x"}}`,
		`null`,
	} {
		_, err := convertDraft("stream", []byte(raw))
		require.Error(t, err, raw)
	}
	_, err := convertDraft("release", []byte(`{"name":"p","script":"x"}`))
	require.Error(t, err)
}
