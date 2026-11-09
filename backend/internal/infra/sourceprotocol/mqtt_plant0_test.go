package sourceprotocol

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Actual Plant0 ingress only. Never publish, restart the simulator, or claim
// that its non-retained QoS 0 messages can be recovered after disconnection.
func TestPlant0RealMQTTReadOnlySubscriptions(t *testing.T) {
	if os.Getenv("PLANT0_REAL_ACCEPTANCE") != "1" {
		t.Skip("requires explicit opt-in for the Plant0 simulation broker")
	}
	def := sourcestream.SourceDefinition{
		Name: "Plant0 protocol acceptance", Protocol: sourcestream.MQTT,
		Endpoint: "mqtt://52.221.121.67:1883", RuntimeLocation: "local",
		MQTT: &sourcestream.MQTTOptions{ClientID: "tier0-plant0-" + uuid.NewString(),
			TopicFilters: []string{"iot/+/telemetry", "vibration/+/data"}, QoS: 0, Ephemeral: true},
	}
	require.NoError(t, def.Validate())
	for _, phase := range []string{"first-connect", "own-runtime-reconnect"} {
		t.Run(phase, func(t *testing.T) {
			runtime := New()
			defer runtime.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()
			inputs := make(chan Input, 256)
			done := make(chan error, 1)
			go func() {
				done <- runtime.Watch(ctx, 970011, def, sourcestream.Credential{}, func(input Input) error {
					select {
					case inputs <- input:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			}()
			groups := map[string]bool{}
			devices := map[string]bool{}
			for len(groups) < 2 {
				select {
				case input := <-inputs:
					topic := strings.TrimPrefix(input.ResourceRef, "mqtt:")
					parts := strings.Split(topic, "/")
					require.Len(t, parts, 3)
					require.NotEmpty(t, parts[1])
					if parts[0] == "iot" {
						require.Equal(t, "telemetry", parts[2])
					} else {
						require.Equal(t, "vibration", parts[0])
						require.Equal(t, "data", parts[2])
					}
					require.NotNil(t, input.Data)
					require.Positive(t, input.ReceivedAt)
					raw, err := json.Marshal(input.Data)
					require.NoError(t, err)
					require.True(t, json.Valid(raw))
					groups[parts[0]], devices[parts[1]] = true, true
				case err := <-done:
					t.Fatalf("watch ended before both actual topic groups arrived: %v", err)
				case <-ctx.Done():
					t.Fatal("actual topic groups not received before bounded deadline")
				}
			}
			t.Logf("actual non-retained inputs: groups=%d deviceKeys=%d; QoS 0 gap replay is not supported", len(groups), len(devices))
			cancel()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(5 * time.Second):
				t.Fatal("our subscription did not stop")
			}
		})
	}
}
