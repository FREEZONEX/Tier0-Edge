// local_agent_test.go 覆盖本机 fleet agent 交互（local_agent.go）：
// 正常路径：未配置 agent 时各入口返回 FLEET_AGENT_NOT_CONFIGURED，
//
//	伪 agent 下的状态/HA/容器/日志/重启成功路径，
//	CurrentNode 状态序列化与 HA 分支。
//
// 异常情况：空 accessToken、agent 返回错误（notConfigured/unavailable/
//
//	FleetAgentResponseError 映射）。
//
// 说明：真实 fleet agent 进程不在测试环境，全部通过伪 LocalFleetAgent 注入。
package cluster

import (
	"context"
	"testing"

	"backend/internal/fleetprotocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFleetAgent 实现 LocalFleetAgent 的测试替身。
type fakeFleetAgent struct {
	statusErr   error
	status      *fleetprotocol.AgentStatus
	haErr       error
	haEnabled   bool
	ha          *fleetprotocol.HAStatus
	configErr   error
	disconnect  *fleetprotocol.AgentStatus
	containers  *fleetprotocol.ContainerSnapshot
	container   *fleetprotocol.Detail
	logs        *fleetprotocol.ContainerLogsResult
	restart     *fleetprotocol.ContainerRestartResult
	lastToken   string
	lastRestart FleetAgentRestartRequest
}

func (f *fakeFleetAgent) Status(context.Context) (*fleetprotocol.AgentStatus, error) {
	return f.status, f.statusErr
}
func (f *fakeFleetAgent) HAStatus(context.Context) (*fleetprotocol.HAStatus, error) {
	if f.haErr != nil {
		return nil, f.haErr
	}
	return &fleetprotocol.HAStatus{Enabled: f.haEnabled}, nil
}
func (f *fakeFleetAgent) ConfigureConnection(_ context.Context, token string) (*fleetprotocol.AgentStatus, error) {
	f.lastToken = token
	return f.status, f.configErr
}
func (f *fakeFleetAgent) DisconnectConnection(context.Context) (*fleetprotocol.AgentStatus, error) {
	return f.disconnect, nil
}
func (f *fakeFleetAgent) Containers(context.Context) (*fleetprotocol.ContainerSnapshot, error) {
	return f.containers, nil
}
func (f *fakeFleetAgent) Container(context.Context, string) (*fleetprotocol.Detail, error) {
	return f.container, nil
}
func (f *fakeFleetAgent) ContainerLogs(context.Context, string, FleetAgentLogsRequest) (*fleetprotocol.ContainerLogsResult, error) {
	return f.logs, nil
}
func (f *fakeFleetAgent) RestartContainer(_ context.Context, _ string, req FleetAgentRestartRequest) (*fleetprotocol.ContainerRestartResult, error) {
	f.lastRestart = req
	return f.restart, nil
}

func TestConfigureCurrentConnection_NotConfiguredAndInvalid(t *testing.T) {
	s := &Service{}
	_, err := s.ConfigureCurrentConnection(context.Background(), "tok")
	require.ErrorIs(t, err, ErrFleetAgentNotConfigured)
	s.localAgent = &fakeFleetAgent{}
	_, err = s.ConfigureCurrentConnection(context.Background(), "  ")
	require.ErrorIs(t, err, ErrInvalid)
}

func TestConfigureCurrentConnection_RejectsMalformedTokenBeforeAgent(t *testing.T) {
	agent := &fakeFleetAgent{status: &fleetprotocol.AgentStatus{Service: "edge", Configured: true}}
	s := &Service{localAgent: agent}
	status, err := s.ConfigureCurrentConnection(context.Background(), "tok-1")
	require.ErrorIs(t, err, ErrInvalid)
	assert.Nil(t, status)
	assert.Empty(t, agent.lastToken)
}

func TestDisconnectCurrentConnection(t *testing.T) {
	s := &Service{}
	_, err := s.DisconnectCurrentConnection(context.Background())
	require.ErrorIs(t, err, ErrFleetAgentNotConfigured)
	s.localAgent = &fakeFleetAgent{disconnect: &fleetprotocol.AgentStatus{ConnectionState: fleetprotocol.ConnectionDisconnected}}
	status, err := s.DisconnectCurrentConnection(context.Background())
	require.NoError(t, err)
	assert.Equal(t, fleetprotocol.ConnectionDisconnected, status.ConnectionState)
}

func TestStopChildConnectionsForHostDisconnectsLocalAgent(t *testing.T) {
	agent := &countingFleetAgent{fakeFleetAgent: &fakeFleetAgent{
		disconnect: &fleetprotocol.AgentStatus{ConnectionState: fleetprotocol.ConnectionDisconnected},
	}}
	s := &Service{localAgent: agent}

	err := s.stopChildConnectionsForHost(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, agent.disconnects)
}

type countingFleetAgent struct {
	*fakeFleetAgent
	disconnects int
}

func (f *countingFleetAgent) DisconnectConnection(ctx context.Context) (*fleetprotocol.AgentStatus, error) {
	f.disconnects++
	return f.fakeFleetAgent.DisconnectConnection(ctx)
}

func TestCurrentNode_NotConfigured(t *testing.T) {
	s := &Service{}
	resp, err := s.CurrentNode(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "edge", resp["role"])
	agent := resp["agent"].(map[string]any)
	assert.Equal(t, "notConfigured", agent["availability"])
}

func TestCurrentNode_AgentErrors(t *testing.T) {
	s := &Service{localAgent: &fakeFleetAgent{statusErr: ErrFleetAgentNotConfigured}}
	resp, err := s.CurrentNode(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "notConfigured", resp["agent"].(map[string]any)["availability"])

	s.localAgent = &fakeFleetAgent{statusErr: ErrFleetAgentUnavailable}
	resp, err = s.CurrentNode(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "unavailable", resp["agent"].(map[string]any)["availability"])

	s.localAgent = &fakeFleetAgent{statusErr: &FleetAgentResponseError{Code: "FLEET_AGENT_BUSY"}}
	resp, err = s.CurrentNode(context.Background())
	require.NoError(t, err)
	agent := resp["agent"].(map[string]any)
	assert.Equal(t, "error", agent["availability"])
	assert.Equal(t, "FLEET_AGENT_BUSY", agent["errorCode"])

	s.localAgent = &fakeFleetAgent{statusErr: context.DeadlineExceeded}
	resp, err = s.CurrentNode(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "error", resp["agent"].(map[string]any)["availability"])
}

func TestCurrentNode_SuccessWithHA(t *testing.T) {
	agent := &fakeFleetAgent{
		status:    &fleetprotocol.AgentStatus{Service: "edge", Version: "1.2.3", Configured: true},
		haEnabled: true,
	}
	s := &Service{localAgent: agent}
	resp, err := s.CurrentNode(context.Background())
	require.NoError(t, err)
	agentMap := resp["agent"].(map[string]any)
	assert.Equal(t, "available", agentMap["availability"])
	assert.Equal(t, "1.2.3", agentMap["version"])
	ha := resp["ha"].(map[string]any)
	assert.Equal(t, true, ha["enabled"])

	// HA 未启用。
	s.localAgent = &fakeFleetAgent{status: agent.status, haEnabled: false}
	resp, err = s.CurrentNode(context.Background())
	require.NoError(t, err)
	assert.Equal(t, false, resp["ha"].(map[string]any)["enabled"])

	// HA 查询失败。
	s.localAgent = &fakeFleetAgent{status: agent.status, haErr: &FleetAgentResponseError{Code: "FLEET_HA_UNAVAILABLE"}}
	resp, err = s.CurrentNode(context.Background())
	require.NoError(t, err)
	ha = resp["ha"].(map[string]any)
	assert.Equal(t, false, ha["enabled"])
	assert.Equal(t, "FLEET_HA_UNAVAILABLE", ha["errorCode"])
}

func TestFleetAgentErrorCode(t *testing.T) {
	assert.Equal(t, "FLEET_X", fleetAgentErrorCode(&FleetAgentResponseError{Code: "FLEET_X"}))
	assert.Equal(t, ErrFleetAgentNotConfigured.Error(), fleetAgentErrorCode(ErrFleetAgentNotConfigured))
	assert.Equal(t, ErrFleetAgentUnavailable.Error(), fleetAgentErrorCode(nil))
	assert.Equal(t, ErrFleetAgentUnavailable.Error(), fleetAgentErrorCode(context.DeadlineExceeded))
}

func TestCurrentContainers(t *testing.T) {
	s := &Service{}
	_, err := s.CurrentContainers(context.Background())
	require.ErrorIs(t, err, ErrFleetAgentNotConfigured)
	agent := &fakeFleetAgent{containers: &fleetprotocol.ContainerSnapshot{
		Containers: []fleetprotocol.Summary{{ContainerID: "c1", State: "running"}},
	}}
	s.localAgent = agent
	snapshot, err := s.CurrentContainers(context.Background())
	require.NoError(t, err)
	// 归一化：Total/Running 被填充。
	assert.Equal(t, 1, snapshot.Summary.Total)
	assert.Equal(t, 1, snapshot.Summary.Running)
}

func TestCurrentContainer(t *testing.T) {
	s := &Service{}
	_, err := s.CurrentContainer(context.Background(), "c1")
	require.ErrorIs(t, err, ErrFleetAgentNotConfigured)
	s.localAgent = &fakeFleetAgent{container: &fleetprotocol.Detail{ImageID: "img-1"}}
	detail, err := s.CurrentContainer(context.Background(), " c1 ")
	require.NoError(t, err)
	assert.Equal(t, "img-1", detail.ImageID)
}

func TestCurrentContainerLogs(t *testing.T) {
	s := &Service{}
	_, err := s.CurrentContainerLogs(context.Background(), CurrentContainerLogsCommand{ContainerID: "c1"})
	require.ErrorIs(t, err, ErrFleetAgentNotConfigured)
	agent := &fakeFleetAgent{logs: &fleetprotocol.ContainerLogsResult{ContainerID: "c1", LineCount: 3}}
	s.localAgent = agent
	timestamps := false
	result, err := s.CurrentContainerLogs(context.Background(), CurrentContainerLogsCommand{
		ContainerID: " c1 ", Tail: 10, Since: 5, Timestamps: &timestamps, LimitBytes: 100,
	})
	require.NoError(t, err)
	assert.Equal(t, 3, result.LineCount)
}

func TestRestartCurrentContainer(t *testing.T) {
	s := &Service{}
	_, err := s.RestartCurrentContainer(context.Background(), CurrentContainerRestartCommand{ContainerID: "c1"})
	require.ErrorIs(t, err, ErrFleetAgentNotConfigured)
	agent := &fakeFleetAgent{restart: &fleetprotocol.ContainerRestartResult{ContainerID: "c1", Status: "ok"}}
	s.localAgent = agent
	result, err := s.RestartCurrentContainer(context.Background(), CurrentContainerRestartCommand{ContainerID: "c1"})
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Status)
	assert.NotEmpty(t, agent.lastRestart.OperationID) // 未指定 RequestID → 自动生成

	result, err = s.RestartCurrentContainer(context.Background(), CurrentContainerRestartCommand{ContainerID: "c1", RequestID: "req-9"})
	require.NoError(t, err)
	assert.Equal(t, "req-9", agent.lastRestart.OperationID)
}
