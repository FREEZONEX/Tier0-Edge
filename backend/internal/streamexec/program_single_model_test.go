package streamexec

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSingleModelManifestRejectsUnknownFields(t *testing.T) {
	for _, raw := range []string{
		`{"sdkVersion":"tier0-stream/v2","sources":[],"topics":[],"entries":[]}`,
		`{"configVersion":null,"sources":[],"topics":[],"entries":[]}`,
		`{"supportedVersions":[],"sources":[],"topics":[],"entries":[]}`,
		`{"extra":true,"sources":[],"topics":[],"entries":[]}`,
		`{"sources":[{"id":"1","resources":[],"sdkVersion":"old"}],"topics":[],"entries":[]}`,
		`{"sources":[],"topics":[],"entries":[{"id":"request","kind":"request","configVersion":1}]}`,
		`null`,
	} {
		t.Run(raw, func(t *testing.T) {
			var manifest ProgramManifest
			require.Error(t, json.Unmarshal([]byte(raw), &manifest))
		})
	}
	var manifest ProgramManifest
	require.NoError(t, json.Unmarshal([]byte(`{"sources":[],"topics":[],"entries":[{"id":"request","kind":"request"}]}`), &manifest))
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.JSONEq(t, `{"sources":[],"topics":[],"entries":[{"id":"request","kind":"request"}]}`, string(raw))
}
