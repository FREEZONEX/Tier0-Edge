package repo

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"strconv"

	"backend/internal/streamexec"
)

const programRequestPoliciesField = "_requestPolicies"
const MaxProgramRequestTargets = 64
const maxProgramRequestPolicyBytes = 128 * 1024

// Private publication facts. They are neither caller-authored descriptors nor
// grants. The execution plan digest protects them with the owning release.
type ProgramRequestTarget struct {
	Kind             string `json:"kind"`
	Action           string `json:"action"`
	Effect           string `json:"effect"`
	SourceID         int64  `json:"sourceId,string,omitempty"`
	ConfigRevision   int64  `json:"configRevision,omitempty"`
	ResourceRef      string `json:"resourceRef,omitempty"`
	ConfigDigest     string `json:"configDigest,omitempty"`
	ResourceDigest   string `json:"resourceDigest,omitempty"`
	Reference        string `json:"reference,omitempty"`
	CollectionID     string `json:"collectionId,omitempty"`
	TopicBindingHash string `json:"topicBindingHash,omitempty"`
	VersionID        int64  `json:"versionId,string,omitempty"`
	Digest           string `json:"digest"`
}

type ProgramRequestPolicy struct {
	EntryID           string                 `json:"entryId"`
	Code              string                 `json:"code"`
	Effect            string                 `json:"effect"`
	EffectUpperBound  bool                   `json:"effectUpperBound"`
	Class             string                 `json:"class"`
	Targets           []ProgramRequestTarget `json:"targets"`
	UnsupportedReason string                 `json:"unsupportedReason,omitempty"`
}

type ProgramRequestPolicyBuilder func(streamexec.ProgramManifest, []Source, []ProgramSourceContract, []ProgramUNSBinding) ([]ProgramRequestPolicy, error)

func programRequestTargetDigest(target ProgramRequestTarget) (string, error) {
	target.Digest = ""
	return programJSONDigest(target)
}

func freezeProgramRequestPolicies(version StreamVersion, sources []Source, contracts []ProgramSourceContract, build ProgramRequestPolicyBuilder) (json.RawMessage, error) {
	manifest, err := version.ProgramV2Manifest()
	if err != nil {
		return nil, err
	}
	bindings, err := version.ProgramV2Bindings()
	if err != nil {
		return nil, err
	}
	policies, err := build(manifest, sources, contracts, bindings)
	if err != nil {
		return nil, err
	}
	for i := range policies {
		for j := range policies[i].Targets {
			if policies[i].Targets[j].Kind == "data" {
				policies[i].Targets[j].VersionID = version.ID
			}
			policies[i].Targets[j].Digest, err = programRequestTargetDigest(policies[i].Targets[j])
			if err != nil {
				return nil, err
			}
		}
		sort.Slice(policies[i].Targets, func(a, b int) bool { return policies[i].Targets[a].Digest < policies[i].Targets[b].Digest })
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i].EntryID < policies[j].EntryID })
	raw, err := json.Marshal(policies)
	if err != nil {
		return nil, ErrInvalidArgument
	}
	if _, err = decodeProgramRequestPolicies(raw, manifest, contracts, bindings, version.ID); err != nil {
		return nil, err
	}
	return raw, nil
}

func decodeProgramRequestPolicies(raw []byte, manifest streamexec.ProgramManifest, contracts []ProgramSourceContract, bindings []ProgramUNSBinding, versionID int64) ([]ProgramRequestPolicy, error) {
	var policies []ProgramRequestPolicy
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&policies) != nil || policies == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrInvalidArgument
	}
	canonical, err := json.Marshal(policies)
	if err != nil || !sameProgramDocument(string(raw), string(canonical)) {
		return nil, ErrInvalidArgument
	}
	requests := map[string]streamexec.ProgramEntry{}
	for _, entry := range manifest.Entries {
		if entry.Request != nil {
			requests[entry.ID] = entry
		}
	}
	if len(requests) != len(policies) {
		return nil, ErrInvalidArgument
	}
	previous := ""
	for _, policy := range policies {
		entry, ok := requests[policy.EntryID]
		if !ok || policy.EntryID <= previous || policy.Code != entry.Request.Code || policy.Effect != entry.Request.SideEffect ||
			policy.EffectUpperBound != entry.ImplicitRequest() || policy.Targets == nil || len(policy.Targets) > MaxProgramRequestTargets {
			return nil, ErrInvalidArgument
		}
		previous = policy.EntryID
		encoded, _ := json.Marshal(policy)
		if len(encoded) > maxProgramRequestPolicyBytes {
			return nil, ErrInvalidArgument
		}
		if policy.UnsupportedReason != "" {
			switch policy.UnsupportedReason {
			case "unsupported-resource", "unbound-data", "read-effect-conflict", "write-target-required", "target-limit":
			default:
				return nil, ErrInvalidArgument
			}
			if policy.Class != "" || len(policy.Targets) != 0 {
				return nil, ErrInvalidArgument
			}
			continue
		}
		class, write, previousTarget := "compute", false, ""
		for _, target := range policy.Targets {
			digest, err := programRequestTargetDigest(target)
			if err != nil || target.Digest != digest || target.Digest <= previousTarget {
				return nil, ErrInvalidArgument
			}
			previousTarget = target.Digest
			effect := "read"
			switch target.Action {
			case "read", "query":
			case "write", "invoke", "publish":
				effect, write = "write", true
			default:
				return nil, ErrInvalidArgument
			}
			if target.Effect != effect || policy.Effect == "read" && effect != "read" {
				return nil, ErrInvalidArgument
			}
			if target.Kind == "source" {
				if target.Action == "query" || target.Reference != "" || target.CollectionID != "" || target.TopicBindingHash != "" || target.VersionID != 0 || !programRequestSourceTarget(target, contracts) {
					return nil, ErrInvalidArgument
				}
				class = "device"
			} else if target.Kind == "data" {
				if target.SourceID != 0 || target.ConfigRevision != 0 || target.ResourceRef != "" || target.ConfigDigest != "" || target.ResourceDigest != "" || versionID <= 0 || target.VersionID != versionID || !programRequestDataTarget(target, bindings) {
					return nil, ErrInvalidArgument
				}
				if class == "compute" {
					class = "data"
				}
			} else {
				return nil, ErrInvalidArgument
			}
		}
		if policy.Class != class || policy.Effect == "write" && !policy.EffectUpperBound && !write || !programRequestTargetsComplete(policy, manifest, contracts, bindings) {
			return nil, ErrInvalidArgument
		}
	}
	return policies, nil
}

