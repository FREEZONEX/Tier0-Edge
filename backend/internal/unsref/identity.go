// Package unsref owns stable UNS identity syntax, independently of storage and SDKs.
package unsref

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

var metadataID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var decimalID = regexp.MustCompile(`^[0-9]+$`)

// StreamRuntimePrefix is reserved for platform execution state and history,
// never for public UNS resources, even when the caller is an administrator.
const StreamRuntimePrefix = "tier0.system.stream."

func IsStreamRuntimeID(id string) bool { return strings.HasPrefix(id, StreamRuntimePrefix) }

func ValidMetadataID(id string) bool { return !IsStreamRuntimeID(id) && metadataID.MatchString(id) }

func ValidLegacyID(id string) bool {
	n, err := strconv.ParseInt(id, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == id
}

func CanonicalUUID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed != uuid.Nil && parsed.String() == id
}

// DefaultMetadataTopic preserves old numeric/UUID declarations. Ambiguous
// numeric or noncanonical UUID-shaped Meta IDs need explicit space=metadata.
func DefaultMetadataTopic(id string) bool {
	if !ValidMetadataID(id) || decimalID.MatchString(id) {
		return false
	}
	if _, err := uuid.Parse(id); err == nil {
		return CanonicalUUID(id)
	}
	return true
}
