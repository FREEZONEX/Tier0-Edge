package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"time"

	"backend/internal/infra/edifact"
	"backend/internal/repo"
)

type directoryCursor struct {
	DirectoryVersion string `json:"directoryVersion"`
	After            string `json:"after,omitempty"`
	Current          string `json:"current,omitempty"`
	FileVersion      string `json:"fileVersion,omitempty"`
	Offset           int    `json:"offset,omitempty"`
}
type directoryListJob struct {
	done   chan struct{}
	cancel context.CancelFunc
	names  []string
	err    error
}

func (s *fileSnapshot) directory(ctx context.Context, d SourceDefinition, selection FileDirectory, c Credential, version string) ([]string, string, error) {
	if version != "" && version == s.directoryVersion {
		return s.directoryNames, version, nil
	}
	if s.directoryJob == nil {
		select {
		case fileTransferWorkers <- struct{}{}:
		default:
			return nil, "", ErrUnavailable
		}
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
		job := &directoryListJob{done: make(chan struct{}), cancel: cancel}
		s.directoryJob = job
		go func() {
			defer func() { cancel(); <-fileTransferWorkers; close(job.done) }()
			job.names, job.err = listFileDirectory(work, d, selection, c)
		}()
	}
	job := s.directoryJob
	select {
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case <-job.done:
	}
	s.directoryJob = nil
	if job.err != nil {
		return nil, "", job.err
	}
	raw, _ := json.Marshal(job.names)
	digest := fileDigest(raw)
	if version != "" && digest != version {
		return nil, "", invalid("cursor", "directoryChanged")
	}
	s.directoryNames, s.directoryVersion = job.names, digest
	return job.names, digest, nil
}
func fileDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func (s *Service) directoryResourcePage(ctx context.Context, row repo.Source, def SourceDefinition, directory FileDirectory, ref, cursor string, limit, maxItems int, preview bool) (any, error) {
	scope := scanScope(ctx, row.ID, row.ConfigRevision, ref, limit, maxItems)
	position := directoryCursor{}
	remaining := maxItems
	if cursor != "" {
		saved, e := s.openScanCursor(cursor, scope)
		if e != nil {
			return nil, e
		}
		if json.Unmarshal([]byte(saved.Link), &position) != nil || len(position.DirectoryVersion) != 64 || position.Offset < 0 || position.Offset > maxSourceFileBytes || position.Current != "" && !directory.accepts(position.Current) || position.After != "" && !directory.accepts(position.After) {
			return nil, invalid("cursor", "invalidOrStale")
		}
		remaining = saved.Remaining
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
	listing, release, e := acquireFileSnapshot(ctx, fileSnapshotKey{row.ID, row.ConfigRevision, "list:" + directory.ID})
	if e != nil {
		return nil, e
	}
	names, version, e := listing.directory(ctx, def, directory, c, position.DirectoryVersion)
	release()
	if e != nil {
		return nil, e
	}
	position.DirectoryVersion = version
	index := sort.SearchStrings(names, position.After)
	if index < len(names) && names[index] == position.After {
		index++
	}
	if position.Current != "" {
		index = sort.SearchStrings(names, position.Current)
		if index == len(names) || names[index] != position.Current {
			return nil, invalid("cursor", "fileVersionChanged")
		}
	}
	items := []map[string]any{}
	more := false
	if index < len(names) {
		name := names[index]
		selection := directoryFileSelection(directory, name)
		snapshot, release, e := acquireFileSnapshot(ctx, fileSnapshotKey{row.ID, row.ConfigRevision, selection.ID})
		if e != nil {
			return nil, e
		}
		defer release()
		if e = snapshot.prepare(ctx, def, selection, c, position.FileVersion); e != nil {
			// A malformed/oversized/replaced file becomes a visible row and cannot
			// permanently block later mailbox entries. Transport failures remain retryable.
			var validation *ValidationError
			if !errors.As(e, &validation) || (validation.Code != "tooLarge" && validation.Code != "fileVersionChanged" && validation.Code != "writeInProgress") {
				return nil, e
			}
			items = append(items, directoryError(row.ID, directory.ID, name, position.FileVersion, validation.Code, 0))
		} else {
			count := limit
			if maxItems > 0 {
				count = min(count, remaining)
			}
			if directory.Format == "edifact" {
				items, e = s.ediRows(ctx, row, directory, name, snapshot, position.Offset, count+1, preview)
			} else {
				items, e = fileSnapshotChunks(snapshot.file, snapshot.version, position.Offset, count+1)
				for _, item := range items {
					item["businessKey"] = "file-" + fileDigest([]byte(strconv.FormatInt(row.ID, 10)+"/"+directory.ID+"/"+name+"/"+item["businessKey"].(string)))
				}
			}
			if e != nil {
				return nil, e
			}
			byteCount := 0
			for i := 0; i < min(count, len(items)); i++ {
				encoded, _ := json.Marshal(items[i])
				byteCount += len(encoded) + len(name) + 160
				if byteCount > 240*1024 {
					count = i
					break
				}
			}
			more = len(items) > count
			if more {
				items = items[:count]
			}
			for _, item := range items {
				item["fileName"] = name
				item["fileSha256"] = snapshot.version
			}
			if more {
				position.Current = name
				position.FileVersion = snapshot.version
				position.Offset += len(items)
			}
		}
		if !more {
			position.After = name
			position.Current = ""
			position.FileVersion = ""
			position.Offset = 0
			more = index+1 < len(names)
		}
	}
	raw, e := json.Marshal(items)
	if e != nil {
		return nil, e
	}
	if len(raw) > 256*1024 {
		return nil, invalid("limit", "upstreamNativePageExceedsByteLimit")
	}
	result := map[string]any{"resourceRef": ref, "configRevision": row.ConfigRevision, "items": items, "collectedAt": time.Now().UnixMilli(), "directoryVersion": version}
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
		payload, _ := json.Marshal(position)
		next, e = s.nextScanCursor(string(payload), remaining, scope)
		if e != nil {
			return nil, e
		}
	}
	result["nextCursor"], result["hasMore"] = next, more
	return result, nil
}

