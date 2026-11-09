package unsref

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdentitySpaces(t *testing.T) {
	for _, id := range []string{"plant0-equipment-state", "collection:pump", "79", "0", "0001", "a.b_c:d-e", "00000000-0000-0000-0000-000000000000"} {
		require.True(t, ValidMetadataID(id), id)
	}
	for _, id := range []string{"", " a", "a/1", "a*", "a>1", "../a", "a\n", strings.Repeat("a", 129)} {
		require.False(t, ValidMetadataID(id), id)
	}
	for _, family := range []string{"state", "runs", "results", "matches", "schedules", "triggers", "effects", "history", "future"} {
		id := StreamRuntimePrefix + family
		require.True(t, IsStreamRuntimeID(id), id)
		require.False(t, ValidMetadataID(id), "system runtime identities are never public, including future families")
		require.False(t, DefaultMetadataTopic(id), id)
	}
	require.False(t, IsStreamRuntimeID("plant.tier0.system.stream.history"))
	require.True(t, ValidMetadataID("tier0.system.stream-history"))
	require.True(t, ValidLegacyID("79"))
	for _, id := range []string{"0", "0001", "-1", "a", "9223372036854775808"} {
		require.False(t, ValidLegacyID(id), id)
	}
	require.True(t, DefaultMetadataTopic("plant0-equipment-state"))
	require.True(t, DefaultMetadataTopic("11111111-1111-4111-8111-111111111111"))
	for _, id := range []string{"79", "0", "0001", "00000000-0000-0000-0000-000000000000", "11111111-1111-4111-8111-11111111111A"} {
		require.False(t, DefaultMetadataTopic(id), id)
		require.True(t, ValidMetadataID(id), "explicit Meta references still use the shared storage identity contract")
	}
}
