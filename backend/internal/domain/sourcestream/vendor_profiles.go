package sourcestream

import (
	"encoding/json"
	"sort"
)

// VendorOperation is a code-owned native operation, not another platform entity.
// ReadOnly allows retrying a rejected session only when no side effect is possible.
type VendorOperation struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	ReadOnly bool   `json:"readOnly"`
}

type VendorProfile struct {
	ID              string            `json:"id"`
	Protocol        Protocol          `json:"protocol"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Document        SourceDefinition  `json:"document"`
	CredentialKinds []string          `json:"credentialKinds"`
	CredentialKeys  []string          `json:"credentialKeys"`
	CredentialAnyOf []string          `json:"credentialAnyOf,omitempty"`
	ParameterKeys   []string          `json:"parameterKeys"`
	ConfigSchema    map[string]any    `json:"configSchema"`
	Operations      []VendorOperation `json:"operations"`
}

func vendorOperations() map[string][]VendorOperation {
	read := func(id, name, method, path string) VendorOperation {
		return VendorOperation{id, name, method, path, true}
	}
	write := func(id, name, method, path string) VendorOperation {
		return VendorOperation{id, name, method, path, false}
	}
	return map[string][]VendorOperation{
		"plant0-simctl":   {read("board", "生产看板状态", "GET", "portal/api/board")},
		"pi-web-api":      {read("servers", "数据服务器", "GET", "dataservers"), read("points", "测点", "GET", "dataservers/{webId}/points"), read("point", "测点路径查询", "GET", "points"), read("value", "快照", "GET", "streams/{webId}/value"), read("recorded", "归档", "GET", "streams/{webId}/recorded"), read("interpolated", "插值", "GET", "streams/{webId}/interpolated"), read("summary", "统计", "GET", "streams/{webId}/summary")},
		"plex-mes":        {read("jobs", "工单", "GET", "scheduling/v1/jobs"), read("operations", "工序", "GET", "scheduling/v1/jobs/{id}/operations"), read("workcenters", "工位", "GET", "production/v1/production-definitions/workcenters"), read("production", "生产记录", "GET", "production/v1/production-history/production-entries"), read("parts", "零件", "GET", "mdm/v1/parts"), write("hold", "挂起工单", "POST", "scheduling/v1/jobs/{id}/hold"), write("release-hold", "解除挂起", "POST", "scheduling/v1/jobs/{id}/release-hold")},
		"wms-rest":        {read("inventory", "库存", "GET", "inventory"), read("lpn", "托盘", "GET", "lpn/{id}"), read("locations", "库位托盘", "GET", "locations/{id}/lpns"), read("pick-tasks", "拣货任务", "GET", "pick-tasks"), read("movements", "移动记录", "GET", "movements"), write("cycle-count", "盘点过账", "POST", "cycle-count")},
		"sap-odata":       {read("service", "服务目录", "GET", ""), read("metadata", "契约", "GET", "$metadata"), read("entity", "实体查询", "GET", "{entity}"), write("update", "修改实体", "PATCH", "{entity}"), write("create", "创建实体", "POST", "{entity}")},
		"yonyou-yonbip":   {read("materials", "物料分页", "POST", "yonbip/digitalModel/product/listproductbycondition")},
		"kingdee-jdy":     {read("materials", "商品分页", "GET", "jdy/v2/bd/material")},
		"ngp":             {read("materials", "商品分页", "POST", "rest")},
		"kingdee-k3cloud": {read("query", "单据查询", "POST", "Kingdee.BOS.WebApi.ServicesStub.DynamicFormService.ExecuteBillQuery.common.kdsvc"), read("view", "单据详情", "POST", "Kingdee.BOS.WebApi.ServicesStub.DynamicFormService.View.common.kdsvc"), write("draft", "暂存", "POST", "Kingdee.BOS.WebApi.ServicesStub.DynamicFormService.Draft.common.kdsvc"), write("save", "保存", "POST", "Kingdee.BOS.WebApi.ServicesStub.DynamicFormService.Save.common.kdsvc"), write("submit", "提交", "POST", "Kingdee.BOS.WebApi.ServicesStub.DynamicFormService.Submit.common.kdsvc"), write("audit", "审核", "POST", "Kingdee.BOS.WebApi.ServicesStub.DynamicFormService.Audit.common.kdsvc"), write("delete", "删除未审核单据", "POST", "Kingdee.BOS.WebApi.ServicesStub.DynamicFormService.Delete.common.kdsvc"), write("operation", "业务操作", "POST", "Kingdee.BOS.WebApi.ServicesStub.DynamicFormService.ExcuteOperation.common.kdsvc")},
		"salesforce":      {read("versions", "API 版本", "GET", "services/data/"), read("objects", "对象目录", "GET", "services/data/{version}/sobjects"), read("describe", "对象描述", "GET", "services/data/{version}/sobjects/{object}/describe"), read("query", "SOQL 查询", "GET", "services/data/{version}/query"), read("query-all", "含删除查询", "GET", "services/data/{version}/queryAll"), read("deleted", "删除记录", "GET", "services/data/{version}/sobjects/{object}/deleted"), write("create", "创建对象", "POST", "services/data/{version}/sobjects/{object}"), write("update", "更新对象", "PATCH", "services/data/{version}/sobjects/{object}/{id}"), write("bulk-start", "提交 Bulk 查询", "POST", "services/data/{version}/jobs/query"), read("bulk-status", "Bulk 状态", "GET", "services/data/{version}/jobs/query/{id}"), read("bulk-results", "Bulk 结果", "GET", "services/data/{version}/jobs/query/{id}/results")},
		"teamcenter":      {read("queries", "Saved Query 目录", "POST", "JsonRestServices/Query-2006-03-SavedQuery/getSavedQueries"), read("query", "执行查询", "POST", "JsonRestServices/Query-2008-06-SavedQuery/executeSavedQueries"), read("objects", "加载对象", "POST", "JsonRestServices/Core-2007-09-DataManagement/loadObjects"), read("properties", "读取属性", "POST", "JsonRestServices/Core-2006-03-DataManagement/getProperties"), read("tickets", "文件票据", "POST", "JsonRestServices/Core-2006-03-FileManagement/getFileReadTickets")},
		"hikvision-isapi": {read("device", "设备信息", "GET", "ISAPI/System/deviceInfo"), read("users", "人员查询", "POST", "ISAPI/AccessControl/UserInfo/Search"), read("events", "门禁事件", "POST", "ISAPI/AccessControl/AcsEvent"), read("capabilities", "设备能力", "GET", "ISAPI/AccessControl/capabilities")},
	}
}

func VendorProfiles() []VendorProfile {
	type spec struct {
		id, name, auth          string
		parameters, credentials []string
	}
	specs := []spec{
		{"pi-web-api", "PI Web API", "basic", []string{"serverName", "pointPath", "pointWebId", "backfillStart", "backfillEnd"}, []string{"username", "password"}},
		{"plant0-simctl", "Plant0 只读生产看板", "none", nil, nil},
		{"plex-mes", "Plex MES", "custom", []string{"tenantId", "pcn"}, []string{"apiKey"}},
		{"wms-rest", "WMS REST", "bearer", nil, []string{"token"}},
		{"sap-odata", "SAP OData", "basic", []string{"client"}, []string{"username", "password"}},
		{"yonyou-yonbip", "用友 YonBIP", "custom", []string{"orgId"}, []string{"appKey", "appSecret"}},
		{"kingdee-jdy", "金蝶云星辰", "custom", []string{"domain"}, []string{"clientId", "clientSecret", "appKey", "appSecret"}},
		{"ngp", "网上管家婆", "custom", []string{"redirectUri"}, []string{"appKey", "appSecret"}},
		{"kingdee-k3cloud", "金蝶云星空", "custom", []string{"acctId", "user", "lcid", "appId"}, []string{"password", "appSecret"}},
		{"salesforce", "Salesforce", "oauth2", []string{"apiVersion"}, []string{"clientSecret"}},
		{"teamcenter", "Teamcenter", "basic", []string{"group", "role", "locale"}, []string{"username", "password"}},
		{"hikvision-isapi", "海康 ISAPI", "basic", nil, []string{"username", "password"}},
	}
	result := make([]VendorProfile, 0, len(specs))
	operations := vendorOperations()
	for _, spec := range specs {
		protocol := HTTP
		connection := &HTTPConnection{Authentication: spec.auth, Parameters: map[string]any{}}
		if spec.auth == "oauth2" {
			protocol = OAuth
			connection.Authentication = ""
			connection.OAuth = &OAuthConfig{GrantType: "client_credentials", ClientAuthentication: "client_secret_post"}
		}
		properties := map[string]any{}
		for _, name := range spec.parameters {
			properties[name] = map[string]any{"type": "string"}
		}
		parameterKeys := append([]string(nil), spec.parameters...)
		additionalKeys := []string{}
		for name, schema := range vendorAcquisitionParameters(spec.id) {
			properties[name] = schema
			additionalKeys = append(additionalKeys, name)
		}
		sort.Strings(additionalKeys)
		parameterKeys = append(parameterKeys, additionalKeys...)
		result = append(result, VendorProfile{ID: spec.id, Protocol: protocol, Name: spec.name, Description: "厂商原生认证与资源操作；脚本负责采集和 UNS 输出。", Document: SourceDefinition{Protocol: protocol, SourceType: spec.id, Role: ClientRole, RuntimeLocation: "local", HTTP: connection}, CredentialKinds: []string{spec.auth}, CredentialKeys: spec.credentials, ParameterKeys: parameterKeys, ConfigSchema: map[string]any{"type": "object", "properties": map[string]any{"http": map[string]any{"type": "object", "properties": map[string]any{"parameters": map[string]any{"type": "object", "properties": properties}}}}}, Operations: operations[spec.id]})
	}
	for i := range result {
		if result[i].ID == "kingdee-k3cloud" {
			result[i].CredentialAnyOf = []string{"password", "appSecret"}
		}
	}
	return result
}

func vendorOperation(sourceType, id string) (VendorOperation, bool) {
	for _, operation := range vendorOperations()[sourceType] {
		if operation.ID == id {
			return operation, true
		}
	}
	return VendorOperation{}, false
}

func vendorResources(sourceType string) []NativeResource {
	result := []NativeResource{}
	for _, operation := range vendorOperations()[sourceType] {
		models := []AccessModel{RequestReply}
		if operation.ReadOnly {
			models = append(models, Publish)
		} else {
			models = append(models, Subscribe)
		}
		schema, _ := json.Marshal(map[string]any{"type": "object", "properties": map[string]any{"params": map[string]any{"type": "object"}, "query": map[string]any{"type": "object"}, "data": map[string]any{}}, "description": "路径参数、查询参数和原生请求体；返回厂商状态、数据和错误。"})
		result = append(result, NativeResource{ID: "vendor:" + sourceType + ":" + operation.ID, Name: operation.Name, Models: models, Schema: schema, Metadata: map[string]any{"method": operation.Method, "path": operation.Path, "readOnly": operation.ReadOnly, "sourceType": sourceType, "contractDeclared": true}})
	}
	return result
}
