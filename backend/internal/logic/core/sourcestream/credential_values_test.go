package sourcestream

// 真实 JSON/path 解析及独立 PG 验证命名凭据不会在 API→domain 边界丢失。
import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"backend/internal/bootstrap"
	"backend/internal/contextx"
	domain "backend/internal/domain/sourcestream"
	"backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/testkit"
	"backend/internal/types"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

func TestSourceCredentialValuesRequestParsing(t *testing.T) {
	request := func(values string) *types.SourceCredentialSaveReq {
		t.Helper()
		r := httptest.NewRequest("PUT", "/api/core/uns/sources/37/credentials", strings.NewReader(`{"sourceId":999,"SourceId":998,"expectedRevision":1,"kind":"custom","values":`+values+`}`))
		r.Header.Set("Content-Type", "application/json")
		r = pathvar.WithVars(r, map[string]string{"sourceId": "37"})
		var parsed types.SourceCredentialSaveReq
		require.NoError(t, httpx.ParseSourceStreamRequest(r, &parsed))
		return &parsed
	}
	parsed := request(`{"token":"fixture-token","signingKey":"fixture-key","sourceId":"999"}`)
	require.EqualValues(t, 37, parsed.SourceId, "body fields and nested secret names cannot overwrite the route identity")
	require.Equal(t, map[string]string{"token": "fixture-token", "signingKey": "fixture-key", "sourceId": "999"}, parsed.Values)
	for _, values := range []string{`[]`, `"value"`, `{"key":42}`, `{"key":true}`, `{"key":{}}`, `{"key":[]}`} {
		r := httptest.NewRequest("PUT", "/api/core/uns/sources/37/credentials", strings.NewReader(`{"expectedRevision":1,"kind":"custom","values":`+values+`}`))
		r.Header.Set("Content-Type", "application/json")
		r = pathvar.WithVars(r, map[string]string{"sourceId": "37"})
		var invalid types.SourceCredentialSaveReq
		require.Error(t, httpx.ParseSourceStreamRequest(r, &invalid), values)
	}
	// encoding/json represents an inner null as an empty string; domain sealing
	// rejects that empty named secret rather than coercing it into valid data.
	nullValue := request(`{"key":null}`)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cipher, err := domain.NewCredentialCipher(key)
	require.NoError(t, err)
	_, err = cipher.Seal(nullValue.SourceId, domain.Credential{Kind: nullValue.Kind, Values: nullValue.Values})
	require.ErrorIs(t, err, domain.ErrCredential)
	legacy := httptest.NewRequest("PUT", "/api/core/uns/sources/37/credentials", strings.NewReader(`{"expectedRevision":1,"kind":"bearer","secret":"fixture-legacy-token"}`))
	legacy.Header.Set("Content-Type", "application/json")
	legacy = pathvar.WithVars(legacy, map[string]string{"sourceId": "37"})
	var optional types.SourceCredentialSaveReq
	require.NoError(t, httpx.ParseSourceStreamRequest(legacy, &optional))
	require.Nil(t, optional.Values, "the new field remains optional for existing credentials")
}

func TestSourceCredentialValuesExecutePersistsEncryptedNamedValues(t *testing.T) {
	previousCommon, previousUNS := repo.GetCommonConn(context.Background()), repo.GetUnsConn(context.Background())
	store, _ := testkit.NewTestDB(t)
	t.Cleanup(func() { repo.SetStoreForTest(previousCommon, previousUNS) })
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	sourceService := domain.New(nil, nil, key)
	t.Cleanup(sourceService.Close)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.source": {}, "uns.stream.manage": {}, "uns.source.credentials": {}, "uns.read": {}}})
	view, err := sourceService.CreateSource(ctx, domain.SourceDefinition{Name: "credential API fixture", Protocol: domain.HTTP, Endpoint: "http://fixture.internal/api", RuntimeLocation: "local", HTTP: &domain.HTTPConnection{Authentication: "custom"}}, 0)
	require.NoError(t, err)
	id, revision := view["id"].(int64), view["revision"].(int64)
	body := fmt.Sprintf(`{"sourceId":999,"expectedRevision":%d,"kind":"custom","values":{"apiKey":"fixture-api-key","signingSecret":"fixture-signing-key"}}`, revision)
	r := httptest.NewRequest("PUT", fmt.Sprintf("/api/core/uns/sources/%d/credentials", id), strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = pathvar.WithVars(r, map[string]string{"sourceId": strconv.FormatInt(id, 10)})
	var req types.SourceCredentialSaveReq
	require.NoError(t, httpx.ParseSourceStreamRequest(r, &req))
	require.Equal(t, id, req.SourceId)
	response, err := execute(ctx, &svc.ServiceContext{App: &bootstrap.App{SourceStream: sourceService}}, "SourceCredentialSave", &req)
	require.NoError(t, err)
	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "fixture-api-key")
	require.NotContains(t, string(encoded), "fixture-signing-key")
	repository := repo.NewSourceStreamRepo(store)
	source, err := repository.Source(ctx, id)
	require.NoError(t, err)
	var def domain.SourceDefinition
	require.NoError(t, json.Unmarshal([]byte(source.Document), &def))
	sealed, err := repository.Credential(ctx, id, def.CredentialRef)
	require.NoError(t, err)
	require.NotContains(t, string(sealed.Ciphertext), "fixture-api-key")
	cipher, err := domain.NewCredentialCipher(key)
	require.NoError(t, err)
	credential, err := cipher.Open(id, sealed.Ciphertext)
	require.NoError(t, err)
	require.Equal(t, req.Values, credential.Values, "execute must map request Values into the encrypted domain credential")
}
