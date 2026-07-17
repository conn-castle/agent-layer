package agentdispatch

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	dispatchStateDir = "dispatch"
	dispatchRunFile  = "dispatch.json"
	// dispatchSessionRetention bounds durable name mappings while leaving
	// provider-owned conversations and diagnostic run evidence untouched.
	dispatchSessionRetention = 30 * 24 * time.Hour
)

const (
	dispatchStatePending     = "pending"
	dispatchStateStarting    = "starting"
	dispatchStateRunning     = "running"
	dispatchStateCompleted   = "completed"
	dispatchStateFailed      = "failed"
	dispatchStateCancelled   = "cancelled"
	dispatchStateInterrupted = "interrupted"

	recoveryRetrySafe         = "retry_safe"
	recoveryResumeRequired    = "resume_required"
	recoveryAcceptanceUnknown = "acceptance_unknown"
	recoveryNotResumable      = "not_resumable"
	sessionStateDurable       = "durable"
	sessionStatePending       = "pending"
)

var errDispatchRunNotFound = errors.New("dispatch run record not found")

// Session is the durable, name-keyed mapping owned by Agent Layer. Provider
// transcripts remain provider-owned; this record contains only the alias
// needed for explicit continuation.
type Session struct {
	Name              string    `json:"name"`
	Agent             string    `json:"agent"`
	Model             string    `json:"model,omitempty"`
	ReasoningEffort   string    `json:"reasoning_effort,omitempty"`
	TargetPinned      bool      `json:"target_pinned,omitempty"`
	ProviderSessionID string    `json:"provider_session_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	LastUsedAt        time.Time `json:"last_used_at"`
	State             string    `json:"state,omitempty"`
	RunID             string    `json:"run_id,omitempty"`
	ActiveRunID       string    `json:"active_run_id,omitempty"`
	ActiveClaimKnown  bool      `json:"active_claim_known,omitempty"`
}

// RunRecord is private, recoverable dispatch evidence. It never contains the
// caller prompt or a provider transcript.
type RunRecord struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	Agent                string     `json:"agent"`
	ProviderVersion      string     `json:"provider_version"`
	Model                string     `json:"model,omitempty"`
	ReasoningEffort      string     `json:"reasoning_effort,omitempty"`
	Skill                string     `json:"skill,omitempty"`
	Mode                 string     `json:"mode"`
	State                string     `json:"state"`
	RecoveryState        string     `json:"recovery_state"`
	Revision             uint64     `json:"revision"`
	Attempt              int        `json:"attempt"`
	PID                  int        `json:"pid,omitempty"`
	ProcessGroupID       int        `json:"process_group_id,omitempty"`
	ProcessStartIdentity string     `json:"process_start_identity,omitempty"`
	StartedAt            time.Time  `json:"started_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	ProviderSessionID    string     `json:"provider_session_id,omitempty"`
	PreviousRunID        string     `json:"previous_run_id,omitempty"`
	ParentRunID          string     `json:"parent_run_id,omitempty"`
	FanoutGroupID        string     `json:"fanout_group_id,omitempty"`
	NotResumable         bool       `json:"not_resumable,omitempty"`
	TerminalReason       string     `json:"terminal_reason,omitempty"`
	LastOutputAt         *time.Time `json:"last_output_at,omitempty"`
	LastActivityAt       *time.Time `json:"last_activity_at,omitempty"`
	LastActivityKind     string     `json:"last_activity_kind,omitempty"`
	AnswerPath           string     `json:"answer_path"`
	StdoutPath           string     `json:"stdout_path"`
	StderrPath           string     `json:"stderr_path"`
	EventsPath           string     `json:"events_path,omitempty"`
	ProviderLogPath      string     `json:"provider_log_path,omitempty"`
}

type dispatchRun struct {
	Record RunRecord
	Dir    string
}

func dispatchStatePath(root string) string {
	return filepath.Join(root, ".agent-layer", "state", dispatchStateDir)
}

func dispatchRunPath(root string) string {
	return filepath.Join(root, ".agent-layer", "tmp", "runs")
}

func sessionPath(root string, name string) (string, error) {
	if !validDispatchName(name) {
		return "", exitError(ExitUsage, fmt.Sprintf("invalid dispatch name %q", name))
	}
	return filepath.Join(dispatchStatePath(root), name+".json"), nil
}

