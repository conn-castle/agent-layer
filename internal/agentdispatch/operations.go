package agentdispatch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/conn-castle/agent-layer/internal/projectpaths"
)

func tryReconcileOrphan(root string, record RunRecord) (RunRecord, error) {
	dir := filepathForRun(root, record.ID)
	var next RunRecord
	acquired, err := tryWithRunLock(dir, func() error {
		updated, applyErr := applyRunEvidenceLocked(dir, func(current *RunRecord) error {
			reconcileOrphanLocked(dir, current, time.Now().UTC())
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
		if next.Mode == dispatchModeFresh && next.RecoveryState == recoveryRetrySafe && next.TerminationConfirmed {
			if downgradeErr := downgradeUnstartedSession(root, next.Name, next.ID); downgradeErr != nil {
				return next, downgradeErr
			}
		}
	}
	if next.TerminationConfirmed {
		if removeErr := removeSessionMCPArtifacts(dir); removeErr != nil {
			return next, removeErr
		}
		if removeErr := removeSessionMCPRuntimeDir(dir); removeErr != nil {
			return next, removeErr
		}
		if releaseErr := tryReleaseIfConfirmed(root, next); releaseErr != nil {
			return next, releaseErr
		}
	}
	return next, nil
}

// reconcileOrphanLocked transitions an expired reservation or provably dead
// invocation to a terminal state under its run lock.
func reconcileOrphanLocked(dir string, current *RunRecord, now time.Time) {
	if current.State == dispatchStateReserved {
		// A reservation has no worker or provider to reconcile; its
		// recorded launcher is the reserve command, which exits at once.
		expireReservation(current, now)
		return
	}
	if terminalDispatchState(current.State) {
		applyTerminationEvidenceInDir(dir, current, nil, true, now)
		return
	}
	if invocationOwnership(*current) != ownershipDead {
		return
	}
	if current.ProviderLaunchIntent && current.PID == 0 && current.ProcessStartIdentity == "" {
		applyTerminationEvidenceInDir(dir, current, nil, false, now)
		current.TerminationObservation = terminationObservationUncertainLaunch
		return
	}
	if current.PID != 0 || current.ProcessGroupID != 0 {
		if !providerProcessGroupDead(current.ProcessGroupID) && !providerProcessGroupReused(*current) {
			liveGroupErr := exitError(ExitUnavailable, fmt.Sprintf("dispatch run %s lost its worker and provider leader but process group %d remains; cannot prove termination, so the active claim is retained; inspect surviving processes and verify their ownership before manual recovery (do not signal a group by ID alone)", current.ID, current.ProcessGroupID))
			applyTerminationEvidenceInDir(dir, current, liveGroupErr, false, now)
			current.TerminationObservation = terminationObservationGroupLive
			return
		}
	} else if !usesLaunchIntentProtocol(*current) {
		applyTerminationEvidenceInDir(dir, current, nil, false, now)
		return
	}
	if !trySessionMCPProcessesStoppedAndFenced(dir, true) {
		liveMCPErr := exitError(ExitUnavailable, fmt.Sprintf("dispatch run %s lost its worker and provider leader but one or more session MCP server processes remain live; cannot prove termination, so the active claim is retained", current.ID))
		applyTerminationEvidenceInDir(dir, current, liveMCPErr, false, now)
		current.TerminationObservation = terminationObservationGroupLive
		return
	}
	current.State = dispatchStateFailed
	current.RecoveryState = recoveryAcceptanceUnknown
	current.CompletedAt = &now
	current.TerminalReason = "dispatch worker stopped before publishing a terminal result"
	if current.SupervisorPID == 0 && current.PID == 0 && !current.ProviderLaunchIntent {
		current.TerminalReason = "dispatch was interrupted before launching its worker"
	}
	// A fresh-mode continuation whose launcher or worker died before writing
	// provider launch intent never contacted a provider, so a conversation
	// without a provider session ID remains continuable (and any provisional
	// caller-assigned session ID is cleared on reconciliation).
	if freshContinuationBeforeProviderLaunch(*current) {
		current.RecoveryState = recoveryRetrySafe
	}
	current.TerminalExitCode = ExitTargetFailure
	applyTerminationEvidenceInDir(dir, current, nil, true, now)
}

func freshContinuationBeforeProviderLaunch(record RunRecord) bool {
	return record.PreviousRunID != "" && record.Mode == dispatchModeFresh && usesLaunchIntentProtocol(record) &&
		!record.ProviderLaunchIntent && record.PID == 0 && record.ProcessGroupID == 0 && record.ProcessStartIdentity == ""
}

func cancelledUnstartedFreshContinuation(record RunRecord) bool {
	return record.State == dispatchStateCancelled && record.RecoveryState == recoveryRetrySafe &&
		record.TerminationConfirmed && freshContinuationBeforeProviderLaunch(record) &&
		(record.ReservationExpiresAt == nil || record.LaunchDigest != "")
}

func tryReleaseIfConfirmed(root string, record RunRecord) error {
	if !record.TerminationConfirmed || record.Name == "" {
		return nil
	}
	_, err := tryWithSessionLock(root, record.Name, func() error {
		return releaseConversationLocked(root, record)
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
			return ownershipForIdentity(record.PID, record.ProcessStartIdentity)
		}
		return ownershipDead
	}
	if record.PID != 0 {
		return ownershipForIdentity(record.PID, record.ProcessStartIdentity)
	}
	// No worker or provider identity was ever published: only the recorded
	// launcher's death proves the invocation was abandoned pre-publication.
	return ownershipForIdentity(record.LauncherPID, record.LauncherStartIdentity)
}

// ownershipForIdentity reports whether the recorded process is provably ours
// (owned), provably gone (dead), or unprovable either way (unknown), for
// example when start-identity capture is unavailable in this environment.
// An alive PID with a different start identity is a reused PID.
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

func filepathForRun(root string, id string) string {
	dir := filepath.Join(dispatchRunPath(root), id)
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		legacy := filepath.Join(root, projectpaths.LegacyRuns, id)
		if _, err := os.Lstat(legacy); !os.IsNotExist(err) { // #nosec G703 -- root is the selected project, and the run ID is a validated UUID.
			return legacy
		}
	}
	return dir
}

const (
	ownershipOwned   = "owned"
	ownershipDead    = "dead"
	ownershipUnknown = "unknown"
)

// Cancel terminates only the exact Agent Layer-owned process group.
func Cancel(request CancelRequest) error {
	depthState, err := checkDispatchOperationAllowed(request.Root, request.Env)
	if err != nil {
		return err
	}
	record, err := resolveInvocationSelector(request.Root, request.ID, request.Handle, request.InvocationID)
	if err != nil {
		return err
	}
	if depthState.Depth > 0 && record.Name != "" {
		if session, loadErr := loadSession(request.Root, record.Name); loadErr == nil {
			if err := checkDispatchStateAllowed(request.Root, depthState, session.MaxDepth); err != nil {
				return err
			}
		} else if !errors.Is(loadErr, errDispatchSessionNotFound) {
			return loadErr
		}
	}
	record, ownedGroup, alreadyConfirmedCancelled, err := beginCancellation(request.Root, record.ID)
	if err != nil {
		return err
	}
	if alreadyConfirmedCancelled {
		runDir := filepathForRun(request.Root, record.ID)
		_ = removeSessionMCPArtifacts(runDir)
		_ = removeSessionMCPRuntimeDir(runDir)
		if err := releaseIfConfirmed(request.Root, record); err != nil {
			return err
		}
		result := publicResult(record)
		result.Error = ""
		return writeJSONResult(writerOrDiscard(request.Stdout), result)
	}
	var terminateErr error
	if ownedGroup != nil {
		terminateErr = ownedGroup.terminateReverified(providerTerminationGrace)
	}
	runDir := filepathForRun(request.Root, record.ID)
	if sessionMCPErr := closeAndTerminateSessionMCPProcesses(runDir, providerTerminationGrace); sessionMCPErr != nil {
		terminateErr = errors.Join(terminateErr, sessionMCPErr)
	}
	confirm := terminateErr == nil
	updated, persistErr := persistTerminationEvidence(runDir, terminateErr, confirm)
	if persistErr != nil {
		return persistErr
	}
	record = updated
	if terminateErr != nil {
		if ownershipForIdentity(record.PID, record.ProcessStartIdentity) != ownershipDead || !providerProcessGroupDead(record.ProcessGroupID) || !sessionMCPProcessesStopped(runDir) {
			_ = writeJSONResult(writerOrDiscard(request.Stdout), publicResult(record))
			return wrapExitError(ExitTargetFailure, "cancel dispatch process group", terminateErr)
		}
		record, persistErr = persistTerminationEvidence(runDir, terminateErr, true)
		if persistErr != nil {
			return persistErr
		}
	}
	if record.TerminationConfirmed {
		if removeErr := removeSessionMCPArtifacts(runDir); removeErr != nil {
			return removeErr
		}
		if removeErr := removeSessionMCPRuntimeDir(runDir); removeErr != nil {
			return removeErr
		}
	}
	if err := releaseIfConfirmed(request.Root, record); err != nil {
		return err
	}
	result := publicResult(record)
	if record.State == dispatchStateCancelled {
		result.Error = ""
	}
	return writeJSONResult(writerOrDiscard(request.Stdout), result)
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
		if current.State == dispatchStateReserved {
			current, err = applyRunEvidenceLocked(dir, func(record *RunRecord) error {
				expireReservation(record, time.Now().UTC())
				return nil
			})
			if err != nil {
				return err
			}
			if current.State == dispatchStateCancelled {
				// Let Cancel perform claim cleanup for this newly expired run.
				record = current
				return nil
			}
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
				if applyTerminationConfirmationInDir(dir, &candidate, time.Now().UTC()) {
					updated, updateErr := applyRunEvidenceLocked(dir, func(record *RunRecord) error {
						*record = candidate
						return nil
					})
					record = updated
					return updateErr
				}
				// If the provider already stopped and only tracked session MCP
				// processes remain live, allow Cancel to terminate them.
				if !sessionMCPProcessesStopped(dir) &&
					(current.PID == 0 || ownershipForIdentity(current.PID, current.ProcessStartIdentity) == ownershipDead) &&
					(providerProcessGroupDead(current.ProcessGroupID) || providerProcessGroupReused(current)) {
					return nil
				}
				group, groupErr := cancellationProcessGroup(dir, current)
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
			group, groupErr := cancellationProcessGroup(dir, current)
			if groupErr != nil {
				// If the provider leader and its process group are already dead
				// and only tracked session MCP processes remain live, allow Cancel
				// to transition to cancelled and terminate those MCP groups.
				if !sessionMCPProcessesStopped(dir) &&
					ownershipForIdentity(current.PID, current.ProcessStartIdentity) == ownershipDead &&
					(providerProcessGroupDead(current.ProcessGroupID) || providerProcessGroupReused(current)) {
					break
				}
				return wrapExitError(ExitUnavailable, fmt.Sprintf("dispatch run %s has no live owned process to cancel", current.ID), groupErr)
			}
			ownedGroup = group
		case current.State == dispatchStateRunning && current.SupervisorPID != 0:
			// The worker is launched but no provider process exists yet; the
			// terminal record plus launch fence stop a later Start.
		case current.State == dispatchStateReserved, current.State == dispatchStatePending, current.State == dispatchStateStarting:
		default:
			return exitError(ExitUnavailable, fmt.Sprintf("dispatch run %s cannot be cancelled from state %s", current.ID, current.State))
		}
		now := time.Now().UTC()
		recovery := recoveryAcceptanceUnknown
		if current.State == dispatchStateReserved || freshContinuationBeforeProviderLaunch(current) {
			recovery = recoveryRetrySafe
		}
		current.State = dispatchStateCancelled
		current.RecoveryState = recovery
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

func cancellationProcessGroup(dir string, record RunRecord) (*ownedProviderProcessGroup, error) {
	// Prove the original group and provider stopped without obtaining a signal
	// capability. A live submitter may still publish a valid result after provider
	// exit. Fence only this copy until cancellation is accepted.
	candidate := record
	if submittersGone(record) && applyTerminationConfirmationInDir(dir, &candidate, time.Now().UTC()) {
		return nil, nil
	}
	group, err := verifiedProviderProcessGroup(record)
	if err != nil {
		return nil, err
	}
	return &group, nil
}
