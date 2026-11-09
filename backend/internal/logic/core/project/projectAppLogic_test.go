package project

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	domainproject "backend/internal/domain/project"
	"backend/internal/repo"
	"backend/internal/svc/svctest"
)

func TestMakeFlowImportName_NoConflict(t *testing.T) {
	name, err := makeFlowImportName("Daily Check", nil, func(string) bool { return false }, 63)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "Daily Check" {
		t.Errorf("name = %q, want Daily Check", name)
	}
}

func TestMakeFlowImportName_RenameOnConflict(t *testing.T) {
	used := map[string]struct{}{"Daily Check": {}}
	exists := func(name string) bool {
		return name == "Daily Check" || name == "Daily Check(1)"
	}
	name, err := makeFlowImportName("Daily Check", used, exists, 63)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "Daily Check(2)" {
		t.Errorf("name = %q, want Daily Check(2)", name)
	}
}

func TestMakeFlowImportName_AvoidSameBatch(t *testing.T) {
	used := map[string]struct{}{"Daily Check": {}, "Daily Check(1)": {}}
	name, err := makeFlowImportName("Daily Check", used, func(string) bool { return false }, 63)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "Daily Check(2)" {
		t.Errorf("name = %q, want Daily Check(2)", name)
	}
}

func TestMakeFlowImportName_TruncateForSuffix(t *testing.T) {
	base := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	name, err := makeFlowImportName(base, map[string]struct{}{base: {}}, func(string) bool { return false }, 63)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa(1)"
	if name != want {
		t.Errorf("name = %q, want %q", name, want)
	}
}

func TestMakeFlowImportName_EmptyBase(t *testing.T) {
	name, err := makeFlowImportName("", nil, func(string) bool { return false }, 63)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "project-flow" {
		t.Errorf("name = %q, want project-flow", name)
	}
}

func TestFlattenTopLevelUnsNodes_FlattensAppFolder(t *testing.T) {
	nodes := []projectUnsNode{
		{
			Name: "MyApp",
			Type: "PATH",
			Children: []projectUnsNode{
				{Name: "State", Type: "PATH"},
				{Name: "Metric", Type: "PATH"},
			},
		},
		{Name: "Other", Type: "PATH"},
	}
	flattenSet := map[string]struct{}{"myapp": {}}
	out := flattenTopLevelUnsNodes(nodes, flattenSet)
	if len(out) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(out))
	}
	if out[0].Name != "State" || out[1].Name != "Metric" || out[2].Name != "Other" {
		t.Errorf("unexpected order/names: %+v", out)
	}
}

func TestFlattenTopLevelUnsNodes_KeepsTopTopicNode(t *testing.T) {
	nodes := []projectUnsNode{
		{
			Name: "MyApp",
			Type: "TOPIC",
		},
	}
	flattenSet := map[string]struct{}{"myapp": {}}
	out := flattenTopLevelUnsNodes(nodes, flattenSet)
	if len(out) != 1 || out[0].Name != "MyApp" {
		t.Fatalf("expected top-level topic node kept, got %+v", out)
	}
}

func TestImportProjectAppUns_PreservesEnableHistory(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	const (
		projectID = int64(101)
		appID     = int64(202)
		appName   = "history-app"
		namespace = "Metric/history_metric"
	)

	packageJSON := func(enableHistory string) []byte {
		field := ""
		if enableHistory != "" {
			field = `,"enableHistory":` + enableHistory
		}
		return []byte(`{"namespace":[{"name":"history-app","type":"PATH","children":[{"name":"Metric","type":"PATH","topicType":"METRIC","children":[{"name":"history_metric","type":"TOPIC","topicType":"METRIC","fields":[{"name":"value","type":"DOUBLE"}]` + field + `}]}]}]}`)
	}
	assertHistory := func(want int16) {
		t.Helper()
		var node repo.UnsNode
		if err := svcCtx.App.Store.CommonDB().Where("namespace = ?", namespace).First(&node).Error; err != nil {
			t.Fatalf("query imported UNS node: %v", err)
		}
		if node.EnableHistory != want {
			t.Fatalf("enableHistory = %d, want %d", node.EnableHistory, want)
		}
	}

	ids, skipped, created, err := importProjectAppUns(ctx, svcCtx, projectID, appID, packageJSON(`"TRUE"`), appName, 1)
	if err != nil {
		t.Fatalf("importProjectAppUns failed: %v", err)
	}
	if len(ids) == 0 || len(created) == 0 || len(skipped) != 0 {
		t.Fatalf("unexpected import result: ids=%v created=%v skipped=%v", ids, created, skipped)
	}
	assertHistory(1)

	// 旧 App 包没有 enableHistory 时，Replace 继续保留目标节点当前配置。
	var legacyPackage projectUnsPackage
	if err := json.Unmarshal(packageJSON(""), &legacyPackage); err != nil {
		t.Fatalf("unmarshal legacy package: %v", err)
	}
	if _, _, _, err := importPackageUns(ctx, svcCtx, projectID, appID, repo.UnsNode{}, legacyPackage.Namespace, []string{appName}, 1, true); err != nil {
		t.Fatalf("replace with legacy package failed: %v", err)
	}
	assertHistory(1)

	// 新包显式关闭时，Replace 应按包内值覆盖。
	var disabledPackage projectUnsPackage
	if err := json.Unmarshal(packageJSON(`"FALSE"`), &disabledPackage); err != nil {
		t.Fatalf("unmarshal disabled package: %v", err)
	}
	if _, _, _, err := importPackageUns(ctx, svcCtx, projectID, appID, repo.UnsNode{}, disabledPackage.Namespace, []string{appName}, 1, true); err != nil {
		t.Fatalf("replace with disabled package failed: %v", err)
	}
	assertHistory(2)
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("hello", 10); got != "hello" {
		t.Errorf("truncateRunes(hello,10) = %q", got)
	}
	if got := truncateRunes("hello world", 5); got != "hello" {
		t.Errorf("truncateRunes(hello world,5) = %q, want hello", got)
	}
	if got := truncateRunes("中文测试", 3); got != "中文测" {
		t.Errorf("truncateRunes(中文测试,3) = %q, want 中文测", got)
	}
}

func TestReadProjectAppPackageFromPath_WrappedFlowsDir(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	writeZipFile(t, zw, "MyApp/meta.json", []byte(`{"name":"wrapped-flow-app"}`))
	writeZipFile(t, zw, "MyApp/flows/a.json", []byte(`[{"id":"wrapped-flow","type":"tab","label":"a"}]`))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	pkg, err := readProjectAppPackageFromPath(zipPath, "app.zip")
	if err != nil {
		t.Fatalf("readProjectAppPackageFromPath failed: %v", err)
	}
	if len(pkg.FlowFiles) != 1 {
		t.Fatalf("expected 1 flow file, got %d", len(pkg.FlowFiles))
	}
	if !strings.Contains(string(pkg.FlowFiles[0].Raw), "wrapped-flow") {
		t.Errorf("unexpected flow content: %s", string(pkg.FlowFiles[0].Raw))
	}
}

