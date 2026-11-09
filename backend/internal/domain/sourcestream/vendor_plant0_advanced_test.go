package sourcestream

// 验证 SAP/Salesforce 有界真实分页、PI 短窗口质量/时间及异常响应拒绝。
// 默认仅运行本地合同夹具；真实读取显式 opt-in，配置只写独占本地 testkit 库。
// 不调用 collect、不创建远端 Source/Stream、不写厂商/UNS/Checkpoint、不注入故障。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

const plant0AdvancedPages = 3

// Explicit fixture selection, never an arbitrary SOQL fragment. These objects
// are documented by Plant0; changing the dataset cannot relax the cursor gate.
func plant0AdvancedSalesforceObject(value string) (string, error) {
	if value == "" {
		return "Account", nil
	}
	switch value {
	case "Account", "Contact", "Case", "Task":
		return value, nil
	default:
		return "", plant0AdvancedError("salesforce-object-scope")
	}
}

type plant0AdvancedPage struct {
	Data map[string]any
	Next string
}

// Never return upstream errors/bodies/URLs to testing's failure formatter.
type plant0AdvancedFailure struct{ label string }

func (e *plant0AdvancedFailure) Error() string { return "advanced-readonly:" + e.label }
func plant0AdvancedError(label string) error   { return &plant0AdvancedFailure{label: label} }

func plant0AdvancedFailureCategory(err error) string {
	for _, known := range []struct {
		err   error
		label string
	}{
		{streamexec.ErrSourceDenied, "source-denied"}, {ErrCredential, "credential"},
		{ErrOAuthRequired, "oauth-required"}, {ErrOAuth, "oauth-failed"},
		{ErrVendorResponse, "vendor-response"}, {context.Canceled, "canceled"},
		{context.DeadlineExceeded, "timeout"}, {streamexec.ErrTimeout, "timeout"},
		{ErrHTTPRequest, "http-failed"}, {repo.ErrConflict, "config-conflict"},
	} {
		if errors.Is(err, known.err) {
			return known.label
		}
	}
	var failure *plant0AdvancedFailure
	if errors.As(err, &failure) {
		switch failure.label {
		case "response-rejected", "response-shape", "rows-shape-or-limit", "row-shape",
			"salesforce-cursor-contract", "identity-not-exact-string", "duplicate-identity",
			"total-row-budget", "real-pagination-not-observed", "empty-or-cyclic-page",
			"timestamp-window", "decimal-precision-not-string", "int64-precision-contract",
			"native-call-budget", "native-read-source-denied", "native-read-credential",
			"native-read-oauth-required", "native-read-oauth-failed", "native-read-vendor-response",
			"native-read-canceled", "native-read-timeout", "native-read-http-failed",
			"native-read-config-conflict", "native-read-unclassified":
			return failure.label
		}
	}
	return "unclassified"
}

// Exact origins/paths were verified against the three registered HTTP-r2 Sources;
// Portal responses cannot authorize another host, port, path, or auth endpoint.
func plant0AdvancedSourceScope(profileID string, d SourceDefinition) error {
	endpoint := map[string]string{
		"sap-odata":  "https://52.221.121.67:8000/sap/opu/odata4/sap/zplant0_factory/srvd_a2x/sap/zplant0_factory/0001/",
		"salesforce": "https://52.221.121.67:8003/",
		"pi-web-api": "https://52.221.121.67:8005/piwebapi",
	}[profileID]
	if endpoint == "" || d.Endpoint != endpoint {
		return plant0AdvancedError("registered-plant0-source-scope")
	}
	if profileID == "salesforce" {
		if d.HTTP == nil || d.HTTP.OAuth == nil || d.HTTP.OAuth.TokenURL != "https://52.221.121.67:8003/services/oauth2/token" {
			return plant0AdvancedError("registered-oauth-scope")
		}
	} else if d.HTTP != nil && d.HTTP.OAuth != nil {
		return plant0AdvancedError("registered-oauth-scope")
	}
	return nil
}

func plant0AdvancedPersistedDefinition(profileID, document string) (SourceDefinition, error) {
	var d SourceDefinition
	if json.Unmarshal([]byte(document), &d) != nil || d.CredentialRef == "" {
		return d, plant0AdvancedError("persisted-definition-or-credential-reference")
	}
	return d, plant0AdvancedSourceScope(profileID, d)
}

func plant0AdvancedResponse(raw []byte) (plant0AdvancedPage, error) {
	var response struct {
		Status     int             `json:"status"`
		Error      json.RawMessage `json:"error"`
		Data       json.RawMessage `json:"data"`
		Pagination struct {
			Next string `json:"next"`
		} `json:"pagination"`
	}
	if len(raw) > 1024*1024 || json.Unmarshal(raw, &response) != nil || response.Status < 200 || response.Status >= 300 ||
		len(response.Error) != 0 && string(response.Error) != "null" {
		return plant0AdvancedPage{}, plant0AdvancedError("response-rejected")
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(response.Data))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || value == nil {
		return plant0AdvancedPage{}, plant0AdvancedError("response-shape")
	}
	return plant0AdvancedPage{Data: value, Next: response.Pagination.Next}, nil
}

