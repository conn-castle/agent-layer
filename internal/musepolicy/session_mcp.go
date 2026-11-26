package musepolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/fsutil"
)

// SessionMCPGrant records the session-scoped MCP server IDs authorized for one
// exact Muse provider session UUID.
type SessionMCPGrant struct {
	SessionID string   `json:"session_id"`
	ServerIDs []string `json:"server_ids"`
}

func resolvePathThroughExistingAncestors(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	cur := filepath.Clean(abs)
	var missing []string
	for {
		resolved, evalErr := filepath.EvalSymlinks(cur)
		if evalErr == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(evalErr, fs.ErrNotExist) {
			return "", evalErr
		}
		if info, lerr := os.Lstat(cur); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("dangling symlink %q", cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", evalErr
		}
		missing = append(missing, filepath.Base(cur))
		cur = parent
	}
}

func canonicalGrantRoot(root string) (string, error) {
	return resolvePathThroughExistingAncestors(root)
}

func grantPathWithinRoot(candidate string, canonicalRoot string) bool {
	if strings.TrimSpace(candidate) == "" || strings.TrimSpace(canonicalRoot) == "" {
		return true
	}
	resolvedCand, err := resolvePathThroughExistingAncestors(candidate)
	if err != nil {
		return true
	}
	rel, err := filepath.Rel(canonicalRoot, resolvedCand)
	if err != nil {
		return true
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func canonicalTempDir() string {
	if resolved, err := filepath.EvalSymlinks("/tmp"); err == nil && resolved != "" {
		return filepath.Clean(resolved)
	}
	return "/tmp"
}

func ensureOwnedPrivateDir(dir string) error {
	if err := fsutil.EnsurePrivateDir(dir); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("private directory %q must not be a symlink", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("private path %q is not a directory", dir)
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("private directory %q has mode %o, want 0700", dir, info.Mode().Perm())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	uid := os.Getuid()
	if !ok || uid < 0 || uint64(stat.Uid) != uint64(uid) {
		return fmt.Errorf("private directory %q is not owned by current user", dir)
	}
	return nil
}

// sessionMCPGrantDirPath returns an out-of-workspace, owner-only directory for
// Muse session MCP approval grants so workspace-writable agents cannot forge
// grants for arbitrary MCP servers.
func sessionMCPGrantDirPath(root string) (string, error) {
	canonical, err := canonicalGrantRoot(root)
	if err != nil {
		return "", fmt.Errorf("resolve Muse session MCP grant root: %w", err)
	}
	var base string
	if cacheDir, cacheErr := os.UserCacheDir(); cacheErr == nil && strings.TrimSpace(cacheDir) != "" {
		candidate := filepath.Join(cacheDir, "agent-layer", "muse-session-mcp")
		if !grantPathWithinRoot(candidate, canonical) {
			base = candidate
		}
	}
	if base == "" {
		base = filepath.Join(canonicalTempDir(), fmt.Sprintf("agent-layer-muse-mcp-%d", os.Getuid()))
	}
	if grantPathWithinRoot(base, canonical) {
		return "", errors.New("muse session MCP grant store resolves inside repository workspace")
	}
	sum := sha256.Sum256([]byte(canonical))
	repoKey := hex.EncodeToString(sum[:16])
	return filepath.Join(base, repoKey), nil
}

func sessionMCPGrantPath(root string, sessionID string) (string, error) {
	if err := validateSessionUUID(sessionID); err != nil {
		return "", err
	}
	dir, err := sessionMCPGrantDirPath(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionID+".json"), nil
}

// WriteSessionMCPGrant atomically persists the session-scoped MCP server IDs
// authorized for one exact Muse provider session UUID outside the workspace.
func WriteSessionMCPGrant(root string, sessionID string, serverIDs []string) error {
	canonical, err := canonicalGrantRoot(root)
	if err != nil {
		return fmt.Errorf("write Muse session MCP grant: %w", err)
	}
	path, err := sessionMCPGrantPath(root, sessionID)
	if err != nil {
		return fmt.Errorf("write Muse session MCP grant: %w", err)
	}
	if _, err := MCPPrefixesForIDs(serverIDs); err != nil {
		return fmt.Errorf("write Muse session MCP grant: %w", err)
	}
	dir := filepath.Dir(path)
	if err := ensureOwnedPrivateDir(filepath.Dir(dir)); err != nil {
		return fmt.Errorf("create Muse session MCP grant base directory: %w", err)
	}
	if err := ensureOwnedPrivateDir(dir); err != nil {
		return fmt.Errorf("create Muse session MCP grant directory: %w", err)
	}
	if grantPathWithinRoot(dir, canonical) {
		return errors.New("write Muse session MCP grant: grant directory resolved inside repository workspace")
	}
	sorted := slices.Clone(serverIDs)
	slices.Sort(sorted)
	data, err := json.Marshal(SessionMCPGrant{SessionID: sessionID, ServerIDs: sorted})
	if err != nil {
		return fmt.Errorf("encode Muse session MCP grant: %w", err)
	}
	data = append(data, '\n')
	if err := fsutil.WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("write Muse session MCP grant: %w", err)
	}
	return nil
}

// RemoveSessionMCPGrant removes the grant file for one Muse provider session
// UUID when present.
func RemoveSessionMCPGrant(root string, sessionID string) error {
	if !isValidSessionUUID(sessionID) {
		return nil
	}
	path, err := sessionMCPGrantPath(root, sessionID)
	if err != nil {
		return fmt.Errorf("remove Muse session MCP grant: %w", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove Muse session MCP grant: %w", err)
	}
	return nil
}

// PruneSessionMCPGrants removes grant files whose provider session UUID is no
// longer referenced by any retained dispatch session. Grants modified after
// notAfter are preserved, and isActive (when provided) is re-checked before
// deletion to prevent races with concurrent Muse dispatches.
func PruneSessionMCPGrants(root string, activeSessionIDs map[string]bool, notAfter time.Time, isActive func(sessionID string) bool) error {
	dir, err := sessionMCPGrantDirPath(root)
	if err != nil {
		return fmt.Errorf("resolve Muse session MCP grant directory: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list Muse session MCP grants: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".json")
		if validateSessionUUID(sessionID) != nil {
			continue
		}
		if activeSessionIDs[sessionID] {
			continue
		}
		if !notAfter.IsZero() {
			if info, statErr := entry.Info(); statErr == nil && info.ModTime().After(notAfter) {
				continue
			}
		}
		if isActive != nil && isActive(sessionID) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("prune Muse session MCP grant %s: %w", sessionID, err)
		}
	}
	return nil
}

func openNoFollowRegularFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := fsutil.RequireRegularFile(info, path); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func readSessionMCPGrant(root string, sessionID string) (SessionMCPGrant, bool, error) {
	path, err := sessionMCPGrantPath(root, sessionID)
	if err != nil {
		return SessionMCPGrant{}, false, nil
	}
	dir := filepath.Dir(path)
	if _, statErr := os.Lstat(dir); errors.Is(statErr, fs.ErrNotExist) {
		return SessionMCPGrant{}, false, nil
	}
	if err := ensureOwnedPrivateDir(dir); err != nil {
		return SessionMCPGrant{}, false, fmt.Errorf("verify Muse session MCP grant directory: %w", err)
	}
	file, err := openNoFollowRegularFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return SessionMCPGrant{}, false, nil
		}
		return SessionMCPGrant{}, false, fmt.Errorf("open Muse session MCP grant: %w", err)
	}
	defer func() { _ = file.Close() }()
	dec := json.NewDecoder(file)
	dec.DisallowUnknownFields()
	var grant SessionMCPGrant
	if err := dec.Decode(&grant); err != nil {
		return SessionMCPGrant{}, false, fmt.Errorf("decode Muse session MCP grant: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return SessionMCPGrant{}, false, errors.New("muse session MCP grant must contain one JSON object")
	}
	if grant.SessionID != sessionID {
		return SessionMCPGrant{}, false, fmt.Errorf("muse session MCP grant session_id mismatch")
	}
	return grant, true, nil
}

func isValidSessionUUID(value string) bool {
	return validateSessionUUID(value) == nil
}

func validateSessionUUID(value string) error {
	if len(value) != 36 {
		return errors.New("invalid session UUID length")
	}
	for index, r := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if r != '-' {
				return errors.New("invalid session UUID separator")
			}
			continue
		}
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return errors.New("invalid session UUID character")
		}
	}
	return nil
}
