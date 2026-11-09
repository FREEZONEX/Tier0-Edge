package unsv2

import (
	"fmt"
	"strings"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
)

const (
	lookupByteBudget    = 8 << 20
	maxLookupOuterRows  = 1000
	maxLookupInputRows  = 10000
	maxLookupOutputRows = 200
)

func (c *pipelineCompiler) lookupIndexReady(collectionID, kind, field string) (bool, error) {
	// Builtin indexes have collection_id first. Only their actual leading
	// second column qualifies; an index containing the field later does not.
	column := ""
	if kind == "records" && field == "_id" {
		column = "id"
	}
	if kind == "samples" && field == "_meta.recordId" {
		column = "record_id"
	}
	if column != "" {
		table := "public.uns_record"
		if kind == "samples" {
			table = "public.uns_sample"
		}
		var ready bool
		err := repo.GetUnsConn(c.ctx).Raw(`SELECT EXISTS(SELECT 1 FROM pg_index i JOIN pg_class ic ON ic.oid=i.indexrelid JOIN pg_am am ON am.oid=ic.relam JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=i.indkey[0] JOIN pg_attribute b ON b.attrelid=i.indrelid AND b.attnum=i.indkey[1] WHERE i.indrelid=to_regclass(?) AND am.amname='btree' AND a.attname='collection_id' AND b.attname=? AND i.indisvalid AND i.indisready AND i.indpred IS NULL)`, table, column).Scan(&ready).Error
		if err != nil || ready {
			return ready, err
		}
	}
	return unsmeta.ReadyQueryIndex(c.ctx, collectionID, kind, field)
}

func lookupGuard(condition, reason, count string) string {
	// The cast is evaluated only on the rejected runtime branch. A stable
	// marker is translated to a public budget fault without leaking SQL.
	return "(CASE WHEN " + condition + " THEN (" + pipelineLiteral("lookup budget exceeded: "+reason+": ") + "||(" + count + ")::text)::bigint ELSE 1 END)=1"
}

