package agentdispatch

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/clients"
)

const fanoutStateDir = "fanouts"

// FanoutChild is immutable child evidence referenced by a fanout manifest.
type FanoutChild struct {
	RunID      string       `json:"run_id"`
	Name       string       `json:"name"`
	Target     FanoutTarget `json:"target"`
	Status     string       `json:"status"`
	ResultPath string       `json:"result_path"`
	Error      string       `json:"error,omitempty"`
}

// FanoutManifest is the aggregate terminal evidence for one synchronous fanout.
type FanoutManifest struct {
	ID          string        `json:"id"`
	State       string        `json:"state"`
	CreatedAt   time.Time     `json:"created_at"`
	CompletedAt *time.Time    `json:"completed_at,omitempty"`
	Children    []FanoutChild `json:"children"`
}

type preparedFanoutChild struct {
	target  FanoutTarget
	request dispatchExecution
}

type fanoutCandidate struct {
	spec    FanoutTarget
	target  targetMeta
	version string
	prompt  []byte
}

// ParseFanoutTarget validates one repeated self-contained --target value.
func ParseFanoutTarget(value string) (FanoutTarget, error) {
	var target FanoutTarget
	seen := map[string]bool{}
	for _, field := range strings.Split(value, ",") {
		key, raw, ok := strings.Cut(field, "=")
		key, raw = strings.TrimSpace(key), strings.TrimSpace(raw)
		if !ok || key == "" || raw == "" {
			return FanoutTarget{}, exitError(ExitUsage, fmt.Sprintf("invalid fanout target %q; expected agent=<provider>[,model=<model>][,reasoning=<effort>]", value))
		}
		if seen[key] {
			return FanoutTarget{}, exitError(ExitUsage, fmt.Sprintf("fanout target %q repeats key %q", value, key))
		}
		seen[key] = true
		switch key {
		case "agent":
			target.Agent = normalizeAgent(raw)
		case "model":
			target.Model = raw
		case "reasoning":
			target.ReasoningEffort = raw
		default:
			return FanoutTarget{}, exitError(ExitUsage, fmt.Sprintf("fanout target %q contains unknown key %q", value, key))
		}
	}
	if !isProvider(target.Agent) {
		return FanoutTarget{}, exitError(ExitUsage, fmt.Sprintf("fanout target %q requires an explicit supported agent", value))
	}
	return target, nil
}

