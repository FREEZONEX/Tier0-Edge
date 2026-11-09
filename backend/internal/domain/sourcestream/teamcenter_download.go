package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"backend/internal/repo"
)

func (s *Service) teamcenterFileTicket(ctx context.Context, row repo.Source, def SourceDefinition, fileUID string, timeout int) (string, error) {
	var response struct{ Tickets []json.RawMessage }
	options := map[string]any{"timeoutMs": timeout, "data": map[string]any{
		"header": map[string]any{"state": map[string]any{}, "policy": map[string]any{}},
		"body":   map[string]any{"files": []string{fileUID}},
	}}
	if err := s.teamcenterFileCall(ctx, row, def, "tickets", options, &response); err != nil {
		return "", err
	}
	var uids, tickets []string
	if len(response.Tickets) != 2 || json.Unmarshal(response.Tickets[0], &uids) != nil || json.Unmarshal(response.Tickets[1], &tickets) != nil ||
		len(uids) != 1 || len(tickets) != 1 || uids[0] != fileUID || tickets[0] == "" || len(tickets[0]) > 8192 {
		return "", invalid("resourceRef", "teamcenterTicketIdentityMismatch")
	}
	recordProgramSecretValues(ctx, map[string]string{"teamcenterFileTicket": tickets[0]})
	return tickets[0], nil
}

// Ticket URLs are assembled only here, from the configured SOA origin and the
// checked ImanFile name. The client retains its TLS policy, IP pinning and cookie
// scope and rejects all redirects. Tickets and cookies never enter page results.
func (s *Service) teamcenterFileGet(ctx context.Context, row repo.Source, def SourceDefinition, selection TeamcenterFileSelection, identity teamcenterFileIdentity, headers http.Header, timeout int) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		ticket, err := s.teamcenterFileTicket(ctx, row, def, identity.FileUID, timeout)
		if err != nil {
			return nil, err
		}
		origin, err := url.Parse(def.Endpoint)
		if err != nil || origin.Scheme != "https" || origin.User != nil {
			return nil, ErrHTTPRequest
		}
		destination := &url.URL{Scheme: origin.Scheme, Host: origin.Host, Path: "/fms/fmsdownload/" + identity.FileName}
		destination.RawQuery = url.Values{"ticket": []string{ticket}}.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, destination.String(), nil)
		if err != nil {
			return nil, ErrHTTPRequest
		}
		req.Header = headers.Clone()
		req.Header.Set("Accept-Encoding", "identity")
		client, err := s.httpManager.client(row, def)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(req)
		if err != nil {
			return nil, safeHTTPFailure(ctx, err)
		}
		if response.StatusCode != http.StatusForbidden || attempt == 1 {
			return response, nil
		}
		response.Body.Close()
		// A new ticket may come from a refreshed SOA session. Recheck the full
		// approved graph before retaining any previous byte-range position.
		current, err := s.resolveTeamcenterFile(ctx, row, def, selection, timeout)
		if err != nil {
			return nil, err
		}
		if current != identity {
			return nil, invalid("file", "fileVersionChanged")
		}
	}
	return nil, ErrHTTPRequest
}

