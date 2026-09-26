package agentdispatch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/sync"
)

const terminalReasonReservationExpired = "reservation expired before it started"
const reservationLaunchClaim = "claimed"

// Reserve durably creates a named invocation that launches nothing. A caller
// that checkpoints the returned name can repeat `start --reservation` while
// the reservation is retained without launching a second agent.
func Reserve(opts ReserveOptions) error {
	project, err := sync.LoadLockedSources(sync.RealSystem{}, opts.Root)
	if err != nil {
		return wrapExitError(ExitConfig, err.Error(), err)
	}
	now := time.Now()
	retention := config.DispatchSessionRetention(project.Config)
	if err := pruneDispatchEvidence(opts.Root, now, retention); err != nil {
		return err
	}
	run, err := newReservedDispatchRun(opts.Root, now.Add(config.DispatchReservationExpiry(project.Config)))
	if err != nil {
		return err
	}
	session, err := reserveSession(opts.Root, run, retention)
	if err != nil {
		return abandonUnpublishedDispatchRun(run.Dir, err)
	}
	result := publicResult(run.Record)
	result.Handle = session.Name
	return writePublicResult(writerOrDiscard(opts.Stdout), result)
}

// startReservation launches a reservation at most once. Every later start
// fails without launching.
func startReservation(opts StartOptions, requested targetMeta, promptText string, stderr io.Writer, env []string, depth int) error {
	stdout := writerOrDiscard(opts.Stdout)
	record, err := resolveReservation(opts.Root, *opts.Reservation)
	if err != nil {
		return err
	}
	// Settle expiry before preparing so a repeated start never depends on the
	// target still being available.
	record, err = retireExpiredReservation(opts.Root, record.ID, time.Now().UTC())
	if err != nil {
		return err
	}
	if record.State != dispatchStateReserved {
		return reportClaimedReservation(record)
	}
	project, target, version, prompt, err := prepareStart(opts, requested, promptText, stderr, depth)
	if err != nil {
		return err
	}
	if err := pruneDispatchEvidence(opts.Root, time.Now(), config.DispatchSessionRetention(project.Config)); err != nil {
		return err
	}
	claudeLineage, err := providerLineageSupported(target.Name, version)
	if err != nil {
		return err
	}
	parent, _ := clients.GetEnv(env, "AL_RUN_ID")
	record, claimed, err := claimReservation(opts.Root, record.ID, func(current *RunRecord) {
		current.Agent = target.Name
		current.ProviderVersion = version
		current.Skill = strings.TrimSpace(opts.Skill)
		current.Role = strings.TrimSpace(opts.Role)
		current.ParentRunID = parent
		if claudeLineage {
			current.LineagePath = filepath.Join(filepathForRun(opts.Root, current.ID), "provider.lineage")
		}
	})
	if err != nil {
		return err
	}
	if !claimed {
		return reportClaimedReservation(record)
	}
	run := &dispatchRun{Record: record, Dir: filepathForRun(opts.Root, record.ID)}
	session, err := claimReservedSession(opts.Root, record.Name, record.ID, target.Name, opts.Model, opts.ReasoningEffort)
	if err != nil {
		return finishDispatchFailure(dispatchExecution{Root: opts.Root, Run: run, Session: Session{Name: record.Name}}, err)
	}
	request := workerRequest{Root: opts.Root, WorkDir: opts.WorkDir, RunID: record.ID, Mode: dispatchModeFresh, Prompt: prompt, Depth: depth + 1, Model: opts.Model, Effort: opts.ReasoningEffort, Skill: opts.Skill}
	return publishInvocation(opts.Root, run, session, request, stdout, opts.launchWorker)
}

// resolveReservation follows a conversation's invocation history to find the
// original reserved invocation, even after the conversation was continued.
func resolveReservation(root string, selector string) (RunRecord, error) {
	selector = strings.TrimSpace(selector)
	notFound := exitError(ExitReservationNotFound, fmt.Sprintf("dispatch reservation %q was not found; check the name or reserve again; nothing was launched", selector))
	if !validDispatchName(selector) {
		return RunRecord{}, notFound
	}
	session, err := loadSession(root, selector)
	var exitErr *ExitError
	if errors.As(err, &exitErr) && exitErr.Code == ExitUsage {
		return RunRecord{}, notFound
	}
	if err != nil {
		return RunRecord{}, err
	}
	seen := make(map[string]bool)
	for id := session.RunID; id != ""; {
		if seen[id] {
			return RunRecord{}, exitError(ExitConfig, fmt.Sprintf("dispatch conversation %q has cyclic invocation history", selector))
		}
		seen[id] = true
		record, err := loadRunRecord(root, id)
		if errors.Is(err, errDispatchRunNotFound) {
			return RunRecord{}, notFound
		}
		if err != nil {
			return RunRecord{}, err
		}
		if record.Name != selector {
			return RunRecord{}, exitError(ExitConfig, fmt.Sprintf("dispatch conversation %q has an invocation with a different name", selector))
		}
		if record.ReservationExpiresAt != nil {
			return record, nil
		}
		id = record.PreviousRunID
	}
	return RunRecord{}, notFound
}

