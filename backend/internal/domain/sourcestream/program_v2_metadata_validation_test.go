package sourcestream

import (
	"encoding/json"
	"testing"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

func TestProgramCanonicalOutputAuthorityAndMixedBatch(t *testing.T) {
	const topicID = "11111111-1111-4111-8111-111111111111"
	const collectionID = "22222222-2222-4222-8222-222222222222"
	manifest := streamexec.ProgramManifest{Topics: []streamexec.ProgramTopic{{TopicID: topicID, Access: "write"}, {CollectionID: collectionID, Access: "read"}, {TopicID: "7"}}}
	result := streamexec.ProgramResult{Outputs: []streamexec.ProgramOutput{{TopicID: topicID, Data: json.RawMessage(`{"recordId":"a","record":{"value":7}}`)}}}
	require.NoError(t, validateProgramV2Outputs(manifest, result))
	bindings := []repo.ProgramUNSBinding{{Reference: manifest.Topics[0], CollectionID: collectionID}}
	writes, err := programMetadataWrites(bindings, result.Outputs)
	require.NoError(t, err)
	require.Equal(t, collectionID, writes[0].CollectionID)
	require.Equal(t, "a", writes[0].Value.RecordID)
	result.Outputs = append(result.Outputs, streamexec.ProgramOutput{TopicID: "7", Data: json.RawMessage(`{"value":7}`)})
	require.Error(t, validateProgramV2Outputs(manifest, result), "different transaction domains cannot share an output batch")
	result.Outputs = []streamexec.ProgramOutput{{CollectionID: collectionID, Data: json.RawMessage(`{"recordId":"a","record":{}}`)}}
	require.Error(t, validateProgramV2Outputs(manifest, result), "a read declaration cannot authorize writes")
	_, err = programMetadataWrites(bindings, result.Outputs)
	require.Error(t, err)
	result.Outputs = []streamexec.ProgramOutput{{TopicID: topicID, Data: json.RawMessage(`{"recordId":"a","record":{},"credential":"forbidden"}`)}}
	_, err = programMetadataWrites(bindings, result.Outputs)
	require.Error(t, err, "structured writes reject unknown envelope members")
}
