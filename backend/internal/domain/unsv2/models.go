package unsv2

import "sort"

func publicModelOperations(resource string) (operations, actions []string) {
	for _, op := range ResourceOperations() {
		if op.Resource != resource {
			continue
		}
		operations = append(operations, op.Name)
		action := op.MetadataAction
		if action == "" {
			action = op.NovaAction
		}
		if action == "" {
			action = op.Mutation
		}
		if action != "" && !contains(actions, action) {
			actions = append(actions, action)
		}
	}
	sort.Strings(operations)
	sort.Strings(actions)
	return
}

// 模型信息属于共享 UNS 契约；HTTP catalog、生成器和 MCP 使用同一来源。
type ModelInfo struct {
	Kind            string   `json:"kind"`
	Storage         string   `json:"storage"`
	Schema          string   `json:"schema,omitempty"`
	ReadPermission  string   `json:"readPermission"`
	WritePermission string   `json:"writePermission,omitempty"`
	Operations      []string `json:"operations"`
	Actions         []string `json:"actions"`
	ReadOnlyFields  []string `json:"readOnlyFields"`
	Rules           []string `json:"rules"`
	Boundary        string   `json:"boundary,omitempty"`
}

func Models() []ModelInfo {
	metaOps := []string{"metadata", "references", "apply"}
	configActions := []string{"meta.create", "meta.replace", "meta.patch", "meta.resourcePatch"}
	models := []ModelInfo{
		{"directory", "uns_meta", "DirectoryData", "uns.read", "uns.write", []string{"namespace", "metadata", "references", "apply"}, append(append([]string{}, configActions...), "meta.copy", "meta.archive", "meta.restore"), []string{"id", "kind", "revision", "createdAt", "updatedAt", "deletedAt"}, []string{"parentId references a directory or null root; never a Topic", "One parent per node; parent directory has many children; cycles forbidden", "Copy/rename/move changes placement; Collection/Record/Sample identities are retained", "Recycle retains Collection data and references; a directory recycles its active subtree and restores the same batch"}, ""},
		{"topic", "uns_meta", "TopicData", "uns.read", "uns.write", []string{"namespace", "metadata", "topics", "references", "apply"}, append(append([]string{}, configActions...), "meta.copy", "meta.archive", "meta.restore"), []string{"id", "kind", "revision", "createdAt", "updatedAt", "deletedAt"}, []string{"Leaf node: cannot have children; parentId is directory or null", "viewMode=records uses one collectionId: Topics N:1 Collection", "viewMode=fields uses explicit fields (kind=record/sample, collectionId, recordId, fieldPath), describing one object, not an inferred multi-row join", "Record field reads current Record; Sample field reads that Record's latest sample; missing is not zero", "Move/copy retains data; archive blocked by retained references"}, ""},
		{"collection", "uns_meta", "CollectionData", "uns.read", "uns.write", append(metaOps, "collections", "records", "samples"), append(append([]string{}, configActions...), "meta.archive", "meta.restore"), []string{"id", "kind", "revision", "createdAt", "updatedAt", "deletedAt"}, []string{"Defines recordSchema and optional sampleSchema, not a physical table per collection", "Collection 1:N Record; Record 1:N Sample by (collectionId,recordId)", "Business-only collections omit sampleSchema; measurements define both schemas", "Schema changes must accept retained data and obey Source/Topic references; full replacement is not a schema migration", "Collections occupy Namespace positions via parentId; copying a directory creates explicit Topic references without copying data"}, ""},
		{"source", "uns_meta", "SourceData", "uns.source", "uns.source.manage", metaOps, append(append([]string{}, configActions...), "meta.archive", "meta.restore"), []string{"configRevision", "credentials"}, []string{"Source owns connection, protocol resources and credential references only", "Acquisition and processing belong to Stream; storage belongs to an independent Collection policy", "Edge Source references an enrolled Edge and exposes its authorized Stream outputs; use an ordinary center Stream to consume them", "Credentials remain on their dedicated API"}, "Configuration changes retain revision and permission guards"},
		{"stream", "uns_meta", "StreamData", "uns.stream", "uns.stream.manage", metaOps, append(append([]string{}, configActions...), "stream.publish"), []string{"releases"}, []string{"Draft defines processor and output schemas, plus optional execution", "Saving a draft never executes; publication creates an immutable numbered release", "Edge execution references a published Source revision and an output Collection; storage is configured independently on that Collection", "A published Edge release executes only on that Edge; changing a draft does not change running configuration", "Processor uses bounded JavaScript process(input,params,context); unrelated edits retain hidden code"}, "Uses the existing acquisition and Stream runtime; no separately editable acquisition binding"},
		{"app", "uns_meta", "AppData", "uns.app.read", "uns.app.manage", append(metaOps, "appContext", "appKeyCreate", "appKeyRevoke"), configActions, []string{}, []string{"collections[] explicitly grants collectionId, alias and record:read/write/delete or sample:read/write", "App N:M Collection; grants cannot exceed issuer permissions; roles never add collection permissions", "platformPermissions[] grants explicit platform capabilities independently of Collection grants; omitted or empty means no platform authorization", "Platform capabilities must be allowlisted and within issuer resources; effective access intersects current App grants, key resources and user resources", "Platform operations recheck the active App and its current grants on every request; removing a grant immediately prevents its use by existing keys", "navigation.topicIds are entry points only, not data grants", "members[] references platform users; roleIds reference this App's roles, members N:M roles", "MCP manages Collection grants/navigation, preserves members/roles; credential APIs are separate; MCP has no consent for platformPermissions"}, "MCP cannot manage members/roles or platform permissions, issue keys, or impersonate an App by request appId"},
		{"record", "uns_record", "RecordDocument", "uns.read", "uns.write", []string{"recordFind", "recordCount", "recordDistinct", "recordAggregatePipeline", "recordTransactionExecute", "recordLock", "recordRenew", "recordUnlock", "recordRestore"}, []string{"record.create", "record.replace", "record.delete", "record.restore", "record.check", "record.read", "record.query", "record.modify", "record.update"}, []string{"revision", "createdAt", "updatedAt", "sourceTime", "lock"}, []string{"Identity is (collectionId,id); sourceId may be null for business data", "data must match the Collection recordSchema and indexes; Record revisions are optional", "record.check/read require collection read permission; shared filters support bounded declared field, array and expression predicates", "Document actions support find/count/distinct, update operators and single upsert, ordered/atomic bulk writes; queries use bare business fields and _id/_meta.* system fields", "Ordinary responses and write snapshots share RecordDocument; aggregate output is defined by its pipeline", "Explicit bindings reference permitted prior results through the existing dependency graph; no arbitrary scripts or SQL", "Record transactions lock participating identities through commit; leases require current principal tokens and support guarded release", "Deletion requires a separate permission and retains a tombstone; restore accepts 1 to 100 IDs and rechecks schema and active unique indexes; retained Samples keep their Record identity"}, ""},
		{"sample", "uns_sample", "SampleDocument", "uns.read", "uns.write", []string{"sampleFind", "sampleAggregate", "sampleLatestValues", "sampleAppend", "sampleDeleteRange"}, []string{"sample.append"}, []string{"receivedAt"}, []string{"Append requires existing (collectionId,recordId) and Collection sampleSchema", "Queries use bare business paths plus _id and _meta.* system fields; time and receivedAt are Unix milliseconds", "Find and aggregate support an optional 1 to 20 recordIds selection and explicit [from,to) range; omitted recordIds selects the Collection", "Find and latest share SampleDocument; latest requires recordIds and returns a real Sample or null for each requested Record", "Delete requires explicit recordIds and [from,to); dryRun counts matching Samples without deletion"}, "Sample updates and replacements are unavailable"},
		{"release", "stream.data.releases", "Release", "uns.stream", "uns.stream.manage", []string{"metadata", "apply"}, []string{"stream.publish"}, []string{"publishedAt", "definition"}, []string{"Identity is (streamId,release); immutable snapshot created by stream.publish", "Query the owning Stream; use expectedRevision and dependsOn before publishing a dependent Stream"}, "Published configuration, not deployment or execution"},
		{"source_state", "uns_meta", "", "uns.source", "", []string{"metadata"}, []string{}, []string{"*"}, []string{"Same id as Source, different kind; runtime owns state/cursor/ACK and instance states"}, "Read-only runtime projection"},
		{"task", "uns_meta", "", "uns.read", "", []string{"jobs"}, []string{}, []string{"*"}, []string{"Batch jobs are owner-scoped; job queries validate current permissions for every operation"}, "Only safe owned batch-job status; no arbitrary task mutation"},
		{"source_plan", "uns_meta", "", "uns.source", "", []string{}, []string{}, []string{"*"}, []string{}, "System-managed; no public CRUD contract enabled"},
		{"source_version", "uns_meta", "", "uns.source", "", []string{}, []string{}, []string{"*"}, []string{}, "System-managed version/tombstone; no public CRUD contract enabled"},
		{"subscription", "uns_meta", "", "uns.read", "", []string{}, []string{}, []string{"*"}, []string{}, "No public subscription CRUD contract enabled"},
	}
	for i := range models {
		m := &models[i]
		switch m.Kind {
		case "directory", "topic", "collection":
			m.Operations, m.Actions = publicModelOperations("metadata")
			m.Operations = append(m.Operations, "references")
			if m.Kind != "collection" {
				m.Operations = append(m.Operations, "namespace", "namespaceCopy")
				m.Actions = append(m.Actions, "meta.copy")
			}
			m.Rules = append(m.Rules, "Metadata document actions retain schema, reference, revision and mounted read-only guards; only bulkWrite permits independent commits")
		case "source", "stream", "app", "record", "sample":
			m.Operations, m.Actions = publicModelOperations(m.Kind)
		case "source_state":
			m.Operations = []string{"sourceRuntimeGet"}
		case "release":
			m.Operations = []string{"streamReleasesList", "streamReleasesGet", "streamPublish"}
		}
	}
	return models
}

