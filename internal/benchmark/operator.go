package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/pelletier/go-toml/v2"
)

const estimatedTaskImageBytes int64 = 4 << 30

// AutomaticTaskConcurrency chooses a conservative worker count. DeepSWE's
// certified amd64 containers are emulated on ARM hosts, where parallel task
// execution is usually slower and substantially more failure-prone.
func AutomaticTaskConcurrency(providerCalls bool) int {
	if providerCalls {
		// Provider cells are long-running, expensive, and each owns a large task
		// image. Serial execution lets the CLI reclaim that image after all cells
		// for one task and avoids multiplying provider rate-limit pressure.
		return 1
	}
	if runtime.GOARCH != benchmarkTaskContainerArchitecture {
		return 1
	}
	workers := runtime.NumCPU() / 4
	if workers < 1 {
		workers = 1
	}
	limit := 4
	if workers > limit {
		workers = limit
	}
	return workers
}

// StudyTaskIDs returns the exact task membership declared by a study's
// selection. It intentionally does not stage treatment inputs or authenticate.
func StudyTaskIDs(studyPath string) ([]string, error) {
	abs, err := filepath.Abs(studyPath)
	if err != nil {
		return nil, fmt.Errorf("resolve benchmark study: %w", err)
	}
	data, err := readRegularNonempty(abs)
	if err != nil {
		return nil, fmt.Errorf("read benchmark study: %w", err)
	}
	var document struct {
		Selection string `toml:"selection"`
	}
	decoder := toml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode benchmark study: %w", err)
	}
	selectionPath, err := resolveStudyPath(filepath.Dir(abs), document.Selection)
	if err != nil {
		return nil, fmt.Errorf("benchmark study selection: %w", err)
	}
	selection, _, err := loadMatrixSelection(selectionPath, nil)
	if err != nil {
		return nil, err
	}
	tasks := make([]string, len(selection.Tasks))
	for index, task := range selection.Tasks {
		tasks[index] = task.ID
	}
	return tasks, nil
}

type dockerDesktopSettings struct {
	DiskSizeMiB int64 `json:"DiskSizeMiB"`
}

var readinessDiskCapacity = detectDockerDiskAvailable

func preflightReadinessDisk(ctx context.Context, tasks []benchmarkPlanTask, keepImages bool) error {
	if len(tasks) == 0 {
		return nil
	}
	available, err := readinessDiskCapacity(ctx)
	if err != nil {
		return fmt.Errorf("determine Docker disk capacity before pulling benchmark images: %w", err)
	}
	imageCount := len(tasks)
	if !keepImages && imageCount > 1 {
		imageCount = 1
	}
	required := int64(imageCount) * estimatedTaskImageBytes
	if available < required {
		return fmt.Errorf("insufficient Docker disk for %d benchmark task image(s): need about %s, %s available; reduce the study/task scope or allow automatic image reclamation", len(tasks), formatBytes(required), formatBytes(available))
	}
	return nil
}

func taskReadinessAlreadyCertified(repoRoot, checkout, task, checksum string) (bool, error) {
	readiness, err := loadTaskReadiness(checkout, task)
	if err != nil {
		return false, err
	}
	// A durable overlay receipt does not imply its locally built image remains;
	// automatic reclamation commonly removes it. Keep its build capacity in the
	// resource preflight even though its logical identity is reproducible.
	if len(readiness.overlay) > 0 {
		return false, nil
	}
	receipt, identity, err := taskEnvironmentCertificationIdentity(readiness, task, checksum)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "environment-certifications", identity+".json")) // #nosec G304 -- content-addressed private state.
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var existing taskReadinessCertification
	return json.Unmarshal(data, &existing) == nil && existing == receipt, nil
}

func detectDockerDiskAvailable(ctx context.Context) (int64, error) {
	if runtime.GOOS == "linux" {
		output, err := exec.CommandContext(ctx, commandDocker, "info", "--format", "{{.DockerRootDir}}").Output()
		if err != nil {
			return 0, err
		}
		var stat syscall.Statfs_t
		if err := syscall.Statfs(string(bytes.TrimSpace(output)), &stat); err != nil {
			return 0, err
		}
		if stat.Bsize <= 0 || stat.Bavail > uint64(math.MaxInt64)/uint64(stat.Bsize) {
			return 0, errors.New("docker filesystem capacity is invalid")
		}
		return int64(stat.Bavail * uint64(stat.Bsize)), nil // #nosec G115 -- the preceding bound proves the product fits int64.
	}
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return 0, err
		}
		settingsPath := filepath.Join(home, "Library", "Group Containers", "group.com.docker", "settings-store.json")
		data, err := os.ReadFile(settingsPath) // #nosec G304 -- fixed Docker Desktop settings path.
		if err != nil {
			return 0, err
		}
		var settings dockerDesktopSettings
		if err := json.Unmarshal(data, &settings); err != nil || settings.DiskSizeMiB <= 0 {
			return 0, errors.New("docker Desktop does not report its virtual disk limit")
		}
		rawPath := filepath.Join(home, "Library", "Containers", "com.docker.docker", "Data", "vms", "0", "data", "Docker.raw")
		info, err := os.Stat(rawPath)
		if err != nil {
			return 0, err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return 0, errors.New("docker Desktop virtual disk usage is unavailable")
		}
		available := settings.DiskSizeMiB*(1<<20) - stat.Blocks*512
		if available < 0 {
			available = 0
		}
		return available, nil
	}
	return 0, fmt.Errorf("docker disk capacity detection is unsupported on %s", runtime.GOOS)
}

func formatBytes(value int64) string {
	const gib = int64(1 << 30)
	return fmt.Sprintf("%.1f GiB", float64(value)/float64(gib))
}
