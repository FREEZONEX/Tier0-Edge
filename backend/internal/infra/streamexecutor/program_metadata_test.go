package streamexecutor

import (
	"encoding/json"
	"testing"

	"backend/internal/streamexec"

	"github.com/stretchr/testify/require"
)

func TestProgramResultCanonicalTargetsRequireExactAuthority(t *testing.T) {
	const collectionID = "11111111-1111-4111-8111-111111111111"
	const otherID = "22222222-2222-4222-8222-222222222222"
	manifest := streamexec.ProgramManifest{Topics: []streamexec.ProgramTopic{{CollectionID: collectionID, Access: "write"}, {TopicID: otherID, Access: "read"}}}
	request := streamexec.ProgramRequest{ExpectedManifest: &manifest}
	result := streamexec.ProgramResult{Manifest: manifest, Outputs: []streamexec.ProgramOutput{{CollectionID: collectionID, Data: json.RawMessage(`{"recordId":"a","record":{}}`)}}}
	require.NoError(t, validateProgramResult(request, result))
	result.Outputs[0].CollectionID = otherID
	require.ErrorIs(t, validateProgramResult(request, result), streamexec.ErrOutput, "different Collections must not share the empty Topic key")
	result.Outputs[0].CollectionID, result.Outputs[0].TopicID = "", otherID
	require.ErrorIs(t, validateProgramResult(request, result), streamexec.ErrOutput, "read-only Topic is not output authority")
	result.Outputs[0].CollectionID = collectionID
	require.ErrorIs(t, validateProgramResult(request, result), streamexec.ErrOutput, "a forged dual reference is invalid")
}
