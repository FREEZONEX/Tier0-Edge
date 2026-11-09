package sourceprotocol

// 真实 MQTT Broker：覆盖持久会话范围切换、连接前排队消息、旧订阅清理，
// 以及持久接收失败/取消不得提前 ACK。只清理各测试自己的随机会话。
import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func mqttToken(t *testing.T, token mqtt.Token) {
	t.Helper()
	require.True(t, token.WaitTimeout(5*time.Second), "MQTT operation timed out")
	require.NoError(t, token.Error())
}

func mqttRecoveryFixture(t *testing.T) (sourcestream.SourceDefinition, string, mqtt.Client) {
	t.Helper()
	broker := os.Getenv("TEST_SOURCE_MQTT_URL")
	if broker == "" {
		t.Skip("requires a dedicated real MQTT protocol test Broker")
	}
	key := "recovery-" + uuid.NewString()
	clientID := "source-9911-" + key + "-acquire"
	def := sourcestream.SourceDefinition{Name: "MQTT recovery test", Protocol: sourcestream.MQTT, Endpoint: strings.Replace(broker, "tcp://", "mqtt://", 1), RuntimeLocation: "local", MQTT: &sourcestream.MQTTOptions{ClientID: key, QoS: 1, TopicFilters: []string{key + "/current"}}}
	clientID += "-" + mqttWatchScope(def)
	publisher := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(key + "-publisher"))
	mqttToken(t, publisher.Connect())
	t.Cleanup(func() {
		publisher.Disconnect(100)
		// Only this fixture's isolated session, never a product Source.
		cleanup := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(clientID).SetCleanSession(true))
		mqttToken(t, cleanup.Connect())
		cleanup.Disconnect(100)
	})
	return def, broker, publisher
}

func mqttSeedSubscriptions(t *testing.T, broker string, def sourcestream.SourceDefinition, filters []string) {
	t.Helper()
	client := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID(fmt.Sprintf("source-9911-%s-acquire-%s", def.MQTT.ClientID, mqttWatchScope(def))).SetCleanSession(false))
	mqttToken(t, client.Connect())
	for _, filter := range filters {
		mqttToken(t, client.Subscribe(filter, 1, func(_ mqtt.Client, message mqtt.Message) { message.Ack() }))
	}
	client.Disconnect(100)
}

func mqttStopWatch(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not stop")
	}
}

