package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"

	"backend/internal/sourceresource"
)

// These digests describe the exact connection and selected native resources at
// publication. They contain neither configuration values nor credentials.
type ProgramSourceContract struct {
	SourceID       int64                     `json:"sourceId,string"`
	ConfigRevision int64                     `json:"configRevision"`
	Protocol       string                    `json:"protocol"`
	ConfigDigest   string                    `json:"configDigest"`
	Resources      []ProgramResourceContract `json:"resources"`
}

type ProgramResourceContract struct {
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
}

func programJSONDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", ErrInvalidArgument
	}
	raw, err = canonicalProgramJSON(raw)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

// DecodeProgramResources adds configured Modbus ranges and Salesforce business
// aliases backed by discovered object descriptors. Keep discovered JSON intact:
// publications freeze exact schema numbers and driver-specific metadata too.
func (source Source) DecodeProgramResources(target any) error {
	raw := []byte(source.Resources)
	if source.Protocol != "modbus-tcp" && source.Protocol != "oauth2" {
		return json.Unmarshal(raw, target)
	}
	var document struct {
		Modbus     *sourceresource.ModbusOptions `json:"modbus"`
		SourceType string                        `json:"sourceType"`
	}
	if err := json.Unmarshal([]byte(source.Document), &document); err != nil {
		return err
	}
	salesforce := source.Protocol == "oauth2" && document.SourceType == "salesforce"
	if !salesforce && (source.Protocol != "modbus-tcp" || document.Modbus == nil || len(document.Modbus.Ranges) == 0) {
		return json.Unmarshal(raw, target)
	}
	var catalog []json.RawMessage
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return err
	}
	if catalog == nil {
		return ErrInvalidArgument
	}
	known := make(map[string]bool, len(catalog))
	for _, item := range catalog {
		var identity struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(item, &identity); err != nil {
			return err
		}
		known[identity.ID] = true
	}
	if salesforce {
		for _, item := range append([]json.RawMessage(nil), catalog...) {
			var resource sourceresource.NativeResource
			if err := json.Unmarshal(item, &resource); err != nil {
				return err
			}
			ref := sourceresource.SalesforceObjectRef(resource)
			if ref == "" {
				continue
			}
			if known[ref] {
				return ErrInvalidArgument // Never substitute an ambiguous physical identity.
			}
			var alias map[string]json.RawMessage
			if err := json.Unmarshal(item, &alias); err != nil {
				return err
			}
			alias["id"], _ = json.Marshal(ref)
			encoded, err := json.Marshal(alias)
			if err != nil {
				return err
			}
			catalog = append(catalog, encoded)
			known[ref] = true
		}
	} else {
		for _, r := range document.Modbus.Ranges {
			if !known["modbus:"+r.ID] {
				item, err := json.Marshal(r.NativeResource(document.Modbus.UnitID))
				if err != nil {
					return err
				}
				catalog = append(catalog, item)
			}
		}
	}
	data, err := json.Marshal(catalog)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func programSourceContract(source Source, refs []string) (ProgramSourceContract, error) {
	result := ProgramSourceContract{SourceID: source.ID, ConfigRevision: source.ConfigRevision, Protocol: source.Protocol, Resources: []ProgramResourceContract{}}
	var configuration map[string]json.RawMessage
	var protocol string
	if source.ID <= 0 || source.ConfigRevision <= 0 || source.DeletedAt != 0 || source.DesiredState == "disabled" ||
		json.Unmarshal([]byte(source.Document), &configuration) != nil || configuration == nil ||
		json.Unmarshal(configuration["protocol"], &protocol) != nil || protocol == "" || protocol != source.Protocol {
		return result, ErrInvalidArgument
	}
	delete(configuration, "name")
	delete(configuration, "description")
	var err error
	result.ConfigDigest, err = programJSONDigest(configuration)
	if err != nil {
		return result, err
	}
	if len(refs) == 0 {
		return result, nil
	}
	selected := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if ref == "" || selected[ref] {
			return result, ErrInvalidArgument
		}
		selected[ref] = true
	}
	var catalog []map[string]json.RawMessage
	if source.DecodeProgramResources(&catalog) != nil || catalog == nil {
		return result, ErrInvalidArgument
	}
	seen := map[string]bool{}
	for _, resource := range catalog {
		var ref string
		if json.Unmarshal(resource["id"], &ref) != nil || !selected[ref] {
			continue
		}
		if seen[ref] {
			return result, ErrInvalidArgument
		}
		seen[ref] = true
		delete(resource, "name")
		var models []string
		if json.Unmarshal(resource["models"], &models) != nil || len(models) == 0 {
			return result, ErrInvalidArgument
		}
		sort.Strings(models)
		resource["models"], _ = json.Marshal(models)
		if raw, ok := resource["metadata"]; ok {
			var metadata map[string]json.RawMessage
			if json.Unmarshal(raw, &metadata) != nil {
				return result, ErrInvalidArgument
			}
			delete(metadata, "metadataVerifiedAt")
			if protocol == "mqtt" {
				delete(metadata, "observed") // Discovery traffic is not a resource capability.
			}
			if len(metadata) == 0 {
				delete(resource, "metadata")
			} else {
				resource["metadata"], _ = json.Marshal(metadata)
			}
		}
		digest, err := programJSONDigest(resource)
		if err != nil {
			return result, err
		}
		result.Resources = append(result.Resources, ProgramResourceContract{Ref: ref, Digest: digest})
	}
	if len(seen) != len(selected) {
		return result, ErrInvalidArgument
	}
	sort.Slice(result.Resources, func(i, j int) bool { return result.Resources[i].Ref < result.Resources[j].Ref })
	return result, nil
}

