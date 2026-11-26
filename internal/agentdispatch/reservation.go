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

// Reserve durably creates a named invocation that launches nothing. Without
// ContinueHandle, it allocates a new conversation for `start --reservation`.
// With ContinueHandle, it reserves the next continuation invocation of that
// existing conversation for `continue --reservation`, returning any unexpired
// pending continuation reservation idempotently.
func Reserve(opts ReserveOptions) error {
	if opts.ContinueHandle != nil {
		return reserveContinuation(opts)
	}
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
	return writeJSONResult(writerOrDiscard(opts.Stdout), result)
}

func reserveContinuation(opts ReserveOptions) error {
	handle := strings.TrimSpace(*opts.ContinueHandle)
	if handle == "" {
		return exitError(ExitUsage, "dispatch reserve --continue requires a conversation handle")
	}
	project, err := sync.LoadLockedSources(sync.RealSystem{}, opts.Root)
	if err != nil {
		return wrapExitError(ExitConfig, err.Error(), err)
	}
	// Verify the session exists before opening its lock file so invalid or
	// unknown handles never create stray lock files.
	if _, err := loadSession(opts.Root, handle); err != nil {
		return err
	}
	// Like ordinary continue, allow the caller to use the named conversation
	// if it is still present, even if it is due for retention pruning.
	retention := config.DispatchSessionRetention(project.Config)
	if err := pruneDispatchEvidenceExcept(opts.Root, time.Now(), retention, handle); err != nil {
		return err
	}
	expiry := config.DispatchReservationExpiry(project.Config)
	var reserved RunRecord
	err = withSessionLock(opts.Root, handle, func() error {
		now := time.Now().UTC()
		session, err := loadSession(opts.Root, handle)
		if err != nil {
			return err
		}
		pending, hasPending, err := settlePendingContinuationReservationLocked(opts.Root, &session, now)
		if err != nil {
			return err
		}
		if hasPending {
			// Return an existing unexpired pending continuation reservation
			// before checking eligibility so a retry after a lost response
			// always recovers the ID rather than stranding the conversation.
			// Launch re-checks eligibility before claiming.
			reserved = pending
			return nil
		}
		current, err := reconcileCurrentSessionRunLocked(opts.Root, &session, now)
		if err != nil {
			return err
		}
		if _, err := checkContinuationEligible(opts.Root, session, current); err != nil {
			return err
		}
		run, err := newReservedContinuationRun(opts.Root, session, current.ID, continuationMode(session), now.Add(expiry))
		if err != nil {
			return err
		}
		path, err := sessionPath(opts.Root, session.Name)
		if err != nil {
			return abandonUnpublishedDispatchRun(run.Dir, err)
		}
		session.ContinuationReservationID = run.Record.ID
		session.LastUsedAt = now
		if err := writeJSONAtomic(path, session); err != nil {
			return abandonUnpublishedDispatchRun(run.Dir, wrapExitError(ExitConfig, "record continuation dispatch reservation", err))
		}
		reserved = run.Record
		return nil
	})
	if err != nil {
		return err
	}
	return writeJSONResult(writerOrDiscard(opts.Stdout), publicResult(reserved))
}