func lockPath(root string, name string) (string, error) {
	if !validDispatchName(name) {
		return "", exitError(ExitUsage, fmt.Sprintf("invalid dispatch name %q", name))
	}
	return filepath.Join(dispatchStatePath(root), name+".lock"), nil
}

func validDispatchName(name string) bool {
	if name == "" || len(name) > 128 || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && r != '-' {
			return false
		}
	}
	return true
}

func newDispatchRun(root string, agent string, version string, mode string) (*dispatchRun, error) {
	id, err := newUUID()
	if err != nil {
		return nil, wrapExitError(ExitTargetFailure, "allocate dispatch run ID", err)
	}
	dir := filepath.Join(dispatchRunPath(root), id)
	if err := os.MkdirAll(dispatchRunPath(root), 0o700); err != nil {
		return nil, wrapExitError(ExitConfig, "create dispatch run directory", err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return nil, wrapExitError(ExitConfig, "create isolated dispatch run directory", err)
	}
	now := time.Now().UTC()
	record := RunRecord{
		ID:              id,
		Agent:           agent,
		ProviderVersion: version,
		Mode:            mode,
		State:           dispatchStatePending,
		RecoveryState:   recoveryRetrySafe,
		StartedAt:       now,
		UpdatedAt:       now,
		AnswerPath:      filepath.Join(dir, "answer.txt"),
		StdoutPath:      filepath.Join(dir, "provider.stdout"),
		StderrPath:      filepath.Join(dir, "provider.stderr"),
	}
	if providerProducesStructuredEvents(agent) {
		record.EventsPath = filepath.Join(dir, "provider.events")
	}
	if err := writeRunRecord(dir, &record); err != nil {
		return nil, err
	}
	return &dispatchRun{Record: record, Dir: dir}, nil
}

func providerProducesStructuredEvents(agent string) bool {
	return agent == AgentClaude || agent == AgentCodex
}

func newUUID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(buf)
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:]), nil
}

var nameSizes = []string{"big", "small", "tiny", "short", "calm", "compact", "micro", "nimble", "slim", "wide"}
var nameShapes = []string{"round", "bright", "silent", "rapid", "steady", "curved", "gentle", "linear", "square", "swift"}
var nameElectrical = []string{"capacitor", "inductor", "resistor", "transistor", "rectifier", "amplifier", "diode", "oscillator", "relay", "transformer"}

func randomDispatchName() (string, error) {
	pick := func(values []string) (string, error) {
		if len(values) == 0 {
			return "", errors.New("empty dispatch name vocabulary")
		}
		limit := bigInt(len(values))
		value, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		return values[value.Int64()], nil
	}
	first, err := pick(nameSizes)
	if err != nil {
		return "", err
	}
	second, err := pick(nameShapes)
	if err != nil {
		return "", err
	}
	third, err := pick(nameElectrical)
	if err != nil {
		return "", err
	}
	return first + "-" + second + "-" + third, nil
}

// bigInt is a narrow seam that keeps randomDispatchName's selection readable.
func bigInt(value int) *big.Int { return big.NewInt(int64(value)) }

func reserveSession(root string, run *dispatchRun) (Session, error) {
	if err := os.MkdirAll(dispatchStatePath(root), 0o700); err != nil {
		return Session{}, wrapExitError(ExitConfig, "create dispatch state directory", err)
	}
	for attempts := 0; attempts < 256; attempts++ {
		name, err := randomDispatchName()
		if err != nil {
			return Session{}, wrapExitError(ExitTargetFailure, "generate dispatch name", err)
		}
		path, err := sessionPath(root, name)
		if err != nil {
			return Session{}, err
		}
		now := time.Now().UTC()
		session := Session{Name: name, Agent: run.Record.Agent, CreatedAt: now, LastUsedAt: now, State: sessionStatePending, RunID: run.Record.ID, ActiveRunID: run.Record.ID, ActiveClaimKnown: true}
		file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- name is generated from fixed vocabularies.
		if errors.Is(openErr, fs.ErrExist) {
			continue
		}
		if openErr != nil {
			return Session{}, wrapExitError(ExitConfig, "reserve dispatch name", openErr)
		}
		encoderErr := json.NewEncoder(file).Encode(session)
		closeErr := file.Close()
		if encoderErr != nil || closeErr != nil {
			_ = os.Remove(path)
			if encoderErr != nil {
				return Session{}, wrapExitError(ExitConfig, "write pending dispatch mapping", encoderErr)
			}
			return Session{}, wrapExitError(ExitConfig, "close pending dispatch mapping", closeErr)
		}
		run.Record.Name = name
		if err := writeRunRecord(run.Dir, &run.Record); err != nil {
			return Session{}, err
		}
		return session, nil
	}
	return Session{}, exitError(ExitConfig, "could not allocate a unique dispatch name after 256 attempts")
}

