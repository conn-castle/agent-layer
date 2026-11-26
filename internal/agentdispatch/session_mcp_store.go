package agentdispatch

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
)

const (
	sessionMCPActiveFile = "session-mcp.active"
	sessionMCPSpecFile   = "spec.json"
	sessionMCPClaudeFile = "claude-mcp.json"
	sessionMCPSubcommand = "__dispatch-session-mcp"
)

// sessionMCPExecutable resolves a child-executable path to the running `al`
// binary for the session MCP wrapper. Unlike executableimage.Self (which
// returns /proc/self/exe on Linux), this path must be executable by external
// provider processes (Codex, Claude, Muse, Grok, Antigravity). Tests may override it.
var sessionMCPExecutable = defaultSessionMCPExecutable

func defaultSessionMCPExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, evalErr := filepath.EvalSymlinks(path); evalErr == nil && resolved != "" {
		return resolved, nil
	}
	return path, nil
}

type sessionMCPSpec struct {
	WorkDir string             `json:"work_dir"`
	Nonce   string             `json:"nonce,omitempty"`
	BaseEnv []string           `json:"base_env,omitempty"`
	Servers []SessionMCPServer `json:"servers"`
}

type stagedSessionMCP struct {
	Executable string
	SpecPath   string
	Digest     string
	Nonce      string
}

// ensureOwnedPrivateDir ensures dir exists as a real (non-symlink) directory
// with 0700 permissions owned by the current effective user ID.
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

func pathWithinDir(candidate string, root string) bool {
	if strings.TrimSpace(candidate) == "" || strings.TrimSpace(root) == "" {
		return false
	}
	absCand, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(absCand); err == nil {
		absCand = resolved
	}
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
	}
	rel, err := filepath.Rel(absRoot, absCand)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// sessionMCPDurableStoreDir returns an out-of-workspace, owner-verified
// directory for per-conversation session MCP definitions retained across
// continuations. It refuses any path inside root.
func sessionMCPDurableStoreDir(root string) (string, error) {
	var base string
	if cacheDir, err := os.UserCacheDir(); err == nil && strings.TrimSpace(cacheDir) != "" {
		candidate := filepath.Join(cacheDir, "agent-layer", "dispatch-session-mcp")
		if !pathWithinDir(candidate, root) {
			base = candidate
		}
	}
	if base == "" {
		base = filepath.Join(os.TempDir(), fmt.Sprintf("agent-layer-session-mcp-%d", os.Getuid()))
	}
	if pathWithinDir(base, root) {
		return "", exitError(ExitConfig, "private session MCP store resolves inside repository workspace")
	}
	if err := ensureOwnedPrivateDir(base); err != nil {
		return "", wrapExitError(ExitConfig, "prepare private session MCP store", err)
	}
	return base, nil
}

// sessionMCPRuntimeRoot returns the per-user private runtime root under
// os.TempDir(). Per-invocation wrapper runtime state lives under os.TempDir()
// rather than the workspace or ~/.cache so read-only provider sandboxes (such
// as Grok's GROK_SANDBOX=read-only, which allows /tmp writes while blocking
// repo and ~/.cache writes) can still execute the wrapper and record PID/PGID
// ownership and MCP handshake proofs securely outside the workspace.
func sessionMCPRuntimeRoot() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("agent-layer-dispatch-%d", os.Getuid()))
}

// sessionMCPRuntimeDirPath computes the private per-run directory path without
// creating any directories.
func sessionMCPRuntimeDirPath(runDir string) (string, error) {
	absRunDir, err := filepath.Abs(runDir)
	if err != nil {
		return "", wrapExitError(ExitConfig, "resolve dispatch run directory", err)
	}
	return filepath.Join(sessionMCPRuntimeRoot(), "runs", privateStoreKey(filepath.Clean(absRunDir))), nil
}

// ensureSessionMCPRuntimeDir creates and owner-verifies the private per-run
// directory hierarchy for runDir.
func ensureSessionMCPRuntimeDir(runDir string) (string, error) {
	rtDir, err := sessionMCPRuntimeDirPath(runDir)
	if err != nil {
		return "", err
	}
	rtRoot := sessionMCPRuntimeRoot()
	runsDir := filepath.Join(rtRoot, "runs")
	for _, dir := range []string{rtRoot, runsDir, rtDir} {
		if err := ensureOwnedPrivateDir(dir); err != nil {
			return "", wrapExitError(ExitConfig, "prepare private session MCP runtime dir", err)
		}
	}
	return rtDir, nil
}