func (s *Service) prepareTeamcenterFile(ctx context.Context, row repo.Source, def SourceDefinition, selection TeamcenterFileSelection, identity teamcenterFileIdentity, snapshot *fileSnapshot, expected string, timeout int) error {
	key := identity.cacheKey()
	if snapshot.checked == nil {
		snapshot.checked = map[string]bool{}
	}
	if snapshot.partialETag != "" && !snapshot.checked[key] {
		snapshot.forgetPartial()
		return invalid("file", "fileVersionChanged")
	}
	headers := http.Header{}
	offset := int64(snapshot.partial.Len())
	if snapshot.partialETag != "" {
		headers.Set("If-Match", snapshot.partialETag)
		headers.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	} else if snapshot.file != nil && snapshot.checked[key] {
		headers.Set("If-None-Match", snapshot.etag)
	}
	// A completed transfer may have lost its confirmation request. Reconfirm
	// its validator instead of issuing an unsatisfiable range at EOF.
	if snapshot.partialETag != "" && offset == snapshot.total {
		return s.confirmTeamcenterFile(ctx, row, def, selection, identity, snapshot, expected, timeout)
	}
	response, err := s.teamcenterFileGet(ctx, row, def, selection, identity, headers, timeout)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	etag := response.Header.Get("ETag")
	if response.StatusCode == http.StatusNotModified && snapshot.file != nil && snapshot.checked[key] {
		if etag != snapshot.etag || expected != "" && expected != snapshot.version {
			return invalid("cursor", "fileVersionChanged")
		}
		return nil
	}
	if response.StatusCode == http.StatusPreconditionFailed || response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		snapshot.forgetPartial()
		return invalid("file", "fileVersionChanged")
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return invalid("file", "teamcenterTicketRejected")
		case http.StatusTooManyRequests:
			return invalid("source", "upstreamRateLimited")
		}
		if response.StatusCode >= 500 {
			return ErrUnavailable
		}
		return fmt.Errorf("Teamcenter FMS status %d", response.StatusCode)
	}
	if !strongFileETag(etag) || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return invalid("file", "strongVersionValidatorRequired")
	}
	if snapshot.partialETag != "" && snapshot.partialETag != etag {
		snapshot.forgetPartial()
		return invalid("file", "fileVersionChanged")
	}
	if response.StatusCode == http.StatusPartialContent {
		want := fmt.Sprintf("bytes %d-%d/%d", offset, identity.Size-1, identity.Size)
		if snapshot.partialETag == "" || response.Header.Get("Content-Range") != want || response.ContentLength != identity.Size-offset {
			snapshot.forgetPartial()
			return invalid("file", "invalidRange")
		}
	} else {
		snapshot.forgetPartial()
		offset = 0
		if response.ContentLength != identity.Size {
			return invalid("file", "sizeMismatch")
		}
		snapshot.partialETag, snapshot.total = etag, identity.Size
		snapshot.checked[key] = true
	}
	_, err = io.Copy(&snapshot.partial, io.LimitReader(response.Body, identity.Size-offset+1))
	if int64(snapshot.partial.Len()) > identity.Size {
		snapshot.forgetPartial()
		return invalid("file", "sizeMismatch")
	}
	if err != nil {
		return safeHTTPFailure(ctx, err)
	}
	if int64(snapshot.partial.Len()) != identity.Size {
		return io.ErrUnexpectedEOF
	}
	return s.confirmTeamcenterFile(ctx, row, def, selection, identity, snapshot, expected, timeout)
}

func (s *Service) confirmTeamcenterFile(ctx context.Context, row repo.Source, def SourceDefinition, selection TeamcenterFileSelection, identity teamcenterFileIdentity, snapshot *fileSnapshot, expected string, timeout int) error {
	current, err := s.resolveTeamcenterFile(ctx, row, def, selection, timeout)
	if err != nil {
		return err
	}
	if current != identity {
		snapshot.forgetPartial()
		return invalid("file", "fileVersionChanged")
	}
	headers := http.Header{"If-None-Match": []string{snapshot.partialETag}}
	response, err := s.teamcenterFileGet(ctx, row, def, selection, identity, headers, timeout)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if (response.StatusCode != http.StatusNotModified && response.StatusCode != http.StatusOK) || response.Header.Get("ETag") != snapshot.partialETag {
		snapshot.forgetPartial()
		return invalid("file", "writeInProgress")
	}
	sum := sha256.Sum256(snapshot.partial.Bytes())
	version := hex.EncodeToString(sum[:])
	if expected != "" && version != expected {
		snapshot.forgetPartial()
		return invalid("cursor", "fileVersionChanged")
	}
	file, err := os.CreateTemp("", "tier0-teamcenter-*")
	if err != nil {
		return err
	}
	if _, err = file.Write(snapshot.partial.Bytes()); err != nil {
		file.Close()
		os.Remove(file.Name())
		return err
	}
	snapshot.install(file, version, snapshot.partialETag)
	snapshot.checked[identity.cacheKey()] = true
	snapshot.forgetPartial()
	return nil
}
