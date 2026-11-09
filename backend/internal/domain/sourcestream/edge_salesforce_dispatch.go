package sourcestream

import "strings"

func salesforcePagedResource(def SourceDefinition, ref string) bool {
	return def.SourceType == "salesforce" && (def.Protocol == HTTP || def.Protocol == OAuth) &&
		(strings.HasPrefix(ref, "vendor:salesforce:bulk:") || strings.HasPrefix(ref, "vendor:salesforce:deleted:"))
}
