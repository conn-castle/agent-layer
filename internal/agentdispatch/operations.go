package agentdispatch

import (
	"fmt"
	"path/filepath"
	"time"
)

func reconcileOrphan(root string, record RunRecord) (RunRecord, error) {
	return tryReconcileOrphan(root, record)
}

func tryReconcileOrphan(root string, record RunRecord) (RunRecord, error) {
	dir := filepathForRun(root, record.ID)
	var next RunRecord
	acquired, err := tryWithRunLock(dir, func() error {
		updated, applyErr := applyRunEvidenceLocked(dir, func(current *RunRecord) error {
			if terminalDispatchState(current.State) {
				applyTerminationEvidence(current, nil, true, time.Now().UTC())
				return nil
			}
			if invocationOwnership(*current) != ownershipDead {
				return nil
			}
			if current.ProviderLaunchIntent && current.PID == 0 && current.ProcessStartIdentity == "" {
				applyTerminationEvidence(current, nil, false, time.Now().UTC())
				current.TerminationObservation = terminationObservationUncertainLaunch
				return nil
			}
			if current.PID != 0 || current.ProcessGroupID != 0 {
				if !providerProcessGroupDead(current.ProcessGroupID) && !providerProcessGroupReused(*current) {
					liveGroupErr := exitError(ExitUnavailable, fmt.Sprintf("dispatch run %s lost its worker and provider leader but process group %d remains; cannot prove termination, so the active claim is retained; inspect surviving processes and verify their ownership before manual recovery (do not signal a group by ID alone)", current.ID, current.ProcessGroupID))
					applyTerminationEvidence(current, liveGroupErr, false, time.Now().UTC())
					current.TerminationObservation = terminationObservationGroupLive
					return nil
				}
			} else if !usesLaunchIntentProtocol(*current) {
				applyTerminationEvidence(current, nil, false, time.Now().UTC())
				return nil
			}
			now := time.Now().UTC()
			current.State = dispatchStateFailed
			current.RecoveryState = recoveryAcceptanceUnknown
			current.CompletedAt = &now
			current.TerminalReason = "dispatch worker stopped before publishing a terminal result"
			if current.SupervisorPID == 0 && current.PID == 0 && !current.ProviderLaunchIntent {
				current.TerminalReason = "dispatch was interrupted before launching its worker"
			}
			current.TerminalExitCode = ExitTargetFailure
			applyTerminationEvidence(current, nil, true, now)
			return nil
		})
		if applyErr != nil {
			return applyErr
		}
		next = updated
		return nil
	})
	if err != nil {
		return record, err
	}
	if !acquired {
		current, loadErr := loadRunRecord(root, record.ID)
		if loadErr != nil {
			return record, loadErr
		}
		return current, nil
	}
	if next.State == dispatchStateFailed {
		if removeErr := removeWorkerRequest(dir); removeErr != nil {
			return next, removeErr
		}
	}
	if next.TerminationConfirmed {
		if releaseErr := tryReleaseIfConfirmed(root, next); releaseErr != nil {
			return next, releaseErr
		}
	}
	return next, nil
}

func tryReleaseIfConfirmed(root string, record RunRecord) error {
	if !record.TerminationConfirmed || record.Name == "" {
		return nil
	}
	_, err := tryWithSessionLock(root, record.Name, func() error {
		return releaseConversationLocked(root, record.Name, record.ID)
	})
	return err
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
	record, err := resolveInvocationSelector(request.Root, request.ID, request.Handle, request.InvocationID)
	if err != nil {
		return err
	}
	record, ownedGroup, alreadyConfirmedCancelled, err := beginCancellation(request.Root, record.ID)
	if err != nil {
		return err
	}
	if alreadyConfirmedCancelled {
		result := publicResult(record)
		result.Error = ""
		return writePublicResult(writerOrDiscard(request.Stdout), result)
	}
	var terminateErr error
	if ownedGroup != nil {
		terminateErr = ownedGroup.terminateReverified(providerTerminationGrace)
	}
	confirm := terminateErr == nil
	updated, persistErr := persistTerminationEvidence(filepathForRun(request.Root, record.ID), terminateErr, confirm)
	if persistErr != nil {
		return persistErr
	}
	record = updated
	if terminateErr != nil {
		if processOwnership(record) != ownershipDead || !providerProcessGroupDead(record.ProcessGroupID) {
			_ = writePublicResult(writerOrDiscard(request.Stdout), publicResult(record))
			return wrapExitError(ExitTargetFailure, "cancel dispatch process group", terminateErr)
		}
		record, persistErr = persistTerminationEvidence(filepathForRun(request.Root, record.ID), terminateErr, true)
		if persistErr != nil {
			return persistErr
		}
	}
	if err := releaseIfConfirmed(request.Root, record); err != nil {
		return err
	}
	result := publicResult(record)
	if record.State == dispatchStateCancelled {
		result.Error = ""
	}
	return writePublicResult(writerOrDiscard(request.Stdout), result)
}