// continueReservation launches a continuation reservation at most once. Every
// later continue of that reservation fails without launching.
func continueReservation(opts ContinueOptions) error {
	stdout := writerOrDiscard(opts.Stdout)
	record, err := resolveContinuationReservation(opts.Root, *opts.Reservation, opts.Handle)
	if err != nil {
		return err
	}
	// Settle expiry and reject retired/started reservations before reading the
	// prompt or preparing the target so replays with a deleted --prompt-file or
	// missing provider still report the exact reservation exit code.
	record, err = retireExpiredReservation(opts.Root, record.ID, time.Now().UTC())
	if err != nil {
		return err
	}
	if record.State != dispatchStateReserved {
		return reportClaimedContinuationReservation(record)
	}
	promptText, err := resolvePromptSource(opts.Context, opts.Prompt, opts.PromptFile)
	if err != nil {
		return reportReservationChangeOrErr(opts.Root, record.ID, err)
	}
	stderr, env, depth, err := dispatchRuntimeInputs(opts.Stderr, opts.Env)
	if err != nil {
		return reportReservationChangeOrErr(opts.Root, record.ID, err)
	}
	session, err := loadSession(opts.Root, record.Name)
	if err != nil {
		return err
	}
	if session.ContinuationReservationID != record.ID {
		// Another process may have just claimed or retired this reservation.
		// Otherwise this reserved record is an unpublished orphan whose ID was
		// never bound to the conversation.
		return reportReservationChangeOrErr(opts.Root, record.ID, exitError(ExitUnavailable, fmt.Sprintf("dispatch continuation reservation %s is not active for conversation %q", record.ID, session.Name)))
	}
	current, err := currentSessionRun(opts.Root, session)
	if err != nil {
		return err
	}
	target, err := checkContinuationEligible(opts.Root, session, current)
	if err != nil {
		return reportReservationChangeOrErr(opts.Root, record.ID, err)
	}
	if opts.beforeProvider != nil {
		release, err := opts.beforeProvider(opts.Context, target.Name)
		if err != nil {
			return reportReservationChangeOrErr(opts.Root, record.ID, err)
		}
		defer release()
	}
	project, target, version, prompt, err := prepareStart(runOptions{
		Root: opts.Root, WorkDir: opts.WorkDir, Prompt: promptText,
		LookPath: opts.LookPath, VersionLookup: opts.VersionLookup,
	}, target, stderr, depth)
	if err != nil {
		return reportReservationChangeOrErr(opts.Root, record.ID, err)
	}
	if err := pruneDispatchEvidence(opts.Root, time.Now(), config.DispatchSessionRetention(project.Config)); err != nil {
		return err
	}
	claudeLineage, err := providerLineageSupported(target.Name, version)
	if err != nil {
		return err
	}
	parent, _ := clients.GetEnv(env, "AL_RUN_ID")
	record, session, claimed, err := claimContinuationReservation(opts.Root, record.Name, record.ID, func(current *RunRecord, s Session, prev RunRecord) {
		current.Agent = target.Name
		current.ProviderVersion = version
		current.Mode = continuationMode(s)
		current.PreviousRunID = prev.ID
		current.ParentRunID = parent
		current.ProviderSessionID = s.ProviderSessionID
		if claudeLineage {
			current.LineagePath = filepath.Join(filepathForRun(opts.Root, current.ID), "provider.lineage")
		}
	})
	if err != nil {
		return err
	}
	if !claimed {
		return reportClaimedContinuationReservation(record)
	}
	run := &dispatchRun{Record: record, Dir: filepathForRun(opts.Root, record.ID)}
	request := continuationWorkerRequest(opts, record.ID, session, prompt, depth)
	return publishInvocation(opts.Context, opts.Root, run, session, request, stdout, opts.launchWorker)
}

func reportReservationChangeOrErr(root string, id string, fallback error) error {
	reloaded, loadErr := loadRunRecord(root, id)
	if loadErr == nil && reloaded.State != dispatchStateReserved {
		return reportClaimedContinuationReservation(reloaded)
	}
	return fallback
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
	project, target, version, prompt, err := prepareStart(opts.runOptions(promptText), requested, stderr, depth)
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
	return publishInvocation(opts.Context, opts.Root, run, session, request, stdout, opts.launchWorker)
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
		if record.ReservationExpiresAt != nil && record.PreviousRunID == "" {
			return record, nil
		}
		id = record.PreviousRunID
	}
	return RunRecord{}, notFound
}

// resolveContinuationReservation resolves a continuation reservation by its
// durable invocation ID. Plain runs, start reservations, unknown IDs, and
// mismatched conversation handles all report ExitReservationNotFound.
func resolveContinuationReservation(root string, selector string, handle string) (RunRecord, error) {
	selector = strings.TrimSpace(selector)
	notFound := exitError(ExitReservationNotFound, fmt.Sprintf("dispatch continuation reservation %q was not found; check the invocation ID or reserve again; nothing was launched", selector))
	if parseUUID(selector) != nil {
		return RunRecord{}, notFound
	}
	record, err := loadRunRecord(root, selector)
	if errors.Is(err, errDispatchRunNotFound) {
		return RunRecord{}, notFound
	}
	if err != nil {
		return RunRecord{}, err
	}
	if record.ReservationExpiresAt == nil || record.PreviousRunID == "" {
		return RunRecord{}, notFound
	}
	if trimmedHandle := strings.TrimSpace(handle); trimmedHandle != "" && record.Name != trimmedHandle {
		return RunRecord{}, notFound
	}
	return record, nil
}

