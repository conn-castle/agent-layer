package probe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// CreateDir creates a unique provider probe directory under tmpRoot, creating
// tmpRoot first when needed. The returned path is absolute.
func CreateDir(tmpRoot string, provider string, probedAt time.Time) (string, error) {
	absTmpRoot, err := filepath.Abs(tmpRoot)
	if err != nil {
		return "", fmt.Errorf("resolve %s probe tmp root: %w", provider, err)
	}
	if err := os.MkdirAll(absTmpRoot, 0o700); err != nil {
		return "", fmt.Errorf("create %s probe tmp root %s: %w", provider, absTmpRoot, err)
	}
	probeDir, err := os.MkdirTemp(absTmpRoot, "probe-"+provider+"-"+probedAt.Format("20060102T150405Z")+"-")
	if err != nil {
		return "", fmt.Errorf("create %s probe dir in %s: %w", provider, absTmpRoot, err)
	}
	return probeDir, nil
}

// WriteOutputs records a provider run's stdout and stderr in probeDir.
func WriteOutputs(probeDir string, provider string, stdout []byte, stderr []byte) error {
	if err := os.WriteFile(filepath.Join(probeDir, "stdout.txt"), stdout, 0o600); err != nil {
		return fmt.Errorf("write %s probe stdout: %w", provider, err)
	}
	if err := os.WriteFile(filepath.Join(probeDir, "stderr.txt"), stderr, 0o600); err != nil {
		return fmt.Errorf("write %s probe stderr: %w", provider, err)
	}
	return nil
}

// ExitCode maps a Run error to the exit code a probe result reports:
// 124 for cancellation or deadline, the process exit code, or -1 when the
// process never produced one.
func ExitCode(err error) int {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 124
	}
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
