package sourcestream

import (
	"testing"

	"backend/internal/repo"
	"github.com/stretchr/testify/require"
)

func TestRunViewPreservesNamedEntryIdentity(t *testing.T) {
	view := runView(repo.StreamRun{EntryID: "pressure-read", Summary: `{"data":{"value":42}}`})
	require.Equal(t, "pressure-read", view["entryId"])
	require.Equal(t, map[string]any{"data": map[string]any{"value": float64(42)}}, view["summary"])
	require.Equal(t, "", runView(repo.StreamRun{})["entryId"])
}
