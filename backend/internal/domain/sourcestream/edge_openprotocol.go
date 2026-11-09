package sourcestream

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	op "backend/internal/infra/openprotocol"
	"backend/internal/repo"
)

const OpenProtocol Protocol = "open-protocol"
const openProtocolResults = "openprotocol:results"
const openProtocolHistory = "openprotocol:history"

type OpenProtocolOptions struct {
	ControllerName string `json:"controllerName"`
	CellID         int    `json:"cellId"`
	ChannelID      int    `json:"channelId"`
	HistoryStartID int64  `json:"historyStartId,omitempty"`
	HistoryEndID   int64  `json:"historyEndId,omitempty"`
	BufferLimit    int    `json:"bufferLimit,omitempty"`
}

func validateOpenProtocol(d SourceDefinition) error {
	if d.Protocol != OpenProtocol || d.ConnectionRole() != ClientRole || d.CredentialRef != "" || d.SourceType != "" || d.OpenProtocol == nil {
		return invalid("openProtocol", "readOnlyClientRequired")
	}
	if d.HTTP != nil || d.MQTT != nil || d.OPCUA != nil || d.Modbus != nil || d.File != nil || d.SQL != nil {
		return invalid("openProtocol", "protocolConfigMismatch")
	}
	u, err := url.Parse(d.Endpoint)
	if err != nil || u.Scheme != "tcp" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return invalid("endpoint", "tcpHostPortRequired")
	}
	host, port, err := net.SplitHostPort(u.Host)
	n, e := strconv.Atoi(port)
	if err != nil || e != nil || host == "" || n < 1 || n > 65535 {
		return invalid("endpoint", "tcpHostPortRequired")
	}
	o := d.OpenProtocol
	if o.ControllerName == "" || len(o.ControllerName) > 25 || strings.TrimSpace(o.ControllerName) != o.ControllerName || o.CellID < 0 || o.CellID > 9999 || o.ChannelID < 0 || o.ChannelID > 99 {
		return invalid("openProtocol", "controllerIdentityRequired")
	}
	for _, r := range o.ControllerName {
		if r < 32 || r > 126 {
			return invalid("openProtocol.controllerName", "asciiRequired")
		}
	}
	if o.BufferLimit != 0 && (o.BufferLimit < 64 || o.BufferLimit > 2048) {
		return invalid("openProtocol.bufferLimit", "outOfRange")
	}
	if o.HistoryStartID < 0 || o.HistoryEndID < 0 || o.HistoryEndID > 9999999999 || o.HistoryStartID > o.HistoryEndID || (o.HistoryStartID == 0) != (o.HistoryEndID == 0) || o.HistoryEndID-o.HistoryStartID >= 10000 {
		return invalid("openProtocol.historyStartId", "boundedOrderedIDRangeRequired")
	}
	return nil
}
func openProtocolLimit(d SourceDefinition) int {
	if d.OpenProtocol.BufferLimit == 0 {
		return 512
	}
	return d.OpenProtocol.BufferLimit
}
func openProtocolResources(d SourceDefinition, revision int64) []edgeResource {
	if validateOpenProtocol(d) != nil {
		return nil
	}
	metadata := map[string]any{"readOnly": true, "revision": 1, "controllerName": d.OpenProtocol.ControllerName, "cellId": d.OpenProtocol.CellID, "channelId": d.OpenProtocol.ChannelID, "identityFields": []string{"sourceId", "controllerName", "cellId", "channelId", "tighteningId"}, "deviceAck": "afterDurableEdgeStorage", "consumerAck": "afterCloudCommit", "transportSecurity": "networkScopeOnly"}
	items := []edgeResource{{ResourceRef: openProtocolResults, Name: "Tightening results", Modes: []string{"read", "receive"}, Permission: "uns.source", ConfigRevision: revision, InputSchema: resourceObject(map[string]any{"limit": map[string]any{"type": "integer", "const": 1}}), OutputSchema: resourceObject(map[string]any{"items": map[string]any{"type": "array", "items": map[string]any{"type": "object"}}}), Metadata: metadata}}
	if d.OpenProtocol.HistoryStartID > 0 {
		items = append(items, edgeResource{ResourceRef: openProtocolHistory, Name: "Tightening history", Modes: []string{"scan"}, Permission: "uns.source", ConfigRevision: revision, InputSchema: resourceObject(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}, "maxItems": map[string]any{"type": "integer", "minimum": 0, "maximum": 10000}}), OutputSchema: items[0].OutputSchema, Metadata: metadata})
	}
	return items
}

type openProtocolCursor struct {
	Next    int64 `json:"next"`
	End     int64 `json:"end"`
	Missing bool  `json:"missing"`
}

