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
)

const (
	readinessAuditSchema    = "deepswe-readiness-audit-v1"
	taskCatalogManifestFile = "manifest.json"

	readinessStatusFailed    = "failed"
	readinessStatusValidated = "validated"
	readinessStatusCertified = "certified"
	readinessStatusBlocked   = "blocked"
)

// ReadinessAuditOptions configures the non-paid audit of every task in the
// pinned DeepSWE checkout.
type ReadinessAuditOptions struct {
	RepoRoot         string
	TaskConcurrency  int
	RemoveTaskImages bool
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
	checkout, err := ensurePinnedBenchmarkCheckout(ctx, options.RepoRoot)
	if err != nil {
		return ReadinessAuditOutcome{}, err
	}
	tasks, err := listPinnedBenchmarkTasks(checkout)
	if err != nil {
		return ReadinessAuditOutcome{}, err
	}

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
	if options.RemoveTaskImages {
		return checkTaskReadinessWithBoundedDisk(ctx, options.RepoRoot, checkout, tasks, checksums, results)
	}

	jobs := make(chan int)
	var workers sync.WaitGroup
	var infrastructureAbort atomic.Bool
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
				if err := auditTaskReadiness(auditContext, options.RepoRoot, checkout, task, checksums[index]); err != nil {
					results[index].Error = err.Error()
					if isReadinessInfrastructureFailure(err) {
						results[index].Status = readinessStatusBlocked
						infrastructureAbort.Store(true)
						cancel()
					} else if errors.Is(err, context.Canceled) && infrastructureAbort.Load() {
						results[index].Status = readinessStatusBlocked
						results[index].Error = "audit canceled after a shared infrastructure failure"
					}
					continue
				}
				results[index].Status = readinessStatusCertified
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

func checkTaskReadinessWithBoundedDisk(ctx context.Context, repoRoot, checkout string, tasks []benchmarkPlanTask, checksums []string, results []ReadinessAuditTask) (ReadinessAuditOutcome, error) {
	var previous *loadedTaskReadiness
	cleanupPrevious := func() error {
		if previous == nil {
			return nil
		}
		err := removeTaskReadinessImages(ctx, *previous)
		previous = nil
		return err
	}
	defer func() { _ = cleanupPrevious() }()

	for index, task := range tasks {
		readiness, err := loadTaskReadiness(checkout, task.ID)
		if err != nil {
			return summarizeReadinessAudit(results), err
		}
		results[index] = ReadinessAuditTask{Task: task.ID, Status: readinessStatusFailed}
		auditErr := auditTaskReadiness(ctx, repoRoot, checkout, task.ID, checksums[index])
		if err := cleanupPrevious(); err != nil {
			return summarizeReadinessAudit(results), fmt.Errorf("reclaim benchmark readiness Docker images: %w", err)
		}
		previous = &readiness
		if auditErr == nil {
			results[index].Status = readinessStatusCertified
			continue
		}
		results[index].Error = auditErr.Error()
		if !isReadinessInfrastructureFailure(auditErr) {
			continue
		}
		results[index].Status = readinessStatusBlocked
		for blocked := index + 1; blocked < len(tasks); blocked++ {
			results[blocked] = ReadinessAuditTask{
				Task:   tasks[blocked].ID,
				Status: readinessStatusBlocked,
				Error:  "audit canceled after a shared infrastructure failure",
			}
		}
		break
	}
	if err := cleanupPrevious(); err != nil {
		return summarizeReadinessAudit(results), fmt.Errorf("reclaim benchmark readiness Docker images: %w", err)
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
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
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
