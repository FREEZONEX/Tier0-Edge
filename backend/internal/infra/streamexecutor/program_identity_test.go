package streamexecutor

// 验证独立进程返回的资源空间不能在结果校验中丢失或被伪造。
import (
	"encoding/json"
	"testing"

	"backend/internal/streamexec"

	"github.com/stretchr/testify/require"
)

func TestProgramResultRetainsMetadataSpace(t *testing.T) {
	manifest := streamexec.ProgramManifest{Topics: []streamexec.ProgramTopic{{TopicID: "79", Space: "metadata", Access: "write"}}}
	request := streamexec.ProgramRequest{ExpectedManifest: &manifest}
	result := streamexec.ProgramResult{Manifest: manifest, Outputs: []streamexec.ProgramOutput{
		{TopicID: "79", Space: "metadata", Data: json.RawMessage(`{"recordId":"pump","record":{"value":7}}`)},
	}}
	require.NoError(t, validateProgramResult(request, result))
	for _, space := range []string{"", "legacy", "unknown"} {
		result.Outputs[0].Space = space
		require.Error(t, validateProgramResult(request, result), space)
	}
}
