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
)

var errDispatchRunNotFound = errors.New("dispatch run record not found")

// Session is the durable, name-keyed mapping owned by Agent Layer. Provider
// transcripts remain provider-owned; this record contains only the alias
// needed for explicit continuation.
type Session struct {
	Name              string    `json:"name"`
	Agent             string    `json:"agent"`
	ProviderSessionID string    `json:"provider_session_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	LastUsedAt        time.Time `json:"last_used_at"`
	State             string    `json:"state,omitempty"`
	RunID             string    `json:"run_id,omitempty"`
}

// RunRecord is private, recoverable dispatch evidence. It never contains the
// caller prompt or a provider transcript.
type RunRecord struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	Agent             string     `json:"agent"`
	ProviderVersion   string     `json:"provider_version"`
	Mode              string     `json:"mode"`
	State             string     `json:"state"`
	Attempt           int        `json:"attempt"`
	PID               int        `json:"pid,omitempty"`
	StartedAt         time.Time  `json:"started_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	ProviderSessionID string     `json:"provider_session_id,omitempty"`
	NotResumable      bool       `json:"not_resumable,omitempty"`
	TerminalReason    string     `json:"terminal_reason,omitempty"`
	LastOutputAt      *time.Time `json:"last_output_at,omitempty"`
	AnswerPath        string     `json:"answer_path"`
	StdoutPath        string     `json:"stdout_path"`
	StderrPath        string     `json:"stderr_path"`
	EventsPath        string     `json:"events_path,omitempty"`
	ProviderLogPath   string     `json:"provider_log_path,omitempty"`
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
		State:           "pending",
		StartedAt:       now,
		UpdatedAt:       now,
		AnswerPath:      filepath.Join(dir, "answer.txt"),
		StdoutPath:      filepath.Join(dir, "provider.stdout"),
		StderrPath:      filepath.Join(dir, "provider.stderr"),
		EventsPath:      filepath.Join(dir, "provider.events"),
	}
	if err := writeRunRecord(dir, &record); err != nil {
		return nil, err
	}
	return &dispatchRun{Record: record, Dir: dir}, nil
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
		session := Session{Name: name, Agent: run.Record.Agent, CreatedAt: now, LastUsedAt: now, State: "pending", RunID: run.Record.ID}
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
	if record == nil {
		return exitError(ExitConfig, "write nil dispatch run record")
	}
	record.UpdatedAt = time.Now().UTC()
	if err := writeJSONAtomic(filepath.Join(dir, dispatchRunFile), record); err != nil {
		return wrapExitError(ExitConfig, "write dispatch run record", err)
	}
	return nil
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
	return record, nil
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
		return "dead"
	}
	return statusUnknown
}
