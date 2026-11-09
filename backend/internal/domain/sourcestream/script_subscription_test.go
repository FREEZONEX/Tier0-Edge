package sourcestream

// 覆盖订阅解析，确保发现阶段不执行代码，拒绝动态或多订阅声明。
import (
	"backend/internal/streamexec"
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSubscriptionDeclaration(t *testing.T) {
	for _, code := range []string{
		`return sources.get("plc").subscribe("iot/+/telemetry", async (input, context) => { return {data: {value: input.value*2, topic: context.topic, text: "中文"}, drop:false, logs:[]}; }, {qos:1});`,
		`const source = sources.get("plc"); source.subscribe("iot/+/telemetry", function(input, context) { return {data: {value: input.value*2, topic: context.topic, text: "中文"}, drop:false, logs:[]}; }, {qos:1});`,
	} {
		sub, err := parseSubscription(code)
		require.NoError(t, err)
		require.NotNil(t, sub)
		require.Equal(t, "plc", sub.Alias)
		require.Equal(t, 1, sub.QoS)
		result, err := streamexec.Execute(context.Background(), streamexec.Request{Code: "return (" + sub.Handler + ")(input, context);", Input: json.RawMessage(`{"value":7}`), Context: json.RawMessage(`{"topic":"iot/pump/telemetry"}`), TimeoutMs: 1000})
		require.NoError(t, err)
		require.JSONEq(t, `{"value":14,"topic":"iot/pump/telemetry","text":"中文"}`, string(result.Data))
	}
	for _, code := range []string{
		`return {data: input};`, "// source.subscribe(ignored)\nreturn {data: 'subscribe'};",
	} {
		sub, err := parseSubscription(code)
		require.NoError(t, err)
		require.Nil(t, sub)
	}
	for _, code := range []string{
		`return sources.get("x").subscribe(input.topic, x=>({data:x}));`,
		`return sources.get("x").subscribe("a/#/b", x=>({data:x}));`,
		`return sources.get("x").subscribe("a", x=>({data:x}), {qos:3});`,
		`return sources.get("x").subscribe("a", x=>({data:x}), {qos:1, timeout:3});`,
		`sources.get("x").subscribe("a", x=>({data:x})); sources.get("x").subscribe("b", x=>({data:x}));`,
		`if (input) sources.get("x").subscribe("a", x=>({data:x}));`,
		`return sources.get("x")["subscribe"]("a", x=>({data:x}));`,
		`const topic="a"; return sources.get("x").subscribe(topic, x=>({data:x}));`,
	} {
		_, err := parseSubscription(code)
		require.ErrorIs(t, err, ErrSubscription, code)
	}
}
