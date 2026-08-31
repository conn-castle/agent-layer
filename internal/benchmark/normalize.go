package benchmark

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

var errPierTaskResultAbsent = errors.New("pier task result is absent")

const (
	codexMCPPreflightEvidence       = "codex-mcp-preflight.json"
	antigravityMCPPreflightEvidence = "antigravity-mcp-preflight.json"
	grokMCPPreflightEvidence        = "grok-mcp-preflight.json"
	dispatchOptionsPreflightFile    = "dispatch-options-preflight.json"
	stdoutArtifactExtension         = ".stdout"
	shortContextBand                = "short_context"
	longContextBand                 = "long_context"
)

type pierTaskResult struct {
	TrialName      string    `json:"trial_name"`
	TaskChecksum   string    `json:"task_checksum"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	AgentExecution *struct {
		StartedAt  time.Time `json:"started_at"`
		FinishedAt time.Time `json:"finished_at"`
	} `json:"agent_execution"`
	VerifierExecution *struct {
		StartedAt  time.Time `json:"started_at"`
		FinishedAt time.Time `json:"finished_at"`
	} `json:"verifier"`
	AgentInfo struct {
		ModelInfo struct {
			Provider string `json:"provider"`
		} `json:"model_info"`
	} `json:"agent_info"`
	AgentResult struct {
		CostUSD *float64 `json:"cost_usd"`
	} `json:"agent_result"`
	VerifierResult *struct {
		Rewards struct {
			Reward    float64 `json:"reward"`
			F2PTotal  int     `json:"f2p_total"`
			F2PPassed int     `json:"f2p_passed"`
			F2P       float64 `json:"f2p"`
			Partial   float64 `json:"partial"`
		} `json:"rewards"`
	} `json:"verifier_result"`
	ExceptionInfo json.RawMessage `json:"exception_info"`
}

func normalizePier(stage string, request ExecutionRequest) (AttemptResult, error) {
	raw, err := readPierTaskResult(stage, request)
	if err != nil {
		return AttemptResult{}, err
	}
	provider := raw.AgentInfo.ModelInfo.Provider
	if provider == "" {
		provider = request.Model.Adapter
	}
	result := AttemptResult{
		SchemaVersion: StorageSchemaVersion, EventID: request.EventID,
		Attempt: request.Attempt, Task: request.Task, TaskChecksum: raw.TaskChecksum,
		EnvironmentIdentity: request.EnvironmentIdentity,
		StartedAt:           raw.StartedAt, FinishedAt: raw.FinishedAt, Provider: provider,
		PublishedModel: request.Model.PublishedIdentifier,
		RuntimeModel:   request.Model.RuntimeIdentifier, ReasoningEffort: request.Effort,
		ProviderClientVersion: request.Model.ProviderClientVersion,
	}
	duration := raw.FinishedAt.Sub(raw.StartedAt).Seconds()
	hasException := len(bytes.TrimSpace(raw.ExceptionInfo)) > 0 && !bytes.Equal(bytes.TrimSpace(raw.ExceptionInfo), []byte("null"))
	terminalTimeout := false
	if hasException {
		terminalTimeout, err = terminalVerifierTestTimeout(stage, raw)
		if err != nil {
			return AttemptResult{}, err
		}
		if !terminalTimeout {
			result.Status = statusFailed
			result.Error = string(raw.ExceptionInfo)
			return result, nil
		}
	}
	if !terminalTimeout {
		terminalTimeout, err = internalVerifierTestTimeout(stage)
		if err != nil {
			return AttemptResult{}, err
		}
	}
	result.Status = statusSuccess
	if terminalTimeout {
		result.VerifierOutcome = verifierOutcomeTestTimeout
		result.F2PTotal, err = pinnedTaskF2PTotal(request)
		if err != nil {
			return AttemptResult{}, err
		}
	} else {
		if raw.VerifierResult == nil {
			return AttemptResult{}, fmt.Errorf("pier result for %s is missing verifier_result", request.Task)
		}
		result.F2PPassed = raw.VerifierResult.Rewards.F2PPassed
		result.F2PTotal = raw.VerifierResult.Rewards.F2PTotal
		result.F2PScore = raw.VerifierResult.Rewards.F2P
		result.PartialScore = raw.VerifierResult.Rewards.Partial
		result.Reward = raw.VerifierResult.Rewards.Reward
	}
	result.CostUSD = raw.AgentResult.CostUSD
	result.CostKind = costKindProviderReported
	result.DurationSeconds = &duration
	result.InvocationCount = 1

	result.PatchBytes, err = submittedPatchBytes(stage)
	if err != nil {
		return AttemptResult{}, err
	}
	result.VerifierBuildFailed, result.BuildErrorExcerpt, err = verifierBuildFailed(stage)
	if err != nil {
		return AttemptResult{}, err
	}
	if request.Model.Adapter == adapterCodex || request.Model.Adapter == adapterAntigravity || request.Model.Adapter == adapterGrok || request.Arm == ArmTreatment {
		var costs treatmentCost
		var costErr error
		switch request.Model.Adapter {
		case adapterCodex:
			costs, costErr = codexAttemptCost(stage)
			result.CostKind = costKindProviderUsage
			if costs.total.minimum != costs.total.maximum {
				result.CostKind += "-range"
			}
		case adapterClaudeCode:
			costs, costErr = treatmentClaudeCost(stage, raw.AgentResult.CostUSD)
			result.CostKind = costKindProviderTotal
		case adapterAntigravity, adapterGrok:
			costs, costErr = streamProviderAttemptCost(stage, request.Model.Adapter, request.Model.RuntimeIdentifier)
			if costs.providerReported {
				result.CostKind = costKindProviderTotal
			} else {
				result.CostKind = costKindProviderUsage
			}
			if !costs.providerReported && costs.total.minimum != costs.total.maximum {
				result.CostKind += "-range"
			}
		default:
			costErr = fmt.Errorf("unsupported treatment cost provider %q", request.Model.Adapter)
		}
		if costErr != nil {
			return AttemptResult{}, costErr
		}
		result.CostUSD = float64Pointer(costs.total.midpoint())
		result.CostMinUSD = float64Pointer(costs.total.minimum)
		result.CostMaxUSD = float64Pointer(costs.total.maximum)
		result.CoordinatorCostUSD = float64Pointer(costs.coordinator.midpoint())
		result.CoordinatorCostMinUSD = float64Pointer(costs.coordinator.minimum)
		result.CoordinatorCostMaxUSD = float64Pointer(costs.coordinator.maximum)
		result.ChildCostUSD = float64Pointer(costs.child.midpoint())
		result.ChildCostMinUSD = float64Pointer(costs.child.minimum)
		result.ChildCostMaxUSD = float64Pointer(costs.child.maximum)
		result.InvocationCount = costs.invocations
	}
	result.DispatchConformant, err = dispatchConformance(stage, request)
	if err != nil {
		return AttemptResult{}, err
	}
	if err := result.Validate(); err != nil {
		return AttemptResult{}, err
	}
	return result, nil
}

func readPierTaskResult(stage string, request ExecutionRequest) (pierTaskResult, error) {
	var paths []string
	err := filepath.WalkDir(filepath.Join(stage, "jobs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && entry.Name() == "result.json" && filepath.Base(filepath.Dir(path)) != "jobs" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return pierTaskResult{}, fmt.Errorf("find Pier task result: %w", err)
	}
	var matches []pierTaskResult
	for _, path := range paths {
		data, err := os.ReadFile(path) // #nosec G122,G304 -- path was discovered below the restricted attempt stage.
		if err != nil {
			return pierTaskResult{}, err
		}
		var identity struct {
			TaskChecksum string `json:"task_checksum"`
		}
		if err := json.Unmarshal(data, &identity); err != nil {
			return pierTaskResult{}, fmt.Errorf("decode Pier result identity: %w", err)
		}
		if identity.TaskChecksum == "" {
			continue
		}
		if request.TaskChecksum == "" || identity.TaskChecksum != request.TaskChecksum {
			return pierTaskResult{}, fmt.Errorf(
				"pier task checksum %q does not match the pinned %s checksum %q",
				identity.TaskChecksum, request.Task, request.TaskChecksum,
			)
		}
		var candidate pierTaskResult
		if err := json.Unmarshal(data, &candidate); err != nil {
			return pierTaskResult{}, fmt.Errorf("decode Pier task result: %w", err)
		}
		matches = append(matches, candidate)
	}
	if len(matches) == 0 {
		return pierTaskResult{}, fmt.Errorf("%w: pier produced 0 matching task results for %s; expected one", errPierTaskResultAbsent, request.Task)
	}
	if len(matches) != 1 {
		return pierTaskResult{}, fmt.Errorf("pier produced %d matching task results for %s; expected one", len(matches), request.Task)
	}
	return matches[0], nil
}

func pierTaskResultMissing(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, errPierTaskResultAbsent)
}

func validatePierTreatmentPreflight(stage string, request ExecutionRequest) error {
	raw, err := readPierTaskResult(stage, request)
	if err != nil {
		return err
	}
	if string(raw.ExceptionInfo) != "" && string(raw.ExceptionInfo) != "null" {
		return fmt.Errorf("treatment runtime preflight failed: %s", raw.ExceptionInfo)
	}
	if request.Bundle != nil && request.Bundle.Manifest.Mode == TreatmentInstructionsAndSkills {
		provider, err := benchmarkProvider(request.Model.Adapter)
		if err != nil {
			return err
		}
		required := provider.PreflightEvidence
		evidenceCounts := map[string]int{
			required:                     0,
			dispatchOptionsPreflightFile: 0,
		}
		err = filepath.WalkDir(filepath.Join(stage, "jobs"), func(_ string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() {
				evidenceCounts[entry.Name()]++
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("inspect runtime preflight evidence: %w", err)
		}
		for _, name := range []string{required, dispatchOptionsPreflightFile} {
			if evidenceCounts[name] != 1 {
				return fmt.Errorf("treatment runtime preflight did not preserve %s", name)
			}
		}
	}
	var providerSessions int
	err = filepath.WalkDir(filepath.Join(stage, "jobs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(path) == ".jsonl" &&
			strings.Contains(path, string(filepath.Separator)+"agent"+string(filepath.Separator)+"sessions"+string(filepath.Separator)) {
			providerSessions++
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("inspect runtime preflight provider sessions: %w", err)
	}
	if providerSessions != 0 {
		return fmt.Errorf("treatment runtime preflight unexpectedly invoked the provider")
	}
	return nil
}

const buildErrorExcerptLines = 20

func verifierBuildFailed(stage string) (bool, string, error) {
	jobsRoot, err := os.OpenRoot(filepath.Join(stage, "jobs"))
	if err != nil {
		return false, "", fmt.Errorf("open verifier diagnostics root: %w", err)
	}
	defer func() { _ = jobsRoot.Close() }()
	failed, excerpt := false, ""
	err = fs.WalkDir(jobsRoot.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == "build-events.jsonl" {
			data, err := jobsRoot.ReadFile(path)
			if err != nil {
				return err
			}
			if excerpt == "" {
				excerpt = buildErrorExcerpt(data)
			}
			return nil
		}
		if (entry.Name() != verifierTestStdoutFile && entry.Name() != verifierRunLogFile) ||
			filepath.Base(filepath.Dir(path)) != executionPhaseVerifier {
			return nil
		}
		data, err := jobsRoot.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte("[build failed]")) ||
			bytes.Contains(data, []byte(`"FailedBuild"`)) {
			failed = true
		}
		return nil
	})
	if err != nil {
		return false, "", fmt.Errorf("inspect verifier build diagnostics: %w", err)
	}
	if !failed {
		return false, "", nil
	}
	return true, excerpt, nil
}

func buildErrorExcerpt(data []byte) string {
	var lines []string
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !bytes.Contains(line, []byte(`"Action":"build-output"`)) {
			continue
		}
		var event struct {
			Output string `json:"Output"`
		}
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		for _, text := range strings.Split(strings.TrimRight(event.Output, "\n"), "\n") {
			if strings.TrimSpace(text) == "" {
				continue
			}
			lines = append(lines, text)
			if len(lines) == buildErrorExcerptLines {
				return strings.Join(lines, "\n")
			}
		}
	}
	return strings.Join(lines, "\n")
}

type dispatchConformanceRecord struct {
	ID              string `json:"id"`
	Agent           string `json:"agent"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
	Role            string `json:"role,omitempty"`
	Mode            string `json:"mode"`
	State           string `json:"state"`
	ParentRunID     string `json:"parent_run_id"`
}

