package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/fsutil"
)

// maxStudySelectionBytes bounds the website-exported selection artifact.
const maxStudySelectionBytes = 4 << 20

// benchmarkPlanTask is the immutable task allocation shared by the study
// scheduler and readiness checks. It deliberately has no legacy-plan fields.
type benchmarkPlanTask struct {
	ID                string
	RepetitionsPerArm int
}

type benchmarkPlan struct {
	Tasks []benchmarkPlanTask
}

// loadedBenchmarkPlan is scheduler state, not a public plan-file schema.
type loadedBenchmarkPlan struct {
	Plan   benchmarkPlan
	Model  Model
	Effort string
}

// ObservedCostRange is the bounded provider cost for one study scope.
type ObservedCostRange struct {
	Midpoint float64 `json:"midpoint"`
	Minimum  float64 `json:"minimum"`
	Maximum  float64 `json:"maximum"`
}

func addObservedCost(left, right ObservedCostRange) ObservedCostRange {
	return ObservedCostRange{Midpoint: left.Midpoint + right.Midpoint, Minimum: left.Minimum + right.Minimum, Maximum: left.Maximum + right.Maximum}
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func regularizedBeta(a, b, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	factor := math.Exp(logGamma(a+b) - logGamma(a) - logGamma(b) + a*math.Log(x) + b*math.Log(1-x))
	if x < (a+1)/(a+b+2) {
		return factor * betaFraction(a, b, x) / a
	}
	return 1 - factor*betaFraction(b, a, 1-x)/b
}

func logGamma(value float64) float64 {
	coefficients := []float64{76.18009172947146, -86.50532032941677, 24.01409824083091, -1.231739572450155, 0.001208650973866179, -0.000005395239384953}
	cursor := value
	temporary := value + 5.5 - (value+0.5)*math.Log(value+5.5)
	series := 1.000000000190015
	for _, coefficient := range coefficients {
		cursor++
		series += coefficient / cursor
	}
	return -temporary + math.Log(2.5066282746310005*series/value)
}

func betaFraction(a, b, x float64) float64 {
	const floor = 1e-300
	qab, qap, qam := a+b, a+1, a-1
	c, d := 1.0, 1-qab*x/qap
	if math.Abs(d) < floor {
		d = floor
	}
	d = 1 / d
	result := d
	for iteration := 1; iteration <= 200; iteration++ {
		doubled, i := float64(2*iteration), float64(iteration)
		coefficient := i * (b - i) * x / ((qam + doubled) * (a + doubled))
		d = 1 + coefficient*d
		if math.Abs(d) < floor {
			d = floor
		}
		c = 1 + coefficient/c
		if math.Abs(c) < floor {
			c = floor
		}
		d = 1 / d
		result *= d * c
		coefficient = -(a + i) * (qab + i) * x / ((a + doubled) * (qap + doubled))
		d = 1 + coefficient*d
		if math.Abs(d) < floor {
			d = floor
		}
		c = 1 + coefficient/c
		if math.Abs(c) < floor {
			c = floor
		}
		d = 1 / d
		if delta := d * c; math.Abs(delta-1) < 3e-14 {
			return result * delta
		} else {
			result *= delta
		}
	}
	return result
}

func prepareBenchmarkTasks(ctx context.Context, repoRoot string, tasks []benchmarkPlanTask) (map[string]string, map[string]string, error) {
	checkout, err := ensurePinnedBenchmarkCheckout(ctx, repoRoot)
	if err != nil {
		return nil, nil, err
	}
	checksums := make(map[string]string, len(tasks))
	var validationErrors []error
	for _, task := range tasks {
		checksum, checksumErr := validateBenchmarkTaskTree(checkout, task.ID)
		if checksumErr != nil {
			validationErrors = append(validationErrors, fmt.Errorf("%s: checksum task tree: %w", task.ID, checksumErr))
			continue
		}
		checksums[task.ID] = checksum
	}
	if err := preflightTaskStartups(checkout, tasks); err != nil {
		validationErrors = append(validationErrors, err)
	}
	if len(validationErrors) > 0 {
		return nil, nil, fmt.Errorf("selected benchmark tasks failed static validation:\n%w", errors.Join(validationErrors...))
	}
	environments, err := certifyBenchmarkTaskEnvironments(ctx, repoRoot, checkout, tasks, checksums)
	if err != nil {
		return nil, nil, err
	}
	return checksums, environments, nil
}

// validateBenchmarkTaskTree validates the files required by the pinned
// DeepSWE runner and returns the immutable task-tree checksum.
func validateBenchmarkTaskTree(checkout, task string) (string, error) {
	root := filepath.Join(checkout, "tasks", task)
	for _, required := range []string{taskTOMLFile, taskInstructionFile, taskPreArtifactsFile, filepath.Join("tests", "test.sh")} {
		info, err := os.Stat(filepath.Join(root, required))
		if err != nil {
			return "", fmt.Errorf("missing %s: %w", required, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("required path %s is a directory", required)
		}
	}
	checksum, err := TaskTreeChecksum(root)
	if err != nil {
		return "", err
	}
	return checksum, nil
}

func armResultPath(stateDir, task string, attempt int) string {
	return filepath.Join(stateDir, "attempts", fmt.Sprintf("%d", attempt), "tasks", task, "result.json")
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode benchmark evidence: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create benchmark evidence directory: %w", err)
	}
	return fsutil.WriteFileAtomic(path, data, 0o600)
}

func writeJSONBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create benchmark evidence directory: %w", err)
	}
	return fsutil.WriteFileAtomic(path, data, 0o600)
}

func readStudyJSON(path string, value any) error {
	data, err := os.ReadFile(path) // #nosec G304 -- callers use verified study evidence paths.
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("decode JSON %s: %w", path, err)
	}
	return nil
}

func copyStringMap(source map[string]string) map[string]string {
	copy := make(map[string]string, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func sameStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
