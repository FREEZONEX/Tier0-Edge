package sourcestream

import (
	"backend/internal/domain/sourcehealth"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"backend/internal/repo"
	_ "modernc.org/sqlite"
)

type qcMapping struct{ columns, fields, keys []string }

var qcTables = map[string]qcMapping{
	"T_JYD":    {strings.Fields("JYDH JYRQ TH PH GDH JYLX JYY SL BHGS JYJG BZ"), strings.Fields("inspectionNo inspectionDate drawingNo batchNo workOrder inspectionType inspector quantity rejectedQuantity result remarks"), strings.Fields("JYDH")},
	"T_JYD_MX": {strings.Fields("JYDH XH XMMC BZC SXPC XXPC SCZ JG"), strings.Fields("inspectionNo lineNo itemName nominalValue upperDeviation lowerDeviation measuredValue result"), strings.Fields("JYDH XH")},
	"T_YHB":    {strings.Fields("YHM XM BM QX"), strings.Fields("username name department permissions"), strings.Fields("YHM")},
}

func fileResources(def SourceDefinition, row repo.Source) []edgeResource {
	if def.Protocol != FileRead || def.File == nil {
		return nil
	}
	var result []edgeResource
	for _, r := range def.File.Resources {
		refs := []string{}
		if len(r.Tables) == 0 {
			refs = append(refs, "file:"+r.ID)
		} else {
			for _, t := range r.Tables {
				refs = append(refs, "qc2000:"+r.ID+":"+t)
			}
		}
		for _, ref := range refs {
			result = append(result, edgeResource{ResourceRef: ref, Name: ref, Modes: []string{"read", "scan"}, Permission: "uns.source", RequiredPermissions: []string{"uns.source", "uns.read"}, ConfigRevision: row.ConfigRevision,
				InputSchema:  resourceObject(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}, "maxItems": map[string]any{"type": "integer", "minimum": 0, "maximum": 10000}}),
				OutputSchema: resourceObject(map[string]any{"items": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, "nextCursor": map[string]string{"type": "string"}, "hasMore": map[string]string{"type": "boolean"}}), Metadata: map[string]any{"identityField": "businessKey", "versionField": "fileSha256"}})
		}
	}
	return append(result, directoryResources(def, row)...)
}

func fileSelection(def SourceDefinition, ref string) (FileSelection, string, error) {
	if def.File != nil {
		for _, r := range def.File.Resources {
			if len(r.Tables) == 0 && ref == "file:"+r.ID {
				return r, "", nil
			}
			for _, t := range r.Tables {
				if ref == "qc2000:"+r.ID+":"+t {
					return r, t, nil
				}
			}
		}
	}
	return FileSelection{}, "", invalid("resourceRef", "undeclared")
}

func (s *Service) fileResourcePage(ctx context.Context, row repo.Source, def SourceDefinition, ref, cursor string, limit, maxItems int, preview bool) (any, error) {
	if limit < 1 || limit > 100 || maxItems < 0 || maxItems > 10000 {
		return nil, invalid("params", "outOfRange")
	}
	if directory, ok := fileDirectoryForRef(def, ref); ok {
		return s.directoryResourcePage(ctx, row, def, directory, ref, cursor, limit, maxItems, preview)
	}
	r, table, e := fileSelection(def, ref)
	if e != nil {
		return nil, e
	}
	scope := scanScope(ctx, row.ID, row.ConfigRevision, ref, limit, maxItems)
	expected, offset, remaining := "", 0, maxItems
	if cursor != "" {
		page, e := s.openScanCursor(cursor, scope)
		if e != nil {
			return nil, e
		}
		version, pos, ok := strings.Cut(page.Link, "/")
		start, e := strconv.Atoi(pos)
		if !ok || len(version) != 64 || e != nil || start < 0 {
			return nil, invalid("cursor", "invalidOrStale")
		}
		expected, offset, remaining = version, start, page.Remaining
	}
	c := Credential{}
	if def.CredentialRef != "" {
		stored, e := repo.NewSourceStreamRepo(ctx).Credential(ctx, row.ID, def.CredentialRef)
		if e != nil {
			return nil, ErrCredential
		}
		c, e = s.cipher.Open(row.ID, stored.Ciphertext)
		if e != nil {
			return nil, e
		}
	}
	snapshot, release, e := acquireFileSnapshot(ctx, fileSnapshotKey{row.ID, row.ConfigRevision, r.ID})
	if e != nil {
		return nil, e
	}
	defer release()
	if e = snapshot.prepare(ctx, def, r, c, expected); e != nil {
		var pending *sourcehealth.Failure
		if errors.As(e, &pending) && pending.Code == "SOURCE_SNAPSHOT_PENDING" {
			return nil, e
		}
		return nil, sourcehealth.Fail("download", sourcehealth.New("data", "download", time.Now(), e).ErrorCode, "Snapshot download failed or timed out before data validation", e)
	}
	file, version := snapshot.file, snapshot.version
	count := limit
	if maxItems > 0 {
		count = min(limit, remaining)
	}
	var items []map[string]any
	if table != "" {
		items, e = qcSnapshotRows(ctx, file, table, offset, count+1, !snapshot.checked[table])
		if e == nil {
			snapshot.checked[table] = true
		}
	} else {
		items, e = fileSnapshotChunks(file, version, offset, count+1)
	}
	if e != nil {
		return nil, e
	}
	more := len(items) > count
	if more {
		items = items[:count]
	}
	for _, item := range items {
		item["businessKey"] = strconv.FormatInt(row.ID, 10) + "/" + r.ID + "/" + item["businessKey"].(string)
		item["fileSha256"] = version
		item["filePath"] = r.Path
	}
	data, e := json.Marshal(items)
	if e != nil {
		return nil, e
	}
	if len(data) > 256*1024 {
		return nil, invalid("limit", "upstreamNativePageExceedsByteLimit")
	}
	result := map[string]any{"resourceRef": ref, "configRevision": row.ConfigRevision, "items": items, "collectedAt": time.Now().UnixMilli()}
	if preview {
		result["returned"] = len(items)
		result["truncated"] = more
		return result, nil
	}
	if maxItems > 0 {
		remaining -= len(items)
		more = more && remaining > 0
	}
	next := ""
	if more {
		next, e = s.nextScanCursor(version+"/"+strconv.Itoa(offset+len(items)), remaining, scope)
		if e != nil {
			return nil, e
		}
	}
	result["nextCursor"], result["hasMore"] = next, more
	return result, nil
}