func persistSession(root string, session Session) error {
	path, err := sessionPath(root, session.Name)
	if err != nil {
		return err
	}
	return withSessionLock(root, session.Name, func() error {
		return writeJSONAtomic(path, session)
	})
}

func claimConversation(root string, name string, runID string) (Session, error) {
	var claimed Session
	err := withSessionLock(root, name, func() error {
		path, err := sessionPath(root, name)
		if err != nil {
			return err
		}
		var session Session
		if err := readJSON(path, &session); err != nil {
			return wrapExitError(ExitConfig, "read dispatch mapping for active claim", err)
		}
		ownerRunID := sessionOwnerRunID(session)
		if ownerRunID != "" && ownerRunID != runID {
			active, loadErr := loadRunRecord(root, ownerRunID)
			if loadErr == nil && activeClaimBlocksReplacement(active) {
				return exitError(ExitUnavailable, fmt.Sprintf("dispatch conversation %q is already active in run %s", name, ownerRunID))
			}
			if loadErr != nil && !errors.Is(loadErr, errDispatchRunNotFound) {
				return loadErr
			}
		}
		session.ActiveRunID = runID
		session.ActiveClaimKnown = true
		session.RunID = runID
		session.LastUsedAt = time.Now().UTC()
		if err := writeJSONAtomic(path, session); err != nil {
			return wrapExitError(ExitConfig, "publish dispatch active claim", err)
		}
		claimed = session
		return nil
	})
	return claimed, err
}

// downgradeUnstartedSession clears durable provider identity from the mapping
// of a fresh run that provably failed before the provider started, so list
// and resume stop advertising a conversation the provider never created. The
// mapping itself is kept for run history.
func downgradeUnstartedSession(root string, name string, runID string) error {
	return withSessionLock(root, name, func() error {
		path, err := sessionPath(root, name)
		if err != nil {
			return err
		}
		var session Session
		if err := readJSON(path, &session); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return wrapExitError(ExitConfig, "read dispatch mapping for pre-start downgrade", err)
		}
		if session.RunID != runID || session.State != sessionStateDurable {
			return nil
		}
		session.ProviderSessionID = ""
		session.State = sessionStatePending
		if err := writeJSONAtomic(path, session); err != nil {
			return wrapExitError(ExitConfig, "downgrade unstarted dispatch mapping", err)
		}
		return nil
	})
}

func releaseConversation(root string, name string, runID string) error {
	return withSessionLock(root, name, func() error {
		path, err := sessionPath(root, name)
		if err != nil {
			return err
		}
		var session Session
		if err := readJSON(path, &session); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return wrapExitError(ExitConfig, "read dispatch mapping for claim release", err)
		}
		if session.ActiveRunID != runID {
			return nil
		}
		session.ActiveRunID = ""
		session.ActiveClaimKnown = true
		return writeJSONAtomic(path, session)
	})
}

func terminalDispatchState(state string) bool {
	switch state {
	case dispatchStateCompleted, dispatchStateFailed, dispatchStateCancelled, dispatchStateInterrupted:
		return true
	default:
		return false
	}
}

// activeClaimBlocksReplacement distinguishes terminal execution evidence from
// completed ownership. Cancellation is published before a live provider has
// necessarily stopped, so only a record that never acquired process identity
// or conservative proof that the recorded process is dead can recover an
// abandoned cancelled claim. Other terminal states are written by the owning
// execution after provider termination or a proven pre-start failure.
func activeClaimBlocksReplacement(record RunRecord) bool {
	if record.State == dispatchStateCancelled {
		return !cancelledClaimReleasable(record)
	}
	return !terminalDispatchState(record.State)
}

func cancelledClaimReleasable(record RunRecord) bool {
	if record.PID == 0 && record.ProcessGroupID == 0 && record.ProcessStartIdentity == "" {
		return true
	}
	return processOwnership(record) == ownershipDead && providerProcessGroupDead(record.ProcessGroupID)
}