// Fanout sends one shared prompt and skill to two or more independently
// recorded targets, waits for all children, then emits one aggregate manifest.
func Fanout(opts FanoutOptions) error {
	if len(opts.Targets) < 2 {
		return exitError(ExitUsage, "dispatch fanout requires at least two --target specifications")
	}
	project, stderr, env, depth, err := loadDispatchProject(opts.Root, opts.Stderr, opts.Env)
	if err != nil {
		return err
	}
	if err := pruneDispatchEvidence(opts.Root, time.Now().UTC()); err != nil {
		return err
	}
	if err := checkDispatchDepth(project.Config, depth); err != nil {
		return err
	}
	promptText, err := ResolvePrompt(opts.PromptArgs, opts.Stdin, opts.ReadStdin)
	if err != nil {
		return err
	}
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if err := prepareProjection(project, opts.Root, stderr, opts.Quiet); err != nil {
		return err
	}
	candidates := make([]fanoutCandidate, 0, len(opts.Targets))
	for _, spec := range opts.Targets {
		target, ok := lookupTarget(spec.Agent)
		if !ok || !targetEnabled(project.Config, spec.Agent) {
			return exitError(ExitConfig, fmt.Sprintf("`al dispatch fanout` target %s is disabled or unsupported", spec.Agent))
		}
		if spec.Model != "" && !agentoptions.Supports(spec.Agent, agentoptions.KindModel) {
			return exitError(ExitUsage, fmt.Sprintf("%s does not support model overrides", spec.Agent))
		}
		if spec.ReasoningEffort != "" && !agentoptions.Supports(spec.Agent, agentoptions.KindReasoningEffort) {
			return exitError(ExitUsage, fmt.Sprintf("%s does not support reasoning overrides", spec.Agent))
		}
		path, pathErr := lookPath(target.Binary)
		if pathErr != nil {
			return exitError(ExitUnavailable, fmt.Sprintf("`al dispatch fanout` target %s requires `%s` on PATH", target.Name, target.Binary))
		}
		target, version, versionErr := compatibleTargetVersionCached(opts.Root, path, target, opts.VersionLookup)
		if versionErr != nil {
			return versionErr
		}
		prompt, promptErr := BuildChildPrompt(project, target.Name, promptText, opts.Skill)
		if promptErr != nil {
			return promptErr
		}
		if err := validateSkillProjection(project.Root, target, opts.Skill); err != nil {
			return err
		}
		candidates = append(candidates, fanoutCandidate{spec: spec, target: target, version: version, prompt: prompt})
	}
	groupID, err := newUUID()
	if err != nil {
		return wrapExitError(ExitTargetFailure, "allocate fanout group ID", err)
	}
	prepared := make([]preparedFanoutChild, 0, len(candidates))
	for _, candidate := range candidates {
		run, runErr := newDispatchRun(opts.Root, candidate.target.Name, candidate.version, dispatchModeFresh)
		if runErr != nil {
			failPreparedFanoutChildren(opts.Root, prepared, stderr, runErr)
			return runErr
		}
		run.Record.FanoutGroupID = groupID
		if parent, ok := clients.GetEnv(env, "AL_RUN_ID"); ok {
			run.Record.ParentRunID = parent
		}
		if err := writeRunRecord(run.Dir, &run.Record); err != nil {
			failPreparedFanoutChildren(opts.Root, prepared, stderr, err)
			return err
		}
		session, reserveErr := reserveSession(opts.Root, run)
		if reserveErr != nil {
			failPreparedFanoutChildren(opts.Root, prepared, stderr, reserveErr)
			return reserveErr
		}
		prepared = append(prepared, preparedFanoutChild{target: candidate.spec, request: dispatchExecution{
			Root: opts.Root, Project: project, Target: candidate.target, Version: candidate.version,
			Prompt: candidate.prompt, Mode: dispatchModeFresh, Run: run, Session: session,
			Stdout: io.Discard, Stderr: stderr, Env: env, Depth: depth + 1,
			Model: candidate.spec.Model, Effort: candidate.spec.ReasoningEffort, NewCommand: opts.NewCommand,
			VersionLookup: opts.VersionLookup,
		}})
	}
	manifest := FanoutManifest{ID: groupID, State: dispatchStateRunning, CreatedAt: time.Now().UTC(), Children: make([]FanoutChild, len(prepared))}
	for index := range prepared {
		manifest.Children[index] = FanoutChild{RunID: prepared[index].request.Run.Record.ID, Name: prepared[index].request.Session.Name, Target: prepared[index].target, Status: dispatchStatePending, ResultPath: prepared[index].request.Run.Record.AnswerPath}
	}
	if err := writeFanoutManifest(opts.Root, manifest); err != nil {
		failPreparedFanoutChildren(opts.Root, prepared, stderr, err)
		return err
	}
	handles := make([]executionHandle, len(prepared))
	for index := range prepared {
		handles[index] = launchExecution(prepared[index].request)
	}
	type childResult struct {
		index int
		err   error
	}
	results := make(chan childResult, len(handles))
	for index, handle := range handles {
		go func(index int, handle executionHandle) { results <- childResult{index: index, err: handle.await()} }(index, handle)
	}
	failed := false
	for range handles {
		completed := <-results
		index := completed.index
		handle := handles[index]
		childErr := completed.err
		record, loadErr := loadRunRecord(opts.Root, handle.runID)
		if loadErr != nil {
			return loadErr
		}
		currentManifest, manifestErr := loadFanoutManifest(opts.Root, groupID)
		if manifestErr != nil {
			return manifestErr
		}
		manifest = currentManifest
		manifest.Children[index].Status = record.State
		if childErr != nil {
			failed = true
			manifest.Children[index].Error = childErr.Error()
		}
		if err := writeFanoutManifest(opts.Root, manifest); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	if manifest.State != dispatchStateCancelled {
		manifest.CompletedAt = &now
		manifest.State = dispatchStateCompleted
		if failed {
			manifest.State = dispatchStateFailed
		}
	}
	if err := writeFanoutManifest(opts.Root, manifest); err != nil {
		return err
	}
	encoder := json.NewEncoder(writerOrDiscard(opts.Stdout))
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		return wrapExitError(ExitTargetFailure, "write fanout manifest", err)
	}
	if failed {
		return exitError(ExitTargetFailure, fmt.Sprintf("dispatch fanout %s completed with one or more failed children", groupID))
	}
	return nil
}

