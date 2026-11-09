package sourceresource

import (
	"regexp"
	"strings"
)

const SalesforceObjectPrefix = "vendor:salesforce:object:"

var salesforceObjectName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,79}$`)

// SalesforceObjectRef derives the bounded business resource only from a
// discovered, read-only object descriptor with the same native identity.
// The catalog and immutable Stream snapshot must use this same derivation.
func SalesforceObjectRef(resource NativeResource) string {
	object, ok := strings.CutPrefix(resource.ID, "vendor:salesforce:describe:")
	if !ok || !salesforceObjectName.MatchString(object) || resource.Metadata["sourceType"] != "salesforce" ||
		resource.Metadata["readOnly"] != true || resource.Metadata["nativeIdentity"] != object {
		return ""
	}
	return SalesforceObjectPrefix + object
}