// reportClaimedReservation explains why a reservation is no longer startable.
func reportClaimedReservation(record RunRecord) error {
	switch {
	case record.LaunchDigest != "":
		return exitError(ExitReservationAlreadyStarted, fmt.Sprintf("dispatch reservation %q already started; wait for it to finish, then use `al dispatch continue %s`; nothing new was launched", record.Name, record.Name))
	case record.State == dispatchStateCancelled && record.TerminalReason == terminalReasonReservationExpired:
		return exitError(ExitReservationExpired, fmt.Sprintf("dispatch reservation %q expired before it started; it never launched, so reserve again", record.Name))
	case record.State == dispatchStateCancelled:
		return exitError(ExitReservationCancelled, fmt.Sprintf("dispatch reservation %q was cancelled before it started; it never launched", record.Name))
	default:
		return exitError(ExitConfig, fmt.Sprintf("dispatch reservation %q has inconsistent state %q", record.Name, record.State))
	}
}

// expireReservation retires an unstarted reservation whose deadline passed.
// Retirement is terminal and fenced, so no later start can launch it.
func expireReservation(record *RunRecord, now time.Time) {
	if record.State != dispatchStateReserved || record.ReservationExpiresAt == nil || now.Before(*record.ReservationExpiresAt) {
		return
	}
	record.State = dispatchStateCancelled
	record.RecoveryState = recoveryRetrySafe
	record.CompletedAt = &now
	record.TerminalReason = terminalReasonReservationExpired
	record.TerminalExitCode = ExitTargetFailure
	applyTerminationEvidence(record, nil, true, now)
}

func retireExpiredReservation(root string, id string, now time.Time) (RunRecord, error) {
	record, err := updateRunEvidence(filepathForRun(root, id), func(current *RunRecord) error {
		expireReservation(current, now)
		return nil
	})
	if err != nil {
		return record, err
	}
	return record, releaseReservation(root, record)
}

// claimReservation is the single linearization point for launching a
// reservation: under the run lock, exactly one start moves it from reserved to
// pending, recording its launch arguments and its own launcher identity for
// crash recovery.
func claimReservation(root string, id string, launch func(*RunRecord)) (RunRecord, bool, error) {
	claimed := false
	record, err := updateRunEvidence(filepathForRun(root, id), func(current *RunRecord) error {
		now := time.Now().UTC()
		expireReservation(current, now)
		if current.State != dispatchStateReserved {
			return nil
		}
		launch(current)
		current.LaunchDigest = reservationLaunchClaim
		current.State = dispatchStatePending
		current.RecoveryState = recoveryRetrySafe
		current.StartedAt = now
		current.LauncherPID = os.Getpid()
		current.LauncherStartIdentity = processStartIdentity(os.Getpid())
		claimed = true
		return nil
	})
	if err != nil || claimed {
		return record, claimed, err
	}
	return record, false, releaseReservation(root, record)
}

// claimReservedSession keeps the run locked through the mapping update so a
// cancelled or fenced reservation cannot reopen its conversation. Acquire the
// session lock first, matching other session operations that reconcile runs.
func claimReservedSession(root string, name string, runID string, agent string, model string, effort string) (Session, error) {
	var claimed Session
	err := withSessionLock(root, name, func() error {
		return withRunLock(filepathForRun(root, runID), func() error {
			record, err := loadRunRecord(root, runID)
			if err != nil {
				return err
			}
			if record.State != dispatchStatePending || record.LaunchFenced || record.TerminationConfirmed {
				return startFencedProviderError(record, exitError(ExitUnavailable, fmt.Sprintf("dispatch reservation %s is no longer pending", runID)))
			}
			session, err := loadSession(root, name)
			if err != nil {
				return err
			}
			if session.RunID != runID || session.ActiveRunID != runID || session.State != sessionStateReserved {
				return exitError(ExitUnavailable, fmt.Sprintf("dispatch reservation %s no longer owns conversation %q", runID, name))
			}
			session.Agent = agent
			session.Model = model
			session.ReasoningEffort = effort
			session.State = sessionStatePending
			session.LastUsedAt = time.Now().UTC()
			path, err := sessionPath(root, name)
			if err != nil {
				return err
			}
			if err := writeJSONAtomic(path, session); err != nil {
				return wrapExitError(ExitConfig, "claim reserved dispatch mapping", err)
			}
			claimed = session
			return nil
		})
	})
	return claimed, err
}

// releaseReservation frees the conversation claim of a reservation retired
// without launching, so retention can later remove it.
func releaseReservation(root string, record RunRecord) error {
	if record.Name == "" || record.LaunchDigest != "" {
		return nil
	}
	return releaseIfConfirmed(root, record)
}
