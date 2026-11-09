package sourcestream

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"backend/internal/repo"
	"backend/internal/streamexec"
)

var programAlias = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)

// ProgramSourceBinding is the common public Source binding. The interpreter
// receives a trusted alias table only after native revision/permission checks.
type ProgramSourceBinding struct {
	Alias          string                     `json:"alias,omitempty"`
	SourceID       string                     `json:"sourceId,omitempty"`
	ConfigRevision int64                      `json:"configRevision,omitempty"`
	Target         *ProgramSourceTarget       `json:"target,omitempty"`
	Resources      []string                   `json:"resources,omitempty"`
	Acquisition    map[string]json.RawMessage `json:"acquisition,omitempty"`
}
type ProgramSourceTarget struct {
	FleetNodeID    string `json:"fleetNodeId"`
	SourceID       string `json:"sourceId"`
	ConfigRevision int64  `json:"configRevision"`
}

func (d StreamDefinition) programBindings() []ProgramSourceBinding {
	if d.ProgramSources != nil {
		return d.ProgramSources
	}
	return d.ProgramSourceBindings
}
func (d StreamDefinition) sourceAliases() map[string]streamexec.ProgramSource {
	aliases := map[string]streamexec.ProgramSource{}
	for _, binding := range d.programBindings() {
		if binding.Alias == "" {
			continue
		}
		resources := append([]string{}, binding.Resources...)
		aliases[binding.Alias] = streamexec.ProgramSource{ID: binding.SourceID, Resources: resources}
	}
	return aliases
}
func (d StreamDefinition) validateProgramBindings() error {
	if d.ProgramSources != nil && d.ProgramSourceBindings != nil {
		return invalid("sources", "mutuallyExclusive")
	}
	bindings := d.programBindings()
	if len(bindings) > 64 {
		return invalid("sources", "outOfRange")
	}
	seen := map[string]bool{}
	ids := map[string]bool{}
	for _, binding := range bindings {
		if binding.Target != nil {
			return invalid("target", "unsupportedCapability")
		}
		id, err := strconv.ParseInt(binding.SourceID, 10, 64)
		if err != nil || id < 1 || strconv.FormatInt(id, 10) != binding.SourceID || binding.ConfigRevision < 1 || ids[binding.SourceID] {
			return repo.ErrInvalidArgument
		}
		ids[binding.SourceID] = true
		if d.ProgramSources != nil || binding.Alias != "" {
			if !programAlias.MatchString(binding.Alias) || seen[binding.Alias] {
				return invalid("sources.alias", "invalidOrDuplicate")
			}
			seen[binding.Alias] = true
		}
		refs := map[string]bool{}
		for _, ref := range binding.Resources {
			if strings.TrimSpace(ref) == "" || len(ref) > 2048 || refs[ref] {
				return invalid("sources.resources", "invalidOrDuplicate")
			}
			refs[ref] = true
		}
		if len(binding.Resources) > 1000 {
			return repo.ErrInvalidArgument
		}
		// 协议字段及资源边界在读取所选 Source 修订后校验。
	}
	return nil
}

// Only persisted immutable releases may use the retired flat script shape.
// This projection never writes or rehashes historical document bytes.
func decodeStoredProgram(raw []byte, target *StreamDefinition) error {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return repo.ErrInvalidArgument
	}
	var code string
	legacy := false
	if json.Unmarshal(object["script"], &code) == nil {
		legacy = true
		timeout := streamexec.DefaultProgramTimeoutMs
		if value, ok := object["timeoutMs"]; ok && json.Unmarshal(value, &timeout) != nil {
			return repo.ErrInvalidArgument
		}
		script, _ := json.Marshal(ScriptDefinition{Code: code, TimeoutMs: timeout})
		object["script"] = script
		delete(object, "timeoutMs")
		converted, err := json.Marshal(object)
		if err != nil {
			return err
		}
		raw = converted
	}
	err := json.Unmarshal(raw, target)
	target.legacyProgram = legacy
	return err
}
