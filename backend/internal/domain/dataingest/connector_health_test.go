package dataingest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type connectorHealthObserverSpy struct {
	signals []ConnectorHealthSignal
}

func (s *connectorHealthObserverSpy) RecordConnectorHealth(_ context.Context, signal ConnectorHealthSignal) {
	s.signals = append(s.signals, signal)
}

func TestService_ReportMQTTConnectorUsesExplicitDeduplicatedStates(t *testing.T) {
	observer := &connectorHealthObserverSpy{}
	service := &Service{}
	service.SetConnectorHealthObserver(observer)

	service.reportMQTTConnector(ConnectorStateLost, "mqttConnectionLost")
	service.reportMQTTConnector(ConnectorStateLost, "mqttConnectionLost")
	service.reportMQTTConnector(ConnectorStateConnected, "")

	require.Len(t, observer.signals, 2)
	require.Equal(t, ConnectorSourceMQTT, observer.signals[0].Source)
	require.Equal(t, ConnectorStateLost, observer.signals[0].State)
	require.Equal(t, "mqttConnectionLost", observer.signals[0].ErrorCode)
	require.False(t, observer.signals[0].ObservedAt.IsZero())
	require.Equal(t, ConnectorStateConnected, observer.signals[1].State)
	require.Empty(t, observer.signals[1].ErrorCode)
}