func TestRealMQTTFirstMessageAfterConfirmedSubscription(t *testing.T) {
	def, _, publisher := mqttRecoveryFixture(t)
	def.MQTT.TopicFilters = []string{def.MQTT.ClientID + "/first/+", def.MQTT.ClientID + "/second/+"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{}, 1)
	ctx = sourcestream.WithWatchObserver(ctx, func(value bool) {
		if value {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	})
	inputs := make(chan Input, 2)
	done := make(chan error, 1)
	go func() {
		done <- New().Watch(ctx, 9911, def, sourcestream.Credential{}, func(input Input) error { inputs <- input; return nil })
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("complete subscription was not confirmed")
	}
	for _, suffix := range []string{"/first/value", "/second/value"} {
		topic := def.MQTT.ClientID + suffix
		mqttToken(t, publisher.Publish(topic, 1, false, `{"value":42}`))
		select {
		case input := <-inputs:
			require.Equal(t, "mqtt:"+topic, input.ResourceRef)
		case <-time.After(5 * time.Second):
			t.Fatal("first non-retained message after readiness was lost")
		}
	}
	mqttStopWatch(t, cancel, done)
}

func TestRealMQTTSharedConsumersCancelIndependently(t *testing.T) {
	def, _, publisher := mqttRecoveryFixture(t)
	runtime := New()
	defer runtime.Close()
	inputs := []chan Input{make(chan Input, 4), make(chan Input, 4)}
	cancels := make([]context.CancelFunc, 2)
	done := []chan error{make(chan error, 1), make(chan error, 1)}
	for i := range inputs {
		ctx, cancel := context.WithCancel(context.Background())
		cancels[i] = cancel
		defer cancel()
		ready := make(chan struct{}, 1)
		ctx = sourcestream.WithWatchObserver(ctx, func(value bool) {
			if value {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
		})
		go func(i int) {
			done[i] <- runtime.Watch(ctx, 9911, def, sourcestream.Credential{}, func(input Input) error { inputs[i] <- input; return nil })
		}(i)
		select {
		case <-ready:
		case <-time.After(5 * time.Second):
			t.Fatal("shared consumer not ready")
		}
	}
	runtime.watchMu.Lock()
	count := len(runtime.watches)
	runtime.watchMu.Unlock()
	require.Equal(t, 1, count, "identical demand must own one protocol watcher")
	mqttToken(t, publisher.Publish(def.MQTT.TopicFilters[0], 1, false, `{"value":1}`))
	for _, input := range inputs {
		select {
		case <-input:
		case <-time.After(5 * time.Second):
			t.Fatal("shared delivery missing")
		}
	}
	mqttStopWatch(t, cancels[0], done[0])
	mqttToken(t, publisher.Publish(def.MQTT.TopicFilters[0], 1, false, `{"value":2}`))
	select {
	case <-inputs[1]:
	case <-time.After(5 * time.Second):
		t.Fatal("remaining consumer interrupted")
	}
	mqttStopWatch(t, cancels[1], done[1])
	require.Eventually(t, func() bool { runtime.watchMu.Lock(); defer runtime.watchMu.Unlock(); return len(runtime.watches) == 0 }, time.Second, 10*time.Millisecond)
}

// Wire-level fixture exercises negative SUBACKs that a library Token does not
// report as errors. It is not used as evidence of Broker auth or industrial IO.
func TestMQTTWatchRequiresCompleteGrantedSUBACK(t *testing.T) {
	for _, test := range []struct {
		name  string
		codes []byte
		ready bool
	}{
		{"accepted", []byte{1}, true}, {"rejected", []byte{0x80}, false},
		{"downgraded", []byte{0}, false}, {"missing-grant", []byte{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()
			serverDone := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					serverDone <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				packet, err := packets.ReadPacket(conn)
				if err != nil {
					serverDone <- err
					return
				}
				if _, ok := packet.(*packets.ConnectPacket); !ok {
					serverDone <- errors.New("expected CONNECT")
					return
				}
				if err = packets.NewControlPacket(packets.Connack).Write(conn); err != nil {
					serverDone <- err
					return
				}
				packet, err = packets.ReadPacket(conn)
				if err != nil {
					serverDone <- err
					return
				}
				subscribe, ok := packet.(*packets.SubscribePacket)
				if !ok {
					serverDone <- errors.New("expected SUBSCRIBE")
					return
				}
				ack := packets.NewControlPacket(packets.Suback).(*packets.SubackPacket)
				ack.MessageID, ack.ReturnCodes = subscribe.MessageID, test.codes
				if err = ack.Write(conn); err != nil {
					serverDone <- err
					return
				}
				_, _ = packets.ReadPacket(conn) // cancel/error must close this attempt
				serverDone <- nil
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var ready atomic.Bool
			ctx = sourcestream.WithWatchObserver(ctx, func(value bool) {
				if value {
					ready.Store(true)
					cancel()
				}
			})
			def := sourcestream.SourceDefinition{Name: "suback fixture", Protocol: sourcestream.MQTT, Endpoint: "mqtt://" + listener.Addr().String(), RuntimeLocation: "local", MQTT: &sourcestream.MQTTOptions{ClientID: "suback", QoS: 1, TopicFilters: []string{"isolated/+"}}}
			err = New().Watch(ctx, 1, def, sourcestream.Credential{}, func(Input) error { return nil })
			if test.ready {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, ErrProtocol)
			}
			require.Equal(t, test.ready, ready.Load())
			select {
			case err := <-serverDone:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("fixture did not stop")
			}
		})
	}
}

func TestRealMQTTCleanSessionsAndIndependentCollectionScopes(t *testing.T) {
	def, broker, publisher := mqttRecoveryFixture(t)
	runtime := New()
	defer runtime.Close()
	other := def.Connection()
	other.MQTT.TopicFilters = []string{def.MQTT.ClientID + "/other"}
	other.MQTT.QoS = 2
	other.MQTT.Subscriptions = map[string]byte{other.MQTT.TopicFilters[0]: 2}
	// Even a previously durable broker session is deliberately reset. Current
	// offline input is not guaranteed until the platform obtains RAW PubAck.
	mqttSeedSubscriptions(t, broker, def, def.MQTT.TopicFilters)
	mqttToken(t, publisher.Publish(def.MQTT.TopicFilters[0], 1, false, `{"phase":"offline"}`))
	type running struct {
		cancel context.CancelFunc
		done   chan error
		inputs chan Input
	}
	start := func(scoped sourcestream.SourceDefinition) running {
		ctx, cancel := context.WithCancel(context.Background())
		ready := make(chan bool, 8)
		ctx = sourcestream.WithWatchObserver(ctx, func(ok bool) {
			if ok {
				select {
				case ready <- true:
				default:
				}
			}
		})
		result := running{cancel, make(chan error, 1), make(chan Input, 8)}
		go func() {
			result.done <- runtime.Watch(ctx, 9911, scoped, sourcestream.Credential{}, func(input Input) error { result.inputs <- input; return nil })
		}()
		select {
		case <-ready:
		case err := <-result.done:
			t.Fatalf("watch ended before ready: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("real SUBACK readiness missing")
		}
		t.Cleanup(cancel)
		return result
	}
	first := start(def)
	second := start(other)
	select {
	case input := <-first.inputs:
		t.Fatalf("clean session resumed offline input: %v", input)
	case <-time.After(150 * time.Millisecond):
	}
	mqttToken(t, publisher.Publish(def.MQTT.TopicFilters[0], 1, false, `{"phase":"first"}`))
	mqttToken(t, publisher.Publish(other.MQTT.TopicFilters[0], 2, false, `{"phase":"second"}`))
	for _, item := range []struct {
		watch running
		phase string
	}{{first, "first"}, {second, "second"}} {
		select {
		case input := <-item.watch.inputs:
			require.Equal(t, item.phase, input.Data.(map[string]any)["phase"])
		case <-time.After(5 * time.Second):
			t.Fatal("fresh scope input missing")
		}
	}
	mqttStopWatch(t, first.cancel, first.done)
	mqttToken(t, publisher.Publish(other.MQTT.TopicFilters[0], 2, false, `{"phase":"survives"}`))
	select {
	case input := <-second.inputs:
		require.Equal(t, "survives", input.Data.(map[string]any)["phase"])
	case err := <-second.done:
		t.Fatalf("another scope cancellation stopped survivor: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("surviving scope stopped")
	}
	mqttStopWatch(t, second.cancel, second.done)
}

// Wire-level MQTT fixture proves ACK timing independently of any Broker's
// offline retention policy. This is a real TCP/MQTT exchange, not HTTP Mock.
func TestMQTTWatchFailureDoesNotSendPubAck(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled=%t", canceled), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()
			def := sourcestream.SourceDefinition{Name: "ack fixture", Protocol: sourcestream.MQTT, Endpoint: "mqtt://" + listener.Addr().String(), MQTT: &sourcestream.MQTTOptions{ClientID: "ack", QoS: 1, TopicFilters: []string{"fixture/value"}}}
			acknowledged := make(chan bool, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					acknowledged <- false
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				if _, err = packets.ReadPacket(conn); err != nil {
					acknowledged <- false
					return
				}
				if err = packets.NewControlPacket(packets.Connack).Write(conn); err != nil {
					acknowledged <- false
					return
				}
				p, err := packets.ReadPacket(conn)
				if err != nil {
					acknowledged <- false
					return
				}
				sub, ok := p.(*packets.SubscribePacket)
				if !ok {
					acknowledged <- false
					return
				}
				suback := packets.NewControlPacket(packets.Suback).(*packets.SubackPacket)
				suback.MessageID, suback.ReturnCodes = sub.MessageID, []byte{1}
				if err = suback.Write(conn); err != nil {
					acknowledged <- false
					return
				}
				message := packets.NewControlPacket(packets.Publish).(*packets.PublishPacket)
				message.TopicName, message.MessageID, message.Qos, message.Payload = "fixture/value", 17, 1, []byte(`{"value":42}`)
				if err = message.Write(conn); err != nil {
					acknowledged <- false
					return
				}
				for {
					p, err = packets.ReadPacket(conn)
					if err != nil {
						acknowledged <- false
						return
					}
					if _, ok = p.(*packets.PubackPacket); ok {
						acknowledged <- true
						return
					}
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			entered := make(chan struct{}, 1)
			done := make(chan error, 1)
			unavailable := errors.New("RAW persistence failed")
			runtime := New()
			defer runtime.Close()
			go func() {
				done <- runtime.Watch(ctx, 9911, def, sourcestream.Credential{}, func(Input) error {
					entered <- struct{}{}
					if canceled {
						<-ctx.Done()
						return ctx.Err()
					}
					return unavailable
				})
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("real MQTT input missing")
			}
			if canceled {
				cancel()
			}
			select {
			case err := <-done:
				if canceled {
					require.ErrorIs(t, err, context.Canceled)
				} else {
					require.ErrorIs(t, err, unavailable)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("failed watch did not stop")
			}
			select {
			case ack := <-acknowledged:
				require.False(t, ack, "failed RAW persistence must not send PUBACK")
			case <-time.After(5 * time.Second):
				t.Fatal("MQTT fixture did not terminate")
			}
		})
	}
}
