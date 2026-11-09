package sourcestream

import (
	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"
)

func programNativePageRef(ref string) bool {
	for _, prefix := range []string{"sql:", "file:", "file-directory:", "qc2000:", "edi:", "openprotocol:"} {
		if strings.HasPrefix(ref, prefix) {
			return true
		}
	}
	return false
}

// Business refs are distinct from the legacy vendor operation refs. In
// particular, do not route all vendor calls (or Hikvision's shared ref) here.
func programBusinessPageRef(def SourceDefinition, ref string) bool {
	return def.ConnectionRole() == ClientRole && vendorPages(def) &&
		(def.SourceType == "salesforce" && strings.HasPrefix(ref, crmObjectPrefix) ||
			def.SourceType == "teamcenter" && strings.HasPrefix(ref, tcQueryPrefix))
}
func programDeclaredResources(def SourceDefinition, row repo.Source, resources []NativeResource) []NativeResource {
	if !supportsExtendedPage(def) {
		return resources
	}
	out := append([]NativeResource{}, resources...)
	seen := map[string]bool{}
	for _, resource := range out {
		seen[resource.ID] = true
	}
	for _, resource := range declaredResources(def, row) {
		if seen[resource.ResourceRef] {
			continue
		}
		schema, _ := json.Marshal(resource.OutputSchema)
		out = append(out, NativeResource{ID: resource.ResourceRef, Name: resource.Name, Models: []AccessModel{Subscribe}, Schema: schema, Metadata: resource.Metadata})
	}
	return out
}
func canReadNativePage(subject contextx.Subject) bool {
	return contextx.HasResource(subject, "uns.source") && contextx.HasResource(subject, "uns.read")
}

func (s *Service) programNativePage(ctx context.Context, row repo.Source, def SourceDefinition, call streamexec.ProgramCall, unattended bool) (json.RawMessage, error) {
	if call.Kind != "source.read" {
		return nil, streamexec.ErrSourceDenied
	}
	var options struct {
		Mode                 string `json:"mode"`
		Limit                int    `json:"limit"`
		MaxItems             int    `json:"maxItems"`
		Cursor               string `json:"cursor"`
		TimeoutMs            int    `json:"timeoutMs"`
		CreatedDateTimeBegin string `json:"createdDateTimeBegin"`
		CreatedDateTimeEnd   string `json:"createdDateTimeEnd"`
		ItemID               string `json:"itemId"`
	}
	if len(call.Options) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(call.Options))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&options) != nil {
			return nil, streamexec.ErrSourceDenied
		}
	}
	if options.Mode == "" {
		options.Mode = "read"
	}
	if options.Limit == 0 {
		options.Limit = 10
	}
	if options.TimeoutMs == 0 {
		options.TimeoutMs = 5000
	}
	business := programBusinessPageRef(def, call.ResourceRef)
	if options.TimeoutMs < 100 || options.TimeoutMs > 10000 || options.Limit < 1 ||
		(options.Limit > 100 && !(business && def.SourceType == "salesforce" && options.Mode == "scan" && options.Limit == 200)) ||
		options.MaxItems < 0 || options.MaxItems > 10000 {
		return nil, streamexec.ErrSourceDenied
	}
	if !business && (options.CreatedDateTimeBegin != "" || options.CreatedDateTimeEnd != "" || options.ItemID != "") ||
		options.ItemID != "" && (def.SourceType != "teamcenter" || !tcItemID.MatchString(options.ItemID)) {
		return nil, streamexec.ErrSourceDenied
	}
	preview := options.Mode == "read"
	if !preview && options.Mode != "scan" || preview && (options.Cursor != "" || options.MaxItems != 0) {
		return nil, streamexec.ErrSourceDenied
	}
	// A scheduled run has frozen Source authority and no interactive caller.
	// If a caller is present, retain the public resource read permissions.
	if subject, ok := contextx.SubjectFrom(ctx); ok || !unattended {
		if !ok || !canReadNativePage(subject) {
			return nil, ErrForbidden
		}
	}
	if row.DesiredState == "disabled" {
		return nil, ErrEdgeReferenceRejected
	}
	if potentiallyWritingSQL(def) {
		ctx = context.WithValue(ctx, sqlProgramExecutionKey{}, true)
	}
	ctx = context.WithValue(ctx, edgeResourceReadContext{}, true)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(options.TimeoutMs)*time.Millisecond)
	defer cancel()
	var value any
	var err error
	if business {
		value, err = s.businessPage(ctx, row, def, call.ResourceRef, options.Cursor, options.Limit, options.MaxItems, options.TimeoutMs, options.CreatedDateTimeBegin, options.CreatedDateTimeEnd, options.ItemID, preview)
	} else {
		value, err = s.extendedPage(ctx, row, def, call.ResourceRef, options.Cursor, options.Limit, options.MaxItems, options.TimeoutMs, preview)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