// reportClaimedReservation explains why a start reservation is no longer startable.
func reportClaimedReservation(record RunRecord) error {
	return reportUnstartableReservation(record, record.Name, fmt.Sprintf("dispatch reservation %q already started; inspect it with `al dispatch inspect %s` and continue only when available; nothing new was launched", record.Name, record.Name))
}

// reportClaimedContinuationReservation explains why a continuation reservation
// is no longer startable.
func reportClaimedContinuationReservation(record RunRecord) error {
	return reportUnstartableReservation(record, record.ID, fmt.Sprintf("dispatch reservation %q already started; inspect it with `al dispatch inspect %s`; nothing new was launched", record.ID, record.ID))
}

func reportUnstartableReservation(record RunRecord, label string, alreadyStartedMsg string) error {
	switch {
	case record.LaunchDigest != "":
		return exitError(ExitReservationAlreadyStarted, alreadyStartedMsg)
	case record.State == dispatchStateCancelled && record.TerminalReason == terminalReasonReservationExpired:
		return exitError(ExitReservationExpired, fmt.Sprintf("dispatch reservation %q expired before it started; it never launched, so reserve again", label))
	case record.State == dispatchStateCancelled:
		return exitError(ExitReservationCancelled, fmt.Sprintf("dispatch reservation %q was cancelled before it started; it never launched", label))
	default:
		return exitError(ExitConfig, fmt.Sprintf("dispatch reservation %q has inconsistent state %q", label, record.State))
	}
}

func pendingContinuationReservationError(handle string, reservationID string) error {
	return exitError(ExitUnavailable, fmt.Sprintf("dispatch conversation %q has a pending continuation reservation %s; launch it with `al dispatch continue --reservation %s` or cancel it with `al dispatch cancel %s`", handle, reservationID, reservationID, reservationID))
}

// checkContinuationEligible enforces the continuation eligibility rules shared
// by ordinary continue, continuation reservation, and reserved continuation launch.
func checkContinuationEligible(root string, session Session, current RunRecord) (targetMeta, error) {
	if session.State == sessionStateReserved {
		if current.LaunchDigest != "" {
			return targetMeta{}, exitError(ExitUnavailable, fmt.Sprintf("dispatch conversation %q cannot be continued: its reserved start of invocation %s was interrupted before the conversation was recorded; inspect that invocation and start a new conversation", session.Name, current.ID))
		}
		return targetMeta{}, exitError(ExitUnavailable, fmt.Sprintf("dispatch conversation %q is a reservation that never started; launch it with `al dispatch start --reservation`", session.Name))
	}
	if !terminalDispatchState(current.State) {
		return targetMeta{}, exitError(ExitUnavailable, fmt.Sprintf("dispatch conversation %q is running", session.Name))
	}
	if ownerRunID := sessionOwnerRunID(session); ownerRunID != "" {
		active := current
		if ownerRunID != current.ID {
			loaded, loadErr := loadRunRecord(root, ownerRunID)
			if loadErr != nil && !errors.Is(loadErr, errDispatchRunNotFound) {
				return targetMeta{}, loadErr
			}
			if loadErr == nil {
				active = loaded
			} else {
				active = RunRecord{}
			}
		}
		if active.ID != "" && activeClaimBlocksReplacement(active) {
			return targetMeta{}, exitError(ExitUnavailable, fmt.Sprintf("dispatch conversation %q is already active in run %s", session.Name, ownerRunID))
		}
	}
	if session.ProviderSessionID == "" && current.RecoveryState != recoveryRetrySafe {
		return targetMeta{}, exitError(ExitUnavailable, fmt.Sprintf("cannot continue dispatch conversation %q: provider acceptance is unknown and no provider session ID was recorded; inspect the previous run before explicitly starting a new conversation", session.Name))
	}
	if current.State == dispatchStateCompleted {
		if _, err := completedResultPath(root, current); err != nil {
			return targetMeta{}, err
		}
	}
	target, ok := lookupTarget(session.Agent)
	if !ok {
		return targetMeta{}, exitError(ExitConfig, fmt.Sprintf("dispatch conversation %q has unsupported provider %q", session.Name, session.Agent))
	}
	return target, nil
}

