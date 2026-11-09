package sourceprotocol

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/require"
)

// 已断开的连接必须在首次操作前重建；不能让第一条控制命令充当探针。
// Client / managed Server 以及 Publish / RPC 共用同一恢复边界。
func TestRealMQTTClosedConnectionReplacedBeforeFirstDispatch(t *testing.T) {
	for _, server := range []bool{false, true} {
		for _, rpc := range []bool{false, true} {
			name := "client-publish"
			if server {
				name = "server-publish"
			}
			if rpc {
				name += "-rpc"
			}
			t.Run(name, func(t *testing.T) {
				def, broker, device := mqttRecoveryFixture(t)
				def.MQTT.RequestTopic, def.MQTT.ReplyTopic, def.MQTT.CorrelationField = def.MQTT.ClientID+"/request", def.MQTT.ClientID+"/reply", "requestId"
				command := def.MQTT.ClientID + "/command"
				wireCommand, wireRequest, wireReply := command, def.MQTT.RequestTopic, def.MQTT.ReplyTopic
				credential := sourcestream.Credential{}
				runtime := New()
				t.Cleanup(runtime.Close)
				pool := runtime.connections
				if server {
					def.Role, def.Endpoint, def.CredentialRef = sourcestream.ServerRole, "mqtt://managed", def.MQTT.ClientID
					def.MQTT.TopicFilters, def.MQTT.ServerSubscribeTopics = []string{"telemetry/+"}, []string{command}
					credential = sourcestream.Credential{Kind: "basic", Username: "device", Secret: "test-only"}
					runtime.RegisterServer(sourcestream.MQTT, MQTTServerDriver{Endpoint: strings.Replace(broker, "tcp://", "mqtt://", 1), Credential: sourcestream.Credential{Kind: "basic", Username: "bridge", Secret: "test-only"}})
					pool = runtime.serverDrivers[sourcestream.MQTT].(MQTTServerDriver).runtime.connections
					prefix := sourcestream.MQTTServerPrefix(9911, def.CredentialRef)
					wireCommand, wireRequest, wireReply = prefix+command, prefix+def.MQTT.RequestTopic, prefix+def.MQTT.ReplyTopic
				}
				var dispatched atomic.Int32
				mqttToken(t, device.Subscribe(wireCommand, 1, func(mqtt.Client, mqtt.Message) { dispatched.Add(1) }))
				mqttToken(t, device.Subscribe(wireRequest, 1, func(c mqtt.Client, message mqtt.Message) {
					dispatched.Add(1)
					c.Publish(wireReply, 1, false, message.Payload())
				}))
				ctx, cancel := context.WithTimeout(WithSourceRevision(context.Background(), 1), 8*time.Second)
				defer cancel()
				require.NoError(t, runtime.Probe(ctx, 9911, def, credential))
				pool.mu.Lock()
				entryCount := len(pool.entries)
				var entry *sourceConnection
				for _, value := range pool.entries {
					entry = value
				}
				pool.mu.Unlock()
				require.Equal(t, 1, entryCount)
				entry.mu.Lock()
				old := entry.value.(mqtt.Client)
				entry.mu.Unlock()
				old.Disconnect(0)
				require.Eventually(t, func() bool { return !old.IsConnectionOpen() }, time.Second, time.Millisecond)
				ref := "mqtt:" + command
				if rpc {
					ref = "mqtt-request-reply"
				}
				var group sync.WaitGroup
				errors := make(chan error, 2)
				for i := 0; i < 2; i++ {
					group.Add(1)
					go func(value int) {
						defer group.Done()
						_, err := runtime.Invoke(ctx, 9911, def, credential, ref, map[string]any{"value": value})
						errors <- err
					}(i)
				}
				group.Wait()
				close(errors)
				for err := range errors {
					require.NoError(t, err, "first new intent must reconnect before dispatch, never retry the operation")
				}
				require.Eventually(t, func() bool { return dispatched.Load() == 2 }, time.Second, 10*time.Millisecond)
				entry.mu.Lock()
				current := entry.value
				entry.mu.Unlock()
				require.NotSame(t, old, current)
				require.NoError(t, entry.ctx.Err(), "reconnection preserves the Source generation")
			})
		}
	}
}

