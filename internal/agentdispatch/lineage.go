package agentdispatch

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/version"
)

const (
	lineageKindToolUse      = "tool_use"
	lineageKindTaskStarted  = "task_started"
	lineageKindTaskTerminal = "task_terminal"
	lineageKindInvalid      = "invalid"
	claudeTaskTypeAgent     = "local_agent"
	claudeTaskStatusStopped = "stopped"
	claudeSummaryProven     = "proven-terminal"

	lineageReasonProviderVersionMissing     = "provider_version_missing"
	lineageReasonProviderVersionMalformed   = "provider_version_malformed"
	lineageReasonProviderVersionUnsupported = "provider_version_unsupported"
	lineageReasonEvidenceAbsent             = "lineage_evidence_absent"
	lineageReasonPathInvalid                = "lineage_path_invalid"
	lineageReasonEvidenceUnreadable         = "lineage_evidence_unreadable"
	lineageReasonEvidenceMalformed          = "lineage_evidence_malformed"
	lineageReasonStructureInvalid           = "lineage_structure_invalid"
	lineageReasonLimitExceeded              = "lineage_limit_exceeded"
	lineageReasonTaskStartAbsent            = "task_start_absent"
	lineageReasonTaskStartMissing           = "task_start_missing"
	lineageReasonTaskIdentifierMissing      = "task_identifier_missing"
	lineageReasonTaskTypeMissing            = "task_type_missing"
	lineageReasonTaskTypeUnknown            = "task_type_unknown"
	lineageReasonTaskTerminalMissing        = "task_terminal_missing"
	lineageReasonTaskStatusUnknown          = "task_status_unknown"
	lineageReasonTaskToolUseMismatch        = "task_tool_use_mismatch"
	lineageReasonTaskRelationshipMissing    = "task_relationship_missing"
	lineageReasonTaskParentUnresolved       = "task_parent_unresolved"
	lineageReasonTaskRelationshipConflict   = "task_relationship_conflict"
	lineageReasonTaskEventOrderInvalid      = "task_event_order_invalid"
	lineageReasonTaskCycle                  = "task_cycle"

	claudeLineageEvidenceMaxLineBytes = 16 * 1024
	claudeLineageFieldMaxBytes        = 1024
)

var claudeLineageReasons = map[string]struct{}{
	lineageReasonProviderVersionMissing: {}, lineageReasonProviderVersionMalformed: {}, lineageReasonProviderVersionUnsupported: {},
	lineageReasonEvidenceAbsent: {}, lineageReasonPathInvalid: {}, lineageReasonEvidenceUnreadable: {}, lineageReasonEvidenceMalformed: {},
	lineageReasonStructureInvalid: {}, lineageReasonLimitExceeded: {}, lineageReasonTaskStartAbsent: {}, lineageReasonTaskStartMissing: {},
	lineageReasonTaskIdentifierMissing: {}, lineageReasonTaskTypeMissing: {}, lineageReasonTaskTypeUnknown: {}, lineageReasonTaskTerminalMissing: {},
	lineageReasonTaskStatusUnknown: {}, lineageReasonTaskToolUseMismatch: {}, lineageReasonTaskRelationshipMissing: {}, lineageReasonTaskParentUnresolved: {},
	lineageReasonTaskRelationshipConflict: {}, lineageReasonTaskEventOrderInvalid: {}, lineageReasonTaskCycle: {},
}

