package sourcestream

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPProtocolAdvertisesImplementedSourceRolesAndCredentials(t *testing.T) {
	for _, definition := range Protocols()["list"].([]map[string]any) {
		if definition["id"] != HTTP {
			continue
		}
		require.ElementsMatch(t, []ConnectionRole{ClientRole, ServerRole}, definition["roles"])
		require.ElementsMatch(t, []string{"basic", "bearer", "custom", "http"}, definition["credentialKinds"])
		// The initial contract remains a client; the UI explicitly chooses Server.
		source := definition["document"].(SourceDefinition)
		require.Equal(t, ClientRole, source.ConnectionRole())
		require.Equal(t, "none", source.HTTPAuthentication())
		return
	}
	t.Fatal("HTTP Source definition is missing")
}
