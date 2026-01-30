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
func readPinnedVersion(sys System, rootDir string) (string, bool, error) {
	path := filepath.Join(rootDir, ".agent-layer", "al.version")
	data, err := sys.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf(messages.DispatchReadPinFailedFmt, path, err)
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		return "", false, fmt.Errorf(messages.DispatchPinFileEmptyFmt, path)
	}
	normalized, err := version.Normalize(raw)
	if err != nil {
		return "", false, fmt.Errorf(messages.DispatchInvalidPinnedVersionFmt, path, err)
	}
	return normalized, true, nil
}
