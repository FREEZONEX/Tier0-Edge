package container

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/fleet-agent/internal/operation"
	"backend/internal/fleetprotocol"
	"backend/internal/runtimeenv"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	containerapi "github.com/moby/moby/api/types/container"
	networkapi "github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
)

const (
	labelManaged        = "com.tier0.fleet.managed"
	labelInstallationID = "com.tier0.fleet.installation-id"
	labelService        = "com.tier0.fleet.service"
	labelRestartable    = "com.tier0.fleet.restartable"
	labelVisible        = "com.tier0.fleet.visible" // Legacy metadata; only labelHidden controls presentation.
	labelHidden         = "com.tier0.fleet.hidden"
	labelHAGroup        = "com.tier0.ha.group"
	labelHAStartOrder   = "com.tier0.ha.start-order"
	labelHARequired     = "com.tier0.ha.health-required"
	labelComposeProject = "com.docker.compose.project"
	labelComposeService = "com.docker.compose.service"

	defaultLogTail       = 500
	maxLogTail           = 2000
	maxLogLookback       = 24 * time.Hour
	defaultLogLimitBytes = 2 << 20
	maxLogLimitBytes     = 2 << 20
	logTimeout           = 30 * time.Second
	restartStopTimeout   = 30 * time.Second
	restartTotalTimeout  = 120 * time.Second
	operationRetention   = 24 * time.Hour
	maxOperationRecords  = 2048
)

var (
	ErrNotConfigured  = errors.New("fleet container discovery is not configured")
	ErrUnavailable    = errors.New("docker engine is unavailable")
	ErrUnsupported    = errors.New("container operations are not supported by this installation")
	ErrNotFound       = errors.New("fleet container not found")
	ErrNotManaged     = errors.New("container is not managed by this installation")
	ErrRestartDenied  = errors.New("container restart is not allowed")
	ErrConflict       = errors.New("container operation is already running")
	ErrInterrupted    = errors.New("container operation was interrupted")
	ErrInvalid        = errors.New("invalid container request")
	ErrGroupEmpty     = errors.New("Fleet HA service group is empty")
	ErrGroupOperation = errors.New("Fleet HA service group operation failed")
	errLogLimit       = errors.New("container log byte limit reached")
)

type HAGroupStatus struct {
	Name            string
	Total           int
	Running         int
	Stopped         int
	Unhealthy       int
	RequiredStopped int
	Failures        []string
	ObservedAt      int64
}

type HAAppContainerSpec struct {
	Name             string                       `json:"name"`
	ImageID          string                       `json:"imageId"`
	Config           *containerapi.Config         `json:"config"`
	HostConfig       *containerapi.HostConfig     `json:"hostConfig"`
	NetworkingConfig *networkapi.NetworkingConfig `json:"networkingConfig,omitempty"`
}