func freezeProgramSourceContracts(version StreamVersion, sources []Source) ([]ProgramSourceContract, error) {
	snapshots, err := version.Sources()
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]Source, len(sources))
	for _, source := range sources {
		byID[source.ID] = source
	}
	var contracts []ProgramSourceContract
	for _, snapshot := range snapshots {
		source, ok := byID[snapshot.SourceID]
		if !ok || source.ConfigRevision != snapshot.ConfigRevision {
			return nil, ErrConflict
		}
		contract, err := programSourceContract(source, snapshot.Resources)
		if err != nil {
			return nil, err
		}
		contracts = append(contracts, contract)
	}
	sort.Slice(contracts, func(i, j int) bool { return contracts[i].SourceID < contracts[j].SourceID })
	return contracts, nil
}

func validateProgramSourceContracts(version StreamVersion, contracts []ProgramSourceContract) error {
	snapshots, err := version.Sources()
	if err != nil || len(snapshots) != len(contracts) {
		return ErrInvalidArgument
	}
	byID := make(map[int64]StreamSourceSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		byID[snapshot.SourceID] = snapshot
	}
	validDigest := func(value string) bool {
		raw, err := hex.DecodeString(value)
		return err == nil && len(raw) == sha256.Size && hex.EncodeToString(raw) == value
	}
	var previous int64
	for _, contract := range contracts {
		snapshot, ok := byID[contract.SourceID]
		if !ok || contract.SourceID <= previous || contract.ConfigRevision != snapshot.ConfigRevision || contract.Protocol == "" ||
			!validDigest(contract.ConfigDigest) || contract.Resources == nil || len(contract.Resources) != len(snapshot.Resources) {
			return ErrInvalidArgument
		}
		previous = contract.SourceID
		refs := append([]string(nil), snapshot.Resources...)
		sort.Strings(refs)
		for i, resource := range contract.Resources {
			if refs[i] != resource.Ref || !validDigest(resource.Digest) {
				return ErrInvalidArgument
			}
		}
	}
	return nil
}

func (p ProgramExecutionPlan) validateSource(source Source) error {
	if p.Contract == programExecutionPlanLegacyContract {
		return nil // Missing historical facts are never reconstructed as grants.
	}
	for _, contract := range p.Sources {
		if contract.SourceID != source.ID {
			continue
		}
		refs := make([]string, 0, len(contract.Resources))
		for _, resource := range contract.Resources {
			refs = append(refs, resource.Ref)
		}
		current, err := programSourceContract(source, refs)
		if err != nil || !reflect.DeepEqual(current, contract) {
			return ErrConflict
		}
		return nil
	}
	return ErrConflict
}

// ValidateProgramExecutionSource preserves legacy execution while requiring a
// v2 publication's physical resource identity before a new host call.
func (v StreamVersion) ValidateProgramExecutionSource(source Source) error {
	plan, err := v.ProgramExecutionPlan()
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return plan.validateSource(source)
}

func (v StreamVersion) validateExecutionSources(sources []Source) error {
	plan, err := v.ProgramExecutionPlan()
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, contract := range plan.Sources {
		found := false
		for _, source := range sources {
			if source.ID == contract.SourceID {
				found = true
				if err := plan.validateSource(source); err != nil {
					return err
				}
			}
		}
		if !found {
			return ErrConflict
		}
	}
	return nil
}