func programRequestTargetsComplete(policy ProgramRequestPolicy, manifest streamexec.ProgramManifest, contracts []ProgramSourceContract, bindings []ProgramUNSBinding) bool {
	for _, source := range contracts {
		genericHTTP := false
		resources := map[string]bool{}
		for _, target := range policy.Targets {
			if target.Kind == "source" && target.SourceID == source.SourceID {
				resources[target.ResourceRef] = true
				genericHTTP = genericHTTP || target.ResourceRef == "http:request" && target.Action == "invoke" && (source.Protocol == "http-api" || source.Protocol == "oauth2")
			}
		}
		if genericHTTP {
			continue
		}
		if len(source.Resources) == 0 {
			ingress := false
			for _, entry := range manifest.Entries {
				ingress = ingress || source.Protocol == "http-api" && entry.Kind == "http-request" && entry.SourceID == strconv.FormatInt(source.SourceID, 10)
			}
			if !ingress {
				return false
			}
		}
		for _, resource := range source.Resources {
			if !resources[resource.Ref] {
				return false
			}
		}
	}
	if len(bindings) != len(manifest.Topics) {
		return false
	}
	for _, binding := range bindings {
		if binding.CollectionID == "" {
			return false
		}
		actions := map[string]bool{}
		for _, target := range policy.Targets {
			if target.Kind == "data" && target.Reference == binding.Reference.Key() {
				actions[target.Action] = true
			}
		}
		if binding.Reference.Readable() && (!actions["read"] || !actions["query"]) || binding.Reference.Writable() && !actions["write"] {
			return false
		}
	}
	return true
}

func programRequestSourceTarget(target ProgramRequestTarget, contracts []ProgramSourceContract) bool {
	for _, source := range contracts {
		if source.SourceID != target.SourceID || source.ConfigRevision != target.ConfigRevision || source.ConfigDigest != target.ConfigDigest {
			continue
		}
		if target.ResourceRef == "http:request" && target.Action == "invoke" && (source.Protocol == "http-api" || source.Protocol == "oauth2") {
			return target.ResourceDigest == source.ConfigDigest
		}
		for _, resource := range source.Resources {
			if resource.Ref == target.ResourceRef && resource.Digest == target.ResourceDigest {
				return true
			}
		}
	}
	return false
}

func programRequestDataTarget(target ProgramRequestTarget, bindings []ProgramUNSBinding) bool {
	for _, binding := range bindings {
		if binding.Reference.Key() != target.Reference || binding.CollectionID == "" || binding.CollectionID != target.CollectionID || binding.TopicBindingHash != target.TopicBindingHash {
			continue
		}
		return (target.Action == "read" || target.Action == "query") && binding.Reference.Readable() || target.Action == "write" && binding.Reference.Writable()
	}
	return false
}

// Missing legacy targets remain missing. The returned digest identifies a
// release binding; current actor, recipient and target grants are still required.
func (v StreamVersion) ProgramRequestPolicy(entryID string) (ProgramRequestPolicy, string, error) {
	plan, err := v.ProgramExecutionPlan()
	if err != nil {
		return ProgramRequestPolicy{}, "", err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(v.ProgramManifest), &fields)
	raw, ok := fields[programRequestPoliciesField]
	if !ok {
		return ProgramRequestPolicy{}, "", ErrNotFound
	}
	var policies []ProgramRequestPolicy
	// ProgramExecutionPlan already validates the entire immutable envelope.
	if json.Unmarshal(raw, &policies) != nil {
		return ProgramRequestPolicy{}, "", ErrInvalidArgument
	}
	for _, policy := range policies {
		if policy.EntryID == entryID {
			digest, err := programJSONDigest([]any{"enterprise-request-binding/v1", plan.Digest, policy})
			return policy, digest, err
		}
	}
	return ProgramRequestPolicy{}, "", ErrNotFound
}
