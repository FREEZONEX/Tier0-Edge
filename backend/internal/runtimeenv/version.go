package runtimeenv

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// BuildPackageVersion identifies the installer, independently of business binaries.
var BuildPackageVersion = "dev"

var productVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-r([1-9][0-9]*))?$`)
var packageSuffixPattern = regexp.MustCompile(`^[0-9a-f]{6}$`)

func ValidPackageVersion(productVersion, packageVersion string) bool {
	return productVersionPattern.MatchString(productVersion) && strings.HasPrefix(packageVersion, productVersion+"-") && packageSuffixPattern.MatchString(strings.TrimPrefix(packageVersion, productVersion+"-"))
}

// CompareProductVersions compares numeric business revisions; build nonces are never ordered.
func CompareProductVersions(a, b string) (int, error) {
	left, right := productVersionPattern.FindStringSubmatch(a), productVersionPattern.FindStringSubmatch(b)
	if left == nil || right == nil {
		return 0, fmt.Errorf("invalid business version: %q / %q", a, b)
	}
	for i := 1; i <= 4; i++ {
		parse := func(s string) (uint64, error) {
			if s == "" {
				return 0, nil
			}
			return strconv.ParseUint(s, 10, 64)
		}
		x, e := parse(left[i])
		if e != nil {
			return 0, e
		}
		y, e := parse(right[i])
		if e != nil {
			return 0, e
		}
		if x < y {
			return -1, nil
		}
		if x > y {
			return 1, nil
		}
	}
	return 0, nil
}

// Only these unpublished acceptance builds used installer changes as patch versions.
// Never turn this normalization into a general downgrade exception.
func NormalizeUnpublishedVersion(version, source string) string {
	known := map[string]string{
		"7b436edb6052df382bd58b9799300c19c67e504e": "3.0.1",
		"352f5cbfc525df12a2c5234e4a9583cd08a875a7": "3.0.2",
		"f2d28fc1689f6d55cc26bb32af2114b57860765b": "3.0.2",
		"646e24cdb66669117c1cceeadf0ac84269f608ad": "3.0.3",
	}
	if known[source] == version && version != "" {
		return "3.0.0"
	}
	return version
}