func directoryError(sourceID int64, id, name, version, code string, position int) map[string]any {
	identity, _ := json.Marshal([]any{sourceID, id, name, version, code, position})
	return map[string]any{"businessKey": "edi-error-" + fileDigest(identity), "kind": "error", "fileName": name, "fileSha256": version, "errorCode": code, "position": position}
}

func (s *Service) ediRows(ctx context.Context, row repo.Source, directory FileDirectory, name string, snapshot *fileSnapshot, offset, count int, preview bool) ([]map[string]any, error) {
	stat, e := snapshot.file.Stat()
	if e != nil {
		return nil, e
	}
	if stat.Size() > edifact.MaxBytes {
		if offset > 0 {
			return []map[string]any{}, nil
		}
		return []map[string]any{directoryError(row.ID, directory.ID, name, snapshot.version, "ediFileTooLarge", 0)}, nil
	}
	raw, e := io.ReadAll(io.NewSectionReader(snapshot.file, 0, stat.Size()))
	if e != nil {
		return nil, e
	}
	records := edifact.Parse(raw)
	identities := map[string]string{}
	for _, record := range records {
		if record.Fingerprint != "" {
			id := fileDigest([]byte(record.Identity))
			if identities[id] == "" {
				identities[id] = record.Fingerprint
			}
		}
	}
	originals := snapshot.ediOriginals
	if originals == nil {
		originals, e = repo.NewSourceStreamRepo(ctx).ObserveEDIContent(ctx, row.ID, directory.ID, identities, !preview)
		if e != nil {
			return nil, e
		}
		if !preview {
			snapshot.ediOriginals = originals
		}
	}
	rows := make([]map[string]any, 0, len(records))
	blockedMessages := map[string]bool{}
	for _, record := range records {
		messageIdentity, _ := json.Marshal([]string{record.Sender, record.Recipient, record.ControlRef, record.MessageRef, record.MessageType})
		if record.Kind == "line" && blockedMessages[string(messageIdentity)] {
			rows = append(rows, directoryError(row.ID, directory.ID, name, snapshot.version, "parentContentIdentityConflict", record.Position))
			continue
		}
		id := fileDigest([]byte(record.Identity))
		prior := originals[id]
		if prior != "" && record.Fingerprint != "" && prior != record.Fingerprint {
			failure := directoryError(row.ID, directory.ID, name, snapshot.version, "contentIdentityConflict", record.Position)
			ediErrorIdentity(failure, record)
			failure["identity"], failure["originalContentSha256"], failure["currentContentSha256"] = record.Identity, prior, record.Fingerprint
			rows = append(rows, failure)
			if record.Kind == "message" {
				blockedMessages[string(messageIdentity)] = true
			}
			if record.Kind == "exchange" {
				break
			}
			continue
		}
		if record.Kind == "error" {
			failure := directoryError(row.ID, directory.ID, name, snapshot.version, record.ErrorCode, record.Position)
			ediErrorIdentity(failure, record)
			failure["identity"] = record.Identity
			rows = append(rows, failure)
			continue
		}
		identity, _ := json.Marshal([]any{row.ID, directory.ID, record.Identity})
		item := map[string]any{"businessKey": "edi-" + fileDigest(identity), "kind": record.Kind, "sender": record.Sender, "recipient": record.Recipient, "controlRef": record.ControlRef, "messageRef": record.MessageRef, "messageType": record.MessageType, "lineNo": record.LineNo, "position": record.Position, "segments": record.Segments, "contentSha256": record.Fingerprint}
		item["data"] = record.Fields
		if record.ErrorCode != "" {
			item["errorCode"] = record.ErrorCode
		}
		encoded, _ := json.Marshal(item)
		if len(encoded) > 64*1024 {
			item = directoryError(row.ID, directory.ID, name, snapshot.version, "ediRecordTooLarge", record.Position)
		}
		rows = append(rows, item)
	}
	if offset > len(rows) {
		return nil, invalid("cursor", "fileVersionChanged")
	}
	return rows[offset:min(len(rows), offset+count)], nil
}

