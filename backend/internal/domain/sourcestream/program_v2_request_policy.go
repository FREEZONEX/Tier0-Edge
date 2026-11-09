package sourcestream

import (
	"encoding/json"
	"strconv"
	"strings"

	"backend/internal/repo"
	"backend/internal/streamexec"
)

// Called only by the repository's locked publication transaction. Unsupported
// closure is explicit and cannot be confused with an authorized compute task.
func programV2RequestPolicies(manifest streamexec.ProgramManifest, sources []repo.Source, contracts []repo.ProgramSourceContract, bindings []repo.ProgramUNSBinding) ([]repo.ProgramRequestPolicy, error) {
	byID := map[int64]repo.Source{}
	for _, source := range sources {
		byID[source.ID] = source
	}
	policies := []repo.ProgramRequestPolicy{}
	for _, entry := range manifest.Entries {
		if entry.Request == nil {
			continue
		}
		policy := repo.ProgramRequestPolicy{EntryID: entry.ID, Code: entry.Request.Code, Effect: entry.Request.SideEffect,
			EffectUpperBound: entry.ImplicitRequest(), Class: "compute", Targets: []repo.ProgramRequestTarget{}}
		for _, sourceContract := range contracts {
			source, ok := byID[sourceContract.SourceID]
			var def SourceDefinition
			var resources []NativeResource
			if !ok || json.Unmarshal([]byte(source.Document), &def) != nil || def.Validate() != nil ||
				source.Resources != "" && source.DecodeProgramResources(&resources) != nil {
				return nil, repo.ErrInvalidArgument
			}
			targets, reason := programV2RequestSourceTargets(manifest, def, resources, sourceContract, policy.Effect)
			if reason != "" {
				policy.UnsupportedReason = reason
				break
			}
			policy.Targets = append(policy.Targets, targets...)
			if len(targets) > 0 {
				policy.Class = "device"
			}
			if len(policy.Targets) > repo.MaxProgramRequestTargets {
				policy.UnsupportedReason = "target-limit"
				break
			}
		}
		if policy.UnsupportedReason == "" && len(bindings) != len(manifest.Topics) {
			policy.UnsupportedReason = "unbound-data"
		}
		for _, binding := range bindings {
			if policy.UnsupportedReason != "" {
				break
			}
			if !binding.Reference.Canonical() || binding.CollectionID == "" {
				policy.UnsupportedReason = "unbound-data"
				break
			}
			actions := []string{}
			if binding.Reference.Readable() {
				actions = append(actions, "read", "query")
			}
			if binding.Reference.Writable() {
				actions = append(actions, "write")
			}
			for _, action := range actions {
				effect := "read"
				if action == "write" {
					effect = "write"
				}
				policy.Targets = append(policy.Targets, repo.ProgramRequestTarget{Kind: "data", Action: action, Effect: effect,
					Reference: binding.Reference.Key(), CollectionID: binding.CollectionID, TopicBindingHash: binding.TopicBindingHash})
			}
			if len(actions) > 0 && policy.Class == "compute" {
				policy.Class = "data"
			}
			if len(policy.Targets) > repo.MaxProgramRequestTargets {
				policy.UnsupportedReason = "target-limit"
			}
		}
		write := false
		for _, target := range policy.Targets {
			write = write || target.Effect == "write"
		}
		if policy.UnsupportedReason == "" {
			if policy.Effect == "read" && write {
				policy.UnsupportedReason = "read-effect-conflict"
			} else if policy.Effect == "write" && !policy.EffectUpperBound && !write {
				policy.UnsupportedReason = "write-target-required"
			}
		}
		if policy.UnsupportedReason != "" {
			policy.Class, policy.Targets = "", []repo.ProgramRequestTarget{}
		}
		policies = append(policies, policy)
	}
	return policies, nil
}

func programV2RequestSourceTargets(manifest streamexec.ProgramManifest, def SourceDefinition, resources []NativeResource, contract repo.ProgramSourceContract, effect string) ([]repo.ProgramRequestTarget, string) {
	targets := []repo.ProgramRequestTarget{}
	if def.Protocol == HTTP || def.Protocol == OAuth {
		if def.HTTP == nil {
			return nil, "unsupported-resource"
		}
		if def.ConnectionRole() == ServerRole {
			// A server-only Source is an ingress binding, not an outgoing HTTP
			// capability. It must actually be bound to an HTTP entry.
			for _, entry := range manifest.Entries {
				if entry.Kind == "http-request" && entry.SourceID == strconv.FormatInt(contract.SourceID, 10) && len(contract.Resources) == 0 {
					return targets, ""
				}
			}
			return nil, "unsupported-resource"
		}
		if effect == "read" {
			// Arbitrary server-bound HTTP options are an invoke capability even
			// when a script currently chooses GET. Method is not an effect proof.
			return nil, "read-effect-conflict"
		}
		if def.SourceType != "" {
			// Vendor native calls additionally depend on code-owned operation
			// templates. A generic HTTP digest cannot attest that extra contract.
			return nil, "unsupported-resource"
		}
		return []repo.ProgramRequestTarget{{Kind: "source", Action: "invoke", Effect: "write", SourceID: contract.SourceID,
			ConfigRevision: contract.ConfigRevision, ResourceRef: "http:request", ConfigDigest: contract.ConfigDigest, ResourceDigest: contract.ConfigDigest}}, ""
	}
	if len(contract.Resources) == 0 {
		return nil, "unsupported-resource"
	}
	for _, frozen := range contract.Resources {
		var resource *NativeResource
		for i := range resources {
			if resources[i].ID == frozen.Ref {
				resource = &resources[i]
				break
			}
		}
		bound, err := programV2ResourceDefinition(def, resource)
		if err != nil {
			return nil, "unsupported-resource"
		}
		actions := programV2NativeActions(bound, resource)
		count := 0
		for _, kind := range actions {
			action, targetEffect := strings.TrimPrefix(kind, "source."), "write"
			if action == "read" {
				targetEffect = "read"
			}
			if effect == "read" && targetEffect != "read" {
				continue // The trusted read-only host rejects these operations.
			}
			targets = append(targets, repo.ProgramRequestTarget{Kind: "source", Action: action, Effect: targetEffect,
				SourceID: contract.SourceID, ConfigRevision: contract.ConfigRevision, ResourceRef: frozen.Ref, ConfigDigest: contract.ConfigDigest, ResourceDigest: frozen.Digest})
			count++
		}
		if count == 0 {
			return nil, "unsupported-resource"
		}
	}
	return targets, ""
}
