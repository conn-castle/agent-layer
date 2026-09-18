package agentdispatch

import (
	"fmt"
	"strings"
)

func selectorCount(values ...string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}

func requireOneSelector(values ...string) error {
	if selectorCount(values...) != 1 {
		return exitError(ExitUsage, "dispatch requires exactly one invocation selector")
	}
	return nil
}

func resolveInvocationSelector(root string, id string, handle string, invocationID string) (RunRecord, error) {
	id = strings.TrimSpace(id)
	handle = strings.TrimSpace(handle)
	invocationID = strings.TrimSpace(invocationID)
	if err := requireOneSelector(id, handle, invocationID); err != nil {
		return RunRecord{}, err
	}
	if invocationID != "" {
		return loadRunRecord(root, invocationID)
	}
	if id != "" && parseUUID(id) == nil {
		return loadRunRecord(root, id)
	}
	if handle == "" {
		handle = id
	}
	session, err := loadSession(root, handle)
	if err != nil {
		return RunRecord{}, err
	}
	return currentSessionRun(root, session)
}

func waitConditionName(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", waitConditionTerminal:
		return waitConditionTerminal, nil
	case waitConditionTerminationConfirmed:
		return waitConditionTerminationConfirmed, nil
	default:
		return "", exitError(ExitUsage, fmt.Sprintf("dispatch wait condition %q is invalid; use terminal or termination_confirmed", value))
	}
}

func waitConditionSatisfied(record RunRecord, condition string) bool {
	switch condition {
	case waitConditionTerminationConfirmed:
		return record.TerminationConfirmed
	default:
		return terminalDispatchState(record.State)
	}
}