func (s *Service) openProtocolPage(ctx context.Context, row repo.Source, d SourceDefinition, ref, cursor string, limit, maxItems int, preview bool) (any, error) {
	if validateOpenProtocol(d) != nil || limit < 1 || limit > 100 || maxItems < 0 || maxItems > 10000 {
		return nil, invalid("params", "invalidOpenProtocolRead")
	}
	if preview {
		if ref != openProtocolResults || cursor != "" || limit != 1 {
			return nil, invalid("resourceRef", "undeclared")
		}
	} else if ref != openProtocolHistory || d.OpenProtocol.HistoryStartID <= 0 {
		return nil, invalid("resourceRef", "undeclared")
	}
	session, err := s.openProtocolSession(ctx, row, d)
	if err != nil {
		return nil, err
	}
	if preview {
		client, err := session.wait(ctx)
		if err != nil {
			return nil, err
		}
		// Starting or joining the single session never creates a second preview socket.
		result, err := client.History(ctx, 0)
		if missingOpenProtocol(err) {
			return map[string]any{"resourceRef": ref, "configRevision": row.ConfigRevision, "collectedAt": time.Now().UnixMilli(), "items": []any{}, "returned": 0, "hasMore": false}, nil
		}
		if err != nil {
			return nil, safeOpenProtocolError(err)
		}
		item := openProtocolRecord(row.ID, result)
		_ = withDurableJournal(ctx, row.ID, row.ConfigRevision, openProtocolLimit(d), func(j *durableJournal) error {
			for i := len(j.Events) - 1; i >= 0; i-- {
				var candidate map[string]any
				if json.Unmarshal(j.Events[i].Data, &candidate) == nil && candidate["tighteningId"] == float64(result.TighteningID) && candidate["history"] == false {
					item = candidate
					break
				}
			}
			return nil
		})
		return map[string]any{"resourceRef": ref, "configRevision": row.ConfigRevision, "collectedAt": time.Now().UnixMilli(), "items": []any{item}, "returned": 1, "hasMore": false}, nil
	}
	scope := scanScope(ctx, row.ID, row.ConfigRevision, ref, limit, maxItems)
	position := openProtocolCursor{Next: d.OpenProtocol.HistoryStartID, End: d.OpenProtocol.HistoryEndID}
	remaining := maxItems
	if cursor != "" {
		c, err := s.openScanCursor(cursor, scope)
		if err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(c.Link), &position) != nil || position.Next < d.OpenProtocol.HistoryStartID || position.Next > position.End || position.End != d.OpenProtocol.HistoryEndID {
			return nil, invalid("cursor", "invalidOrStale")
		}
		remaining = c.Remaining
	}
	size := limit
	if remaining > 0 {
		size = min(size, remaining)
	}
	client, err := session.wait(ctx)
	if err != nil {
		return nil, err
	}
	items := []any{}
	unavailable := []EdgeAcquisitionGap{}
	// limit bounds device requests as well as rows. Missing IDs are not proof
	// of lost tightening events, and never cause an unbounded search loop.
	for attempts := 0; attempts < min(size, 16) && position.Next <= position.End; attempts++ {
		id := position.Next
		result, err := client.History(ctx, id)
		if missingOpenProtocol(err) {
			position.Missing = true
			unavailable = append(unavailable, EdgeAcquisitionGap{Reason: "historyIdUnavailableMayBeNaturalGap", From: strconv.FormatInt(id, 10), To: strconv.FormatInt(id, 10), At: time.Now().UnixMilli()})
		} else if err != nil {
			return nil, safeOpenProtocolError(err)
		} else {
			items = append(items, openProtocolRecord(row.ID, result))
		}
		position.Next++
	}
	if remaining > 0 {
		remaining -= len(items)
	}
	hasMore := position.Next <= position.End
	truncated := hasMore && maxItems > 0 && remaining == 0
	next := ""
	if hasMore && !truncated {
		raw, _ := json.Marshal(position)
		next, err = s.nextScanCursor(string(raw), remaining, scope)
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"resourceRef": ref, "configRevision": row.ConfigRevision, "items": items, "returned": len(items), "nextCursor": next, "hasMore": hasMore && !truncated, "truncated": truncated, "gaps": unavailable, "historyComplete": !hasMore && !position.Missing, "lastAttemptedId": position.Next - 1}, nil
}

func openProtocolRecord(sourceID int64, result op.Result) map[string]any {
	raw, _ := json.Marshal(result)
	var item map[string]any
	_ = json.Unmarshal(raw, &item)
	item["sourceId"] = strconv.FormatInt(sourceID, 10)
	item["torqueUnit"] = "N·m"
	item["angleUnit"] = "degree"
	item["sourceTimezone"] = "Asia/Shanghai"
	return item
}
func missingOpenProtocol(err error) bool {
	var rejected *op.CommandError
	return errors.As(err, &rejected) && rejected.MID == 64 && rejected.Code == 15
}
func safeOpenProtocolError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrEdgeJournalFull) {
		return err
	}
	var rejected *op.CommandError
	if errors.As(err, &rejected) {
		return rejected
	}
	switch err.Error() {
	case "OpenProtocol.controllerIdentityMismatch", "OpenProtocol.sessionOwnedByAnotherProcess", "OpenProtocol.sessionOwnedByAnotherSource", "OpenProtocol.controllerSequenceReset", "OpenProtocol.sessionLeaseLost", "SourceStream.durableEventIdentityConflict", "SourceStream.durableJournalAuditFull":
		return err
	}
	return errors.New("OpenProtocol.transportOrFrameFailed")
}
