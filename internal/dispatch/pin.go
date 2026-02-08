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
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		warning := fmt.Sprintf(messages.DispatchPinFileEmptyWarningFmt, path)
		return "", false, warning, nil
	}
	normalized, err := version.Normalize(raw)
	if err != nil {
		warning := fmt.Sprintf(messages.DispatchInvalidPinnedVersionWarningFmt, path, err)
		return "", false, warning, nil
	}
	return normalized, true, "", nil
}