// sessionOwnerRunID resolves the explicit active claim and the compatibility
// representation used before ActiveRunID existed.
func sessionOwnerRunID(session Session) string {
	if session.ActiveRunID != "" {
		return session.ActiveRunID
	}
	if session.ActiveClaimKnown {
		return ""
	}
	return session.RunID
}

func deleteSession(root string, name string) error {
	path, err := sessionPath(root, name)
	if err != nil {
		return err
	}
	return withSessionLock(root, name, func() error {
		err := os.Remove(path)
		if errors.Is(err, fs.ErrNotExist) {
			return exitError(ExitUsage, fmt.Sprintf("dispatch session %q was not found", name))
		}
		if err != nil {
			return wrapExitError(ExitConfig, "delete dispatch mapping", err)
		}
		return nil
	})
}

func loadSession(root string, name string) (Session, error) {
	path, err := sessionPath(root, name)
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := readJSON(path, &session); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Session{}, exitError(ExitUsage, fmt.Sprintf("dispatch session %q was not found", name))
		}
		return Session{}, wrapExitError(ExitConfig, "read dispatch mapping", err)
	}
	if session.Name != name || !validDispatchName(session.Name) || !isProvider(session.Agent) {
		return Session{}, exitError(ExitConfig, fmt.Sprintf("dispatch session %q is invalid", name))
	}
	return session, nil
}

// pruneExpiredSessions removes inactive, valid mappings whose last use is
// older than the retention window. Corrupt mappings are preserved so normal
// list/inspect diagnostics can report them instead of silently erasing state.
func pruneExpiredSessions(root string, now time.Time) error {
	entries, err := os.ReadDir(dispatchStatePath(root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return wrapExitError(ExitConfig, "list dispatch sessions for retention", err)
	}
	cutoff := now.UTC().Add(-dispatchSessionRetention)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if !validDispatchName(name) {
			continue
		}
		if err := pruneExpiredSession(root, name, cutoff); err != nil {
			return err
		}
	}
	return nil
}

func pruneDispatchEvidence(root string, now time.Time) error {
	if err := pruneExpiredSessions(root, now); err != nil {
		return err
	}
	sessions, err := listSessions(root)
	if err != nil {
		return err
	}
	current := make(map[string]bool, len(sessions))
	for _, session := range sessions {
		if session.RunID != "" {
			current[session.RunID] = true
		}
	}
	cutoff := now.UTC().Add(-dispatchSessionRetention)
	entries, err := os.ReadDir(dispatchRunPath(root))
	if errors.Is(err, fs.ErrNotExist) {
		return pruneExpiredFanoutManifests(root, cutoff)
	}
	if err != nil {
		return wrapExitError(ExitConfig, "list dispatch evidence for retention", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || parseUUID(entry.Name()) != nil || current[entry.Name()] {
			continue
		}
		record, loadErr := loadRunRecord(root, entry.Name())
		if loadErr != nil || !terminalDispatchState(record.State) || record.CompletedAt == nil || !record.CompletedAt.Before(cutoff) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dispatchRunPath(root), entry.Name())); err != nil {
			return wrapExitError(ExitConfig, "prune expired dispatch evidence", err)
		}
	}
	return pruneExpiredFanoutManifests(root, cutoff)
}

// pruneExpiredFanoutManifests applies the same fixed retention window to
// terminal fanout manifests. Nonterminal manifests and manifests whose state
// or age cannot be established are left in place.
func pruneExpiredFanoutManifests(root string, cutoff time.Time) error {
	entries, err := os.ReadDir(fanoutStateRoot(root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return wrapExitError(ExitConfig, "list fanout manifests for retention", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || parseUUID(entry.Name()) != nil {
			continue
		}
		manifest, loadErr := loadFanoutManifest(root, entry.Name())
		if loadErr != nil || !terminalDispatchState(manifest.State) || manifest.CompletedAt == nil || !manifest.CompletedAt.Before(cutoff) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(fanoutStateRoot(root), entry.Name())); err != nil {
			return wrapExitError(ExitConfig, "prune expired fanout manifest", err)
		}
	}
	return nil
}

func pruneExpiredSession(root string, name string, cutoff time.Time) error {
	return withSessionLock(root, name, func() error {
		session, err := loadSession(root, name)
		if err != nil {
			// Retention must not hide or erase corrupt state.
			return err
		}
		lastUsed := session.LastUsedAt
		if lastUsed.IsZero() {
			lastUsed = session.CreatedAt
		}
		if lastUsed.IsZero() || !lastUsed.Before(cutoff) || dispatchSessionActive(root, session) {
			return nil
		}
		path, err := sessionPath(root, name)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return wrapExitError(ExitConfig, "prune expired dispatch mapping", err)
		}
		return nil
	})
}