func ediErrorIdentity(item map[string]any, record edifact.Record) {
	item["sender"], item["recipient"], item["controlRef"] = record.Sender, record.Recipient, record.ControlRef
	item["messageRef"], item["messageType"], item["lineNo"] = record.MessageRef, record.MessageType, record.LineNo
}

func fileDirectoryForRef(def SourceDefinition, ref string) (FileDirectory, bool) {
	if def.File != nil {
		for _, d := range def.File.Directories {
			if d.ref() == ref {
				return d, true
			}
		}
	}
	return FileDirectory{}, false
}

// Keep actual directory roots in the Edge configuration. Discovery exposes only
// declaration IDs and shape, never credential values or arbitrary remote paths.
func directoryResources(def SourceDefinition, row repo.Source) []edgeResource {
	resources := []edgeResource{}
	if def.File == nil {
		return resources
	}
	for _, d := range def.File.Directories {
		resources = append(resources, edgeResource{ResourceRef: d.ref(), Name: d.ID, Modes: []string{"read", "scan"}, Permission: "uns.source", RequiredPermissions: []string{"uns.source", "uns.read"}, ConfigRevision: row.ConfigRevision,
			InputSchema:  resourceObject(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}, "maxItems": map[string]any{"type": "integer", "minimum": 0, "maximum": 10000}}),
			OutputSchema: resourceObject(map[string]any{"items": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, "nextCursor": map[string]string{"type": "string"}, "hasMore": map[string]string{"type": "boolean"}}),
			Metadata:     map[string]any{"format": d.Format, "identityField": "businessKey", "discovery": "boundedDirectorySnapshot", "maxDirectoryEntries": maxDirectoryEntries, "extensions": d.Extensions, "cursorRecovery": "fullRescanOnDirectoryChanged", "delivery": "replayAllFiles", "fileAcknowledgement": "none"}})
	}
	return resources
}