func fileSnapshotChunks(f *os.File, version string, offset, count int) ([]map[string]any, error) {
	const chunk = 1024
	stat, e := f.Stat()
	if e != nil {
		return nil, e
	}
	total := max(1, (int(stat.Size())+chunk-1)/chunk)
	items := []map[string]any{}
	for i := offset; i < min(total, offset+count); i++ {
		var buf [chunk]byte
		n, e := f.ReadAt(buf[:], int64(i)*chunk)
		if e != nil && e != io.EOF {
			return nil, e
		}
		items = append(items, map[string]any{"businessKey": version + "/" + strconv.Itoa(i), "chunkIndex": i, "chunkCount": total, "fileSize": stat.Size(), "contentBase64": base64.StdEncoding.EncodeToString(buf[:n])})
	}
	return items, nil
}

func qcSnapshotRows(ctx context.Context, file *os.File, table string, offset, count int, validate bool) ([]map[string]any, error) {
	m, ok := qcTables[table]
	if !ok {
		return nil, invalid("file", "unknownTable")
	}
	var header [100]byte
	if _, e := file.ReadAt(header[:], 0); e != nil || !bytes.Equal(header[:16], []byte("SQLite format 3\x00")) || header[18] != 1 || header[19] != 1 {
		return nil, invalid("file", "standaloneSQLiteRequired")
	}
	p := filepath.ToSlash(file.Name())
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	uri := url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro&immutable=1&_pragma=trusted_schema%28OFF%29&_pragma=query_only%28ON%29"}
	db, e := sql.Open("sqlite", uri.String())
	if e != nil {
		return nil, e
	}
	defer db.Close()
	// The source exports ordinary tables with no declared keys. Refuse views,
	// virtual tables and snapshots whose business keys are null or non-unique.
	keys := strings.Join(m.keys, ",")
	if validate {
		var typ string
		if e = db.QueryRowContext(ctx, "SELECT type FROM pragma_table_list WHERE schema='main' AND name=?", table).Scan(&typ); e != nil || typ != "table" {
			return nil, invalid("file", "ordinaryTableRequired")
		}
		var check string
		if e = db.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&check); e != nil || check != "ok" {
			return nil, invalid("file", "corruptSnapshot")
		}
		badKeys := []string{m.keys[0] + " IS NULL", "typeof(" + m.keys[0] + ")!='text'", "length(trim(" + m.keys[0] + "))=0"}
		if len(m.keys) == 2 {
			badKeys = append(badKeys, "typeof(XH)!='integer'")
		}
		query := "SELECT EXISTS(SELECT 1 FROM " + table + " WHERE " + strings.Join(badKeys, " OR ") + ") OR EXISTS(SELECT 1 FROM " + table + " GROUP BY " + keys + " HAVING COUNT(*)>1)"
		var bad bool
		if e = db.QueryRowContext(ctx, query).Scan(&bad); e != nil {
			return nil, e
		}
		if bad {
			return nil, invalid("file", "ambiguousBusinessKey")
		}
	}
	rows, e := db.QueryContext(ctx, "SELECT "+strings.Join(m.columns, ",")+" FROM "+table+" ORDER BY "+keys+" LIMIT ? OFFSET ?", count, offset)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(m.fields))
		dest := make([]any, len(vals))
		for i := range vals {
			dest[i] = &vals[i]
		}
		if e = rows.Scan(dest...); e != nil {
			return nil, e
		}
		item := map[string]any{}
		for i, k := range m.fields {
			item[k] = vals[i]
		}
		key, e := json.Marshal(vals[:len(m.keys)])
		if e != nil {
			return nil, e
		}
		item["businessKey"] = fmt.Sprintf("%s:%s", table, key)
		items = append(items, item)
	}
	return items, rows.Err()
}
