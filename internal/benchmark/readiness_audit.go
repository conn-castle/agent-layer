package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	readinessAuditSchema    = "deepswe-readiness-audit-v1"
	taskCatalogManifestFile = "manifest.json"

	readinessStatusFailed    = "failed"
	readinessStatusChecking  = "checking"
	readinessStatusValidated = "validated"
	readinessStatusCertified = "certified"
	readinessStatusBlocked   = "blocked"
)

// ReadinessAuditOptions configures the non-paid audit of every task in the
// pinned DeepSWE checkout.
type ReadinessAuditOptions struct {
	RepoRoot          string
	TaskConcurrency   int
	RemoveTaskImages  bool
	Tasks             []string
	TaskShardIndex    int
	TaskShardCount    int
	TaskTimeout       time.Duration
	ResourcePreflight bool
	OnTaskProgress    func(ReadinessAuditProgress)
}

// ReadinessAuditProgress reports observable task-level progress during an
// audit. Status identifies work that is checking or the task's terminal status.
type ReadinessAuditProgress struct {
	Phase     string
	Message   string
	Task      string
	Status    string
	Completed int
	Required  int
}

// ReadinessAuditTask records the preflight result for one DeepSWE task.
type ReadinessAuditTask struct {
	Task   string `json:"task"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// ReadinessAuditOutcome summarizes the complete task-catalog preflight.
type ReadinessAuditOutcome struct {
	Schema        string               `json:"schema"`
	DeepSWECommit string               `json:"deep_swe_commit"`
	Required      int                  `json:"required"`
	Validated     int                  `json:"validated"`
	Certified     int                  `json:"certified"`
	Failed        int                  `json:"failed"`
	Blocked       int                  `json:"blocked"`
	Tasks         []ReadinessAuditTask `json:"tasks"`
}

// CheckAllTaskReadiness validates and certifies every task in the pinned
// DeepSWE checkout without invoking a provider model or Pier.
func CheckAllTaskReadiness(ctx context.Context, options ReadinessAuditOptions) (ReadinessAuditOutcome, error) {
	if options.RepoRoot == "" {
		return ReadinessAuditOutcome{}, errors.New("DeepSWE readiness audit requires a repository root")
	}
	if options.TaskConcurrency < 1 || options.TaskConcurrency > 8 {
		return ReadinessAuditOutcome{}, errors.New("DeepSWE readiness audit task concurrency must be from 1 to 8")
	}
	if options.RemoveTaskImages && options.TaskConcurrency != 1 {
		return ReadinessAuditOutcome{}, errors.New("DeepSWE readiness audit image removal requires task concurrency 1")
	}
	if options.TaskTimeout < 0 {
		return ReadinessAuditOutcome{}, errors.New("DeepSWE readiness audit task timeout cannot be negative")
	}
	emit := func(phase, message string) {
		if options.OnTaskProgress != nil {
			options.OnTaskProgress(ReadinessAuditProgress{Phase: phase, Message: message})
		}
	}
	emit("checkout", "Preparing the pinned DeepSWE catalog")
	shardIndex, shardCount, err := normalizeReadinessShard(options.TaskShardIndex, options.TaskShardCount)
	if err != nil {
		return ReadinessAuditOutcome{}, err
	}
	checkout, err := ensurePinnedBenchmarkCheckout(ctx, options.RepoRoot)
	if err != nil {
		return ReadinessAuditOutcome{}, err
	}
	tasks, err := listPinnedBenchmarkTasks(checkout)
	if err != nil {
		return ReadinessAuditOutcome{}, err
	}
	tasks, err = selectReadinessTasks(tasks, options.Tasks, shardIndex, shardCount)
	if err != nil {
		return ReadinessAuditOutcome{}, err
	}
	emit("validate", fmt.Sprintf("Validating %d selected task(s)", len(tasks)))

	results := make([]ReadinessAuditTask, len(tasks))
	checksums := make([]string, len(tasks))
	for index, task := range tasks {
		results[index] = ReadinessAuditTask{Task: task.ID, Status: readinessStatusFailed}
		checksum, err := prepareTaskReadiness(options.RepoRoot, checkout, task.ID)
		if err != nil {
			results[index].Error = err.Error()
			continue
		}
		checksums[index] = checksum
		results[index].Status = readinessStatusValidated
	}
	if outcome := summarizeReadinessAudit(results); outcome.Failed > 0 {
		return outcome, nil
	}
	if options.ResourcePreflight {
		emit("resources", "Checking Docker disk capacity before image pulls")
		pending := make([]benchmarkPlanTask, 0, len(tasks))
		for index, task := range tasks {
			certified, err := taskReadinessAlreadyCertified(options.RepoRoot, checkout, task.ID, checksums[index])
			if err != nil {
				return ReadinessAuditOutcome{}, err
			}
			if !certified {
				pending = append(pending, task)
			}
		}
		if err := preflightReadinessDisk(ctx, pending, !options.RemoveTaskImages); err != nil {
			return ReadinessAuditOutcome{}, err
		}
	}
	if options.RemoveTaskImages {
		return checkTaskReadinessWithBoundedDisk(ctx, options, checkout, tasks, checksums, results)
	}

	jobs := make(chan int)
	var workers sync.WaitGroup
	var infrastructureAbort atomic.Bool
	var completed atomic.Int64
	var progressMu sync.Mutex
	emitProgress := func(progress ReadinessAuditProgress) {
		if options.OnTaskProgress == nil {
			return
		}
		progressMu.Lock()
		defer progressMu.Unlock()
		options.OnTaskProgress(progress)
	}
	auditContext, cancel := context.WithCancel(ctx)
	defer cancel()
	workerCount := options.TaskConcurrency
	if workerCount > len(tasks) {
		workerCount = len(tasks)
	}
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				task := tasks[index].ID
				results[index] = ReadinessAuditTask{Task: task, Status: readinessStatusFailed}
				emitProgress(ReadinessAuditProgress{Task: task, Status: readinessStatusChecking, Completed: int(completed.Load()), Required: len(tasks)})
				if err := auditTaskReadinessWithTimeout(auditContext, options, checkout, task, checksums[index]); err != nil {
					results[index].Error = err.Error()
					if isReadinessInfrastructureFailure(err) {
						results[index].Status = readinessStatusBlocked
						infrastructureAbort.Store(true)
						cancel()
					} else if errors.Is(err, context.Canceled) && infrastructureAbort.Load() {
						results[index].Status = readinessStatusBlocked
						results[index].Error = "audit canceled after a shared infrastructure failure"
					}
				} else {
					results[index].Status = readinessStatusCertified
				}
				finished := int(completed.Add(1))
				emitProgress(ReadinessAuditProgress{Task: task, Status: results[index].Status, Completed: finished, Required: len(tasks)})
			}
		}()
	}
	for index := range tasks {
		jobs <- index
	}
	close(jobs)
	workers.Wait()

	return summarizeReadinessAudit(results), nil
}

func checkTaskReadinessWithBoundedDisk(ctx context.Context, options ReadinessAuditOptions, checkout string, tasks []benchmarkPlanTask, checksums []string, results []ReadinessAuditTask) (ReadinessAuditOutcome, error) {
	for index, task := range tasks {
		readiness, err := loadTaskReadiness(checkout, task.ID)
		if err != nil {
			return summarizeReadinessAudit(results), err
		}
		results[index] = ReadinessAuditTask{Task: task.ID, Status: readinessStatusFailed}
		if options.OnTaskProgress != nil {
			options.OnTaskProgress(ReadinessAuditProgress{Task: task.ID, Status: readinessStatusChecking, Completed: index, Required: len(tasks)})
		}
		auditErr := auditTaskReadinessWithTimeout(ctx, options, checkout, task.ID, checksums[index])
		if auditErr == nil {
			results[index].Status = readinessStatusCertified
		} else {
			results[index].Error = auditErr.Error()
			if isReadinessInfrastructureFailure(auditErr) {
				results[index].Status = readinessStatusBlocked
				for blocked := index + 1; blocked < len(tasks); blocked++ {
					results[blocked] = ReadinessAuditTask{
						Task:   tasks[blocked].ID,
						Status: readinessStatusBlocked,
						Error:  "audit canceled after a shared infrastructure failure",
					}
				}
			}
		}
		if options.OnTaskProgress != nil {
			options.OnTaskProgress(ReadinessAuditProgress{Task: task.ID, Status: results[index].Status, Completed: index + 1, Required: len(tasks)})
		}
		if cleanupErr := removeTaskReadinessImages(ctx, readiness); cleanupErr != nil {
			return summarizeReadinessAudit(results), errors.Join(auditErr, fmt.Errorf("reclaim benchmark readiness Docker images: %w", cleanupErr))
		}
		if results[index].Status == readinessStatusBlocked {
			break
		}
	}
	return summarizeReadinessAudit(results), nil
}

func removeTaskReadinessImages(ctx context.Context, readiness loadedTaskReadiness) error {
	images := []string{readiness.pinnedImage}
	if len(readiness.overlay) > 0 {
		images = append([]string{readiness.agentImage}, images...)
	}
	arguments := append([]string{dockerImageResource, "rm", dockerForceFlag}, images...)
	output, err := runTaskReadinessCommand(ctx, arguments...)
	if err != nil && !strings.Contains(strings.ToLower(string(output)), "no such image") {
		return fmt.Errorf("remove exact task images: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func summarizeReadinessAudit(results []ReadinessAuditTask) ReadinessAuditOutcome {
	outcome := ReadinessAuditOutcome{
		Schema:        readinessAuditSchema,
		DeepSWECommit: DeepSWECommit,
		Required:      len(results),
		Tasks:         results,
	}
	for _, result := range results {
		switch result.Status {
		case readinessStatusCertified:
			outcome.Certified++
		case readinessStatusValidated:
			outcome.Validated++
		case readinessStatusBlocked:
			outcome.Blocked++
		default:
			outcome.Failed++
		}
	}
	return outcome
}

func isReadinessInfrastructureFailure(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"no space left on device",
		"toomanyrequests",
		"rate exceeded",
		"readiness timed out after",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func normalizeReadinessShard(index, count int) (int, int, error) {
	if index == 0 && count == 0 {
		return 1, 1, nil
	}
	if count < 1 || count > 32 {
		return 0, 0, errors.New("DeepSWE readiness audit task shard count must be from 1 to 32")
	}
	if index < 1 || index > count {
		return 0, 0, errors.New("DeepSWE readiness audit task shard index must be from 1 to task shard count")
	}
	return index, count, nil
}

func selectReadinessTasks(catalog []benchmarkPlanTask, requested []string, shardIndex, shardCount int) ([]benchmarkPlanTask, error) {
	selected := catalog
	if len(requested) > 0 {
		wanted := make(map[string]struct{}, len(requested))
		for _, task := range requested {
			if !validTaskName(task) {
				return nil, fmt.Errorf("invalid benchmark readiness task %q", task)
			}
			if _, duplicate := wanted[task]; duplicate {
				return nil, fmt.Errorf("duplicate benchmark readiness task %q", task)
			}
			wanted[task] = struct{}{}
		}
		selected = make([]benchmarkPlanTask, 0, len(wanted))
		for _, task := range catalog {
			if _, ok := wanted[task.ID]; ok {
				selected = append(selected, task)
				delete(wanted, task.ID)
			}
		}
		if len(wanted) > 0 {
			unknown := make([]string, 0, len(wanted))
			for task := range wanted {
				unknown = append(unknown, task)
			}
			sort.Strings(unknown)
			return nil, fmt.Errorf("benchmark readiness tasks are not in the pinned catalog: %s", strings.Join(unknown, ", "))
		}
	}
	if shardCount > len(selected) {
		return nil, fmt.Errorf("DeepSWE readiness audit task shard count %d exceeds selected task count %d", shardCount, len(selected))
	}
	shard := make([]benchmarkPlanTask, 0, (len(selected)+shardCount-1)/shardCount)
	for position, task := range selected {
		if position%shardCount == shardIndex-1 {
			shard = append(shard, task)
		}
	}
	return shard, nil
}

func listPinnedBenchmarkTasks(checkout string) ([]benchmarkPlanTask, error) {
	root := filepath.Join(checkout, "tasks")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read pinned DeepSWE task catalog: %w", err)
	}
	tasks := make([]benchmarkPlanTask, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			switch entry.Name() {
			case "README.md", "dataset.toml", taskCatalogManifestFile, "manifest.schema.json":
				continue
			}
			return nil, fmt.Errorf("pinned DeepSWE task catalog contains non-directory %s", entry.Name())
		}
		if !validTaskName(entry.Name()) {
			return nil, fmt.Errorf("pinned DeepSWE task catalog contains invalid task name %q", entry.Name())
		}
		tasks = append(tasks, benchmarkPlanTask{ID: entry.Name()})
	}
	if len(tasks) == 0 {
		return nil, errors.New("pinned DeepSWE task catalog is empty")
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	manifestData, err := os.ReadFile(filepath.Join(root, taskCatalogManifestFile)) // #nosec G304 -- fixed file name below the pinned DeepSWE task catalog.
	if err != nil {
		return nil, fmt.Errorf("read pinned DeepSWE task manifest: %w", err)
	}
	var manifest struct {
		TaskCount int `json:"task_count"`
		Tasks     []struct {
			ID string `json:"task_id"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("parse pinned DeepSWE task manifest: %w", err)
	}
	if manifest.TaskCount != len(tasks) || len(manifest.Tasks) != len(tasks) {
		return nil, fmt.Errorf("pinned DeepSWE task manifest declares %d tasks and %d task records, found %d task directories", manifest.TaskCount, len(manifest.Tasks), len(tasks))
	}
	manifestTasks := make(map[string]struct{}, len(manifest.Tasks))
	for _, manifestTask := range manifest.Tasks {
		if !validTaskName(manifestTask.ID) {
			return nil, fmt.Errorf("pinned DeepSWE task manifest contains invalid task name %q", manifestTask.ID)
		}
		manifestTasks[manifestTask.ID] = struct{}{}
	}
	for _, task := range tasks {
		if _, ok := manifestTasks[task.ID]; !ok {
			return nil, fmt.Errorf("pinned DeepSWE task directory %q is absent from %s", task.ID, taskCatalogManifestFile)
		}
	}
	if len(manifestTasks) != len(tasks) {
		return nil, errors.New("pinned DeepSWE task manifest contains duplicate or unrecognized task IDs")
	}
	return tasks, nil
}