func dispatchConformance(stage string, request ExecutionRequest) (bool, error) {
	if request.Arm != ArmTreatment {
		return true, nil
	}
	if request.Bundle == nil {
		return false, fmt.Errorf("treatment result has no immutable bundle")
	}
	required := append([]string(nil), request.Bundle.Manifest.RequiredRoles...)
	sort.Strings(required)
	if request.Bundle.Manifest.Mode == TreatmentInstructionsAndSkills && len(required) == 0 {
		return true, nil
	}
	var paths []string
	err := filepath.WalkDir(filepath.Join(stage, "jobs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Base(filepath.Dir(path)) == dispatchEvidenceDir &&
			filepath.Ext(path) == ".json" && !isDispatchPreflightEvidence(entry.Name()) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("find treatment dispatch-conformance evidence: %w", err)
	}
	if request.Bundle.Manifest.Mode == TreatmentInstructionsOnly {
		return len(paths) == 0 && len(required) == 0, nil
	}
	if request.Bundle.Manifest.Mode != TreatmentInstructionsAndSkills {
		return false, nil
	}
	slots, err := expectedDispatchSlots(required, request.Bundle.Manifest.DispatchConfig)
	if err != nil {
		return false, err
	}
	if len(paths) == 0 {
		return false, nil
	}
	var eligible []dispatchConformanceRecord
	seenIDs := make(map[string]bool, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path) // #nosec G122,G304 -- path was discovered below the restricted attempt stage.
		if err != nil {
			return false, fmt.Errorf("read treatment dispatch evidence: %w", err)
		}
		var record dispatchConformanceRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return false, fmt.Errorf("decode treatment dispatch evidence: %w", err)
		}
		if record.ID == "" || record.State != dispatchRunStateCompleted {
			return false, nil
		}
		if seenIDs[record.ID] {
			return false, fmt.Errorf("treatment dispatch lifecycle %q is duplicated", record.ID)
		}
		seenIDs[record.ID] = true
		if record.Mode != dispatchRunModeFresh || record.ParentRunID != "" {
			continue
		}
		eligible = append(eligible, record)
	}
	used := make([]bool, len(eligible))
	for _, slot := range slots {
		matched := false
		for index, record := range eligible {
			if used[index] || !dispatchRecordMatchesSlot(record, slot) {
				continue
			}
			used[index] = true
			matched = true
			break
		}
		if !matched {
			return false, nil
		}
	}
	return true, nil
}

