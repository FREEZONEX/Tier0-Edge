package sourcestream

import (
	"encoding/json"
	"testing"

	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

func TestVendorNativeResourceAndURLBoundaries(t *testing.T) {
	d := SourceDefinition{SourceType: "sap-odata", Endpoint: "https://example.test/service/", HTTP: &HTTPConnection{}}
	call := streamexec.ProgramCall{Kind: "source.read", ResourceRef: "vendor:sap-odata:entity:Materials", Options: json.RawMessage(`{"query":{"$top":"1"}}`)}
	options, err := vendorNativeRequest(d, call)
	require.NoError(t, err)
	require.Equal(t, "Materials", options.Path)
	require.Equal(t, []string{"1"}, options.QueryValues["$top"])
	call.Options = json.RawMessage(`{"next":"https://example.test/service/Materials?$skiptoken=page2"}`)
	options, err = vendorNativeRequest(d, call)
	require.NoError(t, err)
	require.Equal(t, []string{"page2"}, options.QueryValues["$skiptoken"])
	for _, next := range []string{"https://other.test/service/Materials", "http://example.test/service/Materials", "https://example.test/service2/Materials", "/metadata/credentials", "https://x:secret@example.test/service/Materials", "https://example.test/service/Materials#frag", "../../secret"} {
		call.Options, _ = json.Marshal(map[string]string{"next": next})
		_, err = vendorNativeRequest(d, call)
		require.Error(t, err, next)
	}
	call.Options = json.RawMessage(`{}`)
	for _, ref := range []string{"vendor:plex-mes:jobs", "vendor:sap-odata:update", "vendor:sap-odata:entity:../outside", "vendor:sap-odata:missing"} {
		call.ResourceRef = ref
		_, err = vendorNativeRequest(d, call)
		require.Error(t, err, ref)
	}
	call.ResourceRef = "vendor:sap-odata:create"
	call.Kind, call.Input = "source.invoke", json.RawMessage(`{"params":{"entity":"Materials"},"data":{"id":"9007199254740999"}}`)
	options, err = vendorNativeRequest(d, call)
	require.NoError(t, err)
	require.Equal(t, "POST", options.Method)
	call.Input = json.RawMessage(`{"params":{"entity":"Materials"},"next":"https://example.test/service/Materials"}`)
	_, err = vendorNativeRequest(d, call)
	require.Error(t, err, "writes never follow a pagination link")
}

func TestVendorPaginationAndBusinessErrors(t *testing.T) {
	for _, data := range []string{`{"@odata.nextLink":"Materials?$skiptoken=2"}`, `{"nextRecordsUrl":"/services/data/v67.0/query/next"}`, `{"Links":{"Next":"https://example.test/piwebapi/page2"}}`} {
		value, err := vendorJSON([]byte(data))
		require.NoError(t, err)
		require.NotEmpty(t, vendorPagination(value)["next"])
	}
	require.Nil(t, vendorPagination(map[string]any{"items": []any{}}))
	for _, example := range []struct{ profile, data string }{
		{"yonyou-yonbip", `{"code":"403"}`},
		{"kingdee-jdy", `{"errcode":1002}`},
		{"ngp", `{"code":401}`},
		{"kingdee-k3cloud", `{"Result":{"ResponseStatus":{"IsSuccess":false}}}`},
		{"teamcenter", `{"ServiceData":{"partialErrors":[{"code":1}]}}`},
	} {
		value, err := vendorJSON([]byte(example.data))
		require.NoError(t, err)
		require.NotEmpty(t, vendorBusinessError(example.profile, value))
	}
	require.Empty(t, vendorBusinessError("teamcenter", map[string]any{"partialErrors": []any{}}))
}

// Teamcenter SOA可能用HTTP200返回异常，不得把未登录当作成功数据。
func TestVendorTeamcenterExceptionEnvelopes(t *testing.T) {
	for _, example := range []struct{ qname, expected string }{
		{"http://teamcenter.com/Schemas/Soa/2006-03/Exceptions.InvalidUserException", "vendorSessionExpired"},
		{"http://teamcenter.com/Schemas/Soa/2006-03/Exceptions.InternalServerException", "vendorException"},
		{"http://teamcenter.com/Schemas/Soa/2006-03/Query.GetSavedQueriesResponse", ""},
		{"untrusted.InvalidUserException", ""},
	} {
		value := map[string]any{".QName": example.qname, "code": 214001, "message": "not used for classification"}
		require.Equal(t, example.expected, vendorBusinessError("teamcenter", value), example.qname)
		require.Empty(t, vendorBusinessError("sap-odata", value), "another vendor must not borrow Teamcenter authentication semantics")
	}
}

// K3查询错误使用二维数组包装；不能把HTTP200当成功，也不能误认正常COUNT行。
func TestVendorK3QueryErrorEnvelopes(t *testing.T) {
	for _, example := range []struct{ name, data, expected string }{
		{"field-error", `[[{"Result":{"ResponseStatus":{"IsSuccess":false,"ErrorCode":"FIELD","MsgCode":0}}}]]`, "vendorBusinessRejected"},
		{"expired-query", `[[{"Result":{"ResponseStatus":{"IsSuccess":false,"MsgCode":1}}}]]`, "vendorSessionExpired"},
		{"expired-query-string", `[[{"Result":{"ResponseStatus":{"IsSuccess":false,"MsgCode":"1"}}}]]`, "vendorSessionExpired"},
		{"expired-view", `{"Result":{"ResponseStatus":{"IsSuccess":false,"MsgCode":1}}}`, "vendorSessionExpired"},
		{"field-error-object", `{"Result":{"ResponseStatus":{"IsSuccess":false,"MsgCode":0}}}`, "vendorBusinessRejected"},
		{"other-code-not-authentication", `[[{"Result":{"ResponseStatus":{"IsSuccess":false,"MsgCode":2}}}]]`, "vendorBusinessRejected"},
		{"success-not-expired", `[[{"Result":{"ResponseStatus":{"IsSuccess":true,"MsgCode":1}}}]]`, ""},
		{"non-boolean-success", `[[{"Result":{"ResponseStatus":{"IsSuccess":"false","MsgCode":1}}}]]`, ""},
		{"count", `[[323]]`, ""},
		{"row", `[[10000001,"M1","Material"]]`, ""},
		{"empty", `[]`, ""},
		{"not-single-envelope", `[[{"Result":{"ResponseStatus":{"IsSuccess":false,"MsgCode":1}}}],[42]]`, ""},
	} {
		t.Run(example.name, func(t *testing.T) {
			value, err := vendorJSON([]byte(example.data))
			require.NoError(t, err)
			require.Equal(t, example.expected, vendorBusinessError("kingdee-k3cloud", value))
			require.Empty(t, vendorBusinessError("sap-odata", value), "another vendor must not borrow K3 error semantics")
		})
	}
}