func dispatchSessionActive(root string, session Session) bool {
	activeRunID := sessionOwnerRunID(session)
	if activeRunID == "" {
		return false
	}
	record, err := loadRunRecord(root, activeRunID)
	if err != nil {
		// Retention must not erase a mapping when its active claim cannot be
		// interpreted conservatively. Explicit operations surface the error.
		return true
	}
	return activeClaimBlocksReplacement(record)
}

func withSessionLock(root string, name string, fn func() error) error {
	path, err := lockPath(root, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return wrapExitError(ExitConfig, "create dispatch state directory", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- path is validated dispatch state.
	if err != nil {
		return wrapExitError(ExitConfig, "open dispatch session lock", err)
	}
	defer func() { _ = file.Close() }()
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX) //nolint:gosec // supported Unix file descriptor.
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return wrapExitError(ExitConfig, "lock dispatch session", err)
	}
	defer func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN) }() //nolint:gosec // supported Unix file descriptor.
	return fn()
}

func writeRunRecord(dir string, record *RunRecord) error {
	return writeRunRecordWithPublisher(dir, record, writeJSONAtomic)
}

// writeRunRecordWithPublisher publishes the next record revision without
// advancing caller-owned concurrency state until publication succeeds.
func writeRunRecordWithPublisher(dir string, record *RunRecord, publish func(string, any) error) error {
	if record == nil {
		return exitError(ExitConfig, "write nil dispatch run record")
	}
	next := *record
	next.Revision++
	next.UpdatedAt = time.Now().UTC()
	if err := validateRunRecord(next); err != nil {
		return err
	}
	return withRunLock(dir, func() error {
		path := filepath.Join(dir, dispatchRunFile)
		var current RunRecord
		if err := readJSON(path, &current); err == nil {
			if current.Revision != record.Revision {
				return exitError(ExitUnavailable, fmt.Sprintf("dispatch run %s changed concurrently (expected revision %d, active revision %d)", record.ID, record.Revision, current.Revision))
			}
			if terminalDispatchState(current.State) && !terminalDispatchState(next.State) {
				return exitError(ExitUnavailable, fmt.Sprintf("dispatch run %s is already terminal (%s)", record.ID, current.State))
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return wrapExitError(ExitConfig, "read dispatch run record before update", err)
		}
		if err := publish(path, next); err != nil {
			return wrapExitError(ExitConfig, "write dispatch run record", err)
		}
		record.Revision = next.Revision
		record.UpdatedAt = next.UpdatedAt
		return nil
	})
}

func withRunLock(dir string, fn func() error) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, ".record.lock"), os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- dir is an Agent Layer-owned run directory.
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil { //nolint:gosec // supported Unix descriptor.
		return err
	}
	defer func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN) }() //nolint:gosec // supported Unix descriptor.
	return fn()
}

func loadRunRecord(root string, id string) (RunRecord, error) {
	if err := parseUUID(id); err != nil {
		return RunRecord{}, exitError(ExitUsage, fmt.Sprintf("dispatch run ID %q is invalid", id))
	}
	dir := filepath.Join(dispatchRunPath(root), id)
	var record RunRecord
	if err := readJSON(filepath.Join(dir, dispatchRunFile), &record); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return RunRecord{}, wrapExitError(ExitUsage, fmt.Sprintf("dispatch run %q was not found", id), errDispatchRunNotFound)
		}
		return RunRecord{}, wrapExitError(ExitConfig, "read dispatch run record", err)
	}
	if record.ID != id {
		return RunRecord{}, exitError(ExitConfig, fmt.Sprintf("dispatch run %q is invalid", id))
	}
	if err := validateRunRecord(record); err != nil {
		return RunRecord{}, err
	}
	return record, nil
}

