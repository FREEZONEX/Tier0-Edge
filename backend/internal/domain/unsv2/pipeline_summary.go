package unsv2

import (
	"strconv"
	"strings"

	"backend/internal/domain/unsmeta"
)

// Summary stages reuse the same field resolver, grouping and projection rules
// as the general pipeline. All comparisons and counts execute in PostgreSQL;
// pagination is applied only after the complete summary has been computed.
func (c *pipelineCompiler) summary(p pipelinePlan, value any, frequencies bool) (pipelinePlan, error) {
	config, ok := value.(map[string]any)
	if !ok || len(config) != 1 {
		return p, pipelineError("$distinct/$frequencies require an object containing only by")
	}
	by, ok := config["by"].(map[string]any)
	if !ok || len(by) < 1 || len(by) > 16 {
		return p, pipelineError("summary by requires 1 to 16 named field references")
	}
	conditions := make([]string, 0, len(by))
	projection := make(map[string]any, len(by)+1)
	groupBy := make(map[string]any, len(by))
	for _, alias := range pipelineKeys(by) {
		if err := pipelineTopKey(alias); err != nil {
			return p, err
		}
		name, _ := pipelinePath(alias)
		if frequencies && name[0] == "count" {
			return p, pipelineError("$frequencies reserves the output field count")
		}
		reference, ok := by[alias].(string)
		if !ok || !strings.HasPrefix(reference, "$") || strings.HasPrefix(reference, "$$") {
			return p, pipelineError("summary by values must be field references such as $data.status; use $set for expressions")
		}
		field, err := c.field("p.doc", strings.TrimPrefix(reference, "$"), p.schema)
		if err != nil {
			return p, err
		}
		// SQL NULL is a missing field. JSON null is a present, distinct value.
		conditions = append(conditions, "("+field.sql+") IS NOT NULL")
		// Keep internal paths short even when an output alias uses the complete
		// public path-length budget. User aliases are restored by projection.
		internalKey := "k" + strconv.Itoa(len(groupBy))
		groupBy[internalKey] = reference
		projection[alias] = "$_id." + internalKey
	}
	p.sql = "SELECT p.doc,p.ord FROM (" + p.sql + ") p WHERE " + strings.Join(conditions, " AND ")
	group := map[string]any{"_id": groupBy}
	if frequencies {
		group["count"] = map[string]any{"$count": map[string]any{}}
		projection["count"] = "$count"
	}
	p, err := c.group(p, group)
	if err != nil {
		return p, err
	}
	return c.project(p, projection, false)
}

// Scalar summaries deliberately produce one document even for an empty input.
// EXISTS can stop once a row is found, while count remains an exact aggregate.
func pipelineScalarSummary(p pipelinePlan, op string, value any) (pipelinePlan, error) {
	key, ok := value.(string)
	if !ok {
		return p, pipelineError(op + " needs an output field name")
	}
	if err := pipelineTopKey(key); err != nil {
		return p, err
	}
	decodedKey, _ := pipelinePath(key)
	key = decodedKey[0]
	kind := "integer"
	if op == "$exists" {
		p.sql = "SELECT jsonb_build_object(" + pipelineLiteral(key) + ",EXISTS(SELECT 1 FROM (" + p.sql + ") p)) AS doc,1::bigint AS ord"
		kind = "boolean"
	} else {
		p.sql = "SELECT jsonb_build_object(" + pipelineLiteral(key) + ",count(*)) AS doc,1::bigint AS ord FROM (" + p.sql + ") p"
	}
	p.schema = &unsmeta.Schema{Type: "object", AdditionalProperties: pipelineClosedObject(), Properties: map[string]unsmeta.Schema{key: {Type: kind}}}
	return p, nil
}