func TestRealMQTTReplyTimeoutDoesNotRepeatAcceptedRequest(t *testing.T) {
	def, _, device := mqttRecoveryFixture(t)
	def.MQTT.RequestTopic, def.MQTT.ReplyTopic, def.MQTT.CorrelationField = def.MQTT.ClientID+"/request", def.MQTT.ClientID+"/reply", "requestId"
	var dispatched atomic.Int32
	mqttToken(t, device.Subscribe(def.MQTT.RequestTopic, 1, func(mqtt.Client, mqtt.Message) { dispatched.Add(1) }))
	runtime := New()
	t.Cleanup(runtime.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := runtime.Invoke(ctx, 9911, def, sourcestream.Credential{}, "mqtt-request-reply", map[string]any{"value": 1})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.EqualValues(t, 1, dispatched.Load(), "an accepted request without a reply must not reconnect and replay")
}

func TestRealMQTTSourceOperationReuseAndConcurrentReplies(t *testing.T) {
	def, _, device := mqttRecoveryFixture(t)
	def.MQTT.RequestTopic = def.MQTT.ClientID + "/requests"
	def.MQTT.ReplyTopic = def.MQTT.ClientID + "/replies"
	def.MQTT.CorrelationField = "request.id"
	mqttToken(t, device.Subscribe(def.MQTT.RequestTopic, 1, func(_ mqtt.Client, message mqtt.Message) {
		var input map[string]any
		if json.Unmarshal(message.Payload(), &input) != nil {
			return
		}
		raw, _ := json.Marshal(input)
		device.Publish(def.MQTT.ReplyTopic, 1, false, raw)
	}))
	runtime := New()
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(WithSourceRevision(context.Background(), 1), 10*time.Second)
	defer cancel()
	require.NoError(t, runtime.Probe(ctx, 9911, def, sourcestream.Credential{}))
	entry, err := runtime.connections.entry(ctx, 9911, def, sourcestream.Credential{})
	require.NoError(t, err)
	entry.mu.Lock()
	client := entry.value.(mqtt.Client)
	entry.mu.Unlock()
	var group sync.WaitGroup
	results := make(chan any, 2)
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func(value int) {
			defer group.Done()
			result, err := runtime.Invoke(ctx, 9911, def, sourcestream.Credential{}, "mqtt-request-reply", map[string]any{"value": value})
			results <- result
			errors <- err
		}(i)
	}
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	values := map[float64]bool{}
	for result := range results {
		values[result.(map[string]any)["value"].(float64)] = true
	}
	require.Equal(t, map[float64]bool{0: true, 1: true}, values, "serialized reply handlers preserve each correlation")
	again, err := runtime.connections.entry(ctx, 9911, def, sourcestream.Credential{})
	require.NoError(t, err)
	require.Same(t, entry, again)
	again.mu.Lock()
	require.Same(t, client, again.value)
	again.mu.Unlock()
	_, err = runtime.Invoke(ctx, 9911, def, sourcestream.Credential{}, "mqtt:"+def.MQTT.ClientID+"/downlink", map[string]any{"value": 3})
	require.NoError(t, err)
	runtime.ReleaseSource(9911)
	require.Eventually(t, func() bool { return !client.IsConnectionOpen() }, time.Second, 10*time.Millisecond)
	require.NoError(t, runtime.Probe(ctx, 9911, def, sourcestream.Credential{}))
	replacement, err := runtime.connections.entry(ctx, 9911, def, sourcestream.Credential{})
	require.NoError(t, err)
	require.NotSame(t, entry, replacement)
	runtime.connections.retire(entry)
	require.NoError(t, runtime.Probe(ctx, 9911, def, sourcestream.Credential{}))
	newest, err := runtime.connections.entry(ctx, 9911, def, sourcestream.Credential{})
	require.NoError(t, err)
	require.Same(t, replacement, newest)
}

func TestRealMQTTRequestReplyUsesUniqueReplyTopics(t *testing.T) {
	def, _, device := mqttRecoveryFixture(t)
	def.MQTT.RequestTopic = def.MQTT.ClientID + "/requests"
	def.MQTT.ReplyTopic = def.MQTT.ClientID + "/replies/{correlationId}"
	def.MQTT.ReplyTopicField = "request.replyTopic"
	def.MQTT.CorrelationField = "request.id"
	replyTopics := make(chan string, 2)
	mqttToken(t, device.Subscribe(def.MQTT.RequestTopic, 1, func(_ mqtt.Client, message mqtt.Message) {
		var input map[string]any
		if json.Unmarshal(message.Payload(), &input) != nil {
			return
		}
		replyTopic := getCorrelation(input, def.MQTT.ReplyTopicField)
		correlation := getCorrelation(input, def.MQTT.CorrelationField)
		if replyTopic == "" || correlation == "" {
			return
		}
		replyTopics <- replyTopic
		raw, _ := json.Marshal(map[string]any{"request": map[string]any{"id": correlation}, "value": input["value"]})
		device.Publish(replyTopic, 1, false, raw)
	}))

	runtime := New()
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(WithSourceRevision(context.Background(), 1), 10*time.Second)
	defer cancel()
	var group sync.WaitGroup
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func(value int) {
			defer group.Done()
			_, err := runtime.Invoke(ctx, 9912, def, sourcestream.Credential{}, "mqtt-request-reply", map[string]any{"value": value})
			errors <- err
		}(i)
	}
	group.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	first, second := <-replyTopics, <-replyTopics
	require.NotEqual(t, first, second)
	require.Contains(t, first, def.MQTT.ClientID+"/replies/")
	require.Contains(t, second, def.MQTT.ClientID+"/replies/")
}