func validateRunRecord(record RunRecord) error {
	validState := map[string]bool{dispatchStatePending: true, dispatchStateStarting: true, dispatchStateRunning: true, dispatchStateCompleted: true, dispatchStateFailed: true, dispatchStateCancelled: true, dispatchStateInterrupted: true}
	validRecovery := map[string]bool{recoveryRetrySafe: true, recoveryResumeRequired: true, recoveryAcceptanceUnknown: true, recoveryNotResumable: true}
	if !validState[record.State] || !validRecovery[record.RecoveryState] {
		return exitError(ExitConfig, fmt.Sprintf("dispatch run %q has invalid execution/recovery state %q/%q", record.ID, record.State, record.RecoveryState))
	}
	if !terminalDispatchState(record.State) && record.CompletedAt != nil {
		return exitError(ExitConfig, fmt.Sprintf("dispatch run %q is nonterminal with a completion timestamp", record.ID))
	}
	if terminalDispatchState(record.State) && record.CompletedAt == nil {
		return exitError(ExitConfig, fmt.Sprintf("dispatch run %q is terminal without a completion timestamp", record.ID))
	}
	if record.State == dispatchStateCompleted && record.RecoveryState != recoveryResumeRequired && record.RecoveryState != recoveryNotResumable {
		return exitError(ExitConfig, fmt.Sprintf("completed dispatch run %q has invalid recovery state %q", record.ID, record.RecoveryState))
	}
	if record.RecoveryState == recoveryNotResumable && !record.NotResumable {
		return exitError(ExitConfig, fmt.Sprintf("dispatch run %q reports not_resumable without provider evidence", record.ID))
	}
	return nil
}

// listRunRecords loads every readable run record. Records that cannot be
// loaded or validated are skipped and reported as warnings so one corrupt
// record cannot hide the history of unrelated conversations; the corrupt
// evidence itself is left in place.
func listRunRecords(root string) ([]RunRecord, []string, error) {
	entries, err := os.ReadDir(dispatchRunPath(root))
	if errors.Is(err, fs.ErrNotExist) {
		return []RunRecord{}, nil, nil
	}
	if err != nil {
		return nil, nil, wrapExitError(ExitConfig, "list dispatch run records", err)
	}
	records := make([]RunRecord, 0, len(entries))
	warnings := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() || parseUUID(entry.Name()) != nil {
			continue
		}
		record, err := loadRunRecord(root, entry.Name())
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipped unreadable dispatch run record %s: %v", entry.Name(), err))
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].StartedAt.Equal(records[j].StartedAt) {
			return records[i].ID < records[j].ID
		}
		return records[i].StartedAt.Before(records[j].StartedAt)
	})
	return records, warnings, nil
}

func parseUUID(value string) error {
	if len(value) != 36 {
		return errors.New("invalid UUID length")
	}
	for index, r := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if r != '-' {
				return errors.New("invalid UUID separator")
			}
			continue
		}
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return errors.New("invalid UUID character")
		}
	}
	return nil
}

func writeJSONAtomic(path string, value any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".dispatch-*.tmp")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer func() { _ = os.Remove(temp) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if err := json.NewEncoder(file).Encode(value); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path) // #nosec G703 -- path and temp share the caller-selected Agent Layer state directory.
}

func writeBytesAtomic(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".dispatch-result-*.tmp")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer func() { _ = os.Remove(temp) }()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path) // #nosec G703 -- temp and destination share Agent Layer's private directory.
}

func readJSON(path string, target any) error {
	file, err := os.Open(path) // #nosec G304 -- caller passes validated Agent Layer paths.
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("contains multiple JSON values")
		}
		return err
	}
	return nil
}

func listSessions(root string) ([]Session, error) {
	entries, err := os.ReadDir(dispatchStatePath(root))
	if errors.Is(err, fs.ErrNotExist) {
		return []Session{}, nil
	}
	if err != nil {
		return nil, wrapExitError(ExitConfig, "list dispatch sessions", err)
	}
	sessions := make([]Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if !validDispatchName(name) {
			return nil, exitError(ExitConfig, fmt.Sprintf("invalid dispatch state file %q", entry.Name()))
		}
		session, err := loadSession(root, name)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Name < sessions[j].Name })
	return sessions, nil
}

func isProvider(agent string) bool {
	return agent == AgentClaude || agent == AgentCodex || agent == AgentAntigravity
}

func processAlive(pid int) string {
	if pid <= 0 {
		return statusUnknown
	}
	err := syscall.Kill(pid, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return processStatusAlive
	}
	if errors.Is(err, syscall.ESRCH) {
		return processStatusDead
	}
	return statusUnknown
}