func prepareTaskReadiness(repoRoot, checkout, task string) (string, error) {
	checksum, err := validateBenchmarkTaskTree(checkout, task)
	if err != nil {
		return "", fmt.Errorf("validate task tree: %w", err)
	}
	stageRoot := filepath.Join(repoRoot, ".agent-layer", "tmp")
	if err := os.MkdirAll(stageRoot, 0o700); err != nil {
		return "", fmt.Errorf("create readiness audit staging root: %w", err)
	}
	stage, err := os.MkdirTemp(stageRoot, "readiness-audit-")
	if err != nil {
		return "", fmt.Errorf("create readiness audit staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if _, err := prepareTaskStartup(checkout, task, stage); err != nil {
		return "", fmt.Errorf("prepare task startup: %w", err)
	}
	return checksum, nil
}

func auditTaskReadiness(ctx context.Context, repoRoot, checkout, task, checksum string) error {
	if _, err := certifyTaskEnvironment(ctx, repoRoot, checkout, task, checksum); err != nil {
		return err
	}
	return nil
}

func auditTaskReadinessWithTimeout(ctx context.Context, options ReadinessAuditOptions, checkout, task, checksum string) error {
	if options.TaskTimeout == 0 {
		return auditTaskReadiness(ctx, options.RepoRoot, checkout, task, checksum)
	}
	taskContext, cancel := context.WithTimeout(ctx, options.TaskTimeout)
	defer cancel()
	err := auditTaskReadiness(taskContext, options.RepoRoot, checkout, task, checksum)
	if errors.Is(taskContext.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("benchmark task %s readiness timed out after %s: %w", task, options.TaskTimeout, context.DeadlineExceeded)
	}
	return err
}