// beginCancellation publishes cancellation while holding the run lock. The
// worker can update process identity at the same time as a caller cancels, so
// a separate read followed by writeRunRecord would expose that routine race as
// an unavailable dispatch command instead of a cancellation result.
func beginCancellation(root string, id string) (RunRecord, *ownedProviderProcessGroup, bool, error) {
	var record RunRecord
	var ownedGroup *ownedProviderProcessGroup
	alreadyConfirmedCancelled := false
	dir := filepathForRun(root, id)
	err := withRunLock(dir, func() error {
		current, err := loadRunRecord(root, id)
		if err != nil {
			return err
		}
		record = current
		if terminalDispatchState(current.State) {
			if current.TerminationConfirmed {
				if current.State == dispatchStateCancelled {
					alreadyConfirmedCancelled = true
					return nil
				}
				state := current.State
				if state == dispatchStateInterrupted {
					state = dispatchStateFailed
				}
				return exitError(ExitUnavailable, fmt.Sprintf("dispatch conversation %q is already %s", current.Name, state))
			}
			switch current.State {
			case dispatchStateCancelled, dispatchStateFailed, dispatchStateInterrupted:
				candidate := current
				if applyTerminationConfirmation(&candidate, time.Now().UTC()) {
					updated, updateErr := applyRunEvidenceLocked(dir, func(record *RunRecord) error {
						*record = candidate
						return nil
					})
					record = updated
					return updateErr
				}
				group, groupErr := cancellationProcessGroup(current)
				if groupErr != nil {
					current.TerminationAttemptError = groupErr.Error()
					current.Revision++
					current.UpdatedAt = time.Now().UTC()
					if err := validateRunRecord(current); err != nil {
						return err
					}
					if err := writeJSONAtomic(filepath.Join(dir, dispatchRunFile), current); err != nil {
						return wrapExitError(ExitConfig, "write dispatch run evidence", err)
					}
					record = current
					return wrapExitError(ExitUnavailable, fmt.Sprintf("dispatch run %s has no live owned process to cancel", current.ID), groupErr)
				}
				ownedGroup = group
				return nil
			default:
				return exitError(ExitUnavailable, fmt.Sprintf("dispatch conversation %q is already %s", current.Name, current.State))
			}
		}
		switch {
		case current.State == dispatchStateRunning && current.PID != 0:
			group, groupErr := cancellationProcessGroup(current)
			if groupErr != nil {
				current.TerminationAttemptError = groupErr.Error()
				current.Revision++
				current.UpdatedAt = time.Now().UTC()
				if err := validateRunRecord(current); err != nil {
					return err
				}
				if err := writeJSONAtomic(filepath.Join(dir, dispatchRunFile), current); err != nil {
					return wrapExitError(ExitConfig, "write dispatch run evidence", err)
				}
				record = current
				return wrapExitError(ExitUnavailable, fmt.Sprintf("dispatch run %s has no live owned process to cancel", current.ID), groupErr)
			}
			ownedGroup = group
		case current.State == dispatchStateRunning && current.SupervisorPID != 0:
			// The worker is launched but no provider process exists yet; the
			// terminal record plus launch fence stop a later Start.
		case current.State == dispatchStatePending, current.State == dispatchStateStarting:
		default:
			return exitError(ExitUnavailable, fmt.Sprintf("dispatch run %s cannot be cancelled from state %s", current.ID, current.State))
		}
		now := time.Now().UTC()
		current.State = dispatchStateCancelled
		current.RecoveryState = recoveryAcceptanceUnknown
		current.CompletedAt = &now
		current.TerminalReason = terminalReasonCancelledByCaller
		current.TerminalExitCode = ExitTargetFailure
		if usesLaunchIntentProtocol(current) {
			current.LaunchFenced = true
		}
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
	return record, ownedGroup, alreadyConfirmedCancelled, err
}

func cancellationProcessGroup(record RunRecord) (*ownedProviderProcessGroup, error) {
	group, err := verifiedProviderProcessGroup(record)
	if err != nil {
		return nil, err
	}
	return &group, nil
}