type HAAppNetworkSpec struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Scope      string            `json:"scope"`
	EnableIPv4 bool              `json:"enableIPv4"`
	EnableIPv6 bool              `json:"enableIPv6"`
	IPAM       networkapi.IPAM   `json:"ipam"`
	Internal   bool              `json:"internal"`
	Attachable bool              `json:"attachable"`
	Options    map[string]string `json:"options,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

type HAAppBundle struct {
	Containers []HAAppContainerSpec `json:"containers"`
	Networks   []HAAppNetworkSpec   `json:"networks,omitempty"`
}

type haGroupContainer struct {
	ID       string
	Service  string
	Order    int
	Required bool
	Running  bool
	Health   string
}

type PortBinding = fleetprotocol.PortBinding
type HealthCheckSummary = fleetprotocol.HealthCheckSummary
type HealthSummary = fleetprotocol.HealthSummary
type Summary = fleetprotocol.Summary

type Detail struct {
	fleetprotocol.Detail
	tty bool
}

type CollectionError = fleetprotocol.CollectionError
type SnapshotSummary = fleetprotocol.SnapshotSummary
type EngineSummary = fleetprotocol.EngineSummary
type Snapshot = fleetprotocol.ContainerSnapshot
type LogsRequest = fleetprotocol.ContainerLogsRequest
type LogsResult = fleetprotocol.ContainerLogsResult
type RestartRequest = fleetprotocol.ContainerRestartRequest
type RestartResult = fleetprotocol.ContainerRestartResult

type restartOperation struct {
	containerID string
	done        chan struct{}
	result      RestartResult
	err         error
	completedAt time.Time
}

type Service struct {
	native         bool
	installationID string
	client         *dockerclient.Client
	initErr        error
	operationMu    sync.Mutex
	operations     map[string]*restartOperation
	running        map[string]string
	operationStore operation.Store
}

func New(installationID string, operationStore operation.Store) *Service {
	service := &Service{
		native:         runtimeenv.IsNative(),
		installationID: strings.TrimSpace(installationID),
		operations:     make(map[string]*restartOperation),
		running:        make(map[string]string),
		operationStore: operationStore,
	}
	if service.native {
		return service
	}
	if service.installationID == "" {
		service.initErr = ErrNotConfigured
		return service
	}
	service.client, service.initErr = dockerclient.New(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if service.initErr != nil {
		service.initErr = fmt.Errorf("%w: %v", ErrUnavailable, service.initErr)
	}
	return service
}

func (s *Service) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	// Native installations have host processes, not managed Docker containers.
	// An empty inventory must not prevent the host snapshot from reaching the center.
	if s != nil && s.native {
		return Snapshot{
			SnapshotID: randomSnapshotID(), ObservedAt: time.Now().UTC().UnixMilli(),
			InstallationID: s.installationID,
			Engine:         EngineSummary{OS: runtime.GOOS, Arch: runtime.GOARCH},
			Containers:     []Summary{}, Errors: []CollectionError{},
		}, nil
	}
	if err := s.ready(); err != nil {
		return Snapshot{}, err
	}
	serverVersion, err := s.client.ServerVersion(ctx, dockerclient.ServerVersionOptions{})
	if err != nil {
		return Snapshot{}, classifyDockerError(err)
	}
	filters := make(dockerclient.Filters).Add("label", labelManaged+"=true").Add("label", labelInstallationID+"="+s.installationID)
	listed, err := s.client.ContainerList(ctx, dockerclient.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return Snapshot{}, classifyDockerError(err)
	}
	result := Snapshot{
		SnapshotID:     randomSnapshotID(),
		ObservedAt:     time.Now().UTC().UnixMilli(),
		InstallationID: s.installationID,
		Engine: EngineSummary{
			Version:    strings.TrimSpace(serverVersion.Version),
			APIVersion: strings.TrimSpace(serverVersion.APIVersion),
			OS:         strings.TrimSpace(serverVersion.Os),
			Arch:       strings.TrimSpace(serverVersion.Arch),
		},
		Containers: make([]Summary, 0, len(listed.Items)),
		Errors:     make([]CollectionError, 0),
	}
	for _, item := range listed.Items {
		if !s.visibleLabels(item.Labels) {
			continue
		}
		detail, inspectErr := s.inspect(ctx, item.ID)
		if inspectErr != nil {
			if errors.Is(inspectErr, ErrNotFound) || errors.Is(inspectErr, ErrNotManaged) {
				continue
			}
			result.Partial = true
			result.Errors = append(result.Errors, CollectionError{ContainerID: item.ID, Code: errorCode(inspectErr)})
			continue
		}
		result.Containers = append(result.Containers, detail.Summary)
	}
	sort.Slice(result.Containers, func(i, j int) bool {
		if result.Containers[i].ServiceName != result.Containers[j].ServiceName {
			return result.Containers[i].ServiceName < result.Containers[j].ServiceName
		}
		return result.Containers[i].ContainerID < result.Containers[j].ContainerID
	})
	result.Summary = fleetprotocol.SummarizeContainers(result.Containers)
	return result, nil
}

func (s *Service) HAReady(ctx context.Context) error {
	if err := s.ready(); err != nil {
		return err
	}
	if _, err := s.client.ServerVersion(ctx, dockerclient.ServerVersionOptions{}); err != nil {
		return classifyDockerError(err)
	}
	return nil
}

func (s *Service) StartHAGroup(ctx context.Context, group string, timeout time.Duration) (HAGroupStatus, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return HAGroupStatus{}, ErrInvalid
	}
	containers, err := s.listHAGroup(ctx, group)
	if err != nil {
		return HAGroupStatus{}, err
	}
	sort.SliceStable(containers, func(i, j int) bool {
		if containers[i].Order != containers[j].Order {
			return containers[i].Order < containers[j].Order
		}
		return containers[i].Service < containers[j].Service
	})
	failures := make([]string, 0)
	for _, item := range containers {
		if item.Running {
			continue
		}
		if _, err := s.client.ContainerStart(ctx, item.ID, dockerclient.ContainerStartOptions{}); err != nil {
			failures = append(failures, item.Service)
		}
	}
	if len(failures) > 0 {
		status, _ := s.HAGroupStatus(ctx, group)
		status.Failures = append(status.Failures, failures...)
		return status, fmt.Errorf("%w: start %s", ErrGroupOperation, strings.Join(failures, ","))
	}
	if timeout <= 0 {
		timeout = restartTotalTimeout
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		status, statusErr := s.HAGroupStatus(ctx, group)
		if statusErr != nil {
			return HAGroupStatus{}, statusErr
		}
		if status.RequiredStopped == 0 && status.Unhealthy == 0 {
			return status, nil
		}
		select {
		case <-ctx.Done():
			return status, ctx.Err()
		case <-deadline.C:
			return status, fmt.Errorf("%w: service group did not become ready", ErrGroupOperation)
		case <-ticker.C:
		}
	}
}

func (s *Service) StopHAGroup(ctx context.Context, group string, timeout time.Duration) (HAGroupStatus, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return HAGroupStatus{}, ErrInvalid
	}
	containers, err := s.listHAGroup(ctx, group)
	if err != nil {
		return HAGroupStatus{}, err
	}
	sort.SliceStable(containers, func(i, j int) bool {
		if containers[i].Order != containers[j].Order {
			return containers[i].Order > containers[j].Order
		}
		return containers[i].Service > containers[j].Service
	})
	if timeout <= 0 {
		timeout = restartStopTimeout
	}
	stopSeconds := min(10, max(1, int(timeout.Seconds())/max(1, len(containers))))
	failures := make([]string, 0)
	for _, item := range containers {
		if !item.Running {
			continue
		}
		stopCtx, cancel := context.WithTimeout(ctx, time.Duration(stopSeconds+5)*time.Second)
		_, err := s.client.ContainerStop(stopCtx, item.ID, dockerclient.ContainerStopOptions{Timeout: &stopSeconds})
		cancel()
		if err != nil {
			failures = append(failures, item.Service)
		}
	}
	status, statusErr := s.HAGroupStatus(ctx, group)
	if statusErr != nil {
		return HAGroupStatus{}, statusErr
	}
	status.Failures = append(status.Failures, failures...)
	if len(failures) > 0 {
		return status, fmt.Errorf("%w: stop %s", ErrGroupOperation, strings.Join(failures, ","))
	}
	return status, nil
}

func (s *Service) HAGroupStatus(ctx context.Context, group string) (HAGroupStatus, error) {
	containers, err := s.listHAGroup(ctx, strings.TrimSpace(group))
	if err != nil {
		return HAGroupStatus{}, err
	}
	status := HAGroupStatus{Name: group, ObservedAt: time.Now().UTC().UnixMilli()}
	for _, item := range containers {
		status.Total++
		if item.Running {
			status.Running++
		} else {
			status.Stopped++
			if item.Required {
				status.RequiredStopped++
			}
		}
		if requiredHAContainerUnhealthy(item) {
			status.Unhealthy++
		}
	}
	return status, nil
}

func (s *Service) HAGroupSnapshot(
	ctx context.Context,
	group string,
	expectedState string,
) (fleetprotocol.HAContainerSnapshot, error) {
	group = strings.TrimSpace(group)
	expectedState = strings.TrimSpace(expectedState)
	if expectedState != "running" && expectedState != "stopped" {
		return fleetprotocol.HAContainerSnapshot{}, ErrInvalid
	}
	containers, err := s.listHAGroup(ctx, group)
	if err != nil {
		return fleetprotocol.HAContainerSnapshot{}, err
	}
	sort.SliceStable(containers, func(i, j int) bool {
		if containers[i].Order != containers[j].Order {
			return containers[i].Order < containers[j].Order
		}
		return containers[i].Service < containers[j].Service
	})
	observedAt := time.Now().UTC().UnixMilli()
	status := HAGroupStatus{Name: group, ObservedAt: observedAt}
	result := fleetprotocol.HAContainerSnapshot{
		Group:         group,
		ExpectedState: expectedState,
		Containers:    make([]fleetprotocol.HAContainerStatus, 0, len(containers)),
		ObservedAt:    observedAt,
	}
	for _, item := range containers {
		status.Total++
		state := "stopped"
		if item.Running {
			state = "running"
			status.Running++
		} else {
			status.Stopped++
			if item.Required {
				status.RequiredStopped++
			}
		}
		if requiredHAContainerUnhealthy(item) {
			status.Unhealthy++
		}
		containerStatus := fleetprotocol.HAContainerStatus{
			ContainerID:   item.ID,
			ServiceName:   item.Service,
			Order:         item.Order,
			Required:      item.Required,
			ExpectedState: expectedState,
			State:         state,
			HealthStatus:  item.Health,
		}
		switch {
		case expectedState == "running" && item.Required && !item.Running:
			containerStatus.DriftRelevant = true
			containerStatus.ReasonCode = "FLEET_HA_REQUIRED_CONTAINER_STOPPED"
		case expectedState == "running" && requiredHAContainerUnhealthy(item):
			containerStatus.DriftRelevant = true
			containerStatus.ReasonCode = "FLEET_HA_REQUIRED_CONTAINER_UNHEALTHY"
		case expectedState == "running" && !item.Required && !item.Running:
			containerStatus.ReasonCode = "FLEET_HA_OPTIONAL_CONTAINER_STOPPED"
		case expectedState == "running" && !item.Required && item.Health == "unhealthy":
			containerStatus.ReasonCode = "FLEET_HA_OPTIONAL_CONTAINER_UNHEALTHY"
		case expectedState == "stopped" && item.Running:
			containerStatus.ReasonCode = "FLEET_HA_STANDBY_CONTAINER_RUNNING"
		}
		result.Containers = append(result.Containers, containerStatus)
	}
	result.Summary = fleetprotocol.HAServiceGroupStatus{
		Name:            status.Name,
		Expected:        expectedState,
		Total:           status.Total,
		Running:         status.Running,
		Stopped:         status.Stopped,
		RequiredStopped: status.RequiredStopped,
		Unhealthy:       status.Unhealthy,
		ObservedAt:      status.ObservedAt,
	}
	return result, nil
}

func requiredHAContainerUnhealthy(item haGroupContainer) bool {
	return item.Required && item.Running && item.Health != "healthy"
}

func (s *Service) listHAGroup(ctx context.Context, group string) ([]haGroupContainer, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if group == "" {
		return nil, ErrInvalid
	}
	filters := make(dockerclient.Filters).
		Add("label", labelManaged+"=true").
		Add("label", labelInstallationID+"="+s.installationID).
		Add("label", labelHAGroup+"="+group)
	listed, err := s.client.ContainerList(ctx, dockerclient.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return nil, classifyDockerError(err)
	}
	out := make([]haGroupContainer, 0, len(listed.Items))
	for _, item := range listed.Items {
		if !s.managedLabels(item.Labels) || strings.TrimSpace(item.Labels[labelHAGroup]) != group {
			continue
		}
		detail, err := s.inspect(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		order, _ := strconv.Atoi(strings.TrimSpace(item.Labels[labelHAStartOrder]))
		out = append(out, haGroupContainer{
			ID:       item.ID,
			Service:  detail.ServiceName,
			Order:    order,
			Required: strings.EqualFold(strings.TrimSpace(item.Labels[labelHARequired]), "true"),
			Running:  detail.State == "running",
			Health:   detail.HealthStatus,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrGroupEmpty, group)
	}
	return out, nil
}

func (s *Service) HAAppSignature(ctx context.Context, group string) (string, error) {
	bundle, _, err := s.collectHAApps(ctx, group)
	if err != nil {
		return "", err
	}
	manifest, err := json.Marshal(bundle)
	if err != nil {
		return "", fmt.Errorf("encode Fleet HA app manifest: %w", err)
	}
	digest := sha256.Sum256(manifest)
	return hex.EncodeToString(digest[:]), nil
}

func (s *Service) ExportHAApps(ctx context.Context, group, outputDir string) (string, error) {
	if err := s.ready(); err != nil {
		return "", err
	}
	if strings.TrimSpace(group) == "" || strings.TrimSpace(outputDir) == "" {
		return "", ErrInvalid
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return "", fmt.Errorf("create Fleet HA app export directory: %w", err)
	}
	bundle, imageRefs, err := s.collectHAApps(ctx, group)
	if err != nil {
		return "", err
	}
	manifest, err := json.Marshal(bundle)
	if err != nil {
		return "", fmt.Errorf("encode Fleet HA app manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "containers.json"), manifest, 0o600); err != nil {
		return "", fmt.Errorf("write Fleet HA app manifest: %w", err)
	}
	if len(imageRefs) > 0 {
		reader, saveErr := s.client.ImageSave(ctx, imageRefs)
		if saveErr != nil {
			return "", classifyDockerError(saveErr)
		}
		defer reader.Close()
		target, createErr := os.OpenFile(filepath.Join(outputDir, "images.tar"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if createErr != nil {
			return "", fmt.Errorf("create Fleet HA app image archive: %w", createErr)
		}
		_, copyErr := io.Copy(target, reader)
		closeErr := target.Close()
		if copyErr != nil {
			return "", fmt.Errorf("save Fleet HA app images: %w", copyErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("close Fleet HA app image archive: %w", closeErr)
		}
	} else if err := os.Remove(filepath.Join(outputDir, "images.tar")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("remove stale Fleet HA app image archive: %w", err)
	}
	digest := sha256.Sum256(manifest)
	return hex.EncodeToString(digest[:]), nil
}

func (s *Service) collectHAApps(ctx context.Context, group string) (HAAppBundle, []string, error) {
	if err := s.ready(); err != nil {
		return HAAppBundle{}, nil, err
	}
	if strings.TrimSpace(group) == "" {
		return HAAppBundle{}, nil, ErrInvalid
	}
	filters := make(dockerclient.Filters).
		Add("label", labelManaged+"=true").
		Add("label", labelInstallationID+"="+s.installationID).
		Add("label", labelHAGroup+"="+group)
	listed, err := s.client.ContainerList(ctx, dockerclient.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return HAAppBundle{}, nil, classifyDockerError(err)
	}
	sort.SliceStable(listed.Items, func(i, j int) bool {
		return listed.Items[i].Labels[labelService] < listed.Items[j].Labels[labelService]
	})
	bundle := HAAppBundle{
		Containers: make([]HAAppContainerSpec, 0),
		Networks:   make([]HAAppNetworkSpec, 0),
	}
	imageRefs := make([]string, 0)
	seenImages := make(map[string]struct{})
	networkNames := make(map[string]struct{})
	for _, item := range listed.Items {
		service := strings.TrimSpace(item.Labels[labelService])
		if !strings.HasPrefix(service, "app-") || !s.managedLabels(item.Labels) {
			continue
		}
		inspected, inspectErr := s.client.ContainerInspect(ctx, item.ID, dockerclient.ContainerInspectOptions{})
		if inspectErr != nil {
			return HAAppBundle{}, nil, classifyDockerError(inspectErr)
		}
		container := inspected.Container
		if container.Config == nil || container.HostConfig == nil {
			return HAAppBundle{}, nil, fmt.Errorf("%w: app container configuration is incomplete", ErrGroupOperation)
		}
		container.HostConfig.RestartPolicy = containerapi.RestartPolicy{Name: containerapi.RestartPolicyDisabled}
		networks := make(map[string]*networkapi.EndpointSettings)
		if container.NetworkSettings != nil {
			for networkName, endpoint := range container.NetworkSettings.Networks {
				if endpoint == nil {
					continue
				}
				networks[networkName] = &networkapi.EndpointSettings{
					IPAMConfig: endpoint.IPAMConfig,
					Aliases:    append([]string(nil), endpoint.Aliases...),
					Links:      append([]string(nil), endpoint.Links...),
					DriverOpts: cloneStringMap(endpoint.DriverOpts),
					GwPriority: endpoint.GwPriority,
				}
				if !isBuiltInDockerNetwork(networkName) {
					networkNames[networkName] = struct{}{}
				}
			}
		}
		name := strings.TrimPrefix(container.Name, "/")
		bundle.Containers = append(bundle.Containers, HAAppContainerSpec{
			Name:       name,
			ImageID:    container.Image,
			Config:     container.Config,
			HostConfig: container.HostConfig,
			NetworkingConfig: &networkapi.NetworkingConfig{
				EndpointsConfig: networks,
			},
		})
		imageRef := strings.TrimSpace(container.Config.Image)
		if imageRef == "" {
			imageRef = strings.TrimSpace(container.Image)
		}
		if _, found := seenImages[imageRef]; imageRef != "" && !found {
			seenImages[imageRef] = struct{}{}
			imageRefs = append(imageRefs, imageRef)
		}
	}
	sortedNetworkNames := make([]string, 0, len(networkNames))
	for networkName := range networkNames {
		sortedNetworkNames = append(sortedNetworkNames, networkName)
	}
	sort.Strings(sortedNetworkNames)
	for _, networkName := range sortedNetworkNames {
		networkResult, inspectErr := s.client.NetworkInspect(ctx, networkName, dockerclient.NetworkInspectOptions{})
		if inspectErr != nil {
			return HAAppBundle{}, nil, classifyDockerError(inspectErr)
		}
		network := networkResult.Network
		if strings.TrimSpace(network.Scope) != "" && network.Scope != "local" {
			return HAAppBundle{}, nil, fmt.Errorf("%w: network %s has unsupported scope %s", ErrGroupOperation, networkName, network.Scope)
		}
		bundle.Networks = append(bundle.Networks, HAAppNetworkSpec{
			Name:       network.Name,
			Driver:     network.Driver,
			Scope:      network.Scope,
			EnableIPv4: network.EnableIPv4,
			EnableIPv6: network.EnableIPv6,
			IPAM:       network.IPAM,
			Internal:   network.Internal,
			Attachable: network.Attachable,
			Options:    cloneStringMap(network.Options),
			Labels:     cloneStringMap(network.Labels),
		})
	}
	sort.Strings(imageRefs)
	return bundle, imageRefs, nil
}

func (s *Service) ImportHAApps(ctx context.Context, group, inputDir string) error {
	if err := s.ready(); err != nil {
		return err
	}
	manifest, err := os.ReadFile(filepath.Join(inputDir, "containers.json"))
	if err != nil {
		return fmt.Errorf("read Fleet HA app manifest: %w", err)
	}
	var bundle HAAppBundle
	if err := json.Unmarshal(manifest, &bundle); err != nil {
		return fmt.Errorf("decode Fleet HA app manifest: %w", err)
	}
	if err := validateHAAppBundle(bundle, s.installationID, group); err != nil {
		return err
	}
	if imageArchive, openErr := os.Open(filepath.Join(inputDir, "images.tar")); openErr == nil {
		result, loadErr := s.client.ImageLoad(ctx, imageArchive)
		_ = imageArchive.Close()
		if loadErr != nil {
			return classifyDockerError(loadErr)
		}
		_, copyErr := io.Copy(io.Discard, result)
		closeErr := result.Close()
		if copyErr != nil {
			return fmt.Errorf("load Fleet HA app images: %w", copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close Fleet HA image load result: %w", closeErr)
		}
	} else if !errors.Is(openErr, os.ErrNotExist) {
		return fmt.Errorf("open Fleet HA app image archive: %w", openErr)
	}
	if err := s.ensureHAAppNetworks(ctx, bundle.Networks); err != nil {
		return err
	}

	filters := make(dockerclient.Filters).
		Add("label", labelManaged+"=true").
		Add("label", labelInstallationID+"="+s.installationID).
		Add("label", labelHAGroup+"="+group)
	current, err := s.client.ContainerList(ctx, dockerclient.ContainerListOptions{All: true, Filters: filters})
	if err != nil {
		return classifyDockerError(err)
	}
	for _, item := range current.Items {
		if !strings.HasPrefix(strings.TrimSpace(item.Labels[labelService]), "app-") {
			continue
		}
		if _, stopErr := s.client.ContainerStop(ctx, item.ID, dockerclient.ContainerStopOptions{Timeout: intPointer(30)}); stopErr != nil && !errdefs.IsNotFound(stopErr) {
			return classifyDockerError(stopErr)
		}
		if _, removeErr := s.client.ContainerRemove(ctx, item.ID, dockerclient.ContainerRemoveOptions{Force: true}); removeErr != nil && !errdefs.IsNotFound(removeErr) {
			return classifyDockerError(removeErr)
		}
	}
	for _, spec := range bundle.Containers {
		spec.HostConfig.RestartPolicy = containerapi.RestartPolicy{Name: containerapi.RestartPolicyDisabled}
		if _, err := s.client.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
			Config:           spec.Config,
			HostConfig:       spec.HostConfig,
			NetworkingConfig: spec.NetworkingConfig,
			Name:             spec.Name,
		}); err != nil {
			return classifyDockerError(err)
		}
	}
	return nil
}

func validateHAAppBundle(bundle HAAppBundle, installationID, group string) error {
	seenContainers := make(map[string]struct{}, len(bundle.Containers))
	for _, spec := range bundle.Containers {
		name := strings.TrimSpace(spec.Name)
		if name == "" || spec.Config == nil || spec.HostConfig == nil || strings.TrimSpace(spec.Config.Image) == "" {
			return fmt.Errorf("%w: invalid Fleet HA app container manifest", ErrGroupOperation)
		}
		if _, found := seenContainers[name]; found {
			return fmt.Errorf("%w: duplicate Fleet HA app container %s", ErrGroupOperation, name)
		}
		seenContainers[name] = struct{}{}
		labels := spec.Config.Labels
		if labels[labelManaged] != "true" ||
			labels[labelInstallationID] != installationID ||
			labels[labelHAGroup] != group ||
			!strings.HasPrefix(strings.TrimSpace(labels[labelService]), "app-") {
			return fmt.Errorf("%w: app container %s is outside the managed Fleet HA scope", ErrGroupOperation, name)
		}
	}
	seenNetworks := make(map[string]struct{}, len(bundle.Networks))
	for _, network := range bundle.Networks {
		name := strings.TrimSpace(network.Name)
		if name == "" || isBuiltInDockerNetwork(name) || strings.TrimSpace(network.Driver) == "" {
			return fmt.Errorf("%w: invalid Fleet HA app network manifest", ErrGroupOperation)
		}
		if network.Scope != "" && network.Scope != "local" {
			return fmt.Errorf("%w: network %s has unsupported scope %s", ErrGroupOperation, name, network.Scope)
		}
		if _, found := seenNetworks[name]; found {
			return fmt.Errorf("%w: duplicate Fleet HA app network %s", ErrGroupOperation, name)
		}
		seenNetworks[name] = struct{}{}
	}
	return nil
}

func (s *Service) ensureHAAppNetworks(ctx context.Context, networks []HAAppNetworkSpec) error {
	for _, network := range networks {
		existing, err := s.client.NetworkInspect(ctx, network.Name, dockerclient.NetworkInspectOptions{})
		if err == nil {
			if strings.TrimSpace(existing.Network.Driver) != strings.TrimSpace(network.Driver) {
				return fmt.Errorf("%w: network %s uses driver %s instead of %s", ErrGroupOperation, network.Name, existing.Network.Driver, network.Driver)
			}
			continue
		}
		if !errdefs.IsNotFound(err) {
			return classifyDockerError(err)
		}
		enableIPv4 := network.EnableIPv4
		enableIPv6 := network.EnableIPv6
		labels := cloneStringMap(network.Labels)
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[labelInstallationID] = s.installationID
		if _, err := s.client.NetworkCreate(ctx, network.Name, dockerclient.NetworkCreateOptions{
			Driver:     network.Driver,
			Scope:      network.Scope,
			EnableIPv4: &enableIPv4,
			EnableIPv6: &enableIPv6,
			IPAM:       &network.IPAM,
			Internal:   network.Internal,
			Attachable: network.Attachable,
			Options:    cloneStringMap(network.Options),
			Labels:     labels,
		}); err != nil {
			return classifyDockerError(err)
		}
	}
	return nil
}

func isBuiltInDockerNetwork(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bridge", "host", "none":
		return true
	default:
		return false
	}
}

func (s *Service) Detail(ctx context.Context, containerID string) (Detail, error) {
	if !validContainerID(containerID) {
		return Detail{}, ErrInvalid
	}
	if err := s.ready(); err != nil {
		return Detail{}, err
	}
	return s.inspect(ctx, containerID)
}

func (s *Service) Logs(ctx context.Context, containerID string, request LogsRequest) (LogsResult, error) {
	if !validContainerID(containerID) {
		return LogsResult{}, ErrInvalid
	}
	if err := s.ready(); err != nil {
		return LogsResult{}, err
	}
	request, err := normalizeLogsRequest(request, time.Now().UTC())
	if err != nil {
		return LogsResult{}, err
	}
	detail, err := s.inspect(ctx, containerID)
	if err != nil {
		return LogsResult{}, err
	}
	logCtx, cancel := context.WithTimeout(ctx, logTimeout)
	defer cancel()
	timestamps := true
	if request.Timestamps != nil {
		timestamps = *request.Timestamps
	}
	options := dockerclient.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       strconv.Itoa(request.Tail),
		Timestamps: timestamps,
	}
	if request.Since > 0 {
		options.Since = time.UnixMilli(request.Since).UTC().Format(time.RFC3339Nano)
	}
	reader, err := s.client.ContainerLogs(logCtx, containerID, options)
	if err != nil {
		return LogsResult{}, classifyDockerError(err)
	}
	defer reader.Close()
	buffer := &limitedBuffer{limit: request.LimitBytes}
	if detailTTY(detail) {
		_, err = io.Copy(buffer, reader)
	} else {
		_, err = stdcopy.StdCopy(buffer, buffer, reader)
	}
	if err != nil && !errors.Is(err, errLogLimit) {
		if errors.Is(logCtx.Err(), context.DeadlineExceeded) {
			return LogsResult{}, context.DeadlineExceeded
		}
		return LogsResult{}, classifyDockerError(err)
	}
	body := buffer.Bytes()
	return LogsResult{
		ContainerID: containerID,
		Logs:        string(body),
		LineCount:   countLines(body),
		Bytes:       len(body),
		Truncated:   buffer.truncated,
		ObservedAt:  time.Now().UTC().UnixMilli(),
	}, nil
}

func (s *Service) Restart(ctx context.Context, containerID string, request RestartRequest) (RestartResult, error) {
	containerID = strings.TrimSpace(containerID)
	request.OperationID = strings.TrimSpace(request.OperationID)
	if !validContainerID(containerID) || !validOperationID(request.OperationID) {
		return RestartResult{}, ErrInvalid
	}
	if err := s.ready(); err != nil {
		return RestartResult{}, err
	}

	now := time.Now().UTC()
	s.operationMu.Lock()
	s.pruneOperationsLocked(now)
	if existing, ok := s.operations[request.OperationID]; ok {
		if existing.containerID != containerID {
			s.operationMu.Unlock()
			return RestartResult{}, ErrInvalid
		}
		done := existing.done
		s.operationMu.Unlock()
		select {
		case <-ctx.Done():
			return RestartResult{}, ctx.Err()
		case <-done:
			return existing.result, existing.err
		}
	}
	if _, busy := s.running[containerID]; busy || len(s.operations) >= maxOperationRecords {
		s.operationMu.Unlock()
		return RestartResult{}, ErrConflict
	}
	record := &restartOperation{containerID: containerID, done: make(chan struct{})}
	s.operations[request.OperationID] = record
	s.running[containerID] = request.OperationID
	s.operationMu.Unlock()

	result, err := s.executeRestart(ctx, containerID, request.OperationID, now)
	s.operationMu.Lock()
	record.result = result
	record.err = err
	record.completedAt = time.Now().UTC()
	delete(s.running, containerID)
	close(record.done)
	s.operationMu.Unlock()
	return result, err
}

func (s *Service) executeRestart(ctx context.Context, containerID, operationID string, startedAt time.Time) (RestartResult, error) {
	result := RestartResult{
		OperationID: operationID,
		ContainerID: containerID,
		Status:      "failed",
		StartedAt:   startedAt.UnixMilli(),
	}
	if s.operationStore != nil {
		existing, created, err := s.operationStore.ClaimOperation(ctx, operation.Record{
			OperationID: operationID,
			Type:        operation.TypeContainerRestart,
			TargetID:    containerID,
			Status:      operation.StatusRunning,
			StartedAt:   result.StartedAt,
			ExpiresAt:   startedAt.Add(operationRetention).UnixMilli(),
		})
		if err != nil {
			return result, fmt.Errorf("%w: persist operation claim", ErrUnavailable)
		}
		if !created {
			return s.replayRestart(ctx, containerID, operationID, existing)
		}
	}
	result, runErr := s.performRestart(ctx, containerID, result)
	if s.operationStore != nil {
		code := ""
		status := operation.StatusSucceeded
		if runErr != nil {
			status = operation.StatusFailed
			code = errorCode(runErr)
		}
		persistCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.operationStore.CompleteOperation(persistCtx, operation.Record{
			OperationID:  operationID,
			Type:         operation.TypeContainerRestart,
			TargetID:     containerID,
			Status:       status,
			ResultDigest: restartResultDigest(result),
			ErrorCode:    code,
			StartedAt:    result.StartedAt,
			FinishedAt:   result.FinishedAt,
			ExpiresAt:    time.Now().UTC().Add(operationRetention).UnixMilli(),
		}); err != nil {
			return result, fmt.Errorf("%w: persist operation result", ErrUnavailable)
		}
	}
	return result, runErr
}

func (s *Service) performRestart(ctx context.Context, containerID string, result RestartResult) (RestartResult, error) {
	operationCtx, cancel := context.WithTimeout(ctx, restartTotalTimeout)
	defer cancel()
	detail, err := s.inspect(operationCtx, containerID)
	if err != nil {
		result.FinishedAt = time.Now().UTC().UnixMilli()
		return result, err
	}
	if !detail.Restartable {
		result.FinishedAt = time.Now().UTC().UnixMilli()
		return result, ErrRestartDenied
	}
	stopTimeoutSeconds := int(restartStopTimeout.Seconds())
	if _, err := s.client.ContainerRestart(operationCtx, containerID, dockerclient.ContainerRestartOptions{Timeout: &stopTimeoutSeconds}); err != nil {
		result.FinishedAt = time.Now().UTC().UnixMilli()
		return result, classifyDockerError(err)
	}
	refreshed, err := s.inspect(operationCtx, containerID)
	if err != nil {
		result.FinishedAt = time.Now().UTC().UnixMilli()
		return result, err
	}
	result.Status = "succeeded"
	result.FinishedAt = time.Now().UTC().UnixMilli()
	result.Container = refreshed.Summary
	return result, nil
}

func (s *Service) replayRestart(ctx context.Context, containerID, operationID string, existing operation.Record) (RestartResult, error) {
	if existing.Type != operation.TypeContainerRestart || existing.TargetID != containerID {
		return RestartResult{}, ErrInvalid
	}
	result := RestartResult{
		OperationID: operationID,
		ContainerID: containerID,
		Status:      existing.Status,
		StartedAt:   existing.StartedAt,
		FinishedAt:  existing.FinishedAt,
	}
	switch existing.Status {
	case operation.StatusSucceeded:
		detail, err := s.inspect(ctx, containerID)
		if err != nil {
			return result, err
		}
		result.Container = detail.Summary
		return result, nil
	case operation.StatusFailed:
		return result, persistedOperationError(existing.ErrorCode)
	default:
		return result, ErrConflict
	}
}

func (s *Service) pruneOperationsLocked(now time.Time) {
	cutoff := now.Add(-operationRetention)
	for operationID, record := range s.operations {
		if !record.completedAt.IsZero() && record.completedAt.Before(cutoff) {
			delete(s.operations, operationID)
		}
	}
}

func (s *Service) inspect(ctx context.Context, containerID string) (Detail, error) {
	inspected, err := s.client.ContainerInspect(ctx, containerID, dockerclient.ContainerInspectOptions{})
	if err != nil {
		return Detail{}, classifyDockerError(err)
	}
	container := inspected.Container
	if container.Config == nil || !s.visibleLabels(container.Config.Labels) {
		return Detail{}, ErrNotManaged
	}
	labels := safeLabels(container.Config.Labels)
	serviceName := firstNonEmpty(labels[labelService], labels[labelComposeService], strings.TrimPrefix(container.Name, "/"), shortContainerID(container.ID))
	displayName := firstNonEmpty(labels[labelComposeService], strings.TrimPrefix(container.Name, "/"), serviceName)
	state := "stopped"
	startedAt := int64(0)
	finishedAt := int64(0)
	health := HealthSummary{Status: "none"}
	if container.State != nil {
		state = normalizeContainerState(string(container.State.Status))
		startedAt = parseDockerTime(container.State.StartedAt)
		finishedAt = parseDockerTime(container.State.FinishedAt)
		health = safeHealth(container.State.Health)
	}
	image := ""
	if container.Config != nil {
		image = container.Config.Image
	}
	summary := Summary{
		ContainerID:    container.ID,
		ContainerName:  strings.TrimPrefix(container.Name, "/"),
		ServiceName:    serviceName,
		DisplayName:    displayName,
		Image:          image,
		State:          state,
		HealthStatus:   health.Status,
		RestartCount:   container.RestartCount,
		StartedAt:      startedAt,
		Restartable:    !restartPermanentlyDenied(serviceName, labels[labelComposeService]),
		Ports:          safePorts(container),
		ComposeService: labels[labelComposeService],
		ComposeProject: labels[labelComposeProject],
		Labels:         labels,
	}
	return Detail{
		Detail: fleetprotocol.Detail{
			Summary:    summary,
			ImageID:    container.Image,
			CreatedAt:  parseDockerTime(container.Created),
			FinishedAt: finishedAt,
			Health:     health,
		},
		tty: container.Config.Tty,
	}, nil
}

func normalizeContainerState(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "running":
		return "running"
	case "restarting":
		return "restarting"
	case "exited", "dead":
		return "exited"
	case "created", "paused", "removing":
		return "stopped"
	default:
		return "stopped"
	}
}

func (s *Service) ready() error {
	if s == nil {
		return ErrUnavailable
	}
	if s.native {
		return ErrUnsupported
	}
	if s.initErr != nil {
		return s.initErr
	}
	if s.client == nil {
		return ErrUnavailable
	}
	return nil
}

func restartPermanentlyDenied(values ...string) bool {
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		switch normalized {
		case "headscale", "headplane", "backend", "pg", "tsdb", "timescale", "timescaledb", "postgres", "postgresql", "redis":
			return true
		}
	}
	return false
}

func (s *Service) managedLabels(labels map[string]string) bool {
	return labels[labelManaged] == "true" && strings.TrimSpace(labels[labelInstallationID]) == s.installationID
}

// visibleLabels keeps ownership and presentation as separate decisions:
// managed + installation-id is the security boundary, while owned containers
// are visible by default unless deployment explicitly marks them hidden.
func (s *Service) visibleLabels(labels map[string]string) bool {
	return s.managedLabels(labels) && labels[labelHidden] != "true"
}

func safeLabels(labels map[string]string) map[string]string {
	out := make(map[string]string, 11)
	for _, key := range []string{
		labelManaged,
		labelInstallationID,
		labelService,
		labelRestartable,
		labelVisible,
		labelHidden,
		labelHAGroup,
		labelHAStartOrder,
		labelHARequired,
		labelComposeProject,
		labelComposeService,
	} {
		if value := strings.TrimSpace(labels[key]); value != "" {
			out[key] = value
		}
	}
	return out
}

func safePorts(container containerapi.InspectResponse) []PortBinding {
	out := make([]PortBinding, 0)
	if container.NetworkSettings == nil {
		return out
	}
	for port, bindings := range container.NetworkSettings.Ports {
		if len(bindings) == 0 {
			out = append(out, PortBinding{ContainerPort: port.Num(), Protocol: string(port.Proto())})
			continue
		}
		for _, binding := range bindings {
			hostIP := ""
			if binding.HostIP.IsValid() {
				hostIP = binding.HostIP.String()
			}
			out = append(out, PortBinding{ContainerPort: port.Num(), Protocol: string(port.Proto()), HostIP: hostIP, HostPort: binding.HostPort})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ContainerPort != out[j].ContainerPort {
			return out[i].ContainerPort < out[j].ContainerPort
		}
		if out[i].Protocol != out[j].Protocol {
			return out[i].Protocol < out[j].Protocol
		}
		if out[i].HostIP != out[j].HostIP {
			return out[i].HostIP < out[j].HostIP
		}
		return out[i].HostPort < out[j].HostPort
	})
	return out
}

func safeHealth(health *containerapi.Health) HealthSummary {
	if health == nil {
		return HealthSummary{Status: "none"}
	}
	result := HealthSummary{Status: string(health.Status), FailingStreak: health.FailingStreak}
	if len(health.Log) == 0 || health.Log[len(health.Log)-1] == nil {
		return result
	}
	last := health.Log[len(health.Log)-1]
	result.LastCheck = &HealthCheckSummary{StartedAt: last.Start.UTC().UnixMilli(), FinishedAt: last.End.UTC().UnixMilli(), ExitCode: last.ExitCode}
	return result
}

func normalizeLogsRequest(request LogsRequest, now time.Time) (LogsRequest, error) {
	if request.Tail == 0 {
		request.Tail = defaultLogTail
	}
	if request.Tail < 1 || request.Tail > maxLogTail {
		return LogsRequest{}, ErrInvalid
	}
	if request.LimitBytes == 0 {
		request.LimitBytes = defaultLogLimitBytes
	}
	if request.LimitBytes < 1 || request.LimitBytes > maxLogLimitBytes {
		return LogsRequest{}, ErrInvalid
	}
	if request.Since > 0 {
		since := time.UnixMilli(request.Since)
		if since.After(now.Add(time.Minute)) || since.Before(now.Add(-maxLogLookback)) {
			return LogsRequest{}, ErrInvalid
		}
	}
	return request, nil
}

func classifyDockerError(err error) error {
	if err == nil {
		return nil
	}
	if errdefs.IsNotFound(err) {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if dockerclient.IsErrConnectionFailed(err) {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, ErrUnsupported):
		return "FLEET_CONTAINER_UNSUPPORTED"
	case errors.Is(err, ErrNotConfigured):
		return "FLEET_INSTALLATION_ID_NOT_CONFIGURED"
	case errors.Is(err, ErrNotFound):
		return "FLEET_CONTAINER_NOT_FOUND"
	case errors.Is(err, ErrNotManaged):
		return "FLEET_CONTAINER_NOT_MANAGED"
	case errors.Is(err, ErrRestartDenied):
		return "FLEET_CONTAINER_RESTART_FORBIDDEN"
	case errors.Is(err, ErrConflict):
		return "FLEET_CONTAINER_OPERATION_CONFLICT"
	case errors.Is(err, ErrInterrupted):
		return "FLEET_OPERATION_INTERRUPTED"
	case errors.Is(err, context.DeadlineExceeded):
		return "FLEET_OPERATION_TIMEOUT"
	case errors.Is(err, context.Canceled):
		return "FLEET_OPERATION_CANCELED"
	default:
		return "FLEET_DOCKER_UNAVAILABLE"
	}
}

func persistedOperationError(code string) error {
	switch strings.TrimSpace(code) {
	case "FLEET_CONTAINER_UNSUPPORTED":
		return ErrUnsupported
	case "FLEET_CONTAINER_NOT_FOUND":
		return ErrNotFound
	case "FLEET_CONTAINER_NOT_MANAGED":
		return ErrNotManaged
	case "FLEET_CONTAINER_RESTART_FORBIDDEN":
		return ErrRestartDenied
	case "FLEET_CONTAINER_OPERATION_CONFLICT":
		return ErrConflict
	case "FLEET_OPERATION_INTERRUPTED":
		return ErrInterrupted
	case "FLEET_OPERATION_TIMEOUT":
		return context.DeadlineExceeded
	case "FLEET_OPERATION_CANCELED":
		return context.Canceled
	default:
		return ErrUnavailable
	}
}

func restartResultDigest(result RestartResult) string {
	body, err := json.Marshal(result)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func validOperationID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 8 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && char != '-' && char != '_' && char != '.' && char != ':' {
			return false
		}
	}
	return true
}

func validContainerID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func randomSnapshotID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	}
	return hex.EncodeToString(buffer)
}

func parseDockerTime(value string) int64 {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed.UTC().UnixMilli()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func shortContainerID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func intPointer(value int) *int {
	return &value
}

func detailTTY(detail Detail) bool {
	return detail.tty
}

func countLines(value []byte) int {
	if len(value) == 0 {
		return 0
	}
	lines := bytes.Count(value, []byte{'\n'})
	if value[len(value)-1] != '\n' {
		lines++
	}
	return lines
}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.truncated = true
		return 0, errLogLimit
	}
	if len(value) <= remaining {
		return b.buffer.Write(value)
	}
	written, _ := b.buffer.Write(value[:remaining])
	b.truncated = true
	return written, errLogLimit
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buffer.Bytes()
}