type claudeLineageEvidence struct {
	Kind            string `json:"kind"`
	TaskID          string `json:"task_id,omitempty"`
	ToolUseID       string `json:"tool_use_id,omitempty"`
	ParentToolUseID string `json:"parent_tool_use_id,omitempty"`
	TaskType        string `json:"task_type,omitempty"`
	Status          string `json:"status,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

// ClaudeDescendantSummary is a factual summary derived from private Claude lineage evidence.
type ClaudeDescendantSummary struct {
	State   string                 `json:"state"`
	Reasons []string               `json:"reasons,omitempty"`
	Tasks   []ClaudeDescendantTask `json:"tasks,omitempty"`
}

// ClaudeDescendantTask identifies one authoritatively started Claude descendant.
type ClaudeDescendantTask struct {
	TaskID       string `json:"task_id"`
	ToolUseID    string `json:"tool_use_id"`
	ParentTaskID string `json:"parent_task_id,omitempty"`
	Status       string `json:"status"`
}

type claudeLineageNormalizer struct {
	ignoredTasks map[string]struct{}
}

func (n *claudeLineageNormalizer) reduce(record structuredRecord) []claudeLineageEvidence {
	eventType, _ := record.Fields[jsonTypeKey].(string)
	subtype, _ := record.Fields["subtype"].(string)
	projection := record.Claude
	switch {
	case eventType == "assistant":
		result := make([]claudeLineageEvidence, 0, len(projection.Content))
		for _, block := range projection.Content {
			if !block.Type.Present {
				if block.ID.Present || block.Name.Present {
					result = append(result, claudeLineageEvidence{Kind: lineageKindInvalid, Reason: lineageReasonStructureInvalid})
				}
				continue
			}
			if block.Type.Value != "tool_use" {
				continue
			}
			if !block.ID.Present || !block.Name.Present {
				result = append(result, claudeLineageEvidence{Kind: lineageKindInvalid, Reason: lineageReasonStructureInvalid})
				continue
			}
			if block.Name.Value != "Agent" {
				continue
			}
			if strings.TrimSpace(block.ID.Value) == "" {
				result = append(result, claudeLineageEvidence{Kind: lineageKindInvalid, Reason: lineageReasonTaskIdentifierMissing})
				continue
			}
			result = append(result, claudeLineageEvidence{Kind: lineageKindToolUse, ToolUseID: block.ID.Value, ParentToolUseID: projection.ParentToolUseID.Value})
		}
		return result
	case eventType == "system" && subtype == "task_started":
		if !projection.TaskType.Present || strings.TrimSpace(projection.TaskType.Value) == "" {
			return []claudeLineageEvidence{{Kind: lineageKindInvalid, Reason: lineageReasonTaskTypeMissing}}
		}
		if strings.TrimSpace(projection.TaskID.Value) == "" || strings.TrimSpace(projection.ToolUseID.Value) == "" {
			return []claudeLineageEvidence{{Kind: lineageKindInvalid, Reason: lineageReasonTaskIdentifierMissing}}
		}
		if projection.TaskType.Value == "local_bash" || projection.TaskType.Value == "local_workflow" {
			n.ignoredTasks[projection.TaskID.Value] = struct{}{}
			return nil
		}
		if projection.TaskType.Value != claudeTaskTypeAgent {
			return []claudeLineageEvidence{{Kind: lineageKindInvalid, Reason: lineageReasonTaskTypeUnknown}}
		}
		return []claudeLineageEvidence{{Kind: lineageKindTaskStarted, TaskID: projection.TaskID.Value, ToolUseID: projection.ToolUseID.Value, TaskType: projection.TaskType.Value}}
	case eventType == "system" && subtype == "task_notification":
		if _, ignored := n.ignoredTasks[projection.TaskID.Value]; ignored {
			return nil
		}
		if strings.TrimSpace(projection.TaskID.Value) == "" {
			return []claudeLineageEvidence{{Kind: lineageKindInvalid, Reason: lineageReasonTaskIdentifierMissing}}
		}
		if projection.Status.Value != dispatchStateCompleted && projection.Status.Value != dispatchStateFailed && projection.Status.Value != claudeTaskStatusStopped {
			return []claudeLineageEvidence{{Kind: lineageKindInvalid, Reason: lineageReasonTaskStatusUnknown}}
		}
		return []claudeLineageEvidence{{Kind: lineageKindTaskTerminal, TaskID: projection.TaskID.Value, ToolUseID: projection.ToolUseID.Value, Status: projection.Status.Value}}
	default:
		return nil
	}
}

type lineageTool struct {
	parentToolUseID string
}

type lineageTask struct {
	taskID       string
	toolUseID    string
	parentToolID string
	parentTaskID string
	status       string
}

var errLineageEvidenceTooLarge = errors.New("lineage evidence record exceeds bounded size")

func deriveClaudeDescendantSummary(root string, record RunRecord) *ClaudeDescendantSummary {
	if record.Agent != AgentClaude {
		return nil
	}
	reasons := make(map[string]struct{})
	add := func(reason string) { reasons[reason] = struct{}{} }
	if strings.TrimSpace(record.ProviderVersion) == "" {
		add(lineageReasonProviderVersionMissing)
		return unknownClaudeSummary(reasons, nil)
	}
	comparison, err := version.Compare(record.ProviderVersion, claudeLineageMinimumVersion)
	if err != nil {
		add(lineageReasonProviderVersionMalformed)
		return unknownClaudeSummary(reasons, nil)
	}
	if comparison < 0 {
		add(lineageReasonProviderVersionUnsupported)
		return unknownClaudeSummary(reasons, nil)
	}
	if record.LineagePath == "" {
		add(lineageReasonEvidenceAbsent)
		return unknownClaudeSummary(reasons, nil)
	}
	expected := filepath.Join(dispatchRunPath(root), record.ID, "provider.lineage")
	if record.LineagePath != expected {
		add(lineageReasonPathInvalid)
		return unknownClaudeSummary(reasons, nil)
	}
	info, err := os.Lstat(record.LineagePath)
	if err != nil {
		add(lineageReasonEvidenceUnreadable)
		return unknownClaudeSummary(reasons, nil)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		add(lineageReasonPathInvalid)
		return unknownClaudeSummary(reasons, nil)
	}
	file, err := os.Open(record.LineagePath) // #nosec G304 -- exact run-local path is checked above.
	if err != nil {
		add(lineageReasonEvidenceUnreadable)
		return unknownClaudeSummary(reasons, nil)
	}
	defer func() { _ = file.Close() }()

	tools := make(map[string]lineageTool)
	tasks := make(map[string]*lineageTask)
	toolTasks := make(map[string]string)
	ordered := make([]*lineageTask, 0)
	seen := make(map[string]struct{})
	reader := bufio.NewReaderSize(file, structuredJSONBufferBytes)
	for {
		line, readErr := readLineBounded(reader, claudeLineageEvidenceMaxLineBytes)
		if readErr != nil && readErr != io.EOF {
			if errors.Is(readErr, errLineageEvidenceTooLarge) {
				add(lineageReasonEvidenceMalformed)
			} else {
				add(lineageReasonEvidenceUnreadable)
			}
			break
		}
		if len(bytes.TrimSpace(line)) > 0 {
			var evidence claudeLineageEvidence
			decoder := json.NewDecoder(bytes.NewReader(line))
			decoder.DisallowUnknownFields()
			if decodeErr := decoder.Decode(&evidence); decodeErr != nil || hasTrailingJSON(decoder) || !validLineageFields(evidence) {
				add(lineageReasonEvidenceMalformed)
			} else {
				encoded, _ := json.Marshal(evidence)
				key := string(encoded)
				if _, duplicate := seen[key]; !duplicate {
					seen[key] = struct{}{}
					applyLineageEvidence(evidence, tools, tasks, toolTasks, &ordered, add)
				}
			}
		}
		if readErr == io.EOF {
			break
		}
	}
	if len(tasks) == 0 {
		add(lineageReasonTaskStartAbsent)
	}
	for toolID := range tools {
		if _, ok := toolTasks[toolID]; !ok {
			add(lineageReasonTaskStartMissing)
		}
	}
	for _, task := range ordered {
		if task.status == "" {
			add(lineageReasonTaskTerminalMissing)
			task.status = statusUnknown
		}
	}
	resolveTaskParents(ordered, toolTasks, add)
	detectTaskCycles(ordered, add)
	if len(reasons) > 0 {
		return unknownClaudeSummary(reasons, ordered)
	}
	return &ClaudeDescendantSummary{State: claudeSummaryProven, Tasks: presentClaudeTasks(ordered)}
}

func applyLineageEvidence(e claudeLineageEvidence, tools map[string]lineageTool, tasks map[string]*lineageTask, toolTasks map[string]string, ordered *[]*lineageTask, add func(string)) {
	switch e.Kind {
	case lineageKindInvalid:
		if _, ok := claudeLineageReasons[e.Reason]; ok {
			add(e.Reason)
		} else {
			add(lineageReasonEvidenceMalformed)
		}
	case lineageKindToolUse:
		if e.ToolUseID == "" {
			add(lineageReasonTaskIdentifierMissing)
			return
		}
		if prior, ok := tools[e.ToolUseID]; ok && prior.parentToolUseID != e.ParentToolUseID {
			add(lineageReasonTaskRelationshipConflict)
			return
		}
		tools[e.ToolUseID] = lineageTool{parentToolUseID: e.ParentToolUseID}
	case lineageKindTaskStarted:
		if e.TaskID == "" || e.ToolUseID == "" {
			add(lineageReasonTaskIdentifierMissing)
			return
		}
		if e.TaskType == "" {
			add(lineageReasonTaskTypeMissing)
		} else if e.TaskType != claudeTaskTypeAgent {
			add(lineageReasonTaskTypeUnknown)
		}
		if prior, ok := tasks[e.TaskID]; ok {
			if prior.toolUseID != e.ToolUseID {
				add(lineageReasonTaskRelationshipConflict)
			}
			return
		}
		if priorTask, ok := toolTasks[e.ToolUseID]; ok && priorTask != e.TaskID {
			add(lineageReasonTaskRelationshipConflict)
			return
		}
		tool, ok := tools[e.ToolUseID]
		if !ok {
			add(lineageReasonTaskRelationshipMissing)
		}
		task := &lineageTask{taskID: e.TaskID, toolUseID: e.ToolUseID}
		if ok && tool.parentToolUseID != "" {
			task.parentToolID = tool.parentToolUseID
			parentID, parentOK := toolTasks[tool.parentToolUseID]
			if !parentOK {
				add(lineageReasonTaskParentUnresolved)
			} else {
				task.parentTaskID = parentID
			}
		}
		tasks[e.TaskID] = task
		toolTasks[e.ToolUseID] = e.TaskID
		*ordered = append(*ordered, task)
	case lineageKindTaskTerminal:
		if e.TaskID == "" {
			add(lineageReasonTaskIdentifierMissing)
			return
		}
		task, ok := tasks[e.TaskID]
		if !ok {
			add(lineageReasonTaskStartMissing)
			add(lineageReasonTaskEventOrderInvalid)
			return
		}
		if e.Status != dispatchStateCompleted && e.Status != dispatchStateFailed && e.Status != claudeTaskStatusStopped {
			add(lineageReasonTaskStatusUnknown)
			return
		}
		if e.ToolUseID != "" && e.ToolUseID != task.toolUseID {
			add(lineageReasonTaskToolUseMismatch)
		}
		if task.status != "" && task.status != e.Status {
			add(lineageReasonTaskRelationshipConflict)
			return
		}
		task.status = e.Status
	default:
		add(lineageReasonEvidenceMalformed)
	}
}

func readLineBounded(reader *bufio.Reader, limit int) ([]byte, error) {
	line := make([]byte, 0, min(limit, structuredJSONBufferBytes))
	overflow := false
	for {
		fragment, err := reader.ReadSlice('\n')
		if !overflow && len(line)+len(fragment) <= limit {
			line = append(line, fragment...)
		} else {
			overflow = true
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if overflow {
			return nil, errLineageEvidenceTooLarge
		}
		return line, err
	}
}

func hasTrailingJSON(decoder *json.Decoder) bool {
	var extra any
	return decoder.Decode(&extra) != io.EOF
}

func validLineageFields(e claudeLineageEvidence) bool {
	if e.Kind == "" || len(e.Kind) > claudeLineageFieldMaxBytes {
		return false
	}
	for _, field := range []string{e.TaskID, e.ToolUseID, e.ParentToolUseID, e.TaskType, e.Status, e.Reason} {
		if len(field) > claudeLineageFieldMaxBytes {
			return false
		}
	}
	return true
}

func detectTaskCycles(tasks []*lineageTask, add func(string)) {
	parents := make(map[string]string, len(tasks))
	for _, task := range tasks {
		parents[task.taskID] = task.parentTaskID
	}
	for _, task := range tasks {
		seen := make(map[string]struct{})
		for current := task.taskID; current != ""; current = parents[current] {
			if _, ok := seen[current]; ok {
				add(lineageReasonTaskCycle)
				break
			}
			seen[current] = struct{}{}
		}
	}
}

func resolveTaskParents(tasks []*lineageTask, toolTasks map[string]string, add func(string)) {
	for _, task := range tasks {
		if task.parentToolID == "" {
			continue
		}
		parentID, ok := toolTasks[task.parentToolID]
		if !ok {
			add(lineageReasonTaskParentUnresolved)
			continue
		}
		task.parentTaskID = parentID
	}
}

func unknownClaudeSummary(reasons map[string]struct{}, tasks []*lineageTask) *ClaudeDescendantSummary {
	result := &ClaudeDescendantSummary{State: statusUnknown, Tasks: presentClaudeTasks(tasks)}
	for reason := range reasons {
		result.Reasons = append(result.Reasons, reason)
	}
	sort.Strings(result.Reasons)
	return result
}

func presentClaudeTasks(tasks []*lineageTask) []ClaudeDescendantTask {
	result := make([]ClaudeDescendantTask, 0, len(tasks))
	for _, task := range tasks {
		status := task.status
		if status == "" {
			status = statusUnknown
		}
		result = append(result, ClaudeDescendantTask{TaskID: task.taskID, ToolUseID: task.toolUseID, ParentTaskID: task.parentTaskID, Status: status})
	}
	return result
}
