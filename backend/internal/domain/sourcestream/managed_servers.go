package sourcestream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"backend/internal/repo"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

// ManagedServerSource is a private runtime message, never an API response or
// persisted version snapshot. Only the supervised protocol process sees secrets.
type ManagedServerSource struct {
	SourceID   int64            `json:"sourceId"`
	Definition SourceDefinition `json:"definition"`
	Credential Credential       `json:"credential"`
}

type managedServerRuntime interface {
	SyncServerSources(context.Context, []ManagedServerSource) error
}

// ReconcileManagedServers renews the current Source set, independently of
// whether a Source has a deployed Stream. A failed full snapshot is never sent
// as an empty list; the receiver expires its previous lease and fails closed.
func (s *Service) ReconcileManagedServers(ctx context.Context) error {
	runtime, ok := s.runtime.(managedServerRuntime)
	if !ok {
		return ErrUnavailable
	}
	r := repo.NewSourceStreamRepo(ctx)
	sources := []ManagedServerSource{}
	for page := 1; ; page++ {
		items, total, err := r.ListSources(ctx, repo.SourceFilter{Protocol: string(OPCUA), Page: page, Size: 200})
		if err != nil {
			return err
		}
		for _, item := range items {
			var def SourceDefinition
			if json.Unmarshal([]byte(item.Document), &def) != nil {
				return repo.ErrInvalidArgument
			}
			if def.ConnectionRole() != ServerRole {
				continue
			}
			if err := def.Validate(); err != nil {
				return fmt.Errorf("managed OPC UA Source %d: %w", item.ID, err)
			}
			credential, err := r.Credential(ctx, item.ID, def.CredentialRef)
			if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, repo.ErrNotFound) {
				continue // Withdraw only the Source whose credentials are unavailable.
			}
			if err != nil {
				return err
			}
			plain, err := s.cipher.Open(item.ID, credential.Ciphertext)
			if err != nil || plain.Kind != "basic" {
				continue
			}
			sources = append(sources, ManagedServerSource{SourceID: item.ID, Definition: def, Credential: plain})
			if len(sources) > 200 {
				return invalid("opcua.serverSources", "outOfRange")
			}
		}
		if int64(page*200) >= total {
			break
		}
	}
	return runtime.SyncServerSources(ctx, sources)
}

// Called only by the elected Stream worker. Loss of the Backend/DB/worker
// ownership stops renewal; Node independently withdraws expired Source nodes.
func (s *Service) ManagedServersLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	defer func() {
		if runtime, ok := s.runtime.(managedServerRuntime); ok {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := runtime.SyncServerSources(closeCtx, []ManagedServerSource{}); err != nil {
				logx.WithContext(closeCtx).Error("SourceStream managed OPC UA withdrawal failed; lease expiry remains active")
			}
		}
	}()
	for ctx.Err() == nil {
		syncCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := s.ReconcileManagedServers(syncCtx)
		cancel()
		if err != nil && ctx.Err() == nil {
			// Do not log runtime request bodies or credentials.
			logx.WithContext(ctx).Error("SourceStream managed OPC UA reconciliation failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