func dispatchRecordMatchesSlot(record dispatchConformanceRecord, slot dispatchSlot) bool {
	return record.Role == slot.role && record.Agent == slot.target.Agent &&
		record.Model == slot.target.Model && record.ReasoningEffort == slot.target.ReasoningEffort
}

func submittedPatchBytes(stage string) (int64, error) {
	var patches []string
	err := filepath.WalkDir(filepath.Join(stage, "jobs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && entry.Name() == benchmarkModelPatchFile &&
			filepath.Base(filepath.Dir(path)) == benchmarkArtifactsDir {
			patches = append(patches, path)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("find submitted model patch: %w", err)
	}
	if len(patches) != 1 {
		return 0, fmt.Errorf("pier produced %d submitted model patches; expected one", len(patches))
	}
	info, err := os.Stat(patches[0])
	if err != nil {
		return 0, fmt.Errorf("inspect submitted model patch: %w", err)
	}
	data, err := os.ReadFile(patches[0]) // #nosec G304 -- path was discovered below the restricted attempt stage.
	if err != nil {
		return 0, fmt.Errorf("read submitted model patch: %w", err)
	}
	for _, forbidden := range []string{
		"diff --git a/AGENTS.md ", "diff --git a/.agents/",
		"diff --git a/.agent-layer/", "diff --git a/docs/agent-layer/",
	} {
		if bytes.Contains(data, []byte(forbidden)) {
			return 0, fmt.Errorf("submitted model.patch contains injected treatment file")
		}
	}
	return info.Size(), nil
}

type codexSessionUsage struct {
	id               string
	isChild          bool
	hasCompleteUsage bool
	cost             costRange
}

type codexTokenUsage struct {
	InputTokens           int  `json:"input_tokens"`
	CachedInputTokens     int  `json:"cached_input_tokens"`
	CacheWriteInputTokens *int `json:"cache_write_input_tokens"`
	OutputTokens          int  `json:"output_tokens"`
}

type costRange struct {
	minimum float64
	maximum float64
}

func (cost costRange) midpoint() float64 { return (cost.minimum + cost.maximum) / 2 }

func (cost *costRange) add(other costRange) {
	cost.minimum += other.minimum
	cost.maximum += other.maximum
}

type treatmentCost struct {
	total            costRange
	coordinator      costRange
	child            costRange
	invocations      int
	providerReported bool
}

func codexAttemptCost(stage string) (treatmentCost, error) {
	pricing, err := loadBenchmarkPricing()
	if err != nil {
		return treatmentCost{}, err
	}
	var sessions []string
	err = filepath.WalkDir(filepath.Join(stage, "jobs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(path) == ".jsonl" &&
			strings.Contains(filepath.ToSlash(path), "/agent/sessions/") {
			sessions = append(sessions, path)
		}
		return nil
	})
	if err != nil {
		return treatmentCost{}, fmt.Errorf("find Codex provider sessions: %w", err)
	}
	if len(sessions) == 0 {
		return treatmentCost{}, fmt.Errorf("benchmark attempt has no Codex provider session evidence")
	}
	dispatchSessions, err := dispatchProviderSessions(stage)
	if err != nil {
		return treatmentCost{}, err
	}
	var result treatmentCost
	sessionIDs := make(map[string]bool, len(sessions))
	for _, path := range sessions {
		usage, err := parseCodexSessionCost(path, pricing)
		if err != nil {
			return treatmentCost{}, err
		}
		if sessionIDs[usage.id] {
			return treatmentCost{}, fmt.Errorf("codex provider session %q is duplicated", usage.id)
		}
		sessionIDs[usage.id] = true
		if !usage.hasCompleteUsage {
			if !dispatchRecordsProveCallerCancellation(dispatchSessions[usage.id]) {
				return treatmentCost{}, fmt.Errorf(
					"codex session %q has no complete request-level usage and is not proven caller-cancelled before usage",
					usage.id,
				)
			}
			continue
		}
		if len(dispatchSessions[usage.id]) > 0 {
			usage.isChild = true
		}
		result.total.add(usage.cost)
		if usage.isChild {
			result.child.add(usage.cost)
		} else {
			result.coordinator.add(usage.cost)
		}
		result.invocations++
	}
	for id := range dispatchSessions {
		if !sessionIDs[id] {
			return treatmentCost{}, fmt.Errorf(
				"codex dispatch session %q has no captured request-level session evidence",
				id,
			)
		}
	}
	return result, nil
}

func treatmentClaudeCost(stage string, coordinator *float64) (treatmentCost, error) {
	if coordinator == nil || *coordinator < 0 {
		return treatmentCost{}, fmt.Errorf("claude treatment coordinator cost is unavailable")
	}
	dispatchSessions, err := dispatchProviderSessions(stage)
	if err != nil {
		return treatmentCost{}, err
	}
	var paths []string
	err = filepath.WalkDir(filepath.Join(stage, "jobs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Base(filepath.Dir(path)) == dispatchEvidenceDir &&
			filepath.Ext(path) == stdoutArtifactExtension {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return treatmentCost{}, fmt.Errorf("find Claude dispatch billing evidence: %w", err)
	}
	result := treatmentCost{
		coordinator: costRange{minimum: *coordinator, maximum: *coordinator},
		invocations: 1,
	}
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		sessionID, cost, parseErr := parseClaudeSessionCost(path)
		if parseErr != nil {
			return treatmentCost{}, parseErr
		}
		if len(dispatchSessions[sessionID]) == 0 {
			return treatmentCost{}, fmt.Errorf("claude dispatch billing session %q has no matching dispatch record", sessionID)
		}
		if seen[sessionID] {
			return treatmentCost{}, fmt.Errorf("claude dispatch billing session %q is duplicated", sessionID)
		}
		seen[sessionID] = true
		result.child.add(costRange{minimum: cost, maximum: cost})
		result.invocations++
	}
	if len(seen) != len(dispatchSessions) {
		return treatmentCost{}, fmt.Errorf(
			"claude treatment has billing evidence for %d of %d dispatch sessions",
			len(seen),
			len(dispatchSessions),
		)
	}
	result.total = result.coordinator
	result.total.add(result.child)
	return result, nil
}

// streamProviderAttemptCost prices the raw, bounded provider streams emitted
// by the custom Pier adapters.  It intentionally does not use a CLI account or
// subscription total: Antigravity reports usage rather than subscription cost,
// and Grok is reconstructed from the provider's request usage records.
func streamProviderAttemptCost(stage, provider, model string) (treatmentCost, error) {
	pricing, err := loadBenchmarkPricing()
	if err != nil {
		return treatmentCost{}, err
	}
	if _, ok := pricing.Providers[provider][model]; !ok {
		return treatmentCost{}, fmt.Errorf("%s usage has no pricing for model %q", provider, model)
	}
	dispatch, err := dispatchProviderSessions(stage)
	if err != nil {
		return treatmentCost{}, err
	}
	var paths []string
	err = filepath.WalkDir(filepath.Join(stage, "jobs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		name := entry.Name()
		inAgentEvidence := strings.Contains(filepath.ToSlash(path), "/agent/")
		inDispatchEvidence := filepath.Base(filepath.Dir(path)) == dispatchEvidenceDir
		if (provider == adapterGrok && ((name == "grok.jsonl" && inAgentEvidence) || (filepath.Ext(name) == stdoutArtifactExtension && inDispatchEvidence))) ||
			(provider == adapterAntigravity && ((name == "antigravity.jsonl" && inAgentEvidence) || (filepath.Ext(name) == stdoutArtifactExtension && inDispatchEvidence))) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return treatmentCost{}, fmt.Errorf("find %s usage evidence: %w", provider, err)
	}
	if len(paths) == 0 {
		return treatmentCost{}, fmt.Errorf("benchmark attempt has no %s provider usage evidence", provider)
	}
	result := treatmentCost{providerReported: provider == adapterGrok}
	seen := map[string]string{}
	coordinatorSessions := 0
	for _, path := range paths {
		id, usage, reportedCost, parseErr := parseStreamProviderUsage(path, provider)
		if parseErr != nil {
			return treatmentCost{}, parseErr
		}
		encoded, marshalErr := json.Marshal(struct {
			Usage           []streamTokenUsage `json:"usage"`
			ReportedCostUSD *float64           `json:"reported_cost_usd,omitempty"`
		}{usage, reportedCost})
		if marshalErr != nil {
			return treatmentCost{}, fmt.Errorf("encode %s provider usage %q: %w", provider, id, marshalErr)
		}
		if previous, found := seen[id]; found {
			// Pier can preserve a coordinator stream in its native location and
			// copy the same artifact as <run-id>.stdout. Identical bytes are one
			// session, while divergent evidence is an attribution failure.
			if previous == string(encoded) {
				continue
			}
			return treatmentCost{}, fmt.Errorf("%s provider usage session %q is duplicated with different usage", provider, id)
		}
		seen[id] = string(encoded)
		child := false
		if records, dispatched := dispatch[id]; dispatched {
			if len(records) != 1 {
				return treatmentCost{}, fmt.Errorf("%s dispatch session %q has %d records; expected exactly one", provider, id, len(records))
			}
			if records[0].state != dispatchRunStateCompleted {
				return treatmentCost{}, fmt.Errorf("%s dispatch session %q is %q, not completed", provider, id, records[0].state)
			}
			child = true
		} else {
			coordinatorSessions++
		}
		if reportedCost != nil {
			if *reportedCost < 0 || math.IsNaN(*reportedCost) || math.IsInf(*reportedCost, 0) {
				return treatmentCost{}, fmt.Errorf("%s provider usage %q has invalid reported cost", provider, id)
			}
			cost := costRange{minimum: *reportedCost, maximum: *reportedCost}
			result.total.add(cost)
			if child {
				result.child.add(cost)
			} else {
				result.coordinator.add(cost)
			}
			result.invocations += len(usage)
			continue
		}
		result.providerReported = false
		for _, item := range usage {
			cost, priceErr := priceStreamRequest(filepath.Base(path), provider, model, item, pricing)
			if priceErr != nil {
				return treatmentCost{}, priceErr
			}
			result.total.add(cost)
			if child {
				result.child.add(cost)
			} else {
				result.coordinator.add(cost)
			}
			result.invocations++
		}
	}
	for id := range dispatch {
		if _, found := seen[id]; !found {
			return treatmentCost{}, fmt.Errorf("%s dispatch session %q has no captured provider usage evidence", provider, id)
		}
	}
	if coordinatorSessions != 1 {
		return treatmentCost{}, fmt.Errorf("benchmark attempt has %d %s coordinator usage sessions; expected exactly one", coordinatorSessions, provider)
	}
	if result.invocations == 0 {
		return treatmentCost{}, fmt.Errorf("benchmark attempt has no priced %s requests", provider)
	}
	return result, nil
}

type streamTokenUsage struct {
	InputTokens          *int `json:"input_tokens"`
	OutputTokens         *int `json:"output_tokens"`
	CacheReadInputTokens *int `json:"cache_read_input_tokens"`
	CacheCreationTokens  *int `json:"cache_creation_input_tokens"`
	ReasoningTokens      *int `json:"reasoning_tokens"`
}

func parseStreamProviderUsage(path, provider string) (string, []streamTokenUsage, *float64, error) {
	file, err := os.Open(path) // #nosec G304 -- path is discovered inside the restricted stage.
	if err != nil {
		return "", nil, nil, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var id string
	var usage []streamTokenUsage
	var reportedCost *float64
	terminals := 0
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event struct {
			Event          string           `json:"event"`
			Type           string           `json:"type"`
			SessionID      string           `json:"sessionId"`
			SessionIDSnake string           `json:"session_id"`
			StopReason     string           `json:"stopReason"`
			TotalCostUSD   *float64         `json:"total_cost_usd"`
			Usage          streamTokenUsage `json:"usage"`
			Result         struct {
				ConversationID string `json:"conversation_id"`
				Status         string `json:"status"`
				Usage          struct {
					InputTokens     *int `json:"input_tokens"`
					OutputTokens    *int `json:"output_tokens"`
					ThinkingTokens  *int `json:"thinking_tokens"`
					CacheReadTokens *int `json:"cache_read_tokens"`
				} `json:"usage"`
			} `json:"result"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return "", nil, nil, fmt.Errorf("decode %s usage %s: %w", provider, filepath.Base(path), err)
		}
		switch provider {
		case adapterGrok:
			if terminals > 0 {
				return "", nil, nil, fmt.Errorf("grok usage %s has records after the terminal end event", filepath.Base(path))
			}
			if event.Type == "usage" {
				usage = append(usage, event.Usage)
			}
			if event.Type == "end" {
				terminals++
				reportedCost = event.TotalCostUSD
				id = event.SessionID
				if id == "" {
					id = event.SessionIDSnake
				}
				if event.StopReason != "end_turn" {
					return "", nil, nil, fmt.Errorf("grok usage %s ended with %q", filepath.Base(path), event.StopReason)
				}
			}
		case adapterAntigravity:
			if event.Event == "result" {
				terminals++
				id = event.Result.ConversationID
				if event.Result.Status != "SUCCESS" {
					return "", nil, nil, fmt.Errorf("antigravity usage %s ended with %q", filepath.Base(path), event.Result.Status)
				}
				zero := 0
				usage = append(usage, streamTokenUsage{
					InputTokens:          event.Result.Usage.InputTokens,
					OutputTokens:         event.Result.Usage.OutputTokens,
					CacheReadInputTokens: event.Result.Usage.CacheReadTokens,
					CacheCreationTokens:  &zero,
					ReasoningTokens:      event.Result.Usage.ThinkingTokens,
				})
			}
		default:
			return "", nil, nil, fmt.Errorf("unsupported stream cost provider %q", provider)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", nil, nil, err
	}
	if terminals != 1 || id == "" || len(usage) == 0 {
		return "", nil, nil, fmt.Errorf("%s usage %s has no single successful terminal event with usage", provider, filepath.Base(path))
	}
	return id, usage, reportedCost, nil
}

func priceStreamRequest(label, provider, model string, usage streamTokenUsage, pricing benchmarkPricing) (costRange, error) {
	entry, ok := pricing.Providers[provider][model]
	if !ok {
		return costRange{}, fmt.Errorf("%s usage %s has no pricing for model %q", provider, label, model)
	}
	if usage.InputTokens == nil || usage.OutputTokens == nil || *usage.InputTokens < 0 || *usage.OutputTokens < 0 {
		return costRange{}, fmt.Errorf("%s usage %s has incomplete request-level token evidence", provider, label)
	}
	cacheRead, creation, contextKnown := 0, 0, true
	if usage.CacheReadInputTokens == nil {
		contextKnown = false
	} else {
		cacheRead = *usage.CacheReadInputTokens
	}
	if usage.CacheCreationTokens == nil {
		contextKnown = false
	} else {
		creation = *usage.CacheCreationTokens
	}
	if cacheRead < 0 || creation < 0 {
		return costRange{}, fmt.Errorf("%s usage %s has invalid cache token counts", provider, label)
	}
	if usage.ReasoningTokens != nil && (*usage.ReasoningTokens < 0 || *usage.ReasoningTokens > *usage.OutputTokens) {
		return costRange{}, fmt.Errorf("%s usage %s has invalid reasoning tokens", provider, label)
	}
	ordinaryInput := *usage.InputTokens
	if provider == adapterAntigravity {
		if cacheRead > ordinaryInput {
			return costRange{}, fmt.Errorf("%s usage %s has more cached than total input tokens", provider, label)
		}
		ordinaryInput -= cacheRead
	}
	// Grok reports ordinary input and cache reads separately; Antigravity's
	// input total includes its cache reads. Cache
	// creation is charged at the ordinary-input rate; it is not subtracted from
	// input and it is never double counted. Their documented reasoning usage is
	// a subset of output, so it is validated but not priced separately.
	return priceUsageBands(provider, label, entry, ordinaryInput, cacheRead, creation, *usage.OutputTokens, contextKnown, pricing.UnitTokens)
}

func priceUsageBands(provider, label string, entry pricingModel, ordinaryInput, cached, creation, output int, contextKnown bool, unit int) (costRange, error) {
	bands := []string{shortContextBand}
	if entry.LongContextThresholdTokens > 0 {
		if !contextKnown {
			bands = []string{shortContextBand, longContextBand}
		} else if ordinaryInput+cached+creation > entry.LongContextThresholdTokens {
			bands = []string{longContextBand}
		}
	}
	result := costRange{minimum: math.Inf(1), maximum: math.Inf(-1)}
	for _, band := range bands {
		rates := entry.Rates[band]
		inputRate, inputOK := rates["uncached_input_tokens"]
		cacheRate, cacheOK := rates["cache_read_input_tokens"]
		creationRate, creationOK := rates["cache_creation_input_tokens"]
		outputRate, outputOK := rates["output_tokens"]
		if !inputOK || !cacheOK || !creationOK || !outputOK {
			return costRange{}, fmt.Errorf("%s usage %s has incomplete %s pricing", provider, label, band)
		}
		value := (float64(ordinaryInput)*inputRate + float64(cached)*cacheRate + float64(creation)*creationRate + float64(output)*outputRate) / float64(unit)
		result.minimum = math.Min(result.minimum, value)
		result.maximum = math.Max(result.maximum, value)
	}
	return result, nil
}

func parseClaudeSessionCost(path string) (string, float64, error) {
	file, err := os.Open(path) // #nosec G304 -- path was discovered below the restricted attempt stage.
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = file.Close() }()
	var sessionID string
	var totalCost *float64
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var event struct {
			Type         string   `json:"type"`
			SessionID    string   `json:"session_id"`
			TotalCostUSD *float64 `json:"total_cost_usd"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return "", 0, fmt.Errorf("decode Claude billing evidence %s: %w", filepath.Base(path), err)
		}
		if event.Type != "result" {
			continue
		}
		if sessionID != "" {
			return "", 0, fmt.Errorf("claude billing evidence %s has multiple terminal results", filepath.Base(path))
		}
		sessionID, totalCost = event.SessionID, event.TotalCostUSD
	}
	if err := scanner.Err(); err != nil {
		return "", 0, err
	}
	if sessionID == "" || totalCost == nil || *totalCost < 0 {
		return "", 0, fmt.Errorf("claude billing evidence %s has no complete terminal cost", filepath.Base(path))
	}
	return sessionID, *totalCost, nil
}

type benchmarkPricing struct {
	UnitTokens int                                `yaml:"unit_tokens"`
	Providers  map[string]map[string]pricingModel `yaml:"providers"`
}

type pricingModel struct {
	EffectiveFrom              string                        `yaml:"effective_from"`
	SourceURL                  string                        `yaml:"source_url"`
	ReasoningOutput            string                        `yaml:"reasoning_output"`
	LongContextThresholdTokens int                           `yaml:"long_context_threshold_tokens"`
	Rates                      map[string]map[string]float64 `yaml:"rates"`
}

func loadBenchmarkPricing() (benchmarkPricing, error) {
	data, err := treatmentAssets.ReadFile("assets/pricing.yaml")
	if err != nil {
		return benchmarkPricing{}, fmt.Errorf("read embedded benchmark pricing: %w", err)
	}
	var pricing benchmarkPricing
	if err := yaml.Unmarshal(data, &pricing); err != nil {
		return benchmarkPricing{}, fmt.Errorf("decode embedded benchmark pricing: %w", err)
	}
	if pricing.UnitTokens != 1_000_000 || len(pricing.Providers[adapterCodex]) == 0 ||
		len(pricing.Providers[adapterGrok]) == 0 || len(pricing.Providers[adapterAntigravity]) == 0 {
		return benchmarkPricing{}, fmt.Errorf("embedded benchmark pricing is incomplete")
	}
	for provider, models := range pricing.Providers {
		for model, entry := range models {
			if entry.EffectiveFrom == "" || entry.SourceURL == "" || entry.ReasoningOutput != "included_in_output" {
				return benchmarkPricing{}, fmt.Errorf("embedded benchmark pricing %s/%s is missing source or reasoning provenance", provider, model)
			}
		}
	}
	return pricing, nil
}

type dispatchProviderSession struct {
	state          string
	terminalReason string
}

func dispatchProviderSessions(stage string) (map[string][]dispatchProviderSession, error) {
	sessions := make(map[string][]dispatchProviderSession)
	err := filepath.WalkDir(filepath.Join(stage, "jobs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Base(filepath.Dir(path)) != dispatchEvidenceDir ||
			filepath.Ext(path) != ".json" || isDispatchPreflightEvidence(entry.Name()) {
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G122,G304 -- path was discovered below the restricted attempt stage.
		if err != nil {
			return err
		}
		var record struct {
			ProviderSessionID string `json:"provider_session_id"`
			State             string `json:"state"`
			TerminalReason    string `json:"terminal_reason"`
		}
		if err := json.Unmarshal(data, &record); err != nil {
			return err
		}
		if record.ProviderSessionID == "" {
			return nil
		}
		sessions[record.ProviderSessionID] = append(sessions[record.ProviderSessionID], dispatchProviderSession{
			state:          record.State,
			terminalReason: record.TerminalReason,
		})
		return nil
	})
	return sessions, err
}

func isDispatchPreflightEvidence(name string) bool {
	switch name {
	case codexMCPPreflightEvidence, antigravityMCPPreflightEvidence, grokMCPPreflightEvidence, dispatchOptionsPreflightFile:
		return true
	default:
		return false
	}
}

func dispatchRecordsProveCallerCancellation(records []dispatchProviderSession) bool {
	if len(records) == 0 {
		return false
	}
	for _, record := range records {
		if record.state != "cancelled" || record.terminalReason != "cancelled by caller" {
			return false
		}
	}
	return true
}

func parseCodexSessionCost(path string, pricing benchmarkPricing) (codexSessionUsage, error) {
	file, err := os.Open(path) // #nosec G304 -- path was discovered below the restricted attempt stage.
	if err != nil {
		return codexSessionUsage{}, err
	}
	defer func() { _ = file.Close() }()
	var result codexSessionUsage
	model := ""
	seen := make(map[[2]int]bool)
	exactCost := 0.0
	cacheWriteTelemetryComplete := true
	cacheWriteTelemetryPopulated := false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var event struct {
			Type    string `json:"type"`
			Payload struct {
				ID     string `json:"id"`
				Source any    `json:"source"`
				Model  string `json:"model"`
				Type   string `json:"type"`
				Info   *struct {
					Last  *codexTokenUsage `json:"last_token_usage"`
					Total *codexTokenUsage `json:"total_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			// Codex can leave an interrupted response-item append before writing
			// the complete replacement event. Response items never carry billing
			// identity or token usage, so their malformed copies are irrelevant to
			// cost reconstruction. Billing-bearing event types remain strict.
			if bytes.Contains(scanner.Bytes(), []byte(`"type":"response_item"`)) {
				continue
			}
			return codexSessionUsage{}, fmt.Errorf("decode codex session %s: %w", filepath.Base(path), err)
		}
		switch event.Type {
		case "session_meta":
			if result.id == "" {
				result.id = event.Payload.ID
				source, _ := event.Payload.Source.(map[string]any)
				if subagent, ok := source["subagent"].(map[string]any); ok {
					_, result.isChild = subagent["thread_spawn"]
				}
			}
		case "turn_context":
			model = event.Payload.Model
		case "event_msg":
			if event.Payload.Type != "token_count" {
				continue
			}
			if event.Payload.Info == nil {
				continue
			}
			if event.Payload.Info.Last == nil || event.Payload.Info.Total == nil {
				return codexSessionUsage{}, fmt.Errorf("codex session %s has incomplete request-level token usage", filepath.Base(path))
			}
			last, cumulative := *event.Payload.Info.Last, *event.Payload.Info.Total
			signature := [2]int{cumulative.InputTokens, cumulative.OutputTokens}
			if seen[signature] {
				continue
			}
			seen[signature] = true
			requestCost, exact, err := priceCodexRequest(filepath.Base(path), model, last, pricing)
			if err != nil {
				return codexSessionUsage{}, err
			}
			result.cost.add(requestCost)
			if exact == nil {
				cacheWriteTelemetryComplete = false
				continue
			}
			if *last.CacheWriteInputTokens > 0 {
				cacheWriteTelemetryPopulated = true
			}
			exactCost += *exact
		}
	}
	if err := scanner.Err(); err != nil {
		return codexSessionUsage{}, err
	}
	if result.id == "" {
		return codexSessionUsage{}, fmt.Errorf("codex session %s has incomplete identity or billing evidence", filepath.Base(path))
	}
	result.hasCompleteUsage = len(seen) > 0
	if cacheWriteTelemetryComplete && cacheWriteTelemetryPopulated {
		result.cost.minimum = exactCost
		result.cost.maximum = exactCost
	}
	return result, nil
}

func priceCodexRequest(label, model string, usage codexTokenUsage, pricing benchmarkPricing) (costRange, *float64, error) {
	if usage.InputTokens < usage.CachedInputTokens || usage.InputTokens < 0 ||
		usage.CachedInputTokens < 0 || usage.OutputTokens < 0 {
		return costRange{}, nil, fmt.Errorf("codex usage %s has invalid token usage", label)
	}
	entry, ok := pricing.Providers[adapterCodex][model]
	if !ok {
		return costRange{}, nil, fmt.Errorf("codex usage %s has no pricing for model %q", label, model)
	}
	band := "short_context"
	if entry.LongContextThresholdTokens > 0 && usage.InputTokens > entry.LongContextThresholdTokens {
		band = "long_context"
	}
	rates := entry.Rates[band]
	uncachedRate, uncachedOK := rates["uncached_input_tokens"]
	cacheCreationRate, creationOK := rates["cache_creation_input_tokens"]
	cachedRate, cachedOK := rates["cache_read_input_tokens"]
	outputRate, outputOK := rates["output_tokens"]
	if !uncachedOK || !creationOK || !cachedOK || !outputOK {
		return costRange{}, nil, fmt.Errorf("codex usage %s has incomplete %s pricing for model %q", label, band, model)
	}
	uncached := usage.InputTokens - usage.CachedInputTokens
	fixed := float64(usage.CachedInputTokens)*cachedRate +
		float64(usage.OutputTokens)*outputRate
	cost := costRange{
		minimum: (float64(uncached)*math.Min(uncachedRate, cacheCreationRate) + fixed) / float64(pricing.UnitTokens),
		maximum: (float64(uncached)*math.Max(uncachedRate, cacheCreationRate) + fixed) / float64(pricing.UnitTokens),
	}
	if usage.CacheWriteInputTokens == nil || *usage.CacheWriteInputTokens < 0 ||
		usage.InputTokens < usage.CachedInputTokens+*usage.CacheWriteInputTokens {
		return cost, nil, nil
	}
	ordinaryInput := usage.InputTokens - usage.CachedInputTokens - *usage.CacheWriteInputTokens
	exact := (float64(ordinaryInput)*uncachedRate +
		float64(*usage.CacheWriteInputTokens)*cacheCreationRate + fixed) /
		float64(pricing.UnitTokens)
	return cost, &exact, nil
}

func float64Pointer(value float64) *float64 { return &value }
