package agentdispatch

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

func publicResult(record RunRecord) Result {
	state := record.State
	if state == dispatchStateInterrupted {
		state = dispatchStateFailed
	}
	return Result{
		Handle:                 record.Name,
		InvocationID:           record.ID,
		State:                  state,
		Error:                  strings.TrimSpace(record.TerminalReason),
		LastActivityAt:         record.LastActivityAt,
		LastOutputAt:           record.LastOutputAt,
		TerminationConfirmed:   record.TerminationConfirmed,
		TerminationConfirmedAt: record.TerminationConfirmedAt,
	}
}

func boolPtr(value bool) *bool { return &value }

func usesLaunchIntentProtocol(record RunRecord) bool {
	return record.LaunchProtocol == launchProtocolIntentBeforeStart
}

func submittersGone(record RunRecord) bool {
	if record.SupervisorPID != 0 && ownershipForIdentity(record.SupervisorPID, record.SupervisorStartIdentity) != ownershipDead {
		return false
	}
	if record.LauncherPID != 0 && ownershipForIdentity(record.LauncherPID, record.LauncherStartIdentity) != ownershipDead {
		return false
	}
	return true
}

func observeTermination(record RunRecord) (confirmed bool, observation string, proof string) {
	if record.TerminationConfirmed {
		return true, record.TerminationObservation, record.TerminationProof
	}
	if record.PID != 0 || record.ProcessGroupID != 0 || record.ProcessStartIdentity != "" {
		if record.ProcessStartIdentity == "" || record.PID <= 0 || record.ProcessGroupID <= 0 {
			return false, terminationObservationIncompleteID, ""
		}
		switch {
		case providerProcessGroupReused(record):
			proof = terminationProofGroupReused
		case providerProcessGroupDead(record.ProcessGroupID):
			// The provider can leave its original group. Group death alone
			// cannot establish that the owned provider process stopped.
			if processOwnership(record) != ownershipDead {
				return false, terminationObservationProviderLive, ""
			}
			proof = terminationProofGroupDead
		default:
			return false, terminationObservationGroupLive, ""
		}
		if usesLaunchIntentProtocol(record) {
			if !record.LaunchFenced {
				return false, terminationObservationUnconfirmed, ""
			}
			return true, "", proof
		}
		if !submittersGone(record) {
			return false, terminationObservationUnconfirmed, ""
		}
		return true, "", proof
	}
	if !usesLaunchIntentProtocol(record) {
		return false, terminationObservationUnconfirmed, ""
	}
	if record.ProviderLaunchIntent {
		return false, terminationObservationUncertainLaunch, ""
	}
	if !record.LaunchFenced {
		return false, terminationObservationUnconfirmed, ""
	}
	return true, "", terminationProofPrelaunchNoIntent
}

func applyLaunchFence(record *RunRecord) {
	record.LaunchFenced = true
}

func applyTerminationObservation(record *RunRecord, observation string, attemptErr error) {
	if observation != "" {
		record.TerminationObservation = observation
	}
	if attemptErr != nil {
		record.TerminationAttemptError = attemptErr.Error()
	}
}

func applyTerminationConfirmation(record *RunRecord, now time.Time) bool {
	if usesLaunchIntentProtocol(*record) {
		applyLaunchFence(record)
	}
	confirmed, observation, proof := observeTermination(*record)
	if observation != "" {
		record.TerminationObservation = observation
	}
	if !confirmed {
		return false
	}
	applyLaunchFence(record)
	record.TerminationConfirmed = true
	if record.TerminationConfirmedAt == nil {
		stamp := now.UTC()
		record.TerminationConfirmedAt = &stamp
	}
	record.TerminationProof = proof
	record.TerminationObservation = ""
	return true
}

func applyTerminationEvidence(record *RunRecord, attemptErr error, confirm bool, now time.Time) {
	if attemptErr != nil {
		record.TerminationAttemptError = attemptErr.Error()
	}
	if confirm {
		applyTerminationConfirmation(record, now)
		return
	}
	if usesLaunchIntentProtocol(*record) {
		applyLaunchFence(record)
	}
	_, observation, _ := observeTermination(*record)
	applyTerminationObservation(record, observation, attemptErr)
}

func persistTerminationEvidence(dir string, attemptErr error, confirm bool) (RunRecord, error) {
	now := time.Now().UTC()
	return updateRunEvidence(dir, func(current *RunRecord) error {
		applyTerminationEvidence(current, attemptErr, confirm, now)
		return nil
	})
}

func persistConfirmedTermination(dir string) (RunRecord, error) {
	return persistTerminationEvidence(dir, nil, true)
}

func releaseIfConfirmed(root string, record RunRecord) error {
	if !record.TerminationConfirmed {
		return nil
	}
	return releaseConversation(root, record.Name, record.ID)
}

func startFencedProviderError(record RunRecord, err error) error {
	if record.State == dispatchStateCancelled {
		return exitError(ExitTargetFailure, fmt.Sprintf("dispatch run %s was cancelled before provider launch", record.ID))
	}
	if record.LaunchFenced {
		return exitError(ExitUnavailable, fmt.Sprintf("dispatch run %s launch is fenced", record.ID))
	}
	return err
}

func primaryUnprovenError(err error) error {
	var unproven *unprovenProviderTerminationError
	if errors.As(err, &unproven) {
		return unproven.Unwrap()
	}
	return err
}