func fanoutStateRoot(root string) string {
	return filepath.Join(root, ".agent-layer", "tmp", fanoutStateDir)
}

func fanoutPath(root string, id string) string {
	return filepath.Join(fanoutStateRoot(root), id, "manifest.json")
}

// failPreparedFanoutChildren finalizes children that were recorded and
// reserved but never launched, so a preparation failure does not leave
// pending runs whose claims only a per-run cancel could release.
func failPreparedFanoutChildren(root string, prepared []preparedFanoutChild, stderr io.Writer, cause error) {
	for _, child := range prepared {
		record := child.request.Run.Record
		now := time.Now().UTC()
		record.State = dispatchStateFailed
		record.RecoveryState = recoveryRetrySafe
		record.CompletedAt = &now
		record.TerminalReason = fmt.Sprintf("fanout preparation failed before launch: %v", cause)
		if err := writeRunRecord(child.request.Run.Dir, &record); err != nil {
			_, _ = fmt.Fprintf(stderr, "warning: could not finalize prepared fanout child %s: %v\n", record.ID, err)
			continue
		}
		if err := releaseConversation(root, child.request.Session.Name, record.ID); err != nil {
			_, _ = fmt.Fprintf(stderr, "warning: could not release claim for fanout child %s: %v\n", child.request.Session.Name, err)
		}
	}
}

func writeFanoutManifest(root string, manifest FanoutManifest) error {
	path := fanoutPath(root, manifest.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(path), "manifest.lock"), os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- fixed private group path.
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil { //nolint:gosec // supported Unix descriptor.
		return err
	}
	defer func() { _ = unix.Flock(int(lock.Fd()), unix.LOCK_UN) }() //nolint:gosec // supported Unix descriptor.
	var current FanoutManifest
	if err := readJSON(path, &current); err == nil && current.State == dispatchStateCancelled && manifest.State != dispatchStateCancelled {
		manifest.State = dispatchStateCancelled
		manifest.CompletedAt = current.CompletedAt
	}
	return writeJSONAtomic(path, manifest)
}

func loadFanoutManifest(root string, id string) (FanoutManifest, error) {
	var manifest FanoutManifest
	if parseUUID(id) != nil {
		return manifest, exitError(ExitUsage, fmt.Sprintf("fanout handle %q is invalid", id))
	}
	if err := readJSON(fanoutPath(root, id), &manifest); err != nil {
		if os.IsNotExist(err) {
			return manifest, exitError(ExitUsage, fmt.Sprintf("fanout %q was not found", id))
		}
		return manifest, wrapExitError(ExitConfig, "read fanout manifest", err)
	}
	if manifest.ID != id {
		return manifest, exitError(ExitConfig, fmt.Sprintf("fanout %q is invalid", id))
	}
	return manifest, nil
}