func plant0AdvancedRows(page plant0AdvancedPage, listKey string) ([]map[string]any, error) {
	values, ok := page.Data[listKey].([]any)
	if !ok || len(values) > 600 {
		return nil, plant0AdvancedError("rows-shape-or-limit")
	}
	rows := make([]map[string]any, 0, len(values))
	for _, value := range values {
		row, ok := value.(map[string]any)
		if !ok {
			return nil, plant0AdvancedError("row-shape")
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func plant0AdvancedSalesforcePage(page plant0AdvancedPage) error {
	done, ok := page.Data["done"].(bool)
	if !ok || done && page.Next != "" || !done && page.Next == "" {
		return plant0AdvancedError("salesforce-cursor-contract")
	}
	return nil
}

// Links go through production vendorNativeRequest on every call, including
// relative links; this helper never substitutes an arbitrary HTTP client.
func plant0AdvancedWalk(read func(string) (plant0AdvancedPage, error), listKey string, keys []string,
	check func(map[string]any) error) (pages, rows int, truncated bool, err error) {
	seenLinks, seenIDs := map[string]bool{}, map[string]bool{}
	next := ""
	for pages < plant0AdvancedPages {
		page, failure := read(next)
		if failure != nil {
			return pages, rows, false, failure
		}
		batch, failure := plant0AdvancedRows(page, listKey)
		if failure != nil {
			return pages, rows, false, failure
		}
		for _, row := range batch {
			identity := make([]string, len(keys))
			for index, key := range keys {
				value, ok := row[key].(string)
				if !ok || value == "" {
					return pages, rows, false, plant0AdvancedError("identity-not-exact-string")
				}
				identity[index] = value
			}
			encoded, _ := json.Marshal(identity)
			if seenIDs[string(encoded)] {
				return pages, rows, false, plant0AdvancedError("duplicate-identity")
			}
			seenIDs[string(encoded)] = true
			if failure = check(row); failure != nil {
				return pages, rows, false, failure
			}
			rows++
			if rows > 600 {
				return pages, rows, false, plant0AdvancedError("total-row-budget")
			}
		}
		pages++
		if page.Next == "" {
			if pages < 2 {
				return pages, rows, false, plant0AdvancedError("real-pagination-not-observed")
			}
			return pages, rows, false, nil
		}
		if len(batch) == 0 || seenLinks[page.Next] {
			return pages, rows, false, plant0AdvancedError("empty-or-cyclic-page")
		}
		seenLinks[page.Next] = true
		next = page.Next
	}
	// This is bounded pagination evidence, never a full dataset/snapshot claim.
	return pages, rows, true, nil
}

type plant0AdvancedProperty struct {
	Name string `xml:"Name,attr"`
	Type string `xml:"Type,attr"`
}

func plant0AdvancedSAPContract(metadata string) (keys, decimalFields, integerFields []string, err error) {
	var document struct {
		Schemas []struct {
			Namespace string `xml:"Namespace,attr"`
			Types     []struct {
				Name string `xml:"Name,attr"`
				Keys []struct {
					Name string `xml:"Name,attr"`
				} `xml:"Key>PropertyRef"`
				Properties []plant0AdvancedProperty `xml:"Property"`
			} `xml:"EntityType"`
			Sets []struct {
				Name string `xml:"Name,attr"`
				Type string `xml:"EntityType,attr"`
			} `xml:"EntityContainer>EntitySet"`
		} `xml:"DataServices>Schema"`
	}
	if len(metadata) > 1024*1024 || xml.Unmarshal([]byte(metadata), &document) != nil {
		return nil, nil, nil, plant0AdvancedError("sap-metadata-shape")
	}
	typeName := ""
	for _, schema := range document.Schemas {
		for _, set := range schema.Sets {
			if set.Name == "Materials" {
				typeName = set.Type
			}
		}
	}
	for _, schema := range document.Schemas {
		for _, entity := range schema.Types {
			if schema.Namespace+"."+entity.Name != typeName {
				continue
			}
			for _, key := range entity.Keys {
				keys = append(keys, key.Name)
			}
			for _, property := range entity.Properties {
				if property.Type == "Edm.Decimal" {
					decimalFields = append(decimalFields, property.Name)
				} else if property.Type == "Edm.Int64" {
					integerFields = append(integerFields, property.Name)
				}
			}
		}
	}
	if len(keys) == 0 || len(keys) > 8 {
		return nil, nil, nil, plant0AdvancedError("sap-material-keys-missing")
	}
	return keys, decimalFields, integerFields, nil
}

func plant0AdvancedTime(value any, start, end time.Time) error {
	text, ok := value.(string)
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		// Salesforce emits offsets without a colon; PI/OData use RFC3339.
		parsed, err = time.Parse("2006-01-02T15:04:05.999999999-0700", text)
	}
	if !ok || err != nil || parsed.Before(start) || parsed.After(end) {
		return plant0AdvancedError("timestamp-window")
	}
	return nil
}

func plant0AdvancedInt64(value any) error {
	switch value := value.(type) {
	case string:
		if _, err := strconv.ParseInt(value, 10, 64); err == nil {
			return nil
		}
	case json.Number:
		// Production vendorNumbers retains only JS-safe integers as numbers;
		// values outside this interval must already be exact decimal strings.
		if integer, err := value.Int64(); err == nil && integer >= -9007199254740991 && integer <= 9007199254740991 {
			return nil
		}
	}
	return plant0AdvancedError("int64-precision-contract")
}

func plant0AdvancedPIQuality(row map[string]any, start, end time.Time) (bool, error) {
	if err := plant0AdvancedTime(row["Timestamp"], start, end); err != nil {
		return false, err
	}
	good, ok := row["Good"].(bool)
	if !ok {
		return false, plant0AdvancedError("pi-quality-missing")
	}
	for _, field := range []string{"Questionable", "Substituted"} {
		if _, ok := row[field].(bool); !ok {
			return false, plant0AdvancedError("pi-quality-flags")
		}
	}
	if _, ok := row["Value"]; !ok {
		return false, plant0AdvancedError("pi-value-missing")
	}
	return !good, nil
}

func TestPlant0VendorAdvancedReadOnly(t *testing.T) {
	if os.Getenv("PLANT0_VENDOR_ADVANCED_READONLY") != "1" {
		t.Skip("explicit Plant0 advanced-readonly opt-in required; no external calls by default")
	}
	salesforceObject, err := plant0AdvancedSalesforceObject(os.Getenv("PLANT0_SALESFORCE_OBJECT"))
	require.True(t, err == nil, "allowed-salesforce-object-required")
	// A caller cannot point testkit's CREATE/DROP lifecycle at a deployed DB.
	db, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	require.True(t, err == nil && db != nil && db.Scheme == "postgres" && db.User != nil && db.User.Username() == "test" &&
		(db.Hostname() == "localhost" || db.Hostname() == "127.0.0.1") && db.Port() == "55432" && db.Path == "/tier0_test", "local-test-database-required")
	testkit.NewTestDB(t)
	s := New(nil, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Cleanup(s.httpManager.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	portal := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer portal.CloseIdleConnections()
	nativeCalls := 0
	for _, profileID := range []string{"sap-odata", "salesforce", "pi-web-api"} {
		t.Run(profileID, func(t *testing.T) {
			profile := VendorProfile{}
			for _, candidate := range VendorProfiles() {
				if candidate.ID == profileID {
					profile = candidate
				}
			}
			portalID := map[string]string{"sap-odata": "erp", "salesforce": "crm", "pi-web-api": "historian"}[profileID]
			req, failure := http.NewRequestWithContext(ctx, http.MethodGet, "https://52.221.121.67/portal/api/datasources/"+portalID+"/connection", nil)
			require.True(t, failure == nil, "portal-request")
			response, failure := portal.Do(req)
			require.True(t, failure == nil, "portal-transport")
			body, failure := io.ReadAll(io.LimitReader(response.Body, 256*1024+1))
			response.Body.Close()
			require.True(t, failure == nil && response.StatusCode == 200 && len(body) <= 256*1024, "portal-response")
			var connection map[string]any
			require.True(t, json.Unmarshal(body, &connection) == nil, "portal-json")
			if profileID == "salesforce" {
				clients, _ := vendorObject(connection["credentials"])["clients"].([]any)
				reader := false
				for _, client := range clients {
					reader = reader || vendorObject(client)["writable"] == false
				}
				require.True(t, reader, "explicit-reader-client-required")
			}
			d, credential := plant0VendorDefinition(profile, connection)
			require.True(t, plant0AdvancedSourceScope(profileID, d) == nil, "registered-plant0-source-or-oauth-scope")
			require.True(t, d.Validate() == nil, "source-definition")
			view, failure := s.CreateSource(ctx, d, 0)
			require.True(t, failure == nil, "local-source-create")
			id := view["id"].(int64)
			_, failure = s.SaveCredential(ctx, id, view["revision"].(int64), credential, "")
			require.True(t, failure == nil, "local-encrypted-credential")
			row, failure := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
			require.True(t, failure == nil, "local-source-read")
			d, failure = plant0AdvancedPersistedDefinition(profileID, row.Document)
			require.True(t, failure == nil, "persisted-source-scope-and-credential-reference")
			read := func(operation string, input vendorNativeInput) (json.RawMessage, error) {
				if nativeCalls >= 20 || ctx.Err() != nil {
					return nil, plant0AdvancedError("native-call-budget")
				}
				nativeCalls++
				input.TimeoutMs = 5000
				raw, _ := json.Marshal(input)
				callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
				defer callCancel()
				result, failure := s.vendorNativeCall(callCtx, row, d, streamexec.ProgramCall{Kind: "source.read", ResourceRef: "vendor:" + profileID + ":" + operation, Options: raw}, "", "")
				if failure != nil {
					category := plant0AdvancedFailureCategory(failure)
					t.Logf("native readonly failure category=%s", category)
					return nil, plant0AdvancedError("native-read-" + category)
				}
				var envelope struct {
					Status int `json:"status"`
				}
				if json.Unmarshal(result, &envelope) == nil && envelope.Status >= 100 && envelope.Status <= 599 {
					t.Logf("native readonly returned status=%d", envelope.Status)
				}
				return result, nil
			}
			if profileID == "pi-web-api" {
				plant0AdvancedPI(t, read)
				return
			}
			keys, listKey := []string{"Id"}, "records"
			fields := []string{"Id", "SystemModstamp"}
			decimalFields := []string{}
			integerFields := []string{}
			if profileID == "sap-odata" {
				raw, failure := read("metadata", vendorNativeInput{Headers: map[string]string{"Accept": "application/xml"}})
				require.True(t, failure == nil, "sap-metadata-read")
				var envelope struct {
					Status int             `json:"status"`
					Error  json.RawMessage `json:"error"`
					Data   string          `json:"data"`
				}
				require.True(t, json.Unmarshal(raw, &envelope) == nil && envelope.Status >= 200 && envelope.Status < 300 &&
					(len(envelope.Error) == 0 || string(envelope.Error) == "null"), "sap-metadata-envelope")
				keys, decimalFields, integerFields, failure = plant0AdvancedSAPContract(envelope.Data)
				require.True(t, failure == nil, "sap-metadata-key-contract")
				fields = append(append([]string{}, keys...), "LastChangeDateTime")
				for _, property := range append(decimalFields, integerFields...) {
					if len(fields) < 12 && !slices.Contains(fields, property) {
						fields = append(fields, property)
					}
				}
				listKey = "value"
			} else {
				raw, failure := read("describe:"+salesforceObject, vendorNativeInput{})
				require.True(t, failure == nil, "salesforce-describe-read")
				page, failure := plant0AdvancedResponse(raw)
				require.True(t, failure == nil, "salesforce-describe-envelope")
				properties, failure := plant0AdvancedRows(page, "fields")
				require.True(t, failure == nil, "salesforce-describe-fields")
				for _, property := range properties {
					if property["name"] == "Legacy_Id__c" {
						fields = append(fields, "Legacy_Id__c")
					}
				}
			}
			end := time.Now().UTC().Truncate(time.Second)
			readPage := func(next string) (plant0AdvancedPage, error) {
				input := vendorNativeInput{Next: next}
				operation := "query"
				if profileID == "sap-odata" {
					input.Headers = map[string]string{"Prefer": "odata.maxpagesize=2", "Accept": "application/json;IEEE754Compatible=true"}
				} else {
					input.Headers = map[string]string{"Sforce-Query-Options": "batchSize=200"}
				}
				if next == "" {
					if profileID == "sap-odata" {
						operation = "entity:Materials"
						input.Query = plant0AdvancedQuery(map[string]string{"$select": strings.Join(fields, ","), "$orderby": strings.Join(keys, ","), "$filter": "LastChangeDateTime le " + end.Format(time.RFC3339), "$top": "6"})
					} else {
						input.Query = plant0AdvancedQuery(map[string]string{"q": "SELECT " + strings.Join(fields, ",") + " FROM " + salesforceObject + " WHERE SystemModstamp <= " + end.Format(time.RFC3339) + " ORDER BY Id LIMIT 600"})
					}
				}
				if profileID == "sap-odata" {
					operation = "entity:Materials"
				}
				raw, failure := read(operation, input)
				if failure != nil {
					return plant0AdvancedPage{}, failure
				}
				page, failure := plant0AdvancedResponse(raw)
				if failure == nil && profileID == "salesforce" {
					failure = plant0AdvancedSalesforcePage(page)
				}
				return page, failure
			}
			pages, rows, truncated, failure := plant0AdvancedWalk(readPage, listKey, keys, func(row map[string]any) error {
				timeField := "SystemModstamp"
				if profileID == "sap-odata" {
					timeField = "LastChangeDateTime"
				}
				if err := plant0AdvancedTime(row[timeField], time.Time{}, end); err != nil {
					return err
				}
				for _, field := range decimalFields {
					if !slices.Contains(fields, field) || row[field] == nil {
						continue
					}
					if _, ok := row[field].(string); !ok {
						return plant0AdvancedError("decimal-precision-not-string")
					}
				}
				for _, field := range integerFields {
					if slices.Contains(fields, field) && row[field] != nil {
						if err := plant0AdvancedInt64(row[field]); err != nil {
							return err
						}
					}
				}
				return nil
			})
			if failure != nil {
				t.Logf("bounded readonly failure category=%s pages=%d rows=%d", plant0AdvancedFailureCategory(failure), pages, rows)
			}
			if profileID == "salesforce" {
				t.Logf("actual readonly dataset=%s pages=%d rows=%d", salesforceObject, pages, rows)
			}
			require.True(t, failure == nil && rows > 0, "bounded-real-pagination-contract")
			t.Logf("actual readonly: profile=%s pages=%d rows=%d truncated=%t snapshotConsistency=unproven incrementalCheckpoint=not-tested", profileID, pages, rows, truncated)
		})
	}
	t.Logf("actual readonly nativeCalls=%d localOwnedDBOnly=true externalBusinessWrites=0 remoteSourceMutations=0", nativeCalls)
}

func plant0AdvancedQuery(values map[string]string) map[string]json.RawMessage {
	result := map[string]json.RawMessage{}
	for key, value := range values {
		result[key], _ = json.Marshal(value)
	}
	return result
}

func plant0AdvancedPI(t *testing.T, read func(string, vendorNativeInput) (json.RawMessage, error)) {
	t.Helper()
	raw, err := read("servers", vendorNativeInput{})
	require.True(t, err == nil, "pi-servers-read")
	page, err := plant0AdvancedResponse(raw)
	require.True(t, err == nil, "pi-servers-envelope")
	servers, err := plant0AdvancedRows(page, "Items")
	require.True(t, err == nil && len(servers) > 0, "pi-server-list")
	server, ok := servers[0]["WebId"].(string)
	require.True(t, ok && server != "", "pi-server-webid")
	raw, err = read("points:"+server, vendorNativeInput{Query: plant0AdvancedQuery(map[string]string{"maxCount": "3"})})
	require.True(t, err == nil, "pi-points-read")
	page, err = plant0AdvancedResponse(raw)
	require.True(t, err == nil, "pi-points-envelope")
	points, err := plant0AdvancedRows(page, "Items")
	require.True(t, err == nil && len(points) > 0 && len(points) <= 3, "pi-point-budget")
	end := time.Now().UTC().Truncate(time.Second)
	start := end.Add(-5 * time.Minute)
	count, bad, truncated := 0, 0, 0
	for _, point := range points {
		webID, ok := point["WebId"].(string)
		require.True(t, ok && webID != "", "pi-point-webid")
		raw, err = read("recorded:"+webID, vendorNativeInput{Query: plant0AdvancedQuery(map[string]string{"startTime": start.Format(time.RFC3339), "endTime": end.Format(time.RFC3339), "maxCount": "32", "boundaryType": "Inside"})})
		require.True(t, err == nil, "pi-recorded-read")
		page, err = plant0AdvancedResponse(raw)
		require.True(t, err == nil, "pi-recorded-envelope")
		rows, err := plant0AdvancedRows(page, "Items")
		require.True(t, err == nil && len(rows) <= 32, "pi-recorded-budget")
		if page.Next != "" || len(rows) == 32 {
			truncated++
		}
		for _, row := range rows {
			isBad, err := plant0AdvancedPIQuality(row, start, end)
			require.True(t, err == nil, "pi-actual-quality-and-time")
			count++
			if isBad {
				bad++
			}
		}
	}
	require.True(t, count > 0, "pi-real-window-samples-required")
	t.Logf("actual readonly: profile=pi-web-api points=%d samples=%d badObserved=%d truncatedWindows=%d", len(points), count, bad, truncated)
	if bad == 0 {
		t.Log("coverage gap: actual Bad-quality preservation NOT VERIFIED; no fault injected")
	}
}

func TestPlant0AdvancedReadOnlyContracts(t *testing.T) {
	t.Run("salesforce-dataset-selection-is-explicit-and-not-SOQL", func(t *testing.T) {
		object, err := plant0AdvancedSalesforceObject("")
		require.NoError(t, err)
		require.Equal(t, "Account", object)
		for _, value := range []string{"Account", "Contact", "Case", "Task"} {
			object, err = plant0AdvancedSalesforceObject(value)
			require.NoError(t, err)
			require.Equal(t, value, object)
		}
		for _, value := range []string{"User", "Account WHERE Id != null", "Contact;DELETE", " Task", "task", "https://other.test"} {
			_, err = plant0AdvancedSalesforceObject(value)
			require.EqualError(t, err, "advanced-readonly:salesforce-object-scope")
		}
	})
	t.Run("reject-http-business-and-envelope-errors", func(t *testing.T) {
		for _, example := range []struct{ input, label string }{
			{`{"status":401,"data":{}}`, "response-rejected"},
			{`{"status":200,"error":{},"data":{}}`, "response-rejected"},
			{`{"status":200,"error":"secret","data":{}}`, "response-rejected"},
			{`{"status":200,"data":[]}`, "response-shape"},
			{`{"status":200,"data":null}`, "response-shape"},
		} {
			_, err := plant0AdvancedResponse([]byte(example.input))
			require.EqualError(t, err, "advanced-readonly:"+example.label)
		}
	})
	t.Run("precision-and-duplicates", func(t *testing.T) {
		rows := []string{`{"status":200,"data":{"value":[{"Id":"9007199254740999","amount":"123456789.0001"}]},"pagination":{"next":"page2"}}`, `{"status":200,"data":{"value":[{"Id":"9007199254741000","amount":"0.0000001"}]}}`}
		index := 0
		pages, count, truncated, err := plant0AdvancedWalk(func(string) (plant0AdvancedPage, error) {
			p, e := plant0AdvancedResponse([]byte(rows[index]))
			index++
			return p, e
		}, "value", []string{"Id"}, func(row map[string]any) error {
			if _, ok := row["amount"].(string); !ok {
				return plant0AdvancedError("decimal")
			}
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, 2, pages)
		require.Equal(t, 2, count)
		require.False(t, truncated)
		index = 0
		_, _, _, err = plant0AdvancedWalk(func(string) (plant0AdvancedPage, error) { index++; return plant0AdvancedResponse([]byte(rows[0])) }, "value", []string{"Id"}, func(map[string]any) error { return nil })
		require.EqualError(t, err, "advanced-readonly:duplicate-identity")
		require.Equal(t, 2, index)
	})
	t.Run("bound-stop-without-fourth-request", func(t *testing.T) {
		calls := 0
		_, count, truncated, err := plant0AdvancedWalk(func(string) (plant0AdvancedPage, error) {
			calls++
			id := strings.Repeat("x", calls)
			return plant0AdvancedPage{Data: map[string]any{"records": []any{map[string]any{"Id": id}}}, Next: id}, nil
		}, "records", []string{"Id"}, func(map[string]any) error { return nil })
		require.NoError(t, err)
		require.Equal(t, 3, calls)
		require.Equal(t, 3, count)
		require.True(t, truncated)
	})
	t.Run("actual-pagination-required", func(t *testing.T) {
		_, _, _, err := plant0AdvancedWalk(func(string) (plant0AdvancedPage, error) {
			return plant0AdvancedPage{Data: map[string]any{"value": []any{map[string]any{"Id": "1"}}}}, nil
		}, "value", []string{"Id"}, func(map[string]any) error { return nil })
		require.EqualError(t, err, "advanced-readonly:real-pagination-not-observed")
	})
	t.Run("salesforce-done-requires-correct-cursor", func(t *testing.T) {
		require.NoError(t, plant0AdvancedSalesforcePage(plant0AdvancedPage{Data: map[string]any{"done": true}}))
		require.NoError(t, plant0AdvancedSalesforcePage(plant0AdvancedPage{Data: map[string]any{"done": false}, Next: "native-next"}))
		for _, page := range []plant0AdvancedPage{
			{Data: map[string]any{"done": false}},
			{Data: map[string]any{"done": true}, Next: "native-next"},
			{Data: map[string]any{}},
		} {
			require.EqualError(t, plant0AdvancedSalesforcePage(page), "advanced-readonly:salesforce-cursor-contract")
		}
	})
	t.Run("cursor-forward-and-stop-on-error", func(t *testing.T) {
		calls := 0
		_, _, _, err := plant0AdvancedWalk(func(next string) (plant0AdvancedPage, error) {
			calls++
			if calls == 1 {
				require.Empty(t, next)
				return plant0AdvancedPage{Data: map[string]any{"value": []any{map[string]any{"Id": "first"}}}, Next: "native-cursor"}, nil
			}
			require.Equal(t, "native-cursor", next)
			return plant0AdvancedPage{}, plant0AdvancedError("response-rejected")
		}, "value", []string{"Id"}, func(map[string]any) error { return nil })
		require.EqualError(t, err, "advanced-readonly:response-rejected")
		require.Equal(t, 2, calls)
	})
	t.Run("numeric-identity-rejected", func(t *testing.T) {
		_, _, _, err := plant0AdvancedWalk(func(string) (plant0AdvancedPage, error) {
			return plant0AdvancedPage{Data: map[string]any{"records": []any{map[string]any{"Id": json.Number("9007199254740999")}}}}, nil
		}, "records", []string{"Id"}, func(map[string]any) error { return nil })
		require.EqualError(t, err, "advanced-readonly:identity-not-exact-string")
	})
	t.Run("native-timestamp-offsets", func(t *testing.T) {
		end := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
		require.NoError(t, plant0AdvancedTime("2026-10-11T12:00:00.000+0000", end.Add(-time.Minute), end))
		require.NoError(t, plant0AdvancedTime("2026-10-11T20:00:00.000+0800", end.Add(-time.Minute), end))
		require.Error(t, plant0AdvancedTime("2026-10-11T12:00:00", end.Add(-time.Minute), end))
	})
	t.Run("sap-csdl-native-types", func(t *testing.T) {
		keys, decimals, integers, err := plant0AdvancedSAPContract(`<Edmx><DataServices><Schema Namespace="Plant"><EntityType Name="Material"><Key><PropertyRef Name="Material"/></Key><Property Name="Material" Type="Edm.String"/><Property Name="Price" Type="Edm.Decimal"/><Property Name="Revision" Type="Edm.Int64"/></EntityType><EntityContainer><EntitySet Name="Materials" EntityType="Plant.Material"/></EntityContainer></Schema></DataServices></Edmx>`)
		require.NoError(t, err)
		require.Equal(t, []string{"Material"}, keys)
		require.Equal(t, []string{"Price"}, decimals)
		require.Equal(t, []string{"Revision"}, integers)
	})
	t.Run("sap-metadata-explicit-xml-accept-survives-native-translation", func(t *testing.T) {
		d := SourceDefinition{SourceType: "sap-odata", Endpoint: "https://52.221.121.67:8000/sap/opu/odata4/sap/zplant0_factory/srvd_a2x/sap/zplant0_factory/0001/", HTTP: &HTTPConnection{}}
		input, err := json.Marshal(vendorNativeInput{Headers: map[string]string{"Accept": "application/xml"}})
		require.NoError(t, err)
		options, err := vendorNativeRequest(d, streamexec.ProgramCall{Kind: "source.read", ResourceRef: "vendor:sap-odata:metadata", Options: input})
		require.NoError(t, err)
		require.Equal(t, http.MethodGet, options.Method)
		require.Equal(t, "$metadata", options.Path)
		require.Equal(t, "application/xml", options.Headers["Accept"])
	})
	t.Run("native-int64-normalization-and-precision", func(t *testing.T) {
		for _, number := range []string{"9007199254740999", "9223372036854775807", "-9223372036854775808"} {
			value := vendorNumbers(json.Number(number))
			require.Equal(t, number, value)
			require.NoError(t, plant0AdvancedInt64(value))
		}
		require.NoError(t, plant0AdvancedInt64(json.Number("42")))
		for _, value := range []any{json.Number("9007199254740999"), json.Number("1.5"), "9223372036854775808", "1e3", float64(42)} {
			require.EqualError(t, plant0AdvancedInt64(value), "advanced-readonly:int64-precision-contract")
		}
	})
	t.Run("real-native-url-boundaries", func(t *testing.T) {
		d := SourceDefinition{SourceType: "salesforce", Endpoint: "https://example.test/services/", HTTP: &HTTPConnection{}}
		for _, next := range []string{"https://other.test/services/data/query", "http://example.test/services/data/query", "https://user:secret@example.test/services/data/query", "/outside/data/query", "https://example.test/services/data/query#fragment"} {
			raw, _ := json.Marshal(vendorNativeInput{Next: next})
			_, err := vendorNativeRequest(d, streamexec.ProgramCall{Kind: "source.read", ResourceRef: "vendor:salesforce:query", Options: raw})
			require.Error(t, err)
		}
	})
	t.Run("registered-exact-source-origins-and-paths", func(t *testing.T) {
		for profileID, endpoint := range map[string]string{
			"sap-odata":  "https://52.221.121.67:8000/sap/opu/odata4/sap/zplant0_factory/srvd_a2x/sap/zplant0_factory/0001/",
			"salesforce": "https://52.221.121.67:8003/",
			"pi-web-api": "https://52.221.121.67:8005/piwebapi",
		} {
			d := SourceDefinition{Endpoint: endpoint, HTTP: &HTTPConnection{}}
			if profileID == "salesforce" {
				d.HTTP.OAuth = &OAuthConfig{TokenURL: "https://52.221.121.67:8003/services/oauth2/token"}
			}
			require.NoError(t, plant0AdvancedSourceScope(profileID, d))
			parsed, err := url.Parse(endpoint)
			require.NoError(t, err)
			for _, mutated := range []string{
				"https://52.221.121.67:443" + parsed.Path,
				"https://52.221.121.67:8999" + parsed.Path,
				"https://52.221.121.68:" + parsed.Port() + parsed.Path,
				strings.Replace(endpoint, "https://", "http://", 1),
				endpoint + "modified",
				endpoint + "?scope=other",
				endpoint + "#fragment",
				strings.Replace(endpoint, "https://", "https://user:secret@", 1),
			} {
				d.Endpoint = mutated
				require.EqualError(t, plant0AdvancedSourceScope(profileID, d), "advanced-readonly:registered-plant0-source-scope")
			}
		}
		require.Error(t, plant0AdvancedSourceScope("unknown", SourceDefinition{}))
	})
	t.Run("registered-exact-oauth-endpoint", func(t *testing.T) {
		d := SourceDefinition{Endpoint: "https://52.221.121.67:8003/", HTTP: &HTTPConnection{}}
		require.EqualError(t, plant0AdvancedSourceScope("salesforce", d), "advanced-readonly:registered-oauth-scope")
		d.HTTP.OAuth = &OAuthConfig{}
		for _, token := range []string{
			"https://52.221.121.67:443/services/oauth2/token",
			"https://52.221.121.67:8000/services/oauth2/token",
			"https://52.221.121.68:8003/services/oauth2/token",
			"http://52.221.121.67:8003/services/oauth2/token",
			"https://52.221.121.67:8003/services/oauth2/other",
			"https://52.221.121.67:8003/services/oauth2/token?scope=other",
			"https://user:secret@52.221.121.67:8003/services/oauth2/token",
		} {
			d.HTTP.OAuth.TokenURL = token
			require.EqualError(t, plant0AdvancedSourceScope("salesforce", d), "advanced-readonly:registered-oauth-scope")
		}
		d.HTTP.OAuth.TokenURL = "https://52.221.121.67:8003/services/oauth2/token"
		require.NoError(t, plant0AdvancedSourceScope("salesforce", d))
		d.Endpoint = "https://52.221.121.67:8005/piwebapi"
		require.EqualError(t, plant0AdvancedSourceScope("pi-web-api", d), "advanced-readonly:registered-oauth-scope")
	})
	t.Run("persisted-credential-reference-reloaded-before-native-read", func(t *testing.T) {
		original := SourceDefinition{Endpoint: "https://52.221.121.67:8005/piwebapi", HTTP: &HTTPConnection{Authentication: "basic"}}
		raw, err := json.Marshal(original)
		require.NoError(t, err)
		_, err = plant0AdvancedPersistedDefinition("pi-web-api", string(raw))
		require.EqualError(t, err, "advanced-readonly:persisted-definition-or-credential-reference")
		persisted := original
		persisted.CredentialRef = "local-fixture-reference"
		raw, err = json.Marshal(persisted)
		require.NoError(t, err)
		reloaded, err := plant0AdvancedPersistedDefinition("pi-web-api", string(raw))
		require.NoError(t, err)
		require.Empty(t, original.CredentialRef)
		require.Equal(t, persisted.CredentialRef, reloaded.CredentialRef)
		require.Equal(t, "basic", reloaded.HTTPAuthentication())
		require.Equal(t, original.Endpoint, reloaded.Endpoint)
		persisted.Endpoint = "https://52.221.121.67:443/piwebapi"
		raw, err = json.Marshal(persisted)
		require.NoError(t, err)
		_, err = plant0AdvancedPersistedDefinition("pi-web-api", string(raw))
		require.EqualError(t, err, "advanced-readonly:registered-plant0-source-scope")
	})
	t.Run("diagnostic-whitelist-never-prints-upstream-errors", func(t *testing.T) {
		for err, expected := range map[error]string{
			streamexec.ErrSourceDenied: "source-denied", ErrCredential: "credential",
			ErrOAuthRequired: "oauth-required", ErrOAuth: "oauth-failed", ErrVendorResponse: "vendor-response",
			context.Canceled: "canceled", context.DeadlineExceeded: "timeout", streamexec.ErrTimeout: "timeout",
			ErrHTTPRequest: "http-failed", repo.ErrConflict: "config-conflict",
		} {
			require.Equal(t, expected, plant0AdvancedFailureCategory(fmt.Errorf("private upstream detail: %w", err)))
		}
		require.Equal(t, "unclassified", plant0AdvancedFailureCategory(errors.New("private upstream body and URL")))
		require.Equal(t, "unclassified", plant0AdvancedFailureCategory(plant0AdvancedError("private unapproved label")))
		require.Equal(t, "salesforce-cursor-contract", plant0AdvancedFailureCategory(plant0AdvancedError("salesforce-cursor-contract")))
		require.Equal(t, "real-pagination-not-observed", plant0AdvancedFailureCategory(plant0AdvancedError("real-pagination-not-observed")))
	})
	t.Run("pi-quality-window-and-bad-value-retained", func(t *testing.T) {
		end := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
		row := map[string]any{"Timestamp": end.Add(-time.Second).Format(time.RFC3339), "Good": false, "Questionable": true, "Substituted": false, "Value": map[string]any{"Name": "No Data"}}
		before, _ := json.Marshal(row)
		bad, err := plant0AdvancedPIQuality(row, end.Add(-time.Minute), end)
		require.NoError(t, err)
		require.True(t, bad)
		after, _ := json.Marshal(row)
		require.Equal(t, before, after)
		row["Timestamp"] = end.Add(time.Second).Format(time.RFC3339)
		_, err = plant0AdvancedPIQuality(row, end.Add(-time.Minute), end)
		require.EqualError(t, err, "advanced-readonly:timestamp-window")
		delete(row, "Good")
		row["Timestamp"] = end.Format(time.RFC3339)
		_, err = plant0AdvancedPIQuality(row, end.Add(-time.Minute), end)
		require.EqualError(t, err, "advanced-readonly:pi-quality-missing")
	})
}
