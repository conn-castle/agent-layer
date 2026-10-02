package versiondispatch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/version"
)

// ReadPinnedVersion reads and normalizes the pinned version from .agent-layer/al.version.
// Empty or invalid pin files return ok=false instead of an error so callers can fall
// through while still receiving the warning text.
func ReadPinnedVersion(rootDir string) (string, bool, string, error) {
	return readPinnedVersion(RealSystem{}, rootDir)
}

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

	normalized, ok, err := version.ParsePin(data)
	if err != nil {
		return "", false, fmt.Sprintf(messages.DispatchInvalidPinnedVersionWarningFmt, path, err), nil
	}
	if !ok {
		return "", false, fmt.Sprintf(messages.DispatchPinFileEmptyWarningFmt, path), nil
	}
	return normalized, true, "", nil
}
