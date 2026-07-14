package version

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

var semverPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// Normalize validates a semantic version and strips a leading "v".
// It returns the normalized version in "X.Y.Z" form.
func Normalize(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf(messages.VersionRequired)
	}
	match := semverPattern.FindStringSubmatch(trimmed)
	if match == nil {
		return "", fmt.Errorf(messages.VersionInvalidFmt, raw)
	}
	return fmt.Sprintf("%s.%s.%s", match[1], match[2], match[3]), nil
}

// IsDev reports whether the version string represents a dev build.
func IsDev(raw string) bool {
	return strings.TrimSpace(raw) == developmentVersion
}

// Compare compares two semantic versions in X.Y.Z form (a leading "v" is
// allowed and stripped via Normalize).
// It returns -1 if a < b, 0 if a == b, and 1 if a > b. A non-nil error is
// returned when either argument is not a valid semantic version.
func Compare(a string, b string) (int, error) {
	aParts, err := Parse(a)
	if err != nil {
		return 0, err
	}
	bParts, err := Parse(b)
	if err != nil {
		return 0, err
	}
	for idx := 0; idx < len(aParts); idx++ {
		if aParts[idx] < bParts[idx] {
			return -1, nil
		}
		if aParts[idx] > bParts[idx] {
			return 1, nil
		}
	}
	return 0, nil
}

// Parse converts a semantic version into its numeric major/minor/patch
// components. The input is normalized first (a leading "v" is stripped), so any
// value accepted by Normalize is accepted here. It returns an error when the
// version is invalid or a segment cannot be represented as an int.
func Parse(raw string) ([3]int, error) {
	normalized, err := Normalize(raw)
	if err != nil {
		return [3]int{}, err
	}
	parts := strings.Split(normalized, ".")
	if len(parts) != 3 {
		return [3]int{}, fmt.Errorf(messages.UpdateInvalidVersionFmt, raw)
	}
	var out [3]int
	for idx, part := range parts {
		value, atoiErr := strconv.Atoi(part)
		if atoiErr != nil {
			return [3]int{}, fmt.Errorf(messages.UpdateInvalidVersionSegmentFmt, part, atoiErr)
		}
		out[idx] = value
	}
	return out, nil
}
