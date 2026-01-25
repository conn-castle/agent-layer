package run

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// Info describes a single Agent Layer run directory.
type Info struct {
	ID  string
	Dir string
}

// Create creates a new run directory under .agent-layer/tmp/runs.
func Create(root string) (*Info, error) {
	if root == "" {
		return nil, fmt.Errorf(messages.RunRootPathRequired)
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	suffix, err := randomSuffix(4)
	if err != nil {
		return nil, fmt.Errorf(messages.RunGenerateIDFailedFmt, err)
	}
	runID := fmt.Sprintf("%s-%s", stamp, suffix)
	dir := filepath.Join(root, ".agent-layer", "tmp", "runs", runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf(messages.RunCreateDirFailedFmt, dir, err)
	}
	return &Info{ID: runID, Dir: dir}, nil
}

func randomSuffix(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Reader.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