func privateStoreKey(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func newSessionMCPNonce() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func privateSessionSpecPath(root string, sessionMCPID string) (string, error) {
	if err := parseUUID(sessionMCPID); err != nil {
		return "", exitError(ExitConfig, fmt.Sprintf("invalid session MCP identifier %q", sessionMCPID))
	}
	base, err := sessionMCPDurableStoreDir(root)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "sessions")
	if err := ensureOwnedPrivateDir(dir); err != nil {
		return "", wrapExitError(ExitConfig, "prepare private session MCP session dir", err)
	}
	return filepath.Join(dir, sessionMCPID+".json"), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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

func readPrivateVerifiedJSON(path string, expectedDigest string, dst any) error {
	if strings.TrimSpace(expectedDigest) == "" {
		return exitError(ExitConfig, "missing session MCP integrity digest")
	}
	file, err := openNoFollowRegularFile(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	const maxPrivateSpecBytes = 8 * maxSessionMCPConfigBytes
	data, err := io.ReadAll(io.LimitReader(file, maxPrivateSpecBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxPrivateSpecBytes {
		return exitError(ExitConfig, "session MCP specification exceeds size limit")
	}
	if actual := sha256Hex(data); actual != expectedDigest {
		return exitError(ExitConfig, "session MCP specification integrity check failed")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing data in session MCP specification")
	}
	return nil
}

func persistPrivateSessionMCPServers(root string, sessionMCPID string, servers []SessionMCPServer) (string, error) {
	normalized := normalizeSessionMCPServers(servers)
	if len(normalized) == 0 {
		return "", nil
	}
	path, err := privateSessionSpecPath(root, sessionMCPID)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", wrapExitError(ExitConfig, "encode private session MCP servers", err)
	}
	data = append(data, '\n')
	if err := fsutil.WriteFileAtomic(path, data, 0o600); err != nil {
		return "", wrapExitError(ExitConfig, "write private session MCP servers", err)
	}
	return sha256Hex(data), nil
}

func loadPrivateSessionMCPServers(root string, sessionMCPID string, expectedDigest string) ([]SessionMCPServer, error) {
	if strings.TrimSpace(expectedDigest) == "" {
		return nil, nil
	}
	path, err := privateSessionSpecPath(root, sessionMCPID)
	if err != nil {
		return nil, err
	}
	var servers []SessionMCPServer
	if err := readPrivateVerifiedJSON(path, expectedDigest, &servers); err != nil {
		return nil, wrapExitError(ExitConfig, fmt.Sprintf("read private session MCP servers %q", sessionMCPID), err)
	}
	return normalizeSessionMCPServers(servers), nil
}

func removePrivateSessionMCPServers(root string, sessionMCPID string) error {
	if strings.TrimSpace(sessionMCPID) == "" {
		return nil
	}
	path, err := privateSessionSpecPath(root, sessionMCPID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return wrapExitError(ExitConfig, "remove private session MCP servers", err)
	}
	return nil
}

// stageSessionMCPSpec writes the private per-invocation server specification
// outside the workspace checkout in the owner-verified runtime directory and
// marks the runtime directory active.
func stageSessionMCPSpec(runDir string, workDir string, baseEnv []string, servers []SessionMCPServer) (stagedSessionMCP, error) {
	if len(servers) == 0 {
		return stagedSessionMCP{}, nil
	}
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return stagedSessionMCP{}, wrapExitError(ExitConfig, "resolve session MCP working directory", err)
	}
	executable, err := sessionMCPExecutable()
	if err != nil {
		return stagedSessionMCP{}, wrapExitError(ExitConfig, "resolve Agent Layer executable for session MCP wrapper", err)
	}
	rtDir, err := ensureSessionMCPRuntimeDir(runDir)
	if err != nil {
		return stagedSessionMCP{}, err
	}
	nonce, err := newSessionMCPNonce()
	if err != nil {
		return stagedSessionMCP{}, wrapExitError(ExitTargetFailure, "generate session MCP handshake nonce", err)
	}
	specPath := filepath.Join(rtDir, sessionMCPSpecFile)
	spec := sessionMCPSpec{
		WorkDir: absWorkDir,
		Nonce:   nonce,
		BaseEnv: withoutSessionMCPEnvironment(baseEnv),
		Servers: normalizeSessionMCPServers(servers),
	}
	data, err := json.Marshal(spec)
	if err != nil {
		return stagedSessionMCP{}, wrapExitError(ExitConfig, "encode session MCP server specification", err)
	}
	data = append(data, '\n')
	if err := fsutil.WriteFileAtomic(specPath, data, 0o600); err != nil {
		return stagedSessionMCP{}, wrapExitError(ExitConfig, "write session MCP server specification", err)
	}
	// Mark the private runtime dir active so termination confirmation knows this
	// invocation can spawn tracked session MCP wrapper process groups.
	if err := publishBytes(filepath.Join(rtDir, sessionMCPActiveFile), []byte("active\n"), os.Rename); err != nil {
		return stagedSessionMCP{}, wrapExitError(ExitConfig, "write session MCP active marker", err)
	}
	return stagedSessionMCP{
		Executable: executable,
		SpecPath:   specPath,
		Digest:     sha256Hex(data),
		Nonce:      nonce,
	}, nil
}

type claudeSessionMCPConfigDoc struct {
	MCPServers map[string]claudeSessionMCPEntry `json:"mcpServers"`
}

type claudeSessionMCPEntry struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func stageClaudeSessionMCPConfig(runDir string, staged stagedSessionMCP, servers []SessionMCPServer) (string, error) {
	rtDir, err := ensureSessionMCPRuntimeDir(runDir)
	if err != nil {
		return "", err
	}
	doc := claudeSessionMCPConfigDoc{
		MCPServers: make(map[string]claudeSessionMCPEntry, len(servers)),
	}
	for _, server := range normalizeSessionMCPServers(servers) {
		doc.MCPServers[server.Name] = claudeSessionMCPEntry{
			Type:    config.TransportStdio,
			Command: staged.Executable,
			Args:    sessionMCPWrapperArgsWithSpec(runDir, server.Name, staged.SpecPath, staged.Digest, false),
		}
	}
	path := filepath.Join(rtDir, sessionMCPClaudeFile)
	if err := writeJSONAtomic(path, doc); err != nil {
		return "", wrapExitError(ExitConfig, "write Claude session MCP config", err)
	}
	return path, nil
}

func sessionMCPWrapperArgsWithSpec(runDir string, serverName string, specPath string, digest string, verifyHandshake bool) []string {
	args := []string{sessionMCPSubcommand, "--run-dir", runDir, "--server", serverName, "--spec", specPath, "--digest", digest}
	if verifyHandshake {
		args = append(args, "--verify-handshake")
	}
	return args
}

func codexSessionMCPConfigArg(runDir string, staged stagedSessionMCP, serverName string) string {
	args := sessionMCPWrapperArgsWithSpec(runDir, serverName, staged.SpecPath, staged.Digest, false)
	quotedArgs := make([]string, len(args))
	for i, arg := range args {
		quotedArgs[i] = fmt.Sprintf("%q", arg)
	}
	return fmt.Sprintf(
		"mcp_servers.%s={command=%q,args=[%s],required=true,default_tools_approval_mode=\"approve\"}",
		serverName,
		staged.Executable,
		strings.Join(quotedArgs, ","),
	)
}

func museSessionMCPConfigArg(runDir string, staged stagedSessionMCP, serverName string) (string, error) {
	payload := struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
		Mode    string   `json:"mode"`
	}{
		Command: staged.Executable,
		Args:    sessionMCPWrapperArgsWithSpec(runDir, serverName, staged.SpecPath, staged.Digest, false),
		Mode:    "required",
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", wrapExitError(ExitConfig, "encode Muse session MCP config", err)
	}
	return fmt.Sprintf("mcpServers.%s=%s", serverName, string(encoded)), nil
}

// removeSessionMCPArtifacts removes private staged session MCP specs and
// launch configs containing caller environment variables once the invocation is
// no longer launching, while preserving process tracking files until
// termination is confirmed.
func removeSessionMCPArtifacts(runDir string) error {
	if runDir == "" {
		return nil
	}
	rtDir, err := sessionMCPRuntimeDirPath(runDir)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(rtDir); errors.Is(statErr, fs.ErrNotExist) {
		return nil
	}
	var errs []error
	for _, name := range []string{sessionMCPSpecFile, sessionMCPClaudeFile} {
		path := filepath.Join(rtDir, name)
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			errs = append(errs, wrapExitError(ExitConfig, fmt.Sprintf("remove staged session MCP file %s", path), rmErr))
		}
	}
	if rmErr := os.RemoveAll(filepath.Join(rtDir, sessionMCPHandshakeDir)); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		errs = append(errs, wrapExitError(ExitConfig, "remove session MCP handshake directory", rmErr))
	}
	return errors.Join(errs...)
}

// removeSessionMCPRuntimeDir removes the entire private per-run session MCP
// runtime directory once termination is durably confirmed or during retention
// pruning.
func removeSessionMCPRuntimeDir(runDir string) error {
	if runDir == "" {
		return nil
	}
	rtDir, err := sessionMCPRuntimeDirPath(runDir)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(rtDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return wrapExitError(ExitConfig, "remove session MCP runtime directory", err)
	}
	return nil
}