func Model(kind string) (ModelInfo, bool) {
	for _, m := range Models() {
		if m.Kind == kind {
			return m, true
		}
	}
	return ModelInfo{}, false
}

func Relations() []map[string]any {
	return []map[string]any{
		{"from": "directory", "path": "data.parentId", "to": "directory", "cardinality": "N:0..1", "order": "parent before child", "constraint": "no cycles; parent cannot be topic"},
		{"from": "topic", "path": "data.parentId", "to": "directory", "cardinality": "N:0..1", "order": "directory before topic"},
		{"from": "topic", "path": "data.collectionId", "to": "collection", "cardinality": "N:1", "when": "viewMode=records", "order": "collection before topic"},
		{"from": "topic", "path": "data.fields[]", "to": "record/sample", "cardinality": "N:M field references", "key": []string{"collectionId", "recordId", "fieldPath", "kind"}, "constraint": "explicit object fields, not an inferred relational join"},
		{"from": "record", "path": "collectionId", "to": "collection", "cardinality": "N:1", "order": "collection before record"},
		{"from": "sample", "path": "collectionId + recordId", "to": "record", "cardinality": "N:1", "order": "record before sample"},
		{"from": "stream", "path": "data.draft.execution.sourceId", "to": "source", "cardinality": "N:1", "order": "publish Source before Stream", "constraint": "sourceRevision must match at publication"},
		{"from": "stream", "path": "data.draft.execution.target.collectionId", "to": "collection", "cardinality": "N:1", "when": "Edge execution is configured"},
		{"from": "app", "path": "data.collections[]", "to": "collection", "cardinality": "N:M", "constraint": "alias unique inside App; permissions do not exceed issuer"},
		{"from": "app", "path": "data.navigation.topicIds[]", "to": "topic", "cardinality": "N:M", "constraint": "navigation is not permission"},
	}
}
