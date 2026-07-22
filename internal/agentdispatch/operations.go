package agentdispatch

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

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
	if terminalDispatchState(record.State) {
		return record, nil
	}
	// Only a definitively dead worker/provider is reconciled. Unprovable ownership
	// (identity capture unavailable) must not terminalize a possibly live run.
	if invocationOwnership(record) != ownershipDead {
		return record, nil
	}
	now := time.Now().UTC()
	record.State = dispatchStateFailed
	record.RecoveryState = recoveryAcceptanceUnknown
	record.CompletedAt = &now
	record.TerminalReason = "dispatch worker stopped before publishing a terminal result"
	if record.SupervisorPID == 0 && record.PID == 0 {
		record.TerminalReason = "dispatch was interrupted before launching its worker"
	}
	record.TerminalExitCode = ExitTargetFailure
	if err := removeWorkerRequest(filepathForRun(root, record.ID)); err != nil {
		return RunRecord{}, err
	}
	if err := writeRunRecord(filepathForRun(root, record.ID), &record); err != nil {
		return RunRecord{}, err
	}
	if err := releaseConversation(root, record.Name, record.ID); err != nil {
		return RunRecord{}, err
	}
	return record, nil
}

// invocationOwnership proves an invocation dead only when every process that
// could still publish its result is provably gone. A reaped provider PID alone
// is normal during the window between provider exit and the worker's terminal
// record write, so a live worker always outweighs a dead provider.
func invocationOwnership(record RunRecord) string {
	if record.SupervisorPID != 0 {
		supervisor := ownershipForIdentity(record.SupervisorPID, record.SupervisorStartIdentity)
		if supervisor != ownershipDead {
			return supervisor
		}
		if record.PID != 0 {
			return processOwnership(record)
		}
		return ownershipDead
	}
	if record.PID != 0 {
		return processOwnership(record)
	}
	// No worker or provider identity was ever published: only the recorded
	// launcher's death proves the invocation was abandoned pre-publication.
	return ownershipForIdentity(record.LauncherPID, record.LauncherStartIdentity)
}

func ownershipForIdentity(pid int, startIdentity string) string {
	switch processAlive(pid) {
	case processStatusAlive:
	case processStatusDead:
		return ownershipDead
	default:
		return ownershipUnknown
	}
	if startIdentity == "" {
		return ownershipUnknown
	}
	current := processStartIdentity(pid)
	if current == "" {
		return ownershipUnknown
	}
	if current == startIdentity {
		return ownershipOwned
	}
	return ownershipDead
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

// Cancel terminates only the exact Agent Layer-owned process group.
func Cancel(request CancelRequest) error {
	id := strings.TrimSpace(request.ID)
	record, err := resolveRunRecord(request.Root, id)
	if err != nil {
		return err
	}
	record, ownedGroup, alreadyCancelled, err := beginCancellation(request.Root, record.ID)
	if err != nil {
		return err
	}
	if alreadyCancelled {
		return writePublicResult(writerOrDiscard(request.Stdout), publicResult{Handle: record.Name, State: dispatchStateCancelled})
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
	return writePublicResult(writerOrDiscard(request.Stdout), publicResult{Handle: record.Name, State: dispatchStateCancelled})
}

// beginCancellation publishes cancellation while holding the run lock. The
// worker can update process identity at the same time as a caller cancels, so
// a separate read followed by writeRunRecord would expose that routine race as
// an unavailable dispatch command instead of a cancellation result.
func beginCancellation(root string, id string) (RunRecord, *ownedProviderProcessGroup, bool, error) {
	var record RunRecord
	var ownedGroup *ownedProviderProcessGroup
	alreadyCancelled := false
	dir := filepathForRun(root, id)
	err := withRunLock(dir, func() error {
		current, err := loadRunRecord(root, id)
		if err != nil {
			return err
		}
		record = current
		if terminalDispatchState(current.State) {
			if current.State == dispatchStateCancelled {
				alreadyCancelled = true
				return nil
			}
			state := current.State
			if state == dispatchStateInterrupted {
				state = dispatchStateFailed
			}
			return exitError(ExitUnavailable, fmt.Sprintf("dispatch conversation %q is already %s", current.Name, state))
		}
		switch {
		case current.State == dispatchStateRunning && current.PID != 0:
			group, groupErr := verifiedProviderProcessGroup(current)
			if groupErr != nil {
				return wrapExitError(ExitUnavailable, fmt.Sprintf("dispatch run %s has no live owned process to cancel", current.ID), groupErr)
			}
			ownedGroup = &group
		case current.State == dispatchStateRunning && current.SupervisorPID != 0:
			// The worker is launched but no provider process exists yet; the
			// terminal record alone stops it.
		case current.State == dispatchStatePending, current.State == dispatchStateStarting:
		default:
			return exitError(ExitUnavailable, fmt.Sprintf("dispatch run %s cannot be cancelled from state %s", current.ID, current.State))
		}
		now := time.Now().UTC()
		current.State = dispatchStateCancelled
		current.RecoveryState = recoveryAcceptanceUnknown
		current.CompletedAt = &now
		current.TerminalReason = "cancelled by caller"
		current.TerminalExitCode = ExitTargetFailure
		current.Revision++
		current.UpdatedAt = now
		if err := validateRunRecord(current); err != nil {
			return err
		}
		if err := writeJSONAtomic(filepath.Join(dir, dispatchRunFile), current); err != nil {
			return wrapExitError(ExitConfig, "write dispatch run record", err)
		}
		record = current
		return nil
	})
	return record, ownedGroup, alreadyCancelled, err
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