func (c *pipelineCompiler) compileLookup(p pipelinePlan, x pipelineLookup, depth int) (pipelinePlan, error) {
	if x.Kind == "" {
		x.Kind = "records"
	}
	if err := validatePipelineRecordScope(x.Kind, x.RecordID, "pipeline.$lookup.recordId"); err != nil {
		return p, err
	}
	if err := pipelineTopKey(x.As); err != nil {
		return p, err
	}
	decodedAs, _ := pipelinePath(x.As)
	x.As = decodedAs[0]
	stage := fmt.Sprintf("lookup_%d", c.stages)
	outer := stage + "_outer"
	local, err := c.field(outer+".doc", x.LocalField, p.schema)
	if err != nil {
		return p, err
	}
	foreign, err := c.source(x.Kind, x.CollectionID, x.RecordID)
	if err != nil {
		return p, err
	}
	f, err := c.field("base:b", x.ForeignField, foreign.schema)
	if err != nil {
		return p, err
	}
	if f.schema.Type != local.schema.Type && !(relationalNumeric(f.schema.Type) && relationalNumeric(local.schema.Type)) || f.schema.Type == "object" || f.schema.Type == "array" {
		return p, pipelineError("lookup fields must have matching scalar types")
	}
	ready, err := c.lookupIndexReady(x.CollectionID, x.Kind, x.ForeignField)
	if err != nil {
		return p, err
	}
	if !ready {
		return p, &Fault{Code: "LOOKUP_INDEX_REQUIRED", Message: "Lookup foreignField requires a ready leading B-tree query index", Path: "pipeline.$lookup.foreignField", Reason: "No usable index for the foreign equality predicate", Repair: "Configure collection.indexes with foreignField first and wait for indexStatus.state=ready"}
	}
	comparison := "COALESCE(" + f.sql + ",'null'::jsonb) = " + local.sql
	if f.native != "" {
		comparison = f.native + " = " + pipelineScalar(local)
		if f.temporal {
			comparison = "(" + f.native + ">=to_timestamp((" + pipelineScalar(local) + ")::numeric/1000) AND " + f.native + "<to_timestamp(((" + pipelineScalar(local) + ")::numeric+1)/1000))"
		}
	}
	table, order, active := "public.uns_record", "b.id", " AND b.deleted_at IS NULL"
	if x.Kind == "samples" {
		table, order, active = "public.uns_sample", "b.time,b.record_id,b.id", " AND b.record_id="+pipelineLiteral(x.RecordID)
	}
	// Bound indexed matches before constructing documents, numbering rows or
	// executing nested aggregation. LIMIT is a probe: exceeding it fails, it
	// never supplies a silently truncated result to an aggregate.
	candidates := stage + "_candidates"
	cost := stage + "_cost"
	inputGuard := lookupGuard(cost+".n>"+fmt.Sprint(maxLookupInputRows)+" OR "+cost+".bytes>"+outer+".budget", "foreign input", cost+".n")
	// Keep candidate costs outside the nested pipeline. A constant-false match
	// can optimize that pipeline away, so a WHERE guard inside its source is
	// insufficient. The final lateral CASE consumes cost before referencing any
	// nested result; both consumers reuse this one materialized candidate set.
	foreignCTEs := candidates + " AS MATERIALIZED (SELECT b.* FROM " + table + " b WHERE b.collection_id=" + pipelineLiteral(x.CollectionID) + active + " AND " + comparison + " AND " + local.sql + " IS NOT NULL AND " + local.sql + " <> 'null'::jsonb LIMIT " + fmt.Sprint(maxLookupInputRows+1) + "), " + cost + " AS MATERIALIZED (SELECT count(*) AS n,COALESCE(sum(octet_length((" + DataDocumentSQL(x.Kind, "b") + ")::text)),0) AS bytes FROM " + candidates + " b)"
	foreign.sql = "SELECT " + DataDocumentSQL(x.Kind, "b") + " AS doc,row_number() OVER (ORDER BY " + order + ") AS ord FROM " + candidates + " b"
	foreign.args = nil
	foreign.base = false
	foreign, err = c.compile(foreign, x.Pipeline, depth+1)
	if err != nil {
		return p, err
	}
	inputs := stage + "_inputs"
	budget := stage + "_budget"
	guarded := stage + "_guarded"
	perRowOverhead := fmt.Sprint(256 + 6*len(x.As))
	outerGuard := lookupGuard(budget+".n>"+fmt.Sprint(maxLookupOuterRows)+" OR "+budget+".bytes+"+budget+".n*"+perRowOverhead+">="+fmt.Sprint(lookupByteBudget), "outer input", budget+".n")
	outputGuard := lookupGuard("lc.n>"+fmt.Sprint(maxLookupOutputRows)+" OR lc.bytes>"+outer+".budget", "foreign output", "lc.n")
	// Partition the stage byte budget among the actual outer rows. This
	// deliberately conservative allocation bounds memory before jsonb_agg,
	// including nested pipelines that expand or synthesize large documents.
	// The checked value also controls row validity. Otherwise a following
	// $exists/$count can prune the items projection and skip both guards.
	// Materialization evaluates the guarded aggregate once; IS NOT NULL keeps
	// it required even when no caller consumes the resulting document.
	evaluated := stage + "_evaluated"
	joined := "WITH " + inputs + " AS MATERIALIZED (SELECT p.doc,p.ord FROM (" + p.sql + ") p LIMIT " + fmt.Sprint(maxLookupOuterRows+1) + "), " + budget + " AS MATERIALIZED (SELECT count(*) AS n,COALESCE(sum(octet_length(doc::text)),0) AS bytes FROM " + inputs + "), " + guarded + " AS MATERIALIZED (SELECT i.doc,i.ord,(" + fmt.Sprint(lookupByteBudget) + "-" + budget + ".bytes-" + budget + ".n*" + perRowOverhead + ")/GREATEST(" + budget + ".n,1) AS budget FROM " + inputs + " i CROSS JOIN " + budget + " WHERE " + outerGuard + ") SELECT " + outer + ".doc || jsonb_build_object(" + pipelineLiteral(x.As) + ",l.items) AS doc," + outer + ".ord FROM " + guarded + " " + outer + " CROSS JOIN LATERAL (WITH " + foreignCTEs + ", lr AS MATERIALIZED (SELECT f.doc,f.ord FROM (" + foreign.sql + ") f ORDER BY f.ord LIMIT " + fmt.Sprint(maxLookupOutputRows+1) + "), lc AS MATERIALIZED (SELECT count(*) AS n,COALESCE(sum(octet_length(doc::text)+2),0) AS bytes FROM lr), " + evaluated + " AS MATERIALIZED (SELECT CASE WHEN " + inputGuard + " THEN (SELECT CASE WHEN " + outputGuard + " THEN (SELECT COALESCE(jsonb_agg(lr.doc ORDER BY lr.ord),'[]'::jsonb) FROM lr) END FROM lc) END AS items FROM " + cost + ") SELECT items FROM " + evaluated + " WHERE items IS NOT NULL) l"
	p.sql = joined
	p.args = append(p.args, foreign.args...)
	p.schema = pipelineClone(p.schema)
	p.schema.Properties[x.As] = unsmeta.Schema{Type: "array", Items: foreign.schema}
	p.base = false
	p.direct = false
	return p, nil
}

func lookupRuntimeError(message string) error {
	if strings.Contains(message, "lookup budget exceeded:") {
		return &Fault{Code: "LOOKUP_BUDGET_EXCEEDED", Message: "Lookup exceeds its row or intermediate byte budget", Reason: "Maximum 1000 outer rows, 10000 indexed matches and 200 output rows per outer row; 8 MiB total stage byte budget is divided among outer rows", Repair: "Narrow the input or foreign equality key; project fewer fields before lookup, or split the query"}
	}
	return nil
}
