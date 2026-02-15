package dispatch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/version"
)

// readPinnedVersion reads and normalizes the pinned version from .agent-layer/al.version.
// Empty or invalid pin files return a warning instead of an error so that dispatch
// can fall through to the current binary version while surfacing the problem to the user.
func readPinnedVersion(sys System, rootDir string) (string, bool, string, error) {
	path := filepath.Join(rootDir, ".agent-layer", "al.version")
	data, err := sys.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, "", nil
		}
		return "", false, "", fmt.Errorf(messages.DispatchReadPinFailedFmt, path, err)
	}

	var (
		versionLine       string
		versionLineNumber int
	)
	for idx, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if versionLineNumber != 0 {
			return "", false, fmt.Sprintf(
				messages.DispatchInvalidPinnedVersionWarningFmt,
				path,
				fmt.Sprintf("multiple version lines (%d and %d)", versionLineNumber, idx+1),
			), nil
		}
		versionLine = trimmed
		versionLineNumber = idx + 1
	}

	if versionLineNumber == 0 {
		return "", false, fmt.Sprintf(messages.DispatchPinFileEmptyWarningFmt, path), nil
	}

	normalized, err := version.Normalize(versionLine)
	if err != nil {
		warningDetail := fmt.Sprintf("line %d: %v", versionLineNumber, err)
		return "", false, fmt.Sprintf(messages.DispatchInvalidPinnedVersionWarningFmt, path, warningDetail), nil
	}
	return normalized, true, "", nil
}
