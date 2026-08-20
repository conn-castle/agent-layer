package benchmark

import (
	"bufio"
	"bytes"
	"encoding/json"
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

const (
	codexMCPPreflightEvidence    = "codex-mcp-preflight.json"
	dispatchOptionsPreflightFile = "dispatch-options-preflight.json"
)

type pierTaskResult struct {
	TrialName    string    `json:"trial_name"`
	TaskChecksum string    `json:"task_checksum"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	AgentInfo    struct {
		ModelInfo struct {
			Provider string `json:"provider"`
		} `json:"model_info"`
	} `json:"agent_info"`
	AgentResult struct {
		CostUSD *float64 `json:"cost_usd"`
	} `json:"agent_result"`
	VerifierResult struct {
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
	if string(raw.ExceptionInfo) != "" && string(raw.ExceptionInfo) != "null" {
		result.Status = statusFailed
		result.Error = string(raw.ExceptionInfo)
		return result, nil
	}
	duration := raw.FinishedAt.Sub(raw.StartedAt).Seconds()
	result.Status = statusSuccess
	result.F2PPassed = raw.VerifierResult.Rewards.F2PPassed
	result.F2PTotal = raw.VerifierResult.Rewards.F2PTotal
	result.F2PScore = raw.VerifierResult.Rewards.F2P
	result.PartialScore = raw.VerifierResult.Rewards.Partial
	result.Reward = raw.VerifierResult.Rewards.Reward
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
	if request.Model.Adapter == adapterCodex || request.Arm == ArmTreatment {
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
	if len(matches) != 1 {
		return pierTaskResult{}, fmt.Errorf("pier produced %d matching task results for %s; expected one", len(matches), request.Task)
	}
	return matches[0], nil
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
		required := codexMCPPreflightEvidence
		if request.Model.Adapter == adapterClaudeCode {
			required = "claude-mcp-preflight.txt"
		}
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
		if (entry.Name() != "test-stdout.txt" && entry.Name() != "run.log") ||
			filepath.Base(filepath.Dir(path)) != "verifier" {
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
	Skill           string `json:"skill,omitempty"`
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
			filepath.Ext(path) == ".json" &&
			entry.Name() != codexMCPPreflightEvidence &&
			entry.Name() != dispatchOptionsPreflightFile {
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
		if record.ID == "" || record.State != "completed" {
			return false, nil
		}
		if seenIDs[record.ID] {
			return false, fmt.Errorf("treatment dispatch lifecycle %q is duplicated", record.ID)
		}
		seenIDs[record.ID] = true
		if record.Mode != "fresh" || record.ParentRunID != "" {
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
	return record.Skill == slot.skill && record.Agent == slot.target.Agent &&
		record.Model == slot.target.Model && record.ReasoningEffort == slot.target.ReasoningEffort
}

func submittedPatchBytes(stage string) (int64, error) {
	var patches []string
	err := filepath.WalkDir(filepath.Join(stage, "jobs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && entry.Name() == "model.patch" &&
			filepath.Base(filepath.Dir(path)) == "artifacts" {
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
	total       costRange
	coordinator costRange
	child       costRange
	invocations int
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
			filepath.Ext(path) == ".stdout" {
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
	UnitTokens int `yaml:"unit_tokens"`
	Providers  map[string]map[string]struct {
		LongContextThresholdTokens int                           `yaml:"long_context_threshold_tokens"`
		Rates                      map[string]map[string]float64 `yaml:"rates"`
	} `yaml:"providers"`
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
	if pricing.UnitTokens != 1_000_000 || len(pricing.Providers[adapterCodex]) == 0 {
		return benchmarkPricing{}, fmt.Errorf("embedded benchmark pricing is incomplete")
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
			filepath.Ext(path) != ".json" {
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
