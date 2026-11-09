package unsv2

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"backend/internal/domain/unsmeta"
	"backend/internal/infra/idgen"
	"github.com/google/uuid"
)

type plan struct {
	AssertLocks []RecordLockAssertion `json:"assertLocks,omitempty"`
	Items       []Change              `json:"items"`
	Order       []int                 `json:"order"`
	Cost        int                   `json:"cost"`
}

func newPlan(batch Batch) (planned *plan, planError error) {
	currentIndex := -1
	defer func() {
		if planError != nil && currentIndex >= 0 {
			planError = itemFault(currentIndex, batch.Items[currentIndex], planError)
		}
	}()
	if len(batch.Items) < 1 || len(batch.Items) > MaxItems {
		return nil, invalid("items", "1 to 10000 operations required")
	}
	if batch.Atomic != nil && !*batch.Atomic {
		return nil, invalid("atomic", "partial batches are not supported")
	}
	if batch.Mode != "" && batch.Mode != "auto" && batch.Mode != "sync" && batch.Mode != "async" {
		return nil, invalid("mode", "expected auto, sync or async")
	}
	if len(batch.AssertLocks) > recordLeaseMaxIDs {
		return nil, invalid("assertLocks", "at most 100 assertions")
	}
	for _, a := range batch.AssertLocks {
		if !unsmeta.ValidID(a.CollectionID) || !unsmeta.ValidID(a.ID) || a.Token == "" || len(a.Token) > 256 {
			return nil, invalid("assertLocks", "valid collectionId, id and token required")
		}
	}
	p := &plan{Items: append([]Change(nil), batch.Items...), AssertLocks: batch.AssertLocks}
	byOp := map[string]int{}
	byIdentity := map[string]int{}
	deps := make([]map[int]bool, len(p.Items))
	for i := range p.Items {
		currentIndex = i
		item := &p.Items[i]
		if item.ManagedBy != nil {
			binding := *item.ManagedBy
			item.ManagedBy = &binding
		}
		deps[i] = map[int]bool{}
		if !unsmeta.ValidID(item.OpID) {
			return nil, invalid("opId", "valid opId required")
		}
		if _, ok := byOp[item.OpID]; ok {
			return nil, invalid("opId", "duplicate opId")
		}
		byOp[item.OpID] = i
		if item.Action == "meta.create" || item.Action == "record.create" || item.Action == "sample.append" {
			if item.ID.Ref != "" {
				return nil, invalid("id", "creation id cannot be a ref")
			}
			if item.ID.ID == "" {
				item.ID.ID = uuid.NewString()
				if item.Kind == "source" || item.Kind == "stream" {
					item.ID.ID = strconv.FormatInt(idgen.NextID(), 10)
				}
			}
			if item.Action == "meta.create" {
				identity := item.Kind + "/" + item.ID.ID
				if _, exists := byIdentity[identity]; exists {
					return nil, invalid("id", "duplicate metadata identity")
				}
				byIdentity[identity] = i
			}
		}
		p.Cost++
		if (item.Action == "meta.replace" || item.Action == "meta.patch" || item.Action == "meta.resourcePatch") && item.Kind == "collection" || item.Action == "meta.copy" {
			p.Cost += SyncItems
		}
		if item.Kind == "source" || item.Kind == "app" || item.Action == "stream.publish" {
			p.Cost += 20
		}
	}
	resolve := func(owner int, value Identity, kind string) (string, error) {
		if value.Ref != "" {
			j, ok := byOp[value.Ref]
			if !ok {
				return "", invalid("ref", "unknown opId")
			}
			target := p.Items[j]
			if target.Action != "meta.create" && target.Action != "record.create" {
				return "", invalid("ref", "ref must name a creation")
			}
			if (kind == "record" && target.Action != "record.create") || (kind != "record" && target.Kind != kind) {
				return "", invalid("ref", "reference kind mismatch")
			}
			deps[owner][j] = true
			return target.ID.ID, nil
		}
		if value.ID != "" {
			if j, ok := byIdentity[kind+"/"+value.ID]; ok && j != owner {
				deps[owner][j] = true
			}
		}
		return value.ID, nil
	}
	for i := range p.Items {
		currentIndex = i
		item := &p.Items[i]
		for _, op := range item.DependsOn {
			j, ok := byOp[op]
			if !ok {
				return nil, invalid("dependsOn", "unknown dependency")
			}
			deps[i][j] = true
		}
		if err := validateBindings(*item); err != nil {
			return nil, err
		}
		for _, binding := range item.Bindings {
			j, ok := byOp[binding.Ref]
			if !ok {
				return nil, invalid("bindings/ref", "unknown result opId")
			}
			if strings.HasPrefix(binding.ResultPath, "data.") && p.Items[j].Action != "record.read" {
				return nil, invalid("bindings/resultPath", "data results require an authorized record.read operation")
			}
			if strings.HasPrefix(binding.ResultPath, "query.") && p.Items[j].Action != "record.query" {
				return nil, invalid("bindings/resultPath", "query results require record.query")
			}
			if binding.Path == "id" && (p.Items[j].CollectionID.Ref != "" || item.CollectionID.Ref != "" || p.Items[j].CollectionID.ID != item.CollectionID.ID) {
				return nil, invalid("bindings/path", "dynamic target must remain in the queried Collection")
			}
			deps[i][j] = true
		}
		var err error
		if item.CollectionID.ID != "" || item.CollectionID.Ref != "" {
			item.CollectionID.ID, err = resolve(i, item.CollectionID, "collection")
			item.CollectionID.Ref = ""
			if err != nil {
				return nil, err
			}
		}
		if item.RecordID.Ref != "" {
			ref := p.Items[byOp[item.RecordID.Ref]]
			item.RecordID.ID, err = resolve(i, item.RecordID, "record")
			item.RecordID.Ref = ""
			if err != nil {
				return nil, err
			}
			coll, err := resolve(i, ref.CollectionID, "collection")
			if err != nil || coll != item.CollectionID.ID {
				return nil, invalid("recordId", "record belongs to a different collection")
			}
		}
		if item.ID.Ref != "" && strings.HasPrefix(item.Action, "record.") {
			reference := item.ID.Ref
			item.ID.ID, err = resolve(i, item.ID, "record")
			if err != nil {
				return nil, err
			}
			ref := p.Items[byOp[reference]]
			coll, err := resolve(i, ref.CollectionID, "collection")
			if err != nil || coll != item.CollectionID.ID {
				return nil, invalid("id", "record belongs to a different collection")
			}
			item.ID.Ref = ""
		}
		if item.StreamID.ID != "" || item.StreamID.Ref != "" {
			item.StreamID.ID, err = resolve(i, item.StreamID, "stream")
			item.StreamID.Ref = ""
			if err != nil {
				return nil, err
			}
		}
		if item.ManagedBy != nil {
			item.ManagedBy.SourceID.ID, err = resolve(i, item.ManagedBy.SourceID, "source")
			item.ManagedBy.SourceID.Ref = ""
			if err != nil {
				return nil, err
			}
		}
		if item.Kind != "" && (item.Action == "meta.create" || item.Action == "meta.replace" || item.Action == "meta.copy") {
			var data map[string]any
			if json.Unmarshal(item.Data, &data) != nil || data == nil {
				return nil, invalid("data", "JSON object required")
			}
			field := func(object map[string]any, key, kind string) error {
				v, ok := object[key]
				if !ok || v == nil {
					return nil
				}
				raw, _ := json.Marshal(v)
				var id Identity
				if json.Unmarshal(raw, &id) != nil {
					return invalid(key, "invalid reference")
				}
				id.ID, err = resolve(i, id, kind)
				if err != nil {
					return err
				}
				object[key] = id.ID
				return nil
			}
			if item.Kind == "directory" || item.Kind == "topic" {
				if err = field(data, "parentId", "directory"); err != nil {
					return nil, err
				}
			}
			if item.Kind == "topic" {
				if err = field(data, "collectionId", "collection"); err != nil {
					return nil, err
				}
				if fields, ok := data["fields"].([]any); ok {
					for _, f := range fields {
						o, ok := f.(map[string]any)
						if !ok {
							return nil, invalid("fields", "field object required")
						}
						if err = field(o, "collectionId", "collection"); err != nil {
							return nil, err
						}
						if err = field(o, "recordId", "record"); err != nil {
							return nil, err
						}
					}
				}
			}
			if item.Kind == "source" {
				config, _ := data["config"].(map[string]any)
				streams, _ := config["streams"].(map[string]any)
				for _, b := range streams {
					o, ok := b.(map[string]any)
					if !ok {
						return nil, invalid("streams", "instance object required")
					}
					if err = field(o, "streamId", "stream"); err != nil {
						return nil, err
					}
					if err = field(o, "topicId", "topic"); err != nil {
						return nil, err
					}
					target, _ := o["target"].(map[string]any)
					if err = field(target, "collectionId", "collection"); err != nil {
						return nil, err
					}
				}
			}
			if item.Kind == "app" {
				grants, _ := data["collections"].([]any)
				for _, g := range grants {
					o, ok := g.(map[string]any)
					if !ok {
						return nil, invalid("collections", "grant object required")
					}
					if err = field(o, "collectionId", "collection"); err != nil {
						return nil, err
					}
				}
				nav, _ := data["navigation"].(map[string]any)
				ids, _ := nav["topicIds"].([]any)
				for j, v := range ids {
					o := map[string]any{"topicId": v}
					if err = field(o, "topicId", "topic"); err != nil {
						return nil, err
					}
					ids[j] = o["topicId"]
				}
			}
			item.Data, _ = json.Marshal(data)
		}
	}
	state := make([]int, len(p.Items))
	var visit func(int) error
	visit = func(i int) error {
		currentIndex = i
		if state[i] == 1 {
			return problem("CYCLIC_DEPENDENCY", "cyclic operation references")
		}
		if state[i] == 2 {
			return nil
		}
		state[i] = 1
		dependencies := make([]int, 0, len(deps[i]))
		for j := range deps[i] {
			dependencies = append(dependencies, j)
		}
		sort.Ints(dependencies)
		for _, j := range dependencies {
			if deps[i][j] {
				if err := visit(j); err != nil {
					return err
				}
			}
		}
		state[i] = 2
		p.Order = append(p.Order, i)
		return nil
	}
	for i := range p.Items {
		if err := visit(i); err != nil {
			return nil, err
		}
	}
	seen := map[string]string{}
	bindingWrites := map[string]map[string]bool{}
	for _, i := range p.Order {
		currentIndex = i
		item := p.Items[i]
		if item.Action == "record.query" || item.Action == "record.check" || item.Action == "record.read" || hasTargetBinding(item) || len(item.Filter) > 0 && item.ID.ID == "" {
			continue
		}
		identity := item.Kind + "/" + item.ID.ID
		if strings.HasPrefix(item.Action, "record.") {
			identity = "record/" + item.CollectionID.ID + "/" + item.ID.ID
		}
		if item.Action == "sample.append" {
			identity = "sample/" + item.CollectionID.ID + "/" + item.RecordID.ID + "/" + item.ID.ID
		}
		if item.Action == "stream.publish" {
			identity = "stream/" + item.StreamID.ID
		}
		bindingID := ""
		if item.Kind == "source" && item.Action == "meta.resourcePatch" {
			var patch resourcePatch
			if json.Unmarshal(item.Data, &patch) == nil && strings.HasPrefix(patch.Operation, "sourceBindings") {
				bindingID = resourceString(patch.Fields, "bindingId")
			}
		}
		distinctBinding := bindingID != "" && bindingWrites[identity] != nil && !bindingWrites[identity][bindingID]
		if previous, ok := seen[identity]; ok && !distinctBinding && !(previous == "meta.create" && item.Action == "stream.publish") {
			return nil, invalid("items", fmt.Sprintf("multiple changes of %s", identity))
		}
		if bindingID != "" {
			if bindingWrites[identity] == nil {
				bindingWrites[identity] = map[string]bool{}
			}
			bindingWrites[identity][bindingID] = true
		}
		seen[identity] = item.Action
	}
	return p, nil
}