// A missing or malformed reservation ID cannot refer to a retained run.
// Preserve other read errors so corrupt evidence is never silently discarded.
func loadContinuationReservation(root string, id string) (RunRecord, error) {
	validID := parseUUID(id) == nil
	if !validID {
		return RunRecord{}, nil
	}
	record, err := loadRunRecord(root, id)
	if errors.Is(err, errDispatchRunNotFound) {
		return RunRecord{}, nil
	}
	return record, err
}

func clearContinuationReservationLocked(root string, session *Session, lastUsedAt time.Time) error {
	path, err := sessionPath(root, session.Name)
	if err != nil {
		return err
	}
	session.ContinuationReservationID = ""
	if lastUsedAt.After(session.LastUsedAt) {
		session.LastUsedAt = lastUsedAt
	}
	if err := writeJSONAtomic(path, *session); err != nil {
		return wrapExitError(ExitConfig, "clear dispatch continuation reservation", err)
	}
	return nil
}

// settlePendingContinuationReservationLocked reconciles any recorded pending
// continuation reservation while the caller holds the session lock. It returns
// (record, true, nil) when an unexpired reservation is still pending, clears
// expired/cancelled/reconciled reservations from session and returns (zero, false, nil),
// or returns an error when a claimed launch is still active or unproven.
func settlePendingContinuationReservationLocked(root string, session *Session, now time.Time) (RunRecord, bool, error) {
	resID := session.ContinuationReservationID
	if resID == "" {
		return RunRecord{}, false, nil
	}
	record, err := loadContinuationReservation(root, resID)
	if err != nil {
		return RunRecord{}, false, err
	}
	if record.ID == "" {
		return RunRecord{}, false, clearContinuationReservationLocked(root, session, now)
	}
	dir := filepathForRun(root, resID)
	var updated RunRecord
	err = withRunLock(dir, func() error {
		record, loadErr := loadContinuationReservation(root, resID)
		if loadErr != nil || record.ID == "" {
			return loadErr
		}
		if record.State == dispatchStateReserved || !record.TerminationConfirmed {
			var applyErr error
			record, applyErr = applyRunEvidenceLocked(dir, func(current *RunRecord) error {
				reconcileOrphanLocked(current, now)
				return nil
			})
			if applyErr != nil {
				return applyErr
			}
		}
		updated = record
		return nil
	})
	if err != nil {
		return RunRecord{}, false, err
	}
	if updated.ID == "" {
		return RunRecord{}, false, clearContinuationReservationLocked(root, session, now)
	}
	if updated.State == dispatchStateReserved {
		return updated, true, nil
	}
	if updated.State == dispatchStateFailed {
		if removeErr := removeWorkerRequest(dir); removeErr != nil {
			return RunRecord{}, false, removeErr
		}
	}
	if updated.TerminationConfirmed {
		return RunRecord{}, false, clearContinuationReservationLocked(root, session, reservationRetirementTime(updated))
	}
	return updated, false, exitError(ExitUnavailable, fmt.Sprintf("dispatch conversation %q is already active in run %s", session.Name, resID))
}

