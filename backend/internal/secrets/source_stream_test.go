package secrets

import "testing"

func TestStreamDeploymentRetiresLegacyMQTTIdentities(t *testing.T) {
	t.Setenv("SOURCE_STREAM_NATS_URL", "nats://test.invalid:4222")
	t.Setenv("MQTT_BACKEND_PASSWORD", "test-only-backend")
	for _, user := range []string{"sourceflow", " eventflow "} {
		if InternalMQTTPassword(user) != "" || AllowInternalMQTTUser(user, "test-only-password") {
			t.Fatalf("retired identity %q must be denied", user)
		}
	}
	if !AllowInternalMQTTUser("backend", "test-only-backend") {
		t.Fatal("active backend identity must remain usable")
	}
}

func TestLegacyDeploymentKeepsConfiguredMQTTIdentity(t *testing.T) {
	t.Setenv("SOURCE_STREAM_NATS_URL", "")
	t.Setenv("MQTT_SOURCEFLOW_PASSWORD", "test-only-legacy")
	if !AllowInternalMQTTUser("sourceflow", "test-only-legacy") {
		t.Fatal("retirement must not modify existing legacy deployments")
	}
}
