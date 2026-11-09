package sourcestream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"backend/internal/repo"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

func httpServerEnabled() bool {
	return os.Getenv("SOURCE_HTTP_ENABLED") == "true" || os.Getenv("SOURCE_HTTP_ENABLED") == "1"
}

func (s *Service) probeHTTPServer(id int64, def SourceDefinition, credential Credential) error {
	if !httpServerEnabled() {
		return invalid("http.server", "notConfigured")
	}
	first, last, err := HTTPServerPortRange()
	if err != nil || def.HTTP == nil || def.HTTP.Port < first || def.HTTP.Port > last {
		return invalid("http.port", "outOfRange")
	}
	if !def.HTTP.Anonymous && credential.Secret == "" && credential.Values["token"] == "" {
		return ErrCredential
	}
	if s.httpServers.View(id, def.HTTP.Port)["state"] == "listening" {
		return nil
	}
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", def.HTTP.Port))
	if err != nil {
		return invalid("http.port", "inUse")
	}
	return listener.Close()
}

// HTTP listeners do not require NATS: synchronous routing is owned by Backend.
func (s *Service) StartHTTPServers(parent context.Context) {
	if !httpServerEnabled() {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.httpServers.mu.Lock()
	if s.httpServers.stop != nil {
		s.httpServers.mu.Unlock()
		cancel()
		return
	}
	s.httpServers.stop = cancel
	s.httpServers.mu.Unlock()
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		defer s.httpServers.Close()
		for ctx.Err() == nil {
			readCtx, done := context.WithTimeout(ctx, 5*time.Second)
			err := s.ReconcileHTTPServers(readCtx)
			done()
			if err != nil && ctx.Err() == nil {
				logx.WithContext(ctx).Error("SourceStream HTTP Server reconciliation failed; stale routes expire closed")
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Service) ReconcileHTTPServers(ctx context.Context) error {
	if !httpServerEnabled() {
		return nil
	}
	first, last, err := HTTPServerPortRange()
	if err != nil {
		return err
	}
	r := repo.NewSourceStreamRepo(ctx)
	executions, err := r.ActiveExecutions(ctx)
	if err != nil {
		return err
	}
	routes := map[int64][]httpRoute{}
	for _, execution := range executions {
		if execution.Stream.Model != "program" {
			continue
		}
		manifest, err := execution.Version.ProgramV2Manifest()
		if err != nil {
			return err
		}
		for _, entry := range manifest.Entries {
			if entry.Kind != "http-request" {
				continue
			}
			id, err := strconv.ParseInt(entry.SourceID, 10, 64)
			if err != nil {
				return repo.ErrInvalidArgument
			}
			routes[id] = append(routes[id], httpRoute{stream: execution.Stream, version: execution.Version, entry: entry})
		}
	}
	snapshots := []httpSourceSnapshot{}
	for page := 1; ; page++ {
		items, total, err := r.ListSources(ctx, repo.SourceFilter{Protocol: string(HTTP), Page: page, Size: 200})
		if err != nil {
			return err
		}
		for _, source := range items {
			var def SourceDefinition
			if json.Unmarshal([]byte(source.Document), &def) != nil {
				return repo.ErrInvalidArgument
			}
			if def.ConnectionRole() != ServerRole {
				continue
			}
			if def.Validate() != nil || def.HTTP.Port < first || def.HTTP.Port > last {
				return repo.ErrInvalidArgument
			}
			credential := Credential{}
			if !def.HTTP.Anonymous {
				stored, err := r.Credential(ctx, source.ID, def.CredentialRef)
				if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, repo.ErrNotFound) {
					continue // An unprovisioned Source must not starve healthy leases.
				}
				if err != nil {
					return err
				}
				credential, err = s.cipher.Open(source.ID, stored.Ciphertext)
				if err != nil || credential.Secret == "" && credential.Values["token"] == "" {
					continue
				}
			}
			ownRoutes := routes[source.ID]
			for i, route := range ownRoutes {
				for _, previous := range ownRoutes[:i] {
					if repo.HTTPRoutesOverlap(route.entry, previous.entry) {
						return repo.ErrConflict
					}
				}
			}
			snapshots = append(snapshots, httpSourceSnapshot{source: source, definition: def, credential: credential, routes: ownRoutes})
		}
		if int64(page*200) >= total {
			break
		}
	}
	return s.httpServers.Sync(s, snapshots)
}