func TestReadProjectAppPackageFromPath_WrappedFlowsDirBeforeMeta(t *testing.T) {
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "app.zip")

	zipFile := mustCreate(t, zipPath)
	zw := zip.NewWriter(zipFile)
	// zip 条目顺序不保证 meta.json 在前：flows/ 条目先出现时也须被收集
	writeZipFile(t, zw, "MyApp/flows/a.json", []byte(`[{"id":"flow-before-meta","type":"tab","label":"a"}]`))
	writeZipFile(t, zw, "MyApp/meta.json", []byte(`{"name":"wrapped-flow-app"}`))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	pkg, err := readProjectAppPackageFromPath(zipPath, "app.zip")
	if err != nil {
		t.Fatalf("readProjectAppPackageFromPath failed: %v", err)
	}
	if len(pkg.FlowFiles) != 1 {
		t.Fatalf("expected 1 flow file, got %d", len(pkg.FlowFiles))
	}
	if !strings.Contains(string(pkg.FlowFiles[0].Raw), "flow-before-meta") {
		t.Errorf("unexpected flow content: %s", string(pkg.FlowFiles[0].Raw))
	}
}

func TestRebindFlowRuntimeID_RewritesTabAndZRefs(t *testing.T) {
	raw := []byte(`[{"id":"f1","type":"tab","label":"Main"},{"id":"n1","type":"inject","z":"f1"},{"id":"n2","type":"debug","z":"other"}]`)
	out, err := rebindFlowRuntimeID(raw, "f2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var nodes []map[string]any
	if err := json.Unmarshal(out, &nodes); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if nodes[0]["id"] != "f2" {
		t.Errorf("tab node id = %v, want f2", nodes[0]["id"])
	}
	if nodes[1]["z"] != "f2" {
		t.Errorf("node z = %v, want f2（引用旧 tab id 的 z 需同步重绑）", nodes[1]["z"])
	}
	if nodes[2]["z"] != "other" {
		t.Errorf("node z = %v, want other（不相关的 z 不动）", nodes[2]["z"])
	}
}

func TestRebindFlowRuntimeID_WrapperFormKeepsTopLevelID(t *testing.T) {
	raw := []byte(`{"id":123,"nodes":[{"id":"f1","type":"tab","label":"Wrapped"},{"id":"n1","type":"inject","z":"f1"}]}`)
	out, err := rebindFlowRuntimeID(raw, "f2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var wrapper struct {
		ID    int64            `json:"id"`
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal(out, &wrapper); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if wrapper.ID != 123 {
		t.Errorf("top-level id = %v, want 123（顶层身份 id 不被改写）", wrapper.ID)
	}
	if wrapper.Nodes[0]["id"] != "f2" || wrapper.Nodes[1]["z"] != "f2" {
		t.Errorf("nodes = %+v, want tab id/z rebound to f2", wrapper.Nodes)
	}
}

func TestRebindFlowRuntimeID_EmptyTabID(t *testing.T) {
	raw := []byte(`[{"id":"","type":"tab","label":"Main"},{"id":"n1","type":"inject","wires":[[]]}]`)
	out, err := rebindFlowRuntimeID(raw, "abcdef0123456789")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var nodes []map[string]any
	if err := json.Unmarshal(out, &nodes); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if nodes[0]["id"] != "abcdef0123456789" {
		t.Errorf("tab node id = %v, want abcdef0123456789", nodes[0]["id"])
	}
	if nodes[1]["id"] != "n1" {
		t.Errorf("non-tab node id = %v, want n1（写回只影响首个 tab 节点）", nodes[1]["id"])
	}
}

func TestRebindFlowRuntimeID_NoTabNodeKeepsRaw(t *testing.T) {
	raw := []byte(`[{"id":"n1","type":"inject"}]`)
	out, err := rebindFlowRuntimeID(raw, "f2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(raw) {
		t.Errorf("raw changed without tab node: got %s", string(out))
	}
}

func TestExtractFlowIdentityID(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int64
	}{
		{"包装形态数字 id", `{"id":123,"nodes":[{"id":"f1","type":"tab"}]}`, 123},
		{"包装形态字符串 id", `{"id":"456","flows":[{"id":"f1","type":"tab"}]}`, 456},
		{"裸 tab 数组无顶层 id", `[{"id":"f1","type":"tab"}]`, 0},
		{"包装形态缺 id", `{"nodes":[{"id":"f1","type":"tab"}]}`, 0},
		{"非法 JSON", `not-json`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractFlowIdentityID([]byte(tc.raw)); got != tc.want {
				t.Errorf("extractFlowIdentityID = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestImportedFlowIdentity_Wrapped(t *testing.T) {
	meta := projectAppMeta{Name: "app-name", DisplayName: "App Display"}
	raw := []byte(`{"id":789,"nodes":[{"id":"f1","type":"tab","label":"Main Flow"},{"id":"n1","type":"inject","z":"f1"}]}`)
	name, identityID, runtimeID := importedFlowIdentity(meta, raw)
	if name != "Main Flow" {
		t.Errorf("name = %q, want Main Flow（取首个 tab label）", name)
	}
	if identityID != 789 {
		t.Errorf("identityID = %d, want 789（顶层 id 为全局身份键）", identityID)
	}
	if runtimeID != "f1" {
		t.Errorf("runtimeID = %q, want f1", runtimeID)
	}
}

// TestPlanFlowImport_TopLevelIDRules 覆盖 BUG-014 的五条用例规则：
// 顶层 id 是全局唯一身份键（平台数据库主键）；flowID 不作为匹配键、不从导入文件覆盖。
func TestPlanFlowImport_TopLevelIDRules(t *testing.T) {
	flow1 := repo.Flow{ID: 100, RuntimeFlowID: "f1", Name: "Flow1", NodeType: 2, ParentID: 10}
	cases := []struct {
		name           string
		identityID     int64
		existing       repo.Flow
		matched        bool
		wantUpdate     bool
		wantTargetID   int64
		wantKeepFlowID string
	}{
		{
			name:           "规则1 相同id不同flowID：更新并保留目标flowID",
			identityID:     100,
			existing:       flow1,
			matched:        true,
			wantUpdate:     true,
			wantTargetID:   100,
			wantKeepFlowID: "f1",
		},
		{
			name:       "规则2 不同id相同flowID：不按flowID命中，新建",
			identityID: 200,
			existing:   repo.Flow{},
			matched:    false,
			wantUpdate: false,
		},
		{
			name:           "规则3 id与flowID分别命中两个Flow：以id为准更新Flow1",
			identityID:     100,
			existing:       flow1,
			matched:        true,
			wantUpdate:     true,
			wantTargetID:   100,
			wantKeepFlowID: "f1",
		},
		{
			name:       "规则4 空id：即使flowID已存在也新建",
			identityID: 0,
			existing:   repo.Flow{},
			matched:    false,
			wantUpdate: false,
		},
		{
			name:       "规则5 非空id不存在但flowID已存在：新建",
			identityID: 999,
			existing:   repo.Flow{},
			matched:    false,
			wantUpdate: false,
		},
		{
			name:           "跨Project命中：更新原Project的Flow，不校验ParentID（EIMP-070）",
			identityID:     100,
			existing:       repo.Flow{ID: 100, RuntimeFlowID: "f1", NodeType: 2, ParentID: 999},
			matched:        true,
			wantUpdate:     true,
			wantTargetID:   100,
			wantKeepFlowID: "f1",
		},
		{
			name:       "命中 folder（node_type=1）：视为未命中，新建",
			identityID: 100,
			existing:   repo.Flow{ID: 100, NodeType: 1},
			matched:    true,
			wantUpdate: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := planFlowImport(tc.identityID, tc.existing, tc.matched)
			if plan.update != tc.wantUpdate {
				t.Fatalf("update = %v, want %v", plan.update, tc.wantUpdate)
			}
			if plan.update {
				if plan.targetID != tc.wantTargetID {
					t.Errorf("targetID = %d, want %d", plan.targetID, tc.wantTargetID)
				}
				if plan.keepRuntimeID != tc.wantKeepFlowID {
					t.Errorf("keepRuntimeID = %q, want %q（保留目标 flowID）", plan.keepRuntimeID, tc.wantKeepFlowID)
				}
			}
		})
	}
}

// TestImportCreatedResources_Merge 新建集合合并保持创建顺序并去重，
// 被顶层 id 命中更新的既有 Flow 不会进入集合（调用方只 append 新建分支的 ID）。
func TestImportCreatedResources_Merge(t *testing.T) {
	var created importCreatedResources
	created.merge(importCreatedResources{UnsNodeIDs: []int64{11, 12}, FlowIDs: []int64{21}})
	created.merge(importCreatedResources{UnsNodeIDs: []int64{12, 13}, FlowIDs: []int64{21, 22}, AssetIDs: []int64{31}})
	if len(created.UnsNodeIDs) != 3 || created.UnsNodeIDs[0] != 11 || created.UnsNodeIDs[1] != 12 || created.UnsNodeIDs[2] != 13 {
		t.Errorf("UnsNodeIDs = %v, want [11 12 13]（去重且保持顺序）", created.UnsNodeIDs)
	}
	if len(created.FlowIDs) != 2 || created.FlowIDs[0] != 21 || created.FlowIDs[1] != 22 {
		t.Errorf("FlowIDs = %v, want [21 22]", created.FlowIDs)
	}
	if len(created.AssetIDs) != 1 || created.AssetIDs[0] != 31 {
		t.Errorf("AssetIDs = %v, want [31]", created.AssetIDs)
	}
}

// TestCheckImportAssetOwned_InvalidIDs 校验归属校验的入参防御：非法 fileID/projectID
// 在触碰 DB 之前直接拒绝（403），不会因缺 DB 而 panic。
func TestCheckImportAssetOwned_InvalidIDs(t *testing.T) {
	ctx := context.Background()
	if err := checkImportAssetOwned(ctx, 0, 5, projectImportAssetOwnerType, projectReplaceAssetOwnerType); !errors.Is(err, domainproject.ErrImportAssetDenied) {
		t.Fatalf("fileID=0 err = %v, want ErrImportAssetDenied", err)
	}
	if err := checkImportAssetOwned(ctx, 5, 0, projectImportAssetOwnerType, projectReplaceAssetOwnerType); !errors.Is(err, domainproject.ErrImportAssetDenied) {
		t.Fatalf("projectID=0 err = %v, want ErrImportAssetDenied", err)
	}
}

// TestFlowCreateDBID 校验新建 Flow 的 DB 主键决策：
// 包内顶层 identityID 非空时保留该 id（同一来源重复导入幂等命中更新语义），
// 空/非正数时返回 0 交由 repo 的 ensureID 生成新 id。
func TestFlowCreateDBID(t *testing.T) {
	if got := flowCreateDBID(0); got != 0 {
		t.Errorf("flowCreateDBID(0) = %d, want 0", got)
	}
	if got := flowCreateDBID(-1); got != 0 {
		t.Errorf("flowCreateDBID(-1) = %d, want 0", got)
	}
	if got := flowCreateDBID(10001); got != 10001 {
		t.Errorf("flowCreateDBID(10001) = %d, want 10001", got)
	}
}

// TestMergeCreatedResourceIDs 校验 Replace 覆盖导入 created 集合的合并规则：
// created_new = (created_old ∩ 本次全量引用) ∪ 本次新建。
// 旧的 created 仍被本次引用则保留归属（删 App 时继续可清理），本次不再引用或
// 由其他来源命中（未出现在本次新建）的既有资源不混入。
func TestMergeCreatedResourceIDs(t *testing.T) {
	cases := []struct {
		name     string
		previous []int64
		current  []int64
		created  []int64
		want     []int64
	}{
		{"Replace 保留仍被引用的旧 created 并并入本次新建", []int64{30}, []int64{30, 40}, []int64{40}, []int64{30, 40}},
		{"旧 created 本次不再引用则移除归属", []int64{50}, []int64{30, 40}, []int64{40}, []int64{40}},
		{"新 App 导入：无旧 created，仅本次新建", nil, []int64{10, 30}, []int64{30}, []int64{30}},
		{"全部命中更新：created 集合保持非空不丢失", []int64{30}, []int64{30}, nil, []int64{30}},
		{"去重：旧 created 与本次新建重复", []int64{30}, []int64{30}, []int64{30}, []int64{30}},
		{"无效 id 过滤", []int64{0, 30}, []int64{30, 0}, []int64{0}, []int64{30}},
		{"空入参", nil, nil, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeCreatedResourceIDs(tc.previous, tc.current, tc.created)
			if !slices.Equal(got, tc.want) {
				t.Errorf("mergeCreatedResourceIDs(%v, %v, %v) = %v, want %v", tc.previous, tc.current, tc.created, got, tc.want)
			}
		})
	}
}

// TestExpandCloudFlowFiles_SplitsCloudWrapper 云端导出的 flows.json 是多 flow 包装
// （{"flows":[{id,flowID,flowName,flowData,flowType,description}]}），须逐条拆出：
// flowName 作显示名、条目 flowType 定 source/event、条目 id 作身份键、description 透传。
func TestExpandCloudFlowFiles_SplitsCloudWrapper(t *testing.T) {
	raw := []byte(`{"flows":[` +
		`{"id":361,"flowID":"tab-a","flowName":"猫猫收藏馆","flowData":"[{\"id\":\"n1\",\"type\":\"inject\",\"z\":\"tab-a\"}]","flowType":1,"description":"source flow"},` +
		`{"id":"362","flowID":"tab-b","flowName":"事件流","flowData":"[{\"id\":\"n2\",\"type\":\"debug\",\"z\":\"tab-b\"}]","flowType":2}` +
		`]}`)
	files := expandCloudFlowFiles([]projectFlowFile{{Path: "flows.json", Name: "flows", Raw: raw, FlowType: 1}})
	if len(files) != 2 {
		t.Fatalf("expected 2 flow files, got %d", len(files))
	}
	first, second := files[0], files[1]
	if first.Name != "猫猫收藏馆" || first.FlowType != 1 || first.IdentityID != 361 || first.Description != "source flow" {
		t.Errorf("unexpected first entry: %+v", first)
	}
	if !strings.Contains(string(first.Raw), `"n1"`) || strings.Contains(string(first.Raw), "flows") {
		t.Errorf("first Raw should be the flowData node array, got %s", string(first.Raw))
	}
	if second.Name != "事件流" || second.FlowType != 2 || second.IdentityID != 362 || second.Description != "" {
		t.Errorf("unexpected second entry: %+v", second)
	}
}

// TestExpandCloudFlowFiles_SplitsSingleCloudEntry update 包顶层单 flow 对象形态
// （{"id","flowID","flowName","flowData",...}，不带 flows 数组包装）同样拆出单条云端条目。
func TestExpandCloudFlowFiles_SplitsSingleCloudEntry(t *testing.T) {
	raw := []byte(`{"id":361,"flowID":"tab-a","flowName":"猫猫收藏馆","flowData":"[{\"id\":\"n1\",\"type\":\"inject\",\"z\":\"tab-a\"}]","flowType":2,"description":"单条"}`)
	files := expandCloudFlowFiles([]projectFlowFile{{Path: "flow.json", Name: "flow", Raw: raw, FlowType: 1}})
	if len(files) != 1 {
		t.Fatalf("expected 1 flow file, got %d", len(files))
	}
	entry := files[0]
	if entry.Name != "猫猫收藏馆" || entry.FlowType != 2 || entry.IdentityID != 361 || entry.Description != "单条" {
		t.Errorf("unexpected entry: %+v", entry)
	}
	if !strings.Contains(string(entry.Raw), `"n1"`) || strings.Contains(string(entry.Raw), "flowData") {
		t.Errorf("Raw should be the flowData node array, got %s", string(entry.Raw))
	}
}

// TestExpandCloudFlowFiles_KeepsNodeRedWrapper BUG-014 的 {"id","flows":[Node-RED 节点]} 包装
// 元素是节点对象（带 type 键、无 flowData），不能误判为云端多 flow 包装。
func TestExpandCloudFlowFiles_KeepsNodeRedWrapper(t *testing.T) {
	raw := []byte(`{"id":123,"flows":[{"id":"f1","type":"tab","label":"Main"},{"id":"n1","type":"inject","z":"f1"}]}`)
	files := expandCloudFlowFiles([]projectFlowFile{{Path: "flow.json", Name: "flow", Raw: raw, FlowType: 1}})
	if len(files) != 1 || string(files[0].Raw) != string(raw) {
		t.Fatalf("BUG-014 wrapper should be kept as-is, got %+v", files)
	}
}

func TestExpandCloudFlowFiles_KeepsBareArray(t *testing.T) {
	raw := []byte(`[{"id":"f1","type":"tab","label":"Main"}]`)
	files := expandCloudFlowFiles([]projectFlowFile{{Path: "flows/a.json", Name: "a", Raw: raw, FlowType: 1}})
	if len(files) != 1 || string(files[0].Raw) != string(raw) {
		t.Fatalf("bare node array should be kept as-is, got %+v", files)
	}
}

func TestImportedFlowUnsNodeIDs_SourceOnly(t *testing.T) {
	ids := []int64{11, 12}
	if got := importedFlowUnsNodeIDs(1, ids); !slices.Equal(got, ids) {
		t.Fatalf("source Flow UNS ids = %v, want %v", got, ids)
	}
	if got := importedFlowUnsNodeIDs(2, ids); got != nil {
		t.Fatalf("event Flow UNS ids = %v, want nil", got)
	}
}

// TestEnsureFlowTabNode_InjectsTabAndRebindsZ 云端 flowData 无 tab 节点：注入 tab（label 取最终名），
// 全部节点的 z 引用统一指向新 flowID。
// TestExpandCloudFlowFiles_KeepsNodeWrapperWithFlowDataMeta 对象同时带顶层 id、nodes 节点数组
// 和非空 flowData 元数据时，不能误判为云端单 flow 条目——否则真实节点会被 flowData 字符串
// 整体替换而丢失（Codex P2：单 flow 识别必须排除含 nodes/flows 键的对象）。
func TestExpandCloudFlowFiles_KeepsNodeWrapperWithFlowDataMeta(t *testing.T) {
	raw := []byte(`{"id":123,"flowData":"meta-value","nodes":[{"id":"n1","type":"inject","z":"f1"}]}`)
	files := expandCloudFlowFiles([]projectFlowFile{{Path: "flow.json", Name: "flow", Raw: raw, FlowType: 1}})
	if len(files) != 1 || string(files[0].Raw) != string(raw) {
		t.Fatalf("node wrapper with flowData metadata should be kept as-is, got %+v", files)
	}
}

func TestEnsureFlowTabNode_InjectsTabAndRebindsZ(t *testing.T) {
	raw := []byte(`[{"id":"n1","type":"inject","z":"old-tab"},{"id":"n2","type":"debug"}]`)
	out, err := ensureFlowTabNode(raw, "new-flow-id", "猫猫收藏馆")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var nodes []map[string]any
	if err := json.Unmarshal(out, &nodes); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected tab + 2 nodes, got %d", len(nodes))
	}
	if nodes[0]["type"] != "tab" || nodes[0]["id"] != "new-flow-id" || nodes[0]["label"] != "猫猫收藏馆" {
		t.Errorf("injected tab = %+v, want id=new-flow-id label=猫猫收藏馆", nodes[0])
	}
	if nodes[1]["z"] != "new-flow-id" || nodes[2]["z"] != "new-flow-id" {
		t.Errorf("z refs not rebound: %+v", nodes)
	}
}

func TestEnsureFlowTabNode_KeepsExistingTab(t *testing.T) {
	raw := []byte(`[{"id":"f1","type":"tab","label":"Main"},{"id":"n1","type":"inject","z":"f1"}]`)
	out, err := ensureFlowTabNode(raw, "new-flow-id", "Main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(raw) {
		t.Errorf("raw changed despite existing tab: got %s", string(out))
	}
}

func TestEnsureFlowTabNode_WrapperWithoutTabKeepsRaw(t *testing.T) {
	raw := []byte(`{"nodes":[{"id":"n1","type":"inject"}]}`)
	out, err := ensureFlowTabNode(raw, "new-flow-id", "Main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(out) != string(raw) {
		t.Errorf("wrapper form should be kept as-is: got %s", string(out))
	}
}

func TestNewImportFlowRootEnsurer_CachesPerFlowType(t *testing.T) {
	calls := make(map[int]int)
	ensure := newImportFlowRootEnsurer(func(flowType int) (repo.Flow, error) {
		calls[flowType]++
		return repo.Flow{ID: int64(100 + flowType)}, nil
	})

	for i := 0; i < 2; i++ {
		root, err := ensure(1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if root.ID != 101 {
			t.Errorf("root.ID = %d, want 101", root.ID)
		}
	}
	if calls[1] != 1 {
		t.Errorf("flowType 1 ensure calls = %d, want 1", calls[1])
	}

	root, err := ensure(2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root.ID != 102 {
		t.Errorf("root.ID = %d, want 102", root.ID)
	}
	if calls[2] != 1 {
		t.Errorf("flowType 2 ensure calls = %d, want 1", calls[2])
	}
}

func TestRewriteImportedMQTTBrokerNodes(t *testing.T) {
	localClient := repo.ConnectClient{ID: 123, ClientID: "local-client", Username: "local-user", Password: "local-pass"}

	decodeNodes := func(t *testing.T, raw []byte) []map[string]any {
		t.Helper()
		var nodes []map[string]any
		if err := json.Unmarshal(raw, &nodes); err != nil {
			t.Fatalf("unmarshal output: %v", err)
		}
		return nodes
	}
	findBroker := func(t *testing.T, nodes []map[string]any) map[string]any {
		t.Helper()
		for _, n := range nodes {
			if n["type"] == "mqtt-broker" {
				return n
			}
		}
		t.Fatalf("mqtt-broker node not found in output: %s", mustJSON(nodes))
		return nil
	}

	t.Run("saas plain rewritten", func(t *testing.T) {
		raw := []byte(`[{"id":"b1","type":"mqtt-broker","broker":"mqtt.pre.tier0.dev","port":"1883","usetls":false}]`)
		nodes := decodeNodes(t, rewriteImportedMQTTBrokerNodes(raw, localClient))
		b := findBroker(t, nodes)
		if b["broker"] != "emqx" || b["port"] != "1883" || b["usetls"] != false {
			t.Fatalf("broker node not rewritten: %v", b)
		}
	})

	t.Run("saas tls rewritten", func(t *testing.T) {
		raw := []byte(`[{"id":"b2","type":"mqtt-broker","broker":"mqtt.pre.tier0.dev","port":"8883","usetls":true,"tls":"tls_tier0_public"}]`)
		nodes := decodeNodes(t, rewriteImportedMQTTBrokerNodes(raw, localClient))
		b := findBroker(t, nodes)
		if b["broker"] != "emqx" || b["port"] != "1883" {
			t.Fatalf("broker/port not rewritten: %v", b)
		}
		if b["usetls"] != false {
			t.Fatalf("usetls should be false: %v", b)
		}
		if _, ok := b["tls"]; ok {
			t.Fatalf("tls should be removed: %v", b)
		}
	})

	t.Run("credentials rewritten", func(t *testing.T) {
		raw := []byte(`[{"id":"b3","type":"mqtt-broker","broker":"mqtt.pre.tier0.dev","port":"1883","clientid":"saas-client","credentials":{"user":"saas-user","password":"saas-pass"}}]`)
		nodes := decodeNodes(t, rewriteImportedMQTTBrokerNodes(raw, localClient))
		b := findBroker(t, nodes)
		if b["clientid"] != "local-client" {
			t.Fatalf("clientid not rewritten: %v", b["clientid"])
		}
		creds, _ := b["credentials"].(map[string]any)
		if creds["user"] != "local-user" || creds["password"] != "local-pass" {
			t.Fatalf("credentials not rewritten: %v", creds)
		}
	})

	t.Run("credentials cleared when no local client", func(t *testing.T) {
		raw := []byte(`[{"id":"b4","type":"mqtt-broker","broker":"mqtt.pre.tier0.dev","port":"1883","clientid":"saas-client","credentials":{"user":"saas-user","password":"saas-pass"}}]`)
		nodes := decodeNodes(t, rewriteImportedMQTTBrokerNodes(raw, repo.ConnectClient{}))
		b := findBroker(t, nodes)
		if _, ok := b["clientid"]; ok {
			t.Fatalf("clientid should be removed when no local client: %v", b)
		}
		if _, ok := b["credentials"]; ok {
			t.Fatalf("credentials should be removed when no local client: %v", b)
		}
	})

	t.Run("local addresses normalized", func(t *testing.T) {
		for _, addr := range []string{"127.0.0.1", "localhost", "::1"} {
			raw := []byte(`[{"id":"b5","type":"mqtt-broker","broker":"` + addr + `","port":"1883"}]`)
			nodes := decodeNodes(t, rewriteImportedMQTTBrokerNodes(raw, localClient))
			b := findBroker(t, nodes)
			if b["broker"] != "emqx" || b["port"] != "1883" {
				t.Fatalf("addr %q not normalized: %v", addr, b)
			}
		}
	})

	t.Run("host field replaced", func(t *testing.T) {
		raw := []byte(`[{"id":"b6","type":"mqtt-broker","host":"mqtt.pre.tier0.dev","port":"1883"}]`)
		nodes := decodeNodes(t, rewriteImportedMQTTBrokerNodes(raw, localClient))
		b := findBroker(t, nodes)
		if b["broker"] != "emqx" {
			t.Fatalf("broker should be set: %v", b)
		}
		if _, ok := b["host"]; ok {
			t.Fatalf("host should be removed: %v", b)
		}
	})

	t.Run("external broker preserved", func(t *testing.T) {
		raw := []byte(`[{"id":"b7","type":"mqtt-broker","broker":"broker.customer.example.com","port":"8883","usetls":true,"tls":"customer_tls","clientid":"cust-client","credentials":{"user":"cust-user","password":"cust-pass"}}]`)
		nodes := decodeNodes(t, rewriteImportedMQTTBrokerNodes(raw, localClient))
		b := findBroker(t, nodes)
		if b["broker"] != "broker.customer.example.com" || b["port"] != "8883" || b["usetls"] != true {
			t.Fatalf("external broker should not be localized: %v", b)
		}
		if b["tls"] != "customer_tls" || b["clientid"] != "cust-client" {
			t.Fatalf("external broker credentials should be preserved: %v", b)
		}
		creds, _ := b["credentials"].(map[string]any)
		if creds["user"] != "cust-user" || creds["password"] != "cust-pass" {
			t.Fatalf("external broker credentials should be preserved: %v", creds)
		}
	})

	t.Run("non broker nodes unchanged", func(t *testing.T) {
		raw := []byte(`[{"id":"n1","type":"inject","payload":"hello"},{"id":"b8","type":"mqtt-broker","broker":"mqtt.pre.tier0.dev","port":"1883"}]`)
		nodes := decodeNodes(t, rewriteImportedMQTTBrokerNodes(raw, localClient))
		if nodes[0]["payload"] != "hello" || nodes[0]["type"] != "inject" {
			t.Fatalf("non broker node changed: %v", nodes[0])
		}
	})

	t.Run("no broker nodes returns as-is", func(t *testing.T) {
		raw := []byte(`[{"id":"n2","type":"inject"}]`)
		out := rewriteImportedMQTTBrokerNodes(raw, localClient)
		if string(out) != string(raw) {
			t.Fatalf("expected unchanged, got %s", out)
		}
	})

	t.Run("invalid json returns as-is", func(t *testing.T) {
		raw := []byte(`{not-json`)
		out := rewriteImportedMQTTBrokerNodes(raw, localClient)
		if string(out) != string(raw) {
			t.Fatalf("expected unchanged, got %s", out)
		}
	})
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestNewImportFlowRootEnsurer_ErrorNotCached(t *testing.T) {
	calls := 0
	wantErr := errors.New("ensure failed")
	ensure := newImportFlowRootEnsurer(func(flowType int) (repo.Flow, error) {
		calls++
		if calls == 1 {
			return repo.Flow{}, wantErr
		}
		return repo.Flow{ID: 101}, nil
	})

	if _, err := ensure(1); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	// 首次报错不缓存，第二次调用应重试并成功
	root, err := ensure(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root.ID != 101 {
		t.Errorf("root.ID = %d, want 101", root.ID)
	}
	if calls != 2 {
		t.Errorf("ensure calls = %d, want 2", calls)
	}
}

// TestRewriteImportedMQTTBrokerNodes_CollapsesMultipleBrokers 验证：
// 同一导入 flow 内有多个可本地化 mqtt-broker 节点时，合并为单个 canonical——
// canonical 生成唯一新 id，其余命中节点被删除，mqtt in/out 的 broker 引用重绑到新 id，
// 只对 canonical 写入本地 EMQX 连接信息与凭证；自定义/外部 broker 原样保留；
// 非 broker 引用字段（inject payload 恰等于旧 broker id）不被误改。
func TestRewriteImportedMQTTBrokerNodes_CollapsesMultipleBrokers(t *testing.T) {
	input := `[
		{"id":"b1","type":"mqtt-broker","name":"emqx","broker":"mqtt.pre.tier0.dev","port":"8883","usetls":true,"tls":"tls_x"},
		{"id":"b2","type":"mqtt-broker","name":"emqx","broker":"mqtt.pre.tier0.dev","port":"8883","usetls":true,"tls":"tls_x"},
		{"id":"b3","type":"mqtt-broker","name":"Customer Broker","broker":"broker.customer.example.com","port":"8883","usetls":true,"tls":"cust"},
		{"id":"n1","type":"mqtt in","broker":"b1","topic":"t/x"},
		{"id":"n2","type":"mqtt out","broker":"b2","topic":"t/y"},
		{"id":"n3","type":"mqtt in","broker":"b3","topic":"t/z"},
		{"id":"n4","type":"inject","payload":"b1"},
		{"id":"n5","type":"subflow","env":[{"name":"broker","type":"mqtt-broker","value":"b1"}]},
		{"id":"n6","type":"subflow","env":[{"name":"label","type":"str","value":"b2"}]}
	]`
	client := repo.ConnectClient{ID: 1, ClientID: "local-cid", Username: "local-user", Password: "local-pass"}
	out := rewriteImportedMQTTBrokerNodes([]byte(input), client)

	var nodes []map[string]any
	if err := json.Unmarshal(out, &nodes); err != nil {
		t.Fatalf("invalid output: %v", err)
	}
	byID := map[string]map[string]any{}
	for _, n := range nodes {
		id, _ := n["id"].(string)
		byID[id] = n
	}

	// 可本地化 broker 收敛为 1 个（b1/b2 合并）；自定义 b3 保留
	var localized []string
	for _, n := range nodes {
		if n["type"] == "mqtt-broker" && n["broker"] == "emqx" {
			localized = append(localized, n["id"].(string))
		}
	}
	if len(localized) != 1 {
		t.Fatalf("expected 1 localized broker after collapse, got %d: %v", len(localized), localized)
	}
	canonicalID := localized[0]
	if _, ok := byID["b1"]; ok {
		t.Fatalf("b1 still present")
	}
	if _, ok := byID["b2"]; ok {
		t.Fatalf("b2 still present")
	}
	if _, ok := byID["b3"]; !ok {
		t.Fatalf("b3 custom broker should be preserved")
	}

	// canonical 写入本地连接信息与凭证
	canon := byID[canonicalID]
	if got := canon["port"]; got != "1883" {
		t.Fatalf("canonical port = %v, want 1883", got)
	}
	if got := canon["usetls"]; got != false {
		t.Fatalf("canonical usetls = %v, want false", got)
	}
	if _, ok := canon["tls"]; ok {
		t.Fatalf("canonical tls should be removed")
	}
	if got := canon["clientid"]; got != "local-cid" {
		t.Fatalf("canonical clientid = %v, want local-cid", got)
	}
	creds, _ := canon["credentials"].(map[string]any)
	if creds["user"] != "local-user" || creds["password"] != "local-pass" {
		t.Fatalf("canonical credentials not localized: %v", creds)
	}

	// mqtt in/out 引用全部重绑到 canonical 新 id
	for _, id := range []string{"n1", "n2"} {
		ref, _ := byID[id]["broker"].(string)
		if ref != canonicalID {
			t.Fatalf("%s broker reference = %q, want %q", id, ref, canonicalID)
		}
	}
	// n3 仍指向保留的 b3
	if ref, _ := byID["n3"]["broker"].(string); ref != "b3" {
		t.Fatalf("n3 broker reference = %q, want b3", ref)
	}
	// 非 mqtt 节点的非引用字段（payload 恰为旧 broker id）不被误改
	if got := byID["n4"]["payload"]; got != "b1" {
		t.Fatalf("n4 payload should stay untouched (non-reference field), got %v", got)
	}
	// env 中 type=="mqtt-broker" 的 value 被重绑到 canonical
	n5env := byID["n5"]["env"].([]any)
	if got := n5env[0].(map[string]any)["value"]; got != canonicalID {
		t.Fatalf("n5 env mqtt-broker value should be rebound to canonical %q, got %v", canonicalID, got)
	}
	// env 中非 mqtt-broker 类型的 value 即使等于旧 id 也不被改
	n6env := byID["n6"]["env"].([]any)
	if got := n6env[0].(map[string]any)["value"]; got != "b2" {
		t.Fatalf("n6 env str value should stay b2, got %v", got)
	}
}

// TestRewriteImportedMQTTBrokerNodes_NoHitKeepsRaw 验证：
// flow 内没有可本地化 broker 时（全部外部/自定义），输入原样保留（等值 JSON 语义）。
func TestRewriteImportedMQTTBrokerNodes_NoHitKeepsRaw(t *testing.T) {
	input := `[
		{"id":"b1","type":"mqtt-broker","broker":"broker.customer.example.com","port":"8883"},
		{"id":"n1","type":"mqtt in","broker":"b1","topic":"t/x"}
	]`
	client := repo.ConnectClient{ID: 1, ClientID: "local-cid", Username: "local-user", Password: "local-pass"}
	out := rewriteImportedMQTTBrokerNodes([]byte(input), client)

	var got, want []map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("invalid output: %v", err)
	}
	if err := json.Unmarshal([]byte(input), &want); err != nil {
		t.Fatalf("invalid input: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i]["id"] != want[i]["id"] || got[i]["broker"] != want[i]["broker"] {
			t.Fatalf("node %d changed: got %v want %v", i, got[i], want[i])
		}
	}
}

// TestRewriteImportedMQTTBrokerNodes_CollapsesAcrossScopes 验证：
// 同一 flow 内可本地化 broker 即使分布在主 tab（无 z）与 subflow（z=subflow id）两个
// scope，也统一收敛为单一 canonical。Node-RED 的 mqtt-broker 是 flow 级全局 config
// 节点（无 z 时全局可见），subflow 实例经 env/z 引用主 tab 的 config 是标准用法；
// 保留多个同 clientid 的 canonical 会让 Node-RED 各建连接、EMQX 按 clientid 互踢
// （session takeover）。
func TestRewriteImportedMQTTBrokerNodes_CollapsesAcrossScopes(t *testing.T) {
	input := `[
		{"id":"b1","type":"mqtt-broker","broker":"mqtt.pre.tier0.dev","port":"8883"},
		{"id":"b2","type":"mqtt-broker","broker":"mqtt.pre.tier0.dev","port":"8883"},
		{"id":"b3","type":"mqtt-broker","broker":"mqtt.pre.tier0.dev","port":"8883","z":"subflow1"},
		{"id":"b4","type":"mqtt-broker","broker":"mqtt.pre.tier0.dev","port":"8883","z":"subflow1"},
		{"id":"n1","type":"mqtt in","broker":"b1","topic":"t/main"},
		{"id":"n2","type":"mqtt out","broker":"b2","topic":"t/main2"},
		{"id":"n3","type":"mqtt in","broker":"b3","topic":"t/sub","z":"subflow1"},
		{"id":"n4","type":"mqtt out","broker":"b4","topic":"t/sub2","z":"subflow1"}
	]`
	client := repo.ConnectClient{ID: 1, ClientID: "local-cid", Username: "local-user", Password: "local-pass"}
	out := rewriteImportedMQTTBrokerNodes([]byte(input), client)

	var nodes []map[string]any
	if err := json.Unmarshal(out, &nodes); err != nil {
		t.Fatalf("invalid output: %v", err)
	}
	byID := map[string]map[string]any{}
	for _, n := range nodes {
		id, _ := n["id"].(string)
		byID[id] = n
	}

	// 主 tab scope 与 subflow scope 统一收敛为 1 个本地化 broker（不跨 scope 保留多个）
	var localized []string
	for _, n := range nodes {
		if n["type"] == "mqtt-broker" && n["broker"] == "emqx" {
			localized = append(localized, n["id"].(string))
		}
	}
	if len(localized) != 1 {
		t.Fatalf("expected 1 localized broker across scopes, got %d: %v", len(localized), localized)
	}
	canonicalID := localized[0]

	// 主 tab 与 subflow 的 mqtt 节点引用同一 canonical（config 节点全局共享）
	for _, id := range []string{"n1", "n2", "n3", "n4"} {
		if byID[id]["broker"] != canonicalID {
			t.Fatalf("%s should reference canonical %q, got %v", id, canonicalID, byID[id]["broker"])
		}
	}
	// 旧 id b1/b2/b3/b4 都不应存在
	for _, old := range []string{"b1", "b2", "b3", "b4"} {
		if _, ok := byID[old]; ok {
			t.Fatalf("old broker id %q still present", old)
		}
	}
}

// TestRewriteImportedMQTTBrokerNodes_CanonicalStaysGlobal 验证：
// canonical 必须落在全局 scope（无 z），否则部署时 splitGlobalNodes 不会把它当全局
// config 节点，主 tab 的 mqtt 节点重绑过来会解析不到 broker。
// 场景 1：subflow 内 broker 排在全局 broker 之前 → canonical 仍选全局的那个。
// 场景 2：全部命中者都带 z → canonical 取第一个并清掉 z。
func TestRewriteImportedMQTTBrokerNodes_CanonicalStaysGlobal(t *testing.T) {
	client := repo.ConnectClient{ID: 1, ClientID: "local-cid", Username: "local-user", Password: "local-pass"}
	decode := func(t *testing.T, raw string) map[string]map[string]any {
		t.Helper()
		var nodes []map[string]any
		if err := json.Unmarshal(rewriteImportedMQTTBrokerNodes([]byte(raw), client), &nodes); err != nil {
			t.Fatalf("invalid output: %v", err)
		}
		byID := map[string]map[string]any{}
		for _, n := range nodes {
			id, _ := n["id"].(string)
			byID[id] = n
		}
		return byID
	}

	t.Run("subflow broker listed first, global one wins", func(t *testing.T) {
		input := `[
			{"id":"b1","type":"mqtt-broker","broker":"mqtt.pre.tier0.dev","port":"8883","z":"subflow1"},
			{"id":"b2","type":"mqtt-broker","broker":"mqtt.pre.tier0.dev","port":"8883"},
			{"id":"n1","type":"mqtt in","broker":"b1","topic":"t/sub","z":"subflow1"},
			{"id":"n2","type":"mqtt out","broker":"b2","topic":"t/main"}
		]`
		byID := decode(t, input)
		canonicalID := byID["n2"]["broker"].(string)
		canon := byID[canonicalID]
		if canon == nil {
			t.Fatalf("canonical broker node missing")
		}
		if _, hasZ := canon["z"]; hasZ {
			t.Fatalf("canonical should be global (no z), got z=%v", canon["z"])
		}
		// subflow 内的 mqtt 节点也重绑到同一全局 canonical
		if byID["n1"]["broker"] != canonicalID {
			t.Fatalf("n1 should reference canonical %q, got %v", canonicalID, byID["n1"]["broker"])
		}
		// 旧 id 都消失
		for _, old := range []string{"b1", "b2"} {
			if _, ok := byID[old]; ok {
				t.Fatalf("old broker id %q still present", old)
			}
		}
	})

	t.Run("all brokers scoped, canonical z cleared", func(t *testing.T) {
		input := `[
			{"id":"b1","type":"mqtt-broker","broker":"mqtt.pre.tier0.dev","port":"8883","z":"subflow1"},
			{"id":"b2","type":"mqtt-broker","broker":"mqtt.pre.tier0.dev","port":"8883","z":"subflow1"},
			{"id":"n1","type":"mqtt in","broker":"b1","topic":"t/sub","z":"subflow1"}
		]`
		byID := decode(t, input)
		canonicalID := byID["n1"]["broker"].(string)
		canon := byID[canonicalID]
		if canon == nil {
			t.Fatalf("canonical broker node missing")
		}
		if _, hasZ := canon["z"]; hasZ {
			t.Fatalf("canonical z should be cleared to become global, got z=%v", canon["z"])
		}
	})
}

// TestRegenerateImportedFlowNodeIDs 验证导入新建路径的节点 id 重生成：
// 除 tab 外全部节点 id 重生成且互不重复；wires/broker/env/subflow: 引用同步重映射；
// tab id 不动；id 与 name 同值的节点 name 保持原值不被引用替换误改；
// 与 broker 本地化串联时仍能被正常识别收敛。
func TestRegenerateImportedFlowNodeIDs(t *testing.T) {
	input := `[
		{"id":"tab1","type":"tab","label":"f"},
		{"id":"emqx","type":"mqtt-broker","name":"emqx","broker":"mqtt.pre.tier0.dev","port":"8883"},
		{"id":"sf1","type":"subflow","name":"sub"},
		{"id":"n1","type":"inject","z":"tab1","wires":[["n2"]]},
		{"id":"n2","type":"mqtt out","z":"tab1","broker":"emqx","topic":"t"},
		{"id":"n3","type":"subflow:sf1","z":"tab1","env":[{"name":"broker","type":"mqtt-broker","value":"emqx"}]}
	]`
	out, err := regenerateImportedFlowNodeIDs([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var nodes []map[string]any
	if err := json.Unmarshal(out, &nodes); err != nil {
		t.Fatalf("invalid output: %v", err)
	}
	origIDs := map[string]bool{"tab1": true, "emqx": true, "sf1": true, "n1": true, "n2": true, "n3": true}
	byType := map[string]map[string]any{}
	seen := map[string]struct{}{}
	for _, n := range nodes {
		typ, _ := n["type"].(string)
		byType[typ] = n
		id, _ := n["id"].(string)
		if typ == "tab" {
			if id != "tab1" {
				t.Fatalf("tab id should stay tab1, got %q", id)
			}
			continue
		}
		if origIDs[id] {
			t.Fatalf("node id %q was not regenerated", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate regenerated id %q", id)
		}
		seen[id] = struct{}{}
	}

	broker := byType["mqtt-broker"]
	newBrokerID, _ := broker["id"].(string)
	// name 保持 "emqx"（id 与 name 同值时不被引用替换误改）
	if got := broker["name"]; got != "emqx" {
		t.Fatalf("broker name clobbered: got %v, want emqx", got)
	}
	// mqtt out 的 broker 引用重映射
	if got := byType["mqtt out"]["broker"]; got != newBrokerID {
		t.Fatalf("mqtt out broker ref = %v, want %q", got, newBrokerID)
	}
	// wires 重映射
	wires := byType["inject"]["wires"].([]any)
	wire0 := wires[0].([]any)[0].(string)
	mqttOutID, _ := byType["mqtt out"]["id"].(string)
	if wire0 != mqttOutID {
		t.Fatalf("wire target %q != regenerated mqtt out id %q", wire0, mqttOutID)
	}
	// subflow 定义与实例引用重映射
	subInst := byType["subflow:sf1"]
	if subInst == nil {
		for _, n := range nodes {
			if typ, _ := n["type"].(string); strings.HasPrefix(typ, "subflow:") {
				subInst = n
			}
		}
	}
	if subInst == nil {
		t.Fatalf("subflow instance node missing")
	}
	subDefID, _ := byType["subflow"]["id"].(string)
	if got := subInst["type"]; got != "subflow:"+subDefID {
		t.Fatalf("subflow instance type = %v, want subflow:%q", got, subDefID)
	}
	// env 里的 mqtt-broker 引用重映射
	envVal := subInst["env"].([]any)[0].(map[string]any)["value"]
	if envVal != newBrokerID {
		t.Fatalf("env broker ref = %v, want %q", envVal, newBrokerID)
	}

	// 与本地化串联：name 保持 "emqx" 所以 rewriteImportedMQTTBrokerNodes 仍能本地化
	client := repo.ConnectClient{ID: 1, ClientID: "local-cid", Username: "local-user", Password: "local-pass"}
	locOut := rewriteImportedMQTTBrokerNodes(out, client)
	var locNodes []map[string]any
	if err := json.Unmarshal(locOut, &locNodes); err != nil {
		t.Fatalf("invalid localize output: %v", err)
	}
	localized := 0
	for _, n := range locNodes {
		if n["type"] == "mqtt-broker" && n["broker"] == "emqx" && n["clientid"] == "local-cid" {
			localized++
		}
	}
	if localized != 1 {
		t.Fatalf("expected 1 localized broker after regen+localize, got %d", localized)
	}
}
