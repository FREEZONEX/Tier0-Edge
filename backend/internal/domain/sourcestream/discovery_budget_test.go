package sourcestream_test

import (
	"context"
	"testing"
	"time"

	"backend/internal/contextx"
	domain "backend/internal/domain/sourcestream"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

type discoveryBudgetFixture struct {
	protocolFixture
	deadline time.Time
}

func (f *discoveryBudgetFixture) Discover(ctx context.Context, id int64, source domain.RuntimeSource, credential domain.Credential, parent string) ([]domain.NativeResource, error) {
	f.deadline, _ = ctx.Deadline()
	return f.protocolFixture.Discover(ctx, id, source, credential, parent)
}

func TestServiceDiscoveryBudgetsPreserveCallerDeadline(t *testing.T) {
	definitions := []struct {
		definition domain.SourceDefinition
		budget     time.Duration
	}{
		{domain.SourceDefinition{Name: "mqtt", Protocol: domain.MQTT, Endpoint: "mqtt://fixture:1883", RuntimeLocation: "local", MQTT: &domain.MQTTConnection{ClientID: "discovery-budget"}}, 10 * time.Second},
		{domain.SourceDefinition{Name: "opcua", Protocol: domain.OPCUA, Endpoint: "opc.tcp://fixture:4840", RuntimeLocation: "local", OPCUA: &domain.OPCUAConnection{SecurityMode: "None", SecurityPolicy: "None"}}, 35 * time.Second},
		{domain.SourceDefinition{Name: "modbus", Protocol: domain.ModbusTCP, Endpoint: "tcp://fixture:502", RuntimeLocation: "local", Modbus: &domain.ModbusConnection{UnitID: 1}}, 10 * time.Second},
	}
	for _, item := range definitions {
		t.Run(item.definition.Name, func(t *testing.T) {
			testkit.NewTestDB(t)
			ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.read": {}, "uns.write": {}, "uns.source": {}}})
			runtime := &discoveryBudgetFixture{}
			service := domain.New(runtime, nil, "")
			source, err := service.CreateSource(ctx, item.definition, 0)
			require.NoError(t, err)
			id := source["id"].(int64)
			revision := source["revision"].(int64)
			started := time.Now()
			_, err = service.Discover(ctx, id, revision, "")
			require.NoError(t, err)
			require.WithinDuration(t, started.Add(item.budget), runtime.deadline, time.Second)

			// A shorter caller deadline remains authoritative for every protocol.
			parentDeadline := time.Now().Add(2 * time.Second)
			limited, cancel := context.WithDeadline(ctx, parentDeadline)
			defer cancel()
			_, err = service.Discover(limited, id, revision, "")
			require.NoError(t, err)
			require.Equal(t, parentDeadline, runtime.deadline)
		})
	}
}
