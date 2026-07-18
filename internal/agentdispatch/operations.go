package agentdispatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Inspection is the stable factual shape emitted by inspect. It deliberately
// reports process transport facts without inferring provider health.
type Inspection struct {
	ID                   string                   `json:"id"`
	Name                 string                   `json:"name"`
	Agent                string                   `json:"agent"`
	Model                string                   `json:"model,omitempty"`
	ReasoningEffort      string                   `json:"reasoning_effort,omitempty"`
	Skill                string                   `json:"skill,omitempty"`
	State                string                   `json:"state"`
	RecoveryState        string                   `json:"recovery_state"`
	Mode                 string                   `json:"mode"`
	Attempt              int                      `json:"attempt"`
	Process              string                   `json:"process"`
	StartedAt            time.Time                `json:"started_at"`
	LastOutputAt         *time.Time               `json:"last_output_at,omitempty"`
	LastActivityAt       *time.Time               `json:"last_activity_at,omitempty"`
	LastActivityKind     string                   `json:"last_activity_kind,omitempty"`
	ProviderConversation string                   `json:"provider_conversation,omitempty"`
	NotResumable         bool                     `json:"not_resumable,omitempty"`
	TerminalReason       string                   `json:"terminal_reason,omitempty"`
	ClaudeDescendants    *ClaudeDescendantSummary `json:"claude_descendants,omitempty"`
	Artifacts            Artifacts                `json:"artifacts"`
}

// Artifacts identifies private diagnostic evidence without embedding it in
// normal command output.
type Artifacts struct {
	Answer      string `json:"answer"`
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	Events      string `json:"events,omitempty"`
	Lineage     string `json:"lineage,omitempty"`
	ProviderLog string `json:"provider_log,omitempty"`
}

