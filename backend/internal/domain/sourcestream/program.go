package sourcestream

import (
	"strings"

	"backend/internal/streamexec"
)

type TriggerKind string

const (
	ManualTrigger   TriggerKind = "manual"
	IntervalTrigger TriggerKind = "interval"
	SourceTrigger   TriggerKind = "source"
	UNSTrigger      TriggerKind = "uns"
	MinIntervalMs               = 1000
	MaxIntervalMs               = 24 * 60 * 60 * 1000
)

// Dependencies are explicit deployment grants, never inferred from JavaScript.
// A trigger-only dependency (for example MQTT) need not permit SDK reads.
type SourceDependency struct {
	Alias         string   `json:"alias"`
	SourceID      int64    `json:"sourceId"`
	ReadResources []string `json:"readResources"`
}

type TriggerDefinition struct {
	Kind         TriggerKind `json:"kind"`
	IntervalMs   int         `json:"intervalMs,omitempty"`
	SourceAlias  string      `json:"sourceAlias,omitempty"`
	ResourceRefs []string    `json:"resourceRefs,omitempty"`
	QoS          int         `json:"qos,omitempty"`
	TopicIDs     []int64     `json:"topicIds,omitempty"`
}

type ProgramDefinition struct {
	Sources []SourceDependency `json:"sources"`
	Trigger TriggerDefinition  `json:"trigger"`
}

func (p ProgramDefinition) Validate(model AccessModel, deployment bool) error {
	if len(p.Sources) > streamexec.MaxSourceGrants {
		return invalid("program.sources", "tooLarge")
	}
	aliases, ids := map[string]bool{}, map[int64]bool{}
	for _, source := range p.Sources {
		// Reuse the SDK's alias/ID/size rules with one placeholder read grant;
		// trigger-only dependencies intentionally have no readable resources.
		grant := streamexec.SourceGrant{Alias: source.Alias, SourceID: source.SourceID, ConfigRevision: 1, ReadResources: []string{"resource"}}
		if !streamexec.ValidSourceGrants([]streamexec.SourceGrant{grant}) || aliases[source.Alias] || ids[source.SourceID] {
			return invalid("program.sources", "duplicateOrInvalid")
		}
		aliases[source.Alias], ids[source.SourceID] = true, true
		if !uniqueResourceRefs(source.ReadResources) {
			return invalid("program.sources.readResources", "duplicateOrInvalid")
		}
	}
	t := p.Trigger
	if t.QoS < 0 || t.QoS > 2 || !uniqueResourceRefs(t.ResourceRefs) {
		return invalid("program.trigger", "invalid")
	}
	seen := map[int64]bool{}
	for _, id := range t.TopicIDs {
		if id <= 0 || id > (1<<53)-1 || seen[id] {
			return invalid("program.trigger.topicIds", "duplicateOrInvalid")
		}
		seen[id] = true
	}
	if len(t.TopicIDs) > streamexec.MaxSourceResources {
		return invalid("program.trigger.topicIds", "tooLarge")
	}
	switch t.Kind {
	case ManualTrigger:
		if model != RequestReply || t.IntervalMs != 0 || t.SourceAlias != "" || len(t.ResourceRefs)+len(t.TopicIDs) != 0 || t.QoS != 0 {
			return invalid("program.trigger", "manualRequestReplyOnly")
		}
	case IntervalTrigger:
		if model != Publish || t.IntervalMs < MinIntervalMs || t.IntervalMs > MaxIntervalMs || t.SourceAlias != "" || len(t.ResourceRefs)+len(t.TopicIDs) != 0 || t.QoS != 0 {
			return invalid("program.trigger", "invalidInterval")
		}
	case SourceTrigger:
		if model != Publish || t.IntervalMs != 0 || len(t.TopicIDs) != 0 || (t.SourceAlias != "" && !aliases[t.SourceAlias]) {
			return invalid("program.trigger", "invalidSource")
		}
		if deployment && (t.SourceAlias == "" || len(t.ResourceRefs) == 0) {
			return invalid("program.trigger.resourceRefs", "required")
		}
	case UNSTrigger:
		if model != Publish || t.IntervalMs != 0 || t.SourceAlias != "" || len(t.ResourceRefs) != 0 || t.QoS != 0 {
			return invalid("program.trigger", "invalidUNS")
		}
		if deployment && len(t.TopicIDs) == 0 {
			return invalid("program.trigger.topicIds", "required")
		}
	default:
		return invalid("program.trigger.kind", "unsupported")
	}
	return nil
}

func uniqueResourceRefs(refs []string) bool {
	if len(refs) > streamexec.MaxSourceResources {
		return false
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" || len(ref) > 65535 || seen[ref] {
			return false
		}
		seen[ref] = true
	}
	return true
}

// MatchesInput uses the actual MQTT topic for context, while selecting messages
// using the declared filter. Other protocols always use stable exact IDs.
func (p ProgramDefinition) MatchesInput(sourceID int64, resourceRef string) bool {
	if p.Trigger.Kind != SourceTrigger {
		return false
	}
	for _, source := range p.Sources {
		if source.Alias != p.Trigger.SourceAlias || source.SourceID != sourceID {
			continue
		}
		for _, ref := range p.Trigger.ResourceRefs {
			if ref == resourceRef || (strings.HasPrefix(ref, "mqtt:") && strings.HasPrefix(resourceRef, "mqtt:") &&
				mqttFilterMatches(strings.TrimPrefix(ref, "mqtt:"), strings.TrimPrefix(resourceRef, "mqtt:"))) {
				return true
			}
		}
	}
	return false
}
