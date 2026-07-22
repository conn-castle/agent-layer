package agentdispatch

import (
	"strings"
)

const (
	lineageKindToolUse      = "tool_use"
	lineageKindTaskStarted  = "task_started"
	lineageKindTaskTerminal = "task_terminal"
	lineageKindInvalid      = "invalid"
	claudeTaskTypeAgent     = "local_agent"
	claudeTaskStatusStopped = "stopped"

	lineageReasonEvidenceMalformed     = "lineage_evidence_malformed"
	lineageReasonStructureInvalid      = "lineage_structure_invalid"
	lineageReasonLimitExceeded         = "lineage_limit_exceeded"
	lineageReasonTaskIdentifierMissing = "task_identifier_missing"
	lineageReasonTaskTypeMissing       = "task_type_missing"
	lineageReasonTaskTypeUnknown       = "task_type_unknown"
	lineageReasonTaskStatusUnknown     = "task_status_unknown"
)

type claudeLineageEvidence struct {
	Kind            string `json:"kind"`
	TaskID          string `json:"task_id,omitempty"`
	ToolUseID       string `json:"tool_use_id,omitempty"`
	ParentToolUseID string `json:"parent_tool_use_id,omitempty"`
	TaskType        string `json:"task_type,omitempty"`
	Status          string `json:"status,omitempty"`
	Reason          string `json:"reason,omitempty"`
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
