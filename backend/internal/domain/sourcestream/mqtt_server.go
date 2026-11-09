package sourcestream

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"backend/internal/repo"
	"gorm.io/gorm"
)

// The dollar-prefixed namespace is not matched by unrelated MQTT #/+ filters.
// Credential rotation also changes the wire namespace, isolating cached sessions.
func MQTTServerPrefix(sourceID int64, credentialRef string) string {
	return fmt.Sprintf("$tier0/source/%d/%s/", sourceID, credentialRef)
}

func MQTTServerClientPrefix(sourceID int64, credentialRef string) string {
	return fmt.Sprintf("source-device-%d-%s-", sourceID, credentialRef)
}

func mqttSourceIdentity(username, clientID string) (int64, string, string, bool) {
	parts := strings.SplitN(username, ":", 4)
	if len(parts) != 4 || parts[0] != "source" {
		return 0, "", "", false
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	prefix := MQTTServerClientPrefix(id, parts[2])
	valid := err == nil && id > 0 && parts[1] == strconv.FormatInt(id, 10) && parts[2] != "" && parts[3] != "" &&
		len(clientID) <= 256 && strings.HasPrefix(clientID, prefix) && len(clientID) > len(prefix)
	return id, parts[2], parts[3], valid
}

func (s *Service) mqttSourceAccess(ctx context.Context, username, clientID string) (SourceDefinition, Credential, int64, bool, error) {
	id, ref, user, ok := mqttSourceIdentity(username, clientID)
	if !ok {
		return SourceDefinition{}, Credential{}, 0, false, nil
	}
	r := repo.NewSourceStreamRepo(ctx)
	source, err := r.Source(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SourceDefinition{}, Credential{}, 0, false, nil
	}
	if err != nil {
		return SourceDefinition{}, Credential{}, 0, false, err
	}
	def, credential, _, err := s.connection(ctx, id, nil)
	if err != nil || def.Protocol != MQTT || def.ConnectionRole() != ServerRole || def.CredentialRef != ref || credential.Kind != "basic" || credential.Username != user {
		return SourceDefinition{}, Credential{}, 0, false, nil
	}
	current, err := r.Source(ctx, id)
	if err != nil {
		return SourceDefinition{}, Credential{}, 0, false, err
	}
	return def, credential, id, current.ConfigRevision == source.ConfigRevision, nil
}

// Source credentials are scoped device identities, never Broker superusers.
func (s *Service) AuthenticateMQTTSource(ctx context.Context, username, password, clientID string) (bool, error) {
	_, credential, _, allowed, err := s.mqttSourceAccess(ctx, username, clientID)
	if !allowed || err != nil {
		return false, err
	}
	a, b := sha256.Sum256([]byte(password)), sha256.Sum256([]byte(credential.Secret))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1, nil
}

func (s *Service) AuthorizeMQTTSource(ctx context.Context, username, clientID, topic, action string) (bool, error) {
	def, _, id, allowed, err := s.mqttSourceAccess(ctx, username, clientID)
	if !allowed || err != nil {
		return false, err
	}
	prefix := MQTTServerPrefix(id, def.CredentialRef)
	if !strings.HasPrefix(topic, prefix) {
		return false, nil
	}
	local := strings.TrimPrefix(topic, prefix)
	filters := []string{}
	switch action {
	case "publish":
		filters = append(filters, def.MQTT.TopicFilters...)
		if def.MQTT.ReplyTopic != "" {
			filters = append(filters, strings.ReplaceAll(def.MQTT.ReplyTopic, "{correlationId}", "+"))
		}
		if !validMQTTTopic(local, false) {
			return false, nil
		}
	case "subscribe":
		filters = append(filters, def.MQTT.ServerSubscribeTopics...)
		if def.MQTT.RequestTopic != "" {
			filters = append(filters, def.MQTT.RequestTopic)
		}
	default:
		return false, nil
	}
	for _, filter := range filters {
		if local == filter || (!strings.ContainsAny(local, "+#") && mqttFilterMatches(filter, local)) {
			return true, nil
		}
	}
	return false, nil
}
