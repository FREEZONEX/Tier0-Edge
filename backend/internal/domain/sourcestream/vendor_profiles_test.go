package sourcestream

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVendorProfileContracts(t *testing.T) {
	profiles := VendorProfiles()
	require.Len(t, profiles, 12)
	seen := map[string]bool{}
	for _, profile := range profiles {
		require.False(t, seen[profile.ID])
		seen[profile.ID] = true
		require.Equal(t, profile.ID, profile.Document.SourceType)
		require.Equal(t, ClientRole, profile.Document.ConnectionRole())
		d := profile.Document
		d.Name, d.Endpoint = "Vendor test", "https://business.example.test/"
		if d.Protocol == OAuth {
			d.HTTP.OAuth.ClientID, d.HTTP.OAuth.TokenURL = "fixture", "https://business.example.test/token"
		}
		require.NoError(t, d.Validate())
		d.Role = ServerRole
		require.Error(t, d.Validate(), "vendor profiles cannot expose an inbound server")
		require.NotEmpty(t, profile.Operations)
		resources := vendorResources(profile.ID)
		require.Len(t, resources, len(profile.Operations))
		for index, resource := range resources {
			require.True(t, json.Valid(resource.Schema))
			require.Equal(t, "vendor:"+profile.ID+":"+profile.Operations[index].ID, resource.ID)
			require.Equal(t, true, resource.Metadata["contractDeclared"])
			require.NotContains(t, resource.Metadata, "metadataVerifiedAt", "a code descriptor alone is not a live discovery")
		}
		if profile.ID == "ngp" {
			require.NotContains(t, profile.CredentialKeys, "refreshToken", "first-time merchant authorization has no refresh token")
		}
		if profile.ID == "kingdee-k3cloud" {
			require.Equal(t, []string{"password", "appSecret"}, profile.CredentialAnyOf)
		} else {
			require.Empty(t, profile.CredentialAnyOf)
		}
	}
	require.Len(t, Protocols()["sourceTypes"].([]VendorProfile), 12)
	unknown := SourceDefinition{Name: "unknown", Endpoint: "https://business.example.test", Protocol: HTTP, RuntimeLocation: "local", SourceType: "unknown"}
	require.Error(t, unknown.Validate())
}
