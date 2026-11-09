package cluster

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"backend/internal/config"
	"backend/internal/contextx"
	auditdomain "backend/internal/domain/audit"
	"backend/internal/domain/fleettunnel"
	domainlicense "backend/internal/domain/license"
	"backend/internal/repo"

	"github.com/zeromicro/go-zero/core/logx"
)

type Service struct {
	connectionMu        sync.Mutex
	fleetControlSubject func(context.Context, contextx.Subject) (contextx.Subject, error)
	fleetTokens         map[string]cachedFleetToken
	audit               *auditdomain.Service
	cfg                 config.ClusterConf
	ingestCfg           config.DataIngestConf
	repo                *repo.ClusterRepo
	fleetRepo           *repo.FleetRepo
	localAgent          LocalFleetAgent
	fleet               *fleetSessionRegistry
	mu                  sync.Mutex
	closeOnce           sync.Once
	closed              chan struct{}
	tunnel              *fleettunnel.Service
}

type FleetAccountAssignmentCommand struct {
	FleetNodeID      string
	CenterUserIDs    []int64
	ExpectedRevision int64
	UserID           int64
}

func (s *Service) BindFleetTunnel(tunnel *fleettunnel.Service) {
	if s != nil {
		s.tunnel = tunnel
	}
}

func (s *Service) AuthorizeFleetTunnel(ctx context.Context, identity fleettunnel.Identity) (string, error) {
	if s == nil {
		return "", ErrUnauthorized
	}
	client, allowed, err := repo.NewConnectClientRepo(ctx).FindAuthorizedMqttClient(
		ctx, identity.Username, identity.Password, identity.ClientID,
	)
	if err != nil {
		return "", err
	}
	if !allowed || client.Type != repo.ConnectClientTypeEdge {
		return "", ErrUnauthorized
	}
	node, err := s.fleetRepo.GetNodeByConnectClientID(ctx, client.ID, false)
	if err != nil || strings.TrimSpace(node.FleetNodeID) == "" || node.FleetNodeID != strings.TrimSpace(identity.NodeID) {
		return "", ErrUnauthorized
	}
	return node.FleetNodeID, nil
}

func (s *Service) ValidateFleetTunnelNode(ctx context.Context, fleetNodeID string) error {
	if s == nil {
		return ErrUnauthorized
	}
	_, err := s.fleetRepo.GetNode(ctx, strings.TrimSpace(fleetNodeID), false)
	return err
}

func (s *Service) fleetTunnelStatus(fleetNodeID string) fleettunnel.Status {
	if s == nil || s.tunnel == nil {
		return fleettunnel.Status{Status: "offline"}
	}
	return s.tunnel.Status(fleetNodeID)
}

func New(ctx context.Context, cfg config.ClusterConf, ingestCfg config.DataIngestConf, localAgent LocalFleetAgent) *Service {
	service := &Service{
		cfg:        cfg,
		ingestCfg:  ingestCfg,
		repo:       repo.NewClusterRepo(ctx),
		fleetRepo:  repo.NewFleetRepo(ctx),
		localAgent: localAgent,
		closed:     make(chan struct{}),
	}
	service.fleet = newFleetSessionRegistry(service)
	service.fleet.loadPersistedProjections(ctx)
	if err := service.fleetRepo.CancelInterruptedOperations(ctx, time.Now().UTC().UnixMilli()); err != nil {
		logFleetOperationRecoveryError(ctx, err)
	}
	if err := service.fleetRepo.MarkOnlineNodesOffline(ctx, time.Now().UTC().UnixMilli()); err != nil {
		// The listener is not running yet; persisted sessions belong to the
		// previous process. Keep startup available if resetting them fails.
		logFleetSessionResetError(ctx, err)
	}
	return service
}

// ReconcileLicenseRuntime refreshes Cluster processes whose lifetime depends
// on the active Enterprise plan. It is safe to call after every license change.
func (s *Service) ReconcileLicenseRuntime() {
	if s == nil {
		return
	}
	if licenseService := domainlicense.Default(); licenseService != nil && licenseService.IsHost() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := s.stopChildConnectionsForHost(ctx); err != nil {
			logx.Errorf("stop child connections for Fleet center role: %v", err)
		}
		cancel()
	}
	if s.fleet == nil {
		return
	}
	s.fleet.reconcileLicenseRuntime()
}

func logFleetOperationRecoveryError(ctx context.Context, err error) {
	logx.WithContext(ctx).Errorf("cancel interrupted fleet operations: %v", err)
}

func (s *Service) edgeToken(client repo.ConnectClient, node repo.FleetNode) string {
	raw := strings.TrimSpace(client.ClientID + "&" + client.Password)
	if strings.TrimSpace(client.ClientID) == "" || strings.TrimSpace(client.Password) == "" {
		return ""
	}
	return appendTokenMetadata(raw, tokenMetadata{
		Version:             1,
		CenterType:          "enterprise",
		SyncMode:            syncModeEdgeEdge,
		NodeID:              node.FleetNodeID,
		NodeName:            firstNonEmpty(node.Name, client.Name),
		ClientID:            client.ClientID,
		Username:            client.Username,
		Password:            client.Password,
		HTTPBaseURL:         s.cfg.ExternalHTTPBaseURL,
		NetworkBootstrapURL: s.cfg.NetworkBootstrapURL,
		MqttURL:             s.cfg.ExternalMqttBrokerHost,
		TopicRoot:           defaultTopicRoot,
	})
}

var (
	ErrInvalid                 = errors.New("invalid cluster request")
	ErrInvalidJSON             = errors.New("invalid cluster json")
	ErrUnauthorized            = errors.New("invalid cluster token")
	ErrScopeRevisionConflict   = errors.New("FLEET_UNS_SCOPE_REVISION_CONFLICT")
	ErrAccountRevisionConflict = errors.New("FLEET_ACCOUNT_ASSIGNMENT_REVISION_CONFLICT")
	ErrAccountNotAssignable    = errors.New("FLEET_ACCOUNT_NOT_ASSIGNABLE")
	ErrFleetNodeQuotaExceeded  = errors.New("FLEET_NODE_QUOTA_EXCEEDED")
	ErrFleetNodeOnline         = errors.New("FLEET_NODE_ONLINE")
	ErrSyncNotActive           = errors.New("fleet sync connection is not active")
	ErrHostCannotConnect       = errors.New("cluster host cannot connect as child")
)

// BindAudit connects platform operation auditing before the cluster starts.
func (s *Service) BindAudit(service *auditdomain.Service) { s.audit = service }