// Inspect writes one factual dispatch inspection by current name or immutable
// run UUID. It does not mutate dispatch state or provider processes.
func Inspect(request InspectionRequest) error {
	stdout := writerOrDiscard(request.Stdout)
	inspection, err := resolveInspection(request.Root, strings.TrimSpace(request.ID))
	if err != nil {
		return err
	}
	if request.JSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(inspection)
	}
	_, err = fmt.Fprintf(stdout, "Dispatch: %s\nAgent: %s\n", inspection.Name, inspection.Agent)
	if err != nil {
		return err
	}
	if inspection.Model != "" {
		if _, err := fmt.Fprintf(stdout, "Model: %s\n", inspection.Model); err != nil {
			return err
		}
	}
	if inspection.ReasoningEffort != "" {
		if _, err := fmt.Fprintf(stdout, "Reasoning effort: %s\n", inspection.ReasoningEffort); err != nil {
			return err
		}
	}
	if inspection.Skill != "" {
		if _, err := fmt.Fprintf(stdout, "Skill: %s\n", inspection.Skill); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(stdout, "Mode: %s\nState: %s\nRecovery: %s\nProcess: %s\nAttempt: %d\nStarted: %s\n", inspection.Mode, inspection.State, inspection.RecoveryState, inspection.Process, inspection.Attempt, inspection.StartedAt.Format(time.RFC3339)); err != nil {
		return err
	}
	if inspection.LastOutputAt != nil {
		if _, err := fmt.Fprintf(stdout, "Last output: %s\n", inspection.LastOutputAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if inspection.LastActivityAt != nil {
		if _, err := fmt.Fprintf(stdout, "Last activity: %s (%s)\n", inspection.LastActivityAt.Format(time.RFC3339), inspection.LastActivityKind); err != nil {
			return err
		}
	}
	conversation := inspection.ProviderConversation
	if conversation == "" {
		conversation = dispatchStatePending
	}
	if _, err := fmt.Fprintf(stdout, "Provider conversation: %s\n", conversation); err != nil {
		return err
	}
	if inspection.NotResumable {
		if _, err := fmt.Fprintln(stdout, "Provider status: not resumable"); err != nil {
			return err
		}
	}
	if inspection.TerminalReason != "" {
		if _, err := fmt.Fprintf(stdout, "Terminal reason: %s\n", inspection.TerminalReason); err != nil {
			return err
		}
	}
	if inspection.ClaudeDescendants != nil {
		if _, err := fmt.Fprintf(stdout, "Claude descendants: %s", inspection.ClaudeDescendants.State); err != nil {
			return err
		}
		if inspection.ClaudeDescendants.State == claudeSummaryProven {
			counts := map[string]int{dispatchStateCompleted: 0, dispatchStateFailed: 0, claudeTaskStatusStopped: 0}
			for _, task := range inspection.ClaudeDescendants.Tasks {
				counts[task.Status]++
			}
			if _, err := fmt.Fprintf(stdout, " (completed=%d failed=%d stopped=%d)\n", counts[dispatchStateCompleted], counts[dispatchStateFailed], counts[claudeTaskStatusStopped]); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintf(stdout, " (%s)\n", strings.Join(inspection.ClaudeDescendants.Reasons, ",")); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(stdout, "Diagnostics: stdout=%s stderr=%s", inspection.Artifacts.Stdout, inspection.Artifacts.Stderr); err != nil {
		return err
	}
	if inspection.Artifacts.Lineage != "" {
		if _, err := fmt.Fprintf(stdout, " lineage=%s", inspection.Artifacts.Lineage); err != nil {
			return err
		}
	}
	if inspection.Artifacts.ProviderLog != "" {
		if _, err := fmt.Fprintf(stdout, " provider_log=%s", inspection.Artifacts.ProviderLog); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(stdout)
	return err
}

func resolveInspection(root string, id string) (Inspection, error) {
	if id == "" {
		return Inspection{}, exitError(ExitUsage, "dispatch inspect requires a friendly name or run UUID")
	}
	var record RunRecord
	if err := parseUUID(id); err == nil {
		loaded, loadErr := loadRunRecord(root, id)
		if loadErr != nil {
			return Inspection{}, loadErr
		}
		record = loaded
	} else {
		session, loadErr := loadSession(root, id)
		if loadErr != nil {
			return Inspection{}, loadErr
		}
		if session.RunID == "" {
			return Inspection{}, exitError(ExitConfig, fmt.Sprintf("dispatch session %q has no run record", session.Name))
		}
		loaded, recordErr := loadRunRecord(root, session.RunID)
		if recordErr != nil {
			return Inspection{}, recordErr
		}
		record = loaded
	}
	record, err := reconcileOrphan(root, record)
	if err != nil {
		return Inspection{}, err
	}
	inspection := inspectionFromRecord(record)
	inspection.ClaudeDescendants = deriveClaudeDescendantSummary(root, record)
	return inspection, nil
}

func inspectionFromRecord(record RunRecord) Inspection {
	return Inspection{
		ID:                   record.ID,
		Name:                 record.Name,
		Agent:                record.Agent,
		Model:                record.Model,
		ReasoningEffort:      record.ReasoningEffort,
		Skill:                record.Skill,
		State:                record.State,
		RecoveryState:        record.RecoveryState,
		Mode:                 record.Mode,
		Attempt:              record.Attempt,
		Process:              processAlive(record.PID),
		StartedAt:            record.StartedAt,
		LastOutputAt:         record.LastOutputAt,
		LastActivityAt:       record.LastActivityAt,
		LastActivityKind:     record.LastActivityKind,
		ProviderConversation: record.ProviderSessionID,
		NotResumable:         record.NotResumable,
		TerminalReason:       record.TerminalReason,
		Artifacts: Artifacts{
			Answer:      record.AnswerPath,
			Stdout:      record.StdoutPath,
			Stderr:      record.StderrPath,
			Events:      record.EventsPath,
			Lineage:     record.LineagePath,
			ProviderLog: record.ProviderLogPath,
		},
	}
}

func reconcileOrphan(root string, record RunRecord) (RunRecord, error) {
	if record.State == dispatchStateCancelled {
		// Cancellation is terminal user-visible evidence, but the active claim
		// remains owned until the execution releases it or the recorded wrapper
		// never acquired process identity or is provably dead. Inspection may
		// perform that conservative recovery.
		if !cancelledClaimReleasable(record) {
			return record, nil
		}
		if err := releaseConversation(root, record.Name, record.ID); err != nil {
			return RunRecord{}, err
		}
		return record, nil
	}
	if record.State != dispatchStateRunning {
		return record, nil
	}
	// Only a definitively dead wrapper is reconciled. Unprovable ownership
	// (identity capture unavailable) must not terminalize a possibly live run.
	if processOwnership(record) != ownershipDead {
		return record, nil
	}
	now := time.Now().UTC()
	record.State = dispatchStateInterrupted
	record.RecoveryState = recoveryAcceptanceUnknown
	record.CompletedAt = &now
	record.TerminalReason = "owned provider wrapper is no longer running"
	if err := writeRunRecord(filepathForRun(root, record.ID), &record); err != nil {
		return RunRecord{}, err
	}
	if err := releaseConversation(root, record.Name, record.ID); err != nil {
		return RunRecord{}, err
	}
	return record, nil
}

func filepathForRun(root string, id string) string { return filepath.Join(dispatchRunPath(root), id) }

const (
	ownershipOwned   = "owned"
	ownershipDead    = "dead"
	ownershipUnknown = "unknown"
)

// processOwnership reports whether the recorded wrapper is provably ours
// (owned), provably gone (dead), or unprovable either way (unknown), for
// example when start-identity capture is unavailable in this environment.
func processOwnership(record RunRecord) string {
	switch processAlive(record.PID) {
	case processStatusAlive:
	case processStatusDead:
		return ownershipDead
	default:
		return ownershipUnknown
	}
	if record.ProcessStartIdentity == "" {
		return ownershipUnknown
	}
	current := processStartIdentity(record.PID)
	if current == "" {
		return ownershipUnknown
	}
	if current == record.ProcessStartIdentity {
		return ownershipOwned
	}
	// An alive PID with a different start identity is a reused PID.
	return ownershipDead
}

func processOwned(record RunRecord) bool {
	return processOwnership(record) == ownershipOwned
}

// History writes every retained immutable turn for one friendly name.
func History(request HistoryRequest) error {
	name := strings.TrimSpace(request.Name)
	session, err := loadSession(request.Root, name)
	if err != nil {
		return err
	}
	if session.RunID == "" {
		return exitError(ExitConfig, fmt.Sprintf("dispatch session %q has no run record", session.Name))
	}
	records, warnings, err := listRunRecords(request.Root)
	if err != nil {
		return err
	}
	stderr := writerOrDiscard(request.Stderr)
	for _, warning := range warnings {
		if _, err := fmt.Fprintf(stderr, "warning: %s\n", warning); err != nil {
			return err
		}
	}
	type historyRun struct {
		RunRecord
		ClaudeDescendants *ClaudeDescendantSummary `json:"claude_descendants,omitempty"`
	}
	byID := make(map[string]RunRecord, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}
	failuresByID := make(map[string]error, len(warnings))
	for _, warning := range warnings {
		failuresByID[warning.ID] = warning.Err
	}
	history := make([]historyRun, 0)
	seen := make(map[string]struct{})
	nextID := session.RunID
	retentionBoundary := false
	for nextID != "" {
		if _, duplicate := seen[nextID]; duplicate {
			return exitError(ExitConfig, fmt.Sprintf("dispatch history for %q contains a cycle at run %s", name, nextID))
		}
		seen[nextID] = struct{}{}
		record, ok := byID[nextID]
		if !ok {
			loadErr, listedFailure := failuresByID[nextID]
			if nextID == session.RunID {
				if listedFailure {
					return wrapExitError(ExitConfig, fmt.Sprintf("dispatch history for %q cannot read current run %s: %v; preserve its evidence and repair or restore the run record", name, nextID, loadErr), loadErr)
				}
				return exitError(ExitConfig, fmt.Sprintf("dispatch history for %q cannot find current run %s; restore the run record or repair the session mapping", name, nextID))
			}
			if listedFailure && !errors.Is(loadErr, errDispatchRunNotFound) {
				return wrapExitError(ExitConfig, fmt.Sprintf("dispatch history for %q cannot read predecessor run %s: %v; preserve its evidence and repair or restore the run record", name, nextID, loadErr), loadErr)
			}
			retentionBoundary = true
			break
		}
		if record.Name != name {
			return exitError(ExitConfig, fmt.Sprintf("dispatch history for %q includes run %s recorded for %q; preserve its evidence and repair the session mapping or predecessor link", name, record.ID, record.Name))
		}
		history = append(history, historyRun{RunRecord: record, ClaudeDescendants: deriveClaudeDescendantSummary(request.Root, record)})
		nextID = record.PreviousRunID
	}
	slices.Reverse(history)
	stdout := writerOrDiscard(request.Stdout)
	if request.JSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(struct {
			Name              string       `json:"name"`
			RetentionBoundary bool         `json:"retention_boundary"`
			Runs              []historyRun `json:"runs"`
		}{Name: name, RetentionBoundary: retentionBoundary, Runs: history})
	}
	for _, record := range history {
		if _, err := fmt.Fprintf(stdout, "%s\t%s\t%s\t%s", record.ID, record.Mode, record.State, record.StartedAt.Format(time.RFC3339)); err != nil {
			return err
		}
		if record.ClaudeDescendants != nil {
			if _, err := fmt.Fprintf(stdout, "\tclaude_descendants=%s", record.ClaudeDescendants.State); err != nil {
				return err
			}
			if len(record.ClaudeDescendants.Reasons) > 0 {
				if _, err := fmt.Fprintf(stdout, "(%s)", strings.Join(record.ClaudeDescendants.Reasons, ",")); err != nil {
					return err
				}
			}
		}
		if _, err := fmt.Fprintln(stdout); err != nil {
			return err
		}
	}
	if retentionBoundary {
		_, err = fmt.Fprintln(stdout, "History begins at the 30-day retention boundary.")
	}
	return err
}

// Cancel terminates only the exact Agent Layer-owned process group.
func Cancel(request CancelRequest) error {
	id := strings.TrimSpace(request.ID)
	if parseUUID(id) == nil {
		if _, statErr := os.Stat(fanoutPath(request.Root, id)); statErr == nil {
			return cancelFanout(request.Root, id)
		}
	}
	record, err := resolveRunRecord(request.Root, id)
	if err != nil {
		return err
	}
	if terminalDispatchState(record.State) {
		return exitError(ExitUnavailable, fmt.Sprintf("dispatch run %s is already terminal (%s)", record.ID, record.State))
	}
	var ownedGroup *ownedProviderProcessGroup
	if record.State == dispatchStateRunning {
		group, groupErr := verifiedProviderProcessGroup(record)
		if groupErr != nil {
			return wrapExitError(ExitUnavailable, fmt.Sprintf("dispatch run %s has no live owned process to cancel", record.ID), groupErr)
		}
		ownedGroup = &group
	} else if record.State != dispatchStatePending && record.State != dispatchStateStarting {
		return exitError(ExitUnavailable, fmt.Sprintf("dispatch run %s cannot be cancelled from state %s", record.ID, record.State))
	}
	now := time.Now().UTC()
	record.State = dispatchStateCancelled
	record.RecoveryState = recoveryAcceptanceUnknown
	record.CompletedAt = &now
	record.TerminalReason = "cancelled by caller"
	if err := writeRunRecord(filepathForRun(request.Root, record.ID), &record); err != nil {
		return err
	}
	if ownedGroup != nil {
		if err := ownedGroup.terminateReverified(providerTerminationGrace); err != nil {
			if processOwnership(record) != ownershipDead || !providerProcessGroupDead(record.ProcessGroupID) {
				return wrapExitError(ExitTargetFailure, "cancel dispatch process group", err)
			}
		}
		if err := releaseConversation(request.Root, record.Name, record.ID); err != nil {
			return err
		}
	}
	// The owning execution releases the claim after its provider wait path has
	// stopped. Cancel may release it first only after the shared terminator has
	// proven that the complete process group is gone.
	return nil
}

func cancelFanout(root string, id string) error {
	manifest, err := loadFanoutManifest(root, id)
	if err != nil {
		return err
	}
	for index := range manifest.Children {
		child := &manifest.Children[index]
		record, loadErr := loadRunRecord(root, child.RunID)
		if loadErr != nil {
			_ = writeFanoutManifest(root, manifest)
			return loadErr
		}
		if terminalDispatchState(record.State) {
			child.Status = record.State
			continue
		}
		if cancelErr := Cancel(CancelRequest{Root: root, ID: child.RunID}); cancelErr != nil {
			// Persist the children already reconciled so a partial
			// cancellation reports accurate aggregate progress.
			_ = writeFanoutManifest(root, manifest)
			return cancelErr
		}
		child.Status = dispatchStateCancelled
	}
	now := time.Now().UTC()
	manifest.State = dispatchStateCancelled
	manifest.CompletedAt = &now
	return writeFanoutManifest(root, manifest)
}

func resolveRunRecord(root string, id string) (RunRecord, error) {
	if parseUUID(id) == nil {
		return loadRunRecord(root, id)
	}
	session, err := loadSession(root, id)
	if err != nil {
		return RunRecord{}, err
	}
	if session.RunID == "" {
		return RunRecord{}, exitError(ExitConfig, fmt.Sprintf("dispatch session %q has no run record", id))
	}
	return loadRunRecord(root, session.RunID)
}

// List writes all current name-keyed reservations and durable conversations.
func List(request ListRequest) error {
	stdout := writerOrDiscard(request.Stdout)
	if err := pruneExpiredSessions(request.Root, time.Now()); err != nil {
		return err
	}
	sessions, err := listSessions(request.Root)
	if err != nil {
		return err
	}
	if request.JSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(sessions)
	}
	if len(sessions) == 0 {
		_, err := fmt.Fprintln(stdout, "No dispatch sessions.")
		return err
	}
	for _, session := range sessions {
		if _, err := fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", session.Name, session.Agent, session.State, session.LastUsedAt.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}

// Delete releases an inactive Agent Layer mapping. Provider conversations and
// transcripts are intentionally untouched.
func Delete(root string, name string) error {
	name = strings.TrimSpace(name)
	session, err := loadSession(root, name)
	if err != nil {
		return err
	}
	if ownerRunID := sessionOwnerRunID(session); ownerRunID != "" {
		record, recordErr := loadRunRecord(root, ownerRunID)
		if recordErr != nil && !errors.Is(recordErr, errDispatchRunNotFound) {
			return recordErr
		}
		if recordErr == nil && activeClaimBlocksReplacement(record) {
			return exitError(ExitUnavailable, fmt.Sprintf("dispatch session %q is active; inspect it or wait for terminal completion before deleting the mapping", name))
		}
	}
	return deleteSession(root, name)
}