// reconcileCurrentSessionRunLocked reconciles the current invocation under
// the already-held session lock, clearing its active claim and any unused
// provisional provider session ID when termination is durably confirmed.
func reconcileCurrentSessionRunLocked(root string, session *Session, now time.Time) (RunRecord, error) {
	current, err := currentSessionRun(root, *session)
	if err != nil {
		return RunRecord{}, err
	}
	if session.State == sessionStateReserved {
		return current, nil
	}
	dir := filepathForRun(root, current.ID)
	if !current.TerminationConfirmed {
		current, err = updateRunEvidence(dir, func(r *RunRecord) error {
			if !terminalDispatchState(r.State) {
				reconcileOrphanLocked(r, now)
			} else if confirmed, _, _ := observeTermination(*r); confirmed && !r.TerminationConfirmed {
				// Use existing evidence for a terminal run. A continuation
				// must not establish a launch fence to allow replacement.
				applyTerminationConfirmation(r, now)
			}
			return nil
		})
		if err != nil {
			return RunRecord{}, err
		}
	}
	if current.State == dispatchStateFailed {
		if removeErr := removeWorkerRequest(dir); removeErr != nil {
			return RunRecord{}, removeErr
		}
	}
	if current.TerminationConfirmed {
		changed := false
		if current.State == dispatchStateFailed && current.Mode == dispatchModeFresh && current.RecoveryState == recoveryRetrySafe {
			changed = clearProvisionalSessionID(session, current.ID)
		}
		if session.ActiveRunID == current.ID {
			session.ActiveRunID = ""
			session.ActiveClaimKnown = true
			changed = true
		}
		if changed {
			path, err := sessionPath(root, session.Name)
			if err != nil {
				return RunRecord{}, err
			}
			if err := writeJSONAtomic(path, *session); err != nil {
				return RunRecord{}, wrapExitError(ExitConfig, "release reconciled dispatch mapping", err)
			}
		}
	}
	return current, nil
}

// claimContinuationReservation is the single linearization point for launching
// a continuation reservation. It holds the session lock then run lock (matching
// the global lock order), re-checks expiry and eligibility, transitions the
// reservation from reserved to pending, and switches the conversation's active
// claim to it.
func claimContinuationReservation(root string, name string, id string, launch func(*RunRecord, Session, RunRecord)) (RunRecord, Session, bool, error) {
	var record RunRecord
	var claimedSession Session
	claimed := false
	dir := filepathForRun(root, id)
	err := withSessionLock(root, name, func() error {
		return withRunLock(dir, func() error {
			current, err := loadRunRecord(root, id)
			if err != nil {
				return err
			}
			if current.State != dispatchStateReserved {
				record = current
				if current.TerminationConfirmed {
					return releaseConversationLocked(root, current)
				}
				return nil
			}
			var session Session
			updated, err := applyRunEvidenceLocked(dir, func(r *RunRecord) error {
				expireReservation(r, time.Now().UTC())
				if r.State != dispatchStateReserved {
					return nil
				}
				mapping, err := loadSession(root, name)
				if err != nil {
					return err
				}
				session = mapping
				if session.ContinuationReservationID != id {
					return exitError(ExitUnavailable, fmt.Sprintf("dispatch continuation reservation %s no longer owns conversation %q", id, name))
				}
				prevRun, err := currentSessionRun(root, session)
				if err != nil {
					return err
				}
				if _, err := checkContinuationEligible(root, session, prevRun); err != nil {
					return err
				}
				next := *r
				launch(&next, session, prevRun)
				next.LauncherPID = os.Getpid()
				next.LauncherStartIdentity = processStartIdentity(next.LauncherPID)
				now := time.Now().UTC()
				expireReservation(r, now)
				if r.State == dispatchStateReserved {
					next.LaunchDigest = reservationLaunchClaim
					next.State = dispatchStatePending
					next.RecoveryState = recoveryRetrySafe
					next.StartedAt = now
					*r = next
				}
				return nil
			})
			if err != nil {
				return err
			}
			record = updated
			if record.State != dispatchStatePending {
				return releaseConversationLocked(root, record)
			}
			claimed = true
			session.ContinuationReservationID = ""
			session.ActiveRunID = id
			session.ActiveClaimKnown = true
			session.RunID = id
			session.LastUsedAt = record.StartedAt
			path, err := sessionPath(root, name)
			if err != nil {
				return err
			}
			if err := writeJSONAtomic(path, session); err != nil {
				return wrapExitError(ExitConfig, "claim reserved continuation mapping", err)
			}
			claimedSession = session
			return nil
		})
	})
	if claimed && err != nil {
		// The run claim was persisted but the session mapping update failed.
		// A worker was never launched, even if this run inherited a provider ID.
		run := &dispatchRun{Record: record, Dir: dir}
		err = finishDispatchFailure(dispatchExecution{Root: root, Run: run, Session: Session{Name: name}}, &preStartFailure{err: err})
	}
	return record, claimedSession, claimed, err
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
