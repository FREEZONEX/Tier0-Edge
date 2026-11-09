package sourcestream

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

// Opt in explicitly. The public portal is a simulation environment; this test
// performs authentication/metadata/query only and never writes its business data.
// Credentials remain in memory and this test's private encrypted database.
func TestPlant0VendorReadOnly(t *testing.T) {
	if os.Getenv("PLANT0_VENDOR_READONLY") != "1" {
		t.Skip("set PLANT0_VENDOR_READONLY=1 for actual Plant0 protocol reads")
	}
	store, _ := testkit.NewTestDB(t)
	s := New(nil, programV2RealExecutor{}, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Cleanup(s.Close)
	ctx := programV2ServiceContext("uns.stream.manage", "uns.stream.execute", "uns.source", "uns.source.credentials", "uns.read")
	portal := &http.Client{Timeout: 30 * time.Second}
	ids := map[string]string{"pi-web-api": "historian", "plex-mes": "mes", "wms-rest": "wms", "sap-odata": "erp", "yonyou-yonbip": "yonyou", "kingdee-jdy": "kingdee", "ngp": "ngp", "kingdee-k3cloud": "k3cloud", "salesforce": "crm", "teamcenter": "plm", "hikvision-isapi": "hikvision"}
	for _, profile := range VendorProfiles() {
		t.Run(profile.ID, func(t *testing.T) {
			res, err := portal.Get("https://52.221.121.67/portal/api/datasources/" + ids[profile.ID] + "/connection")
			require.NoError(t, err)
			defer res.Body.Close()
			require.Equal(t, 200, res.StatusCode)
			body, err := io.ReadAll(io.LimitReader(res.Body, 256*1024))
			require.NoError(t, err)
			var connection map[string]any
			require.NoError(t, json.Unmarshal(body, &connection))
			d, credential := plant0VendorDefinition(profile, connection)
			require.NoError(t, d.Validate())
			view, err := s.CreateSource(ctx, d, 0)
			require.NoError(t, err)
			id := view["id"].(int64)
			view, err = s.SaveCredential(ctx, id, view["revision"].(int64), credential, "")
			require.NoError(t, err)
			if profile.ID == "ngp" && credential.Values["refreshToken"] == "" {
				t.Skip("pending merchant authorization: portal provides no grant/refresh token; not counted as passed")
			}
			_, err = s.Probe(ctx, id, nil, 10000)
			require.NoError(t, err, "actual authentication/metadata failed: %s", profile.ID)
			view, err = s.Source(ctx, id)
			require.NoError(t, err)
			resources, err := s.Discover(ctx, id, view["revision"].(int64), "")
			require.NoError(t, err)
			require.Positive(t, resources["total"].(int))
			row, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal([]byte(row.Document), &d))
			options, err := vendorProbeRequest(d)
			require.NoError(t, err)
			operation := map[string]string{"pi-web-api": "servers", "plex-mes": "workcenters", "wms-rest": "inventory", "sap-odata": "service", "yonyou-yonbip": "materials", "kingdee-jdy": "materials", "ngp": "materials", "kingdee-k3cloud": "query", "salesforce": "objects", "teamcenter": "queries", "hikvision-isapi": "device"}[profile.ID]
			input := vendorNativeInput{Data: options.JSON, Query: map[string]json.RawMessage{}}
			for key, values := range options.QueryValues {
				input.Query[key], err = json.Marshal(values)
				require.NoError(t, err)
			}
			raw, err := json.Marshal(input)
			require.NoError(t, err)
			result, err := s.vendorNativeCall(ctx, row, d, streamexec.ProgramCall{Kind: "source.read", ResourceRef: "vendor:" + profile.ID + ":" + operation, Options: raw}, "", "")
			require.NoError(t, err)
			var response struct {
				Status int             `json:"status"`
				Data   json.RawMessage `json:"data"`
				Error  json.RawMessage `json:"error"`
			}
			require.NoError(t, json.Unmarshal(result, &response))
			require.GreaterOrEqual(t, response.Status, 200)
			require.Less(t, response.Status, 300)
			require.Empty(t, response.Error)
			require.NotEmpty(t, response.Data)
			ref := "vendor:" + profile.ID + ":" + operation
			code := fmt.Sprintf(`module.exports=api=>{const source=api.sources.use({id:%q,resources:[%q]});api.stream.onRequest({id:'sample'},()=>source.read(%q,%s));};`, strconv.FormatInt(id, 10), ref, ref, raw)
			previewDocument := programV2Definition(code)
			previewDocument.Script.TimeoutMs = 5000
			preview, err := s.TestEntry(ctx, previewDocument, map[string]any{"data": nil}, 0, "fetch", "sample")
			require.NoError(t, err)
			require.Equal(t, "live-read", preview["sourceInputMode"])
			require.True(t, preview["dryRun"].(bool))
			require.False(t, preview["sideEffects"].(bool))
			require.Len(t, preview["targets"], 0)
			previewJSON, err := json.Marshal(preview["result"])
			require.NoError(t, err)
			var previewResult struct {
				Data struct {
					Status int             `json:"status"`
					Error  json.RawMessage `json:"error"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(previewJSON, &previewResult))
			require.GreaterOrEqual(t, previewResult.Data.Status, 200)
			require.Less(t, previewResult.Data.Status, 300)
			require.Empty(t, previewResult.Data.Error)
			if profile.ID == "teamcenter" {
				// Saved Query目录成功不代表会话可以读取实体。正式Goja执行
				// 一个最多两项的真实查询，拒绝SOA异常及缺失结果。
				entityCode := fmt.Sprintf(`module.exports=api=>{
 const source=api.sources.use({id:%q,resources:['vendor:teamcenter:queries','vendor:teamcenter:query','vendor:teamcenter:objects','vendor:teamcenter:properties']});
 api.stream.onRequest({id:'entities'},async()=>{
  const catalog=await source.read('vendor:teamcenter:queries',{data:{header:{},body:{}}});
  if(catalog.status!==200||catalog.error)throw new Error('query-catalog-rejected');
  const query=catalog.data.queries.find(q=>q.name==='Item...');
  if(!query)throw new Error('item-query-missing');
  const response=await source.read('vendor:teamcenter:query',{data:{header:{},body:{queries:[{query:query.query,entries:['Item ID'],values:['*'],maxNumToReturn:2,limitList:[],clientId:'stream-readonly-test'}]}}});
  if(response.status!==200||response.error)return {ok:false,reason:'entity-query-rejected',status:response.status,code:response.error&&response.error.code,qname:response.data&&response.data['.QName']};
  if(!Array.isArray(response.data.arrayOfResults))return {ok:false,reason:'entity-result-missing',fields:Object.keys(response.data||{})};
  const uids=response.data.arrayOfResults.flatMap(result=>result.objectUIDS||[]);
  if(!uids.length||uids.length>2||uids.some(uid=>typeof uid!=='string'))return {ok:false,reason:'entity-identities-invalid'};
  const loaded=await source.read('vendor:teamcenter:objects',{data:{header:{},body:{uids}}});
  if(loaded.status!==200||loaded.error)return {ok:false,reason:'load-rejected'};
  const models=loaded.data.modelObjects||{};
  if(uids.some(uid=>!models[uid]||models[uid].uid!==uid))return {ok:false,reason:'load-identities-missing'};
  const properties=await source.read('vendor:teamcenter:properties',{data:{header:{},body:{objects:uids.map(uid=>({uid,type:models[uid].type})),attributes:['item_id','object_name','object_type']}}});
  if(properties.status!==200||properties.error)return {ok:false,reason:'properties-rejected'};
  const objects=properties.data.modelObjects||{};
  if(uids.some(uid=>!objects[uid]||!objects[uid].props||!objects[uid].props.item_id||!objects[uid].props.object_name))return {ok:false,reason:'entity-properties-missing'};
  return {ok:true,count:uids.length};
 });
};`, strconv.FormatInt(id, 10))
				entityDocument := programV2Definition(entityCode)
				entityDocument.Script.TimeoutMs = 5000
				entityPreview, err := s.TestEntry(ctx, entityDocument, map[string]any{"data": nil}, 0, "fetch", "entities")
				require.NoError(t, err, "actual Teamcenter entity query, not only metadata")
				entityJSON, err := json.Marshal(entityPreview["result"])
				require.NoError(t, err)
				var entityResult struct {
					Data struct {
						OK    bool `json:"ok"`
						Count int  `json:"count"`
					} `json:"data"`
				}
				require.NoError(t, json.Unmarshal(entityJSON, &entityResult))
				require.True(t, entityResult.Data.OK, "sanitized entity result: %s", entityJSON)
				require.Positive(t, entityResult.Data.Count)
				require.LessOrEqual(t, entityResult.Data.Count, 2)
			}
			var records int64
			require.NoError(t, store.UnsDB().Model(&repo.UnsRecord{}).Count(&records).Error)
			require.Zero(t, records)
			var samples int64
			require.NoError(t, store.UnsDB().Model(&repo.UnsSample{}).Count(&samples).Error)
			require.Zero(t, samples)
			// Report identifiers/counts only, never credential-bearing bodies.
			t.Logf("actual read passed: sourceType=%s operation=%s status=%d discovered=%d bytes=%d", profile.ID, operation, response.Status, resources["total"], len(response.Data))
		})
	}
}

func plant0VendorDefinition(profile VendorProfile, connection map[string]any) (SourceDefinition, Credential) {
	d := profile.Document
	d.Name = "Plant0 readonly " + profile.ID
	c := vendorObject(connection["credentials"])
	d.Endpoint = vendorString(c["base_url"])
	if d.Endpoint == "" {
		d.Endpoint = vendorString(connection["endpoint"])
	}
	if profile.ID == "sap-odata" {
		d.Endpoint = vendorString(connection["endpoint"])
	}
	u, _ := url.Parse(strings.TrimSpace(strings.Split(d.Endpoint, "|")[0]))
	switch profile.ID {
	case "pi-web-api":
		u.Path = "/piwebapi"
	case "kingdee-k3cloud":
		u.Path = "/K3Cloud/"
	case "teamcenter":
		u.Path = "/tc/"
	case "salesforce", "hikvision-isapi", "plex-mes", "yonyou-yonbip", "kingdee-jdy", "ngp":
		u.Path = "/"
	}
	d.Endpoint = u.String()
	parameters := d.HTTP.Parameters
	basic := func(user, password string) Credential {
		return Credential{Kind: "basic", Username: user, Secret: password}
	}
	named := func(values map[string]string) Credential { return Credential{Kind: "custom", Values: values} }
	get := func(key string) string { return vendorString(c[key]) }
	reader := func(key string) map[string]any {
		items, _ := c[key].([]any)
		for _, item := range items {
			obj := vendorObject(item)
			if obj["writable"] == false {
				return obj
			}
		}
		if len(items) > 0 {
			return vendorObject(items[0])
		}
		return map[string]any{}
	}
	switch profile.ID {
	case "pi-web-api":
		return d, basic(get("username"), get("password"))
	case "plex-mes":
		parameters["tenantId"], parameters["pcn"] = c["tenant_id"], c["pcn"]
		return d, named(map[string]string{"apiKey": vendorString(reader("apps")["api_key"])})
	case "wms-rest":
		return d, Credential{Kind: "bearer", Secret: get("bearer_token")}
	case "sap-odata":
		parameters["client"] = c["client"]
		return d, basic(get("reader_user"), get("reader_password"))
	case "yonyou-yonbip":
		parameters["orgId"] = c["orgId"]
		return d, named(map[string]string{"appKey": get("appKey"), "appSecret": get("appSecret")})
	case "kingdee-jdy":
		parameters["domain"] = c["domain"]
		return d, named(map[string]string{"clientId": get("clientId"), "clientSecret": get("clientSecret"), "appKey": get("app_key"), "appSecret": get("app_secret")})
	case "ngp":
		if redirects, _ := c["redirect_uris"].([]any); len(redirects) > 0 {
			parameters["redirectUri"] = redirects[0]
		}
		values := map[string]string{"appKey": get("app_key"), "appSecret": get("app_secret")}
		if token := get("refresh_token"); token != "" {
			values["refreshToken"] = token
		}
		return d, named(values)
	case "kingdee-k3cloud":
		parameters["acctId"], parameters["user"], parameters["lcid"], parameters["appId"] = c["acct_id"], c["reader_user"], c["lcid"], c["app_id"]
		return d, named(map[string]string{"password": get("reader_password"), "appSecret": get("app_secret")})
	case "salesforce":
		client := reader("clients")
		parameters["apiVersion"] = "v" + strings.TrimPrefix(get("api_version"), "v")
		d.HTTP.OAuth.ClientID = vendorString(client["client_id"])
		d.HTTP.OAuth.TokenURL = strings.TrimRight(get("login_url"), "/") + "/services/oauth2/token"
		return d, Credential{Kind: "oauth2", Secret: vendorString(client["client_secret"]), OAuth: &OAuthCredential{}}
	case "teamcenter":
		parameters["group"], parameters["role"], parameters["locale"] = c["group"], c["role"], "en_US"
		return d, basic(get("user"), get("password"))
	case "hikvision-isapi":
		return d, basic(get("user"), get("password"))
	}
	return d, Credential{}
}
