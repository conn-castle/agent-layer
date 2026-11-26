package agentdispatch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/fsutil"
)

const (
	sessionMCPProcsFile      = "session-mcp-procs.json"
	sessionMCPProcsLockFile  = ".session-mcp-procs.lock"
	sessionMCPClosedFile     = "session-mcp.closed"
	sessionMCPHandshakeDir   = "session-mcp-handshake"
	sessionMCPEnvStripPrefix = "AL_SESSION_MCP_"
	mcpMethodToolsList       = "tools/list"
)

// sessionMCPExec is a narrow seam around syscall.Exec for unit testing.
var sessionMCPExec = syscall.Exec

type sessionMCPProcess struct {
	Server               string `json:"server"`
	PID                  int    `json:"pid"`
	ProcessGroupID       int    `json:"process_group_id"`
	ProcessStartIdentity string `json:"process_start_identity"`
}

type sessionMCPProcsDocument struct {
	Processes []sessionMCPProcess `json:"processes"`
}

type sessionMCPHandshakeStatus struct {
	Server               string `json:"server"`
	Nonce                string `json:"nonce,omitempty"`
	PID                  int    `json:"pid,omitempty"`
	ProcessStartIdentity string `json:"process_start_identity,omitempty"`
	ChildPID             int    `json:"child_pid,omitempty"`
	ChildStartIdentity   string `json:"child_start_identity,omitempty"`
	Initialized          bool   `json:"initialized"`
	ToolsListed          bool   `json:"tools_listed"`
	Failed               bool   `json:"failed,omitempty"`
	Error                string `json:"error,omitempty"`
}

// RunSessionMCPServer is the hidden `al __dispatch-session-mcp` entry point.
func RunSessionMCPServer(runDir string, serverName string, specPath string, digest string) error {
	return RunSessionMCPServerWithOptions(runDir, serverName, specPath, digest, false)
}

// RunSessionMCPServerWithOptions runs the hidden `al __dispatch-session-mcp`
// wrapper. It verifies the out-of-workspace spec digest before isolating the
// process group, recording ownership in the private runtime directory,
// restoring caller environment and working directory, and executing or relaying
// the target stdio MCP server.
func RunSessionMCPServerWithOptions(runDir string, serverName string, specPath string, digest string, verifyHandshake bool) error {
	if strings.TrimSpace(runDir) == "" || strings.TrimSpace(serverName) == "" || strings.TrimSpace(specPath) == "" || strings.TrimSpace(digest) == "" {
		return exitError(ExitUsage, "session MCP wrapper requires --run-dir, --server, --spec, and --digest")
	}
	if err := validateSessionMCPServerName(serverName); err != nil {
		return err
	}
	rtDir, err := ensureSessionMCPRuntimeDir(runDir)
	if err != nil {
		return err
	}
	var specNonce string
	recordFail := func(err error, safeReason string) error {
		if verifyHandshake && err != nil {
			if safeReason == "" {
				safeReason = "session MCP startup failed"
			}
			_ = writeSessionMCPHandshakeStatus(rtDir, sessionMCPHandshakeStatus{
				Server:               serverName,
				Nonce:                specNonce,
				PID:                  os.Getpid(),
				ProcessStartIdentity: processStartIdentity(os.Getpid()),
				Failed:               true,
				Error:                safeReason,
			})
		}
		return err
	}
	var spec sessionMCPSpec
	if err := readPrivateVerifiedJSON(specPath, digest, &spec); err != nil {
		return recordFail(wrapExitError(ExitConfig, "read session MCP server specification", err), "failed to read verified session MCP specification")
	}
	specNonce = spec.Nonce
	var target *SessionMCPServer
	for i := range spec.Servers {
		if spec.Servers[i].Name == serverName {
			target = &spec.Servers[i]
			break
		}
	}
	if target == nil {
		return recordFail(exitError(ExitConfig, fmt.Sprintf("session MCP server %q not found in run specification", serverName)), "server not found in run specification")
	}
	if err := validateSessionMCPServer(*target); err != nil {
		return recordFail(err, "invalid session MCP server definition")
	}
	if strings.TrimSpace(spec.WorkDir) == "" || !filepath.IsAbs(spec.WorkDir) {
		return recordFail(exitError(ExitConfig, fmt.Sprintf("session MCP server %q has invalid working directory %q", serverName, spec.WorkDir)), "invalid session MCP working directory")
	}

	if err := registerSessionMCPProcess(rtDir, serverName); err != nil {
		return recordFail(err, "session MCP wrapper registration rejected")
	}

	baseEnv := spec.BaseEnv
	if len(baseEnv) == 0 {
		baseEnv = os.Environ()
	}
	execEnv := buildSessionMCPProcessEnv(baseEnv, target.Env)
	if err := os.Chdir(spec.WorkDir); err != nil {
		return recordFail(wrapExitError(ExitTargetFailure, fmt.Sprintf("enter session MCP working directory %q", spec.WorkDir), err), "failed to enter working directory")
	}
	resolvedBinary, err := resolveSessionMCPCommand(target.Command, execEnv)
	if err != nil {
		return recordFail(wrapExitError(ExitTargetFailure, fmt.Sprintf("resolve session MCP server %q command %q", serverName, target.Command), err), fmt.Sprintf("command %q not executable or not found in PATH", target.Command))
	}
	if verifyHandshake {
		return runSessionMCPServerWithHandshake(rtDir, serverName, spec.Nonce, resolvedBinary, target.Args, spec.WorkDir, execEnv)
	}
	argv := append([]string{target.Command}, target.Args...)
	if err := sessionMCPExec(resolvedBinary, argv, execEnv); err != nil {
		return wrapExitError(ExitTargetFailure, fmt.Sprintf("exec session MCP server %q (%q)", serverName, resolvedBinary), err)
	}
	return nil
}

func sessionMCPHandshakePath(rtDir string, serverName string) string {
	return filepath.Join(rtDir, sessionMCPHandshakeDir, serverName+".json")
}

func writeSessionMCPHandshakeStatus(rtDir string, status sessionMCPHandshakeStatus) error {
	path := sessionMCPHandshakePath(rtDir, status.Server)
	if err := ensureOwnedPrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	return writeJSONAtomic(path, status)
}

func readSessionMCPHandshakeStatus(rtDir string, serverName string) (sessionMCPHandshakeStatus, bool, error) {
	path := sessionMCPHandshakePath(rtDir, serverName)
	file, err := openNoFollowRegularFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return sessionMCPHandshakeStatus{}, false, nil
		}
		return sessionMCPHandshakeStatus{}, false, err
	}
	defer func() { _ = file.Close() }()
	var status sessionMCPHandshakeStatus
	dec := json.NewDecoder(file)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&status); err != nil {
		return sessionMCPHandshakeStatus{}, false, err
	}
	return status, true, nil
}

func checkSessionMCPHandshakesReady(runDir string, servers []string, expectedNonce string) (bool, []string, error) {
	if len(servers) == 0 {
		return true, nil, nil
	}
	rtDir, err := sessionMCPRuntimeDirPath(runDir)
	if err != nil {
		return false, nil, err
	}
	var registered sessionMCPProcsDocument
	if _, statErr := os.Stat(filepath.Join(rtDir, sessionMCPProcsFile)); statErr == nil {
		loaded, readErr := readSessionMCPProcsLocked(rtDir)
		if readErr != nil {
			return false, nil, readErr
		}
		registered = loaded
	}
	var pending []string
	for _, name := range servers {
		status, ok, err := readSessionMCPHandshakeStatus(rtDir, name)
		if err != nil {
			return false, nil, wrapExitError(ExitConfig, fmt.Sprintf("read session MCP handshake status for %q", name), err)
		}
		if !ok {
			pending = append(pending, name)
			continue
		}
		if status.Failed {
			return false, nil, fmt.Errorf("session MCP server %q failed during startup: %s", name, status.Error)
		}
		if expectedNonce != "" && status.Nonce != expectedNonce {
			pending = append(pending, name)
			continue
		}
		if status.PID <= 0 || status.ProcessStartIdentity == "" {
			return false, nil, fmt.Errorf("session MCP server %q handshake status missing process identity", name)
		}
		switch ownershipForIdentity(status.PID, status.ProcessStartIdentity) {
		case ownershipOwned:
			if processIsZombie(status.PID) {
				return false, nil, fmt.Errorf("session MCP server %q exited before inference started", name)
			}
		case ownershipDead:
			return false, nil, fmt.Errorf("session MCP server %q exited before inference started", name)
		default:
			return false, nil, fmt.Errorf("cannot verify live ownership of session MCP server %q before inference", name)
		}
		if status.ChildPID > 0 {
			if status.ChildStartIdentity == "" {
				return false, nil, fmt.Errorf("session MCP server %q handshake status missing child process identity", name)
			}
			switch ownershipForIdentity(status.ChildPID, status.ChildStartIdentity) {
			case ownershipOwned:
				if processIsZombie(status.ChildPID) {
					return false, nil, fmt.Errorf("session MCP server %q child process exited before inference started", name)
				}
			case ownershipDead:
				return false, nil, fmt.Errorf("session MCP server %q child process exited before inference started", name)
			default:
				return false, nil, fmt.Errorf("cannot verify live ownership of session MCP server %q child process before inference", name)
			}
		}
		if !sessionMCPProcessRegistered(registered.Processes, name, status.PID, status.ProcessStartIdentity) {
			pending = append(pending, name)
			continue
		}
		if !status.Initialized || !status.ToolsListed {
			pending = append(pending, name)
		}
	}
	return len(pending) == 0, pending, nil
}

func sessionMCPProcessRegistered(procs []sessionMCPProcess, server string, pid int, startIdentity string) bool {
	for _, proc := range procs {
		if proc.Server == server && proc.PID == pid && proc.ProcessGroupID == pid && proc.ProcessStartIdentity == startIdentity {
			return true
		}
	}
	return false
}

func validateMCPHandshakeResult(method string, raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("%s returned empty result", method)
	}
	switch method {
	case acpMethodInitialize:
		var initRes struct {
			ProtocolVersion string          `json:"protocolVersion"`
			Capabilities    json.RawMessage `json:"capabilities"`
			ServerInfo      json.RawMessage `json:"serverInfo"`
		}
		if err := json.Unmarshal(trimmed, &initRes); err != nil {
			return fmt.Errorf("%s returned malformed result object", method)
		}
		if strings.TrimSpace(initRes.ProtocolVersion) == "" {
			return fmt.Errorf("%s result missing protocolVersion", method)
		}
		var capabilities map[string]any
		if len(bytes.TrimSpace(initRes.Capabilities)) == 0 || bytes.Equal(bytes.TrimSpace(initRes.Capabilities), []byte("null")) || json.Unmarshal(initRes.Capabilities, &capabilities) != nil || capabilities == nil {
			return fmt.Errorf("%s result missing capabilities object", method)
		}
		var serverInfo struct {
			Name string `json:"name"`
		}
		if len(bytes.TrimSpace(initRes.ServerInfo)) == 0 || bytes.Equal(bytes.TrimSpace(initRes.ServerInfo), []byte("null")) || json.Unmarshal(initRes.ServerInfo, &serverInfo) != nil || strings.TrimSpace(serverInfo.Name) == "" {
			return fmt.Errorf("%s result missing serverInfo.name", method)
		}
		return nil
	case mcpMethodToolsList:
		var listRes struct {
			Tools json.RawMessage `json:"tools"`
		}
		if err := json.Unmarshal(trimmed, &listRes); err != nil {
			return fmt.Errorf("%s returned malformed result object", method)
		}
		toolsTrimmed := bytes.TrimSpace(listRes.Tools)
		if len(toolsTrimmed) == 0 || bytes.Equal(toolsTrimmed, []byte("null")) {
			return fmt.Errorf("%s result missing tools array", method)
		}
		var tools []json.RawMessage
		if json.Unmarshal(toolsTrimmed, &tools) != nil {
			return fmt.Errorf("%s result missing tools array", method)
		}
		for i, rawTool := range tools {
			toolTrimmed := bytes.TrimSpace(rawTool)
			if len(toolTrimmed) == 0 || bytes.Equal(toolTrimmed, []byte("null")) {
				return fmt.Errorf("%s result tools[%d] must be a tool object", method, i)
			}
			var toolObj struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(toolTrimmed, &toolObj) != nil || strings.TrimSpace(toolObj.Name) == "" {
				return fmt.Errorf("%s result tools[%d] missing valid tool name", method, i)
			}
		}
		return nil
	}
	return nil
}

func readBoundedLine(reader *bufio.Reader, maxBytes int) ([]byte, error) {
	var buf bytes.Buffer
	for {
		chunk, isPrefix, err := reader.ReadLine()
		if len(chunk) > 0 {
			if buf.Len()+len(chunk) > maxBytes {
				return nil, fmt.Errorf("MCP stdio JSON-RPC line exceeded %d byte limit", maxBytes)
			}
			buf.Write(chunk)
		}
		if isPrefix {
			if err != nil {
				return buf.Bytes(), err
			}
			continue
		}
		if err == nil {
			buf.WriteByte('\n')
			return buf.Bytes(), nil
		}
		return buf.Bytes(), err
	}
}

// runSessionMCPServerWithHandshake spawns the target stdio MCP server as a
// child in this wrapper's already-isolated process group and transparently
// relays stdin/stdout while recording validated initialize and tools/list completion.
func runSessionMCPServerWithHandshake(rtDir, serverName, nonce, resolvedBinary string, args []string, workDir string, env []string) error {
	// Ignore SIGPIPE so writing to a closed stdout pipe returns EPIPE instead
	// of killing the wrapper before it signals and reaps the child server.
	signal.Ignore(syscall.SIGPIPE)
	defer signal.Reset(syscall.SIGPIPE)

	pid := os.Getpid()
	status := sessionMCPHandshakeStatus{
		Server:               serverName,
		Nonce:                nonce,
		PID:                  pid,
		ProcessStartIdentity: processStartIdentity(pid),
	}
	// Reset any stale handshake file for this server immediately on startup.
	_ = writeSessionMCPHandshakeStatus(rtDir, status)

	failStartup := func(err error, msg, safeReason string) error {
		status.Failed = true
		status.Error = safeReason
		_ = writeSessionMCPHandshakeStatus(rtDir, status)
		return wrapExitError(ExitTargetFailure, msg, err)
	}

	cmd := exec.CommandContext(context.Background(), resolvedBinary, args...) // #nosec G204 -- caller-configured session MCP server executable.
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stderr = os.Stderr
	childStdin, err := cmd.StdinPipe()
	if err != nil {
		return failStartup(err, fmt.Sprintf("open stdin for session MCP server %q", serverName), "failed to open child stdin pipe")
	}
	childStdoutR, childStdoutW, err := os.Pipe()
	if err != nil {
		return failStartup(err, fmt.Sprintf("open stdout for session MCP server %q", serverName), "failed to open child stdout pipe")
	}
	defer func() { _ = childStdoutR.Close() }()
	cmd.Stdout = childStdoutW
	if err := cmd.Start(); err != nil {
		_ = childStdoutW.Close()
		return failStartup(err, fmt.Sprintf("start session MCP server %q (%q)", serverName, resolvedBinary), "failed to start server process")
	}
	_ = childStdoutW.Close()
	status.ChildPID = cmd.Process.Pid
	status.ChildStartIdentity = processStartIdentity(cmd.Process.Pid)
	_ = writeSessionMCPHandshakeStatus(rtDir, status)

	const childShutdownGrace = 2 * time.Second
	var shutdownOnce sync.Once
	var killTimerMu sync.Mutex
	var killTimer *time.Timer
	stopKillTimer := func() {
		killTimerMu.Lock()
		if killTimer != nil {
			killTimer.Stop()
		}
		killTimerMu.Unlock()
	}
	defer stopKillTimer()
	terminateChildBounded := func() {
		shutdownOnce.Do(func() {
			_ = childStdin.Close()
			if cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
			killTimerMu.Lock()
			killTimer = time.AfterFunc(childShutdownGrace, func() {
				if cmd.Process != nil {
					_ = cmd.Process.Signal(syscall.SIGKILL)
				}
				_ = childStdoutR.Close()
			})
			killTimerMu.Unlock()
		})
	}

	// Forward termination signals to the child so it is always reaped cleanly.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	stopSig := make(chan struct{})
	defer func() {
		signal.Stop(sigCh)
		close(stopSig)
	}()
	go func() {
		select {
		case sig := <-sigCh:
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		case <-stopSig:
		}
	}()

	var mu sync.Mutex
	pending := make(map[string]string) // canonical JSON-RPC ID -> method ("initialize" or "tools/list")
	publish := func() {
		_ = writeSessionMCPHandshakeStatus(rtDir, status)
	}

	// Relay os.Stdin -> childStdin, tracking initialize & tools/list requests.
	go func() {
		defer func() { _ = childStdin.Close() }()
		reader := bufio.NewReaderSize(os.Stdin, 64*1024)
		for {
			line, readErr := readBoundedLine(reader, acpMaxFrameBytes)
			if len(line) > 0 {
				var req struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
				}
				if json.Unmarshal(bytes.TrimSpace(line), &req) == nil && len(req.ID) > 0 {
					if req.Method == acpMethodInitialize || req.Method == mcpMethodToolsList {
						mu.Lock()
						pending[string(bytes.TrimSpace(req.ID))] = req.Method
						mu.Unlock()
					}
				}
				if _, writeErr := childStdin.Write(line); writeErr != nil {
					return
				}
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					mu.Lock()
					if !status.Failed {
						status.Failed = true
						status.Error = readErr.Error()
						publish()
					}
					mu.Unlock()
					terminateChildBounded()
				}
				return
			}
		}
	}()

	// Relay childStdoutR -> os.Stdout, recording validated initialize & tools/list
	// outcomes AFTER forwarding the line. On os.Stdout write failure (EPIPE) or
	// oversized frame, terminate the child with bounded SIGKILL escalation and
	// pipe closure so a SIGTERM-ignoring server or descendant can never hang Wait.
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		reader := bufio.NewReaderSize(childStdoutR, 64*1024)
		for {
			line, readErr := readBoundedLine(reader, acpMaxFrameBytes)
			if len(line) > 0 {
				if _, writeErr := os.Stdout.Write(line); writeErr != nil {
					terminateChildBounded()
					_, _ = io.Copy(io.Discard, reader)
					return
				}
				observeMCPHandshakeResponse(line, &mu, pending, &status, publish)
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					mu.Lock()
					if !status.Failed {
						status.Failed = true
						status.Error = readErr.Error()
						publish()
					}
					mu.Unlock()
					terminateChildBounded()
					_, _ = io.Copy(io.Discard, reader)
				}
				return
			}
		}
	}()

	<-stdoutDone
	waitErr := cmd.Wait()
	stopKillTimer()

	mu.Lock()
	if (!status.Initialized || !status.ToolsListed) && !status.Failed {
		status.Failed = true
		if waitErr != nil {
			status.Error = fmt.Sprintf("server exited before completing initialize/tools/list: %v", waitErr)
		} else {
			status.Error = "server exited before completing initialize/tools/list"
		}
		publish()
	}
	mu.Unlock()
	return waitErr
}

func observeMCPHandshakeResponse(line []byte, mu *sync.Mutex, pending map[string]string, status *sessionMCPHandshakeStatus, publish func()) {
	var resp struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  *acpRPCError    `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(line), &resp) != nil || len(resp.ID) == 0 || resp.Method != "" {
		return
	}
	key := string(bytes.TrimSpace(resp.ID))
	mu.Lock()
	defer mu.Unlock()
	method, tracked := pending[key]
	if !tracked {
		return
	}
	delete(pending, key)
	if resp.Error != nil {
		status.Failed = true
		status.Error = fmt.Sprintf("%s returned JSON-RPC error %d", method, resp.Error.Code)
	} else if valErr := validateMCPHandshakeResult(method, resp.Result); valErr != nil {
		status.Failed = true
		status.Error = valErr.Error()
	} else if method == acpMethodInitialize {
		status.Initialized = true
	} else if method == mcpMethodToolsList {
		status.ToolsListed = true
	}
	publish()
}

func buildSessionMCPProcessEnv(base []string, overrides map[string]string) []string {
	env := withoutSessionMCPEnvironment(base)
	if len(overrides) == 0 {
		return env
	}
	for _, key := range slices.Sorted(maps.Keys(overrides)) {
		env = clients.SetEnv(env, key, overrides[key])
	}
	return env
}

func withoutSessionMCPEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		if !strings.HasPrefix(entry, sessionMCPEnvStripPrefix) {
			result = append(result, entry)
		}
	}
	return result
}

func resolveSessionMCPCommand(command string, env []string) (string, error) {
	if strings.ContainsRune(command, filepath.Separator) || strings.ContainsRune(command, '/') {
		abs, err := filepath.Abs(command)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return "", err
		}
		if err := fsutil.RequireExecutable(abs, info); err != nil {
			return "", err
		}
		return abs, nil
	}
	pathEnv, _ := clients.GetEnv(env, "PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		if !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, command)
		info, err := os.Stat(candidate) // #nosec G703 -- PATH lookup for caller-specified session MCP server command.
		if err != nil {
			continue
		}
		if err := fsutil.RequireExecutable(candidate, info); err != nil {
			continue
		}
		return filepath.Abs(candidate)
	}
	return "", fmt.Errorf("executable %q not found in PATH", command)
}

func withSessionMCPProcsLock(rtDir string, fn func() error) error {
	acquired, err := flockAndRun(
		filepath.Join(rtDir, sessionMCPProcsLockFile),
		unix.LOCK_EX,
		"open session MCP process lock",
		"lock session MCP process list",
		fn,
	)
	if err != nil {
		return err
	}
	if !acquired {
		return exitError(ExitConfig, "session MCP process lock was not acquired")
	}
	return nil
}

func tryWithSessionMCPProcsLock(rtDir string, fn func() error) (bool, error) {
	return flockAndRun(
		filepath.Join(rtDir, sessionMCPProcsLockFile),
		unix.LOCK_EX|unix.LOCK_NB,
		"open session MCP process lock",
		"try-lock session MCP process list",
		fn,
	)
}

func registerSessionMCPProcess(rtDir string, serverName string) error {
	return withSessionMCPProcsLock(rtDir, func() error {
		if _, err := os.Lstat(filepath.Join(rtDir, sessionMCPClosedFile)); err == nil {
			return exitError(ExitUnavailable, "dispatch invocation is terminating; refusing to start session MCP server")
		} else if !errors.Is(err, fs.ErrNotExist) {
			return wrapExitError(ExitConfig, "check session MCP close fence", err)
		}
		pid := os.Getpid()
		if err := syscall.Setpgid(0, 0); err != nil {
			pgid, pgErr := syscall.Getpgid(0)
			if pgErr != nil || pgid != pid {
				return wrapExitError(ExitTargetFailure, "isolate session MCP process group", err)
			}
		}
		startIdentity := processStartIdentity(pid)
		if startIdentity == "" {
			return exitError(ExitTargetFailure, "capture session MCP process start identity")
		}
		doc, err := readSessionMCPProcsLocked(rtDir)
		if err != nil {
			return err
		}
		doc.Processes = append(doc.Processes, sessionMCPProcess{
			Server:               serverName,
			PID:                  pid,
			ProcessGroupID:       pid,
			ProcessStartIdentity: startIdentity,
		})
		if err := writeJSONAtomic(filepath.Join(rtDir, sessionMCPProcsFile), doc); err != nil {
			return wrapExitError(ExitConfig, "record session MCP process ownership", err)
		}
		return nil
	})
}

func readSessionMCPProcsLocked(rtDir string) (sessionMCPProcsDocument, error) {
	var doc sessionMCPProcsDocument
	path := filepath.Join(rtDir, sessionMCPProcsFile)
	if err := readJSON(path, &doc); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return sessionMCPProcsDocument{}, nil
		}
		return sessionMCPProcsDocument{}, wrapExitError(ExitConfig, "read session MCP process list", err)
	}
	return doc, nil
}

// closeAndTerminateSessionMCPProcesses fences any new session MCP wrapper
// starts for runDir and terminates every tracked session MCP process group,
// returning non-nil if any tracked group could not be proven stopped.
func closeAndTerminateSessionMCPProcesses(runDir string, grace time.Duration) error {
	if runDir == "" {
		return nil
	}
	rtDir, err := sessionMCPRuntimeDirPath(runDir)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(rtDir); errors.Is(statErr, fs.ErrNotExist) {
		return nil
	} else if statErr != nil {
		return wrapExitError(ExitConfig, "inspect session MCP runtime directory", statErr)
	}
	var procs []sessionMCPProcess
	err = withSessionMCPProcsLock(rtDir, func() error {
		closedPath := filepath.Join(rtDir, sessionMCPClosedFile)
		if err := publishBytes(closedPath, []byte("closed\n"), os.Rename); err != nil {
			return wrapExitError(ExitConfig, "write session MCP close fence", err)
		}
		doc, readErr := readSessionMCPProcsLocked(rtDir)
		if readErr != nil {
			return readErr
		}
		procs = doc.Processes
		return nil
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, proc := range procs {
		if err := terminateSessionMCPProcess(proc, grace); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func terminateSessionMCPProcess(proc sessionMCPProcess, grace time.Duration) error {
	if proc.PID <= 0 || proc.ProcessGroupID <= 0 || proc.PID != proc.ProcessGroupID || proc.ProcessStartIdentity == "" {
		return fmt.Errorf("session MCP server %q has invalid process group ownership identity", proc.Server)
	}
	record := RunRecord{
		PID:                  proc.PID,
		ProcessGroupID:       proc.ProcessGroupID,
		ProcessStartIdentity: proc.ProcessStartIdentity,
	}
	if providerProcessGroupReused(record) {
		return nil
	}
	if providerProcessGroupDead(proc.ProcessGroupID) && ownershipForIdentity(proc.PID, proc.ProcessStartIdentity) == ownershipDead {
		return nil
	}
	group := ownedProviderProcessGroup{
		pid:   proc.PID,
		pgid:  proc.ProcessGroupID,
		start: proc.ProcessStartIdentity,
	}
	if err := group.terminate(grace); err != nil {
		if errors.Is(err, errProviderGroupIdentityMismatch) && providerProcessGroupReused(record) {
			return nil
		}
		return fmt.Errorf("terminate session MCP server %q (pgid %d): %w", proc.Server, proc.ProcessGroupID, err)
	}
	if !providerProcessGroupDead(proc.ProcessGroupID) && !providerProcessGroupReused(record) {
		return fmt.Errorf("session MCP server %q (pgid %d) remained live after termination", proc.Server, proc.ProcessGroupID)
	}
	return nil
}

// sessionMCPProcessesStopped reports whether every tracked session MCP process
// group for runDir is provably dead (or its PID/PGID has been reused by an
// unrelated process).
func sessionMCPProcessesStopped(runDir string) bool {
	return trySessionMCPProcessesStoppedAndFenced(runDir, false)
}

func trySessionMCPProcessesStoppedAndFenced(runDir string, fence bool) bool {
	if runDir == "" {
		return true
	}
	rtDir, err := sessionMCPRuntimeDirPath(runDir)
	if err != nil {
		return false
	}
	if _, statErr := os.Stat(rtDir); errors.Is(statErr, fs.ErrNotExist) {
		return true
	}
	_, activeErr := os.Lstat(filepath.Join(rtDir, sessionMCPActiveFile))
	_, procsErr := os.Lstat(filepath.Join(rtDir, sessionMCPProcsFile))
	if errors.Is(activeErr, fs.ErrNotExist) && errors.Is(procsErr, fs.ErrNotExist) {
		return true
	}
	var doc sessionMCPProcsDocument
	stopped := false
	acquired, err := tryWithSessionMCPProcsLock(rtDir, func() error {
		if fence {
			closedPath := filepath.Join(rtDir, sessionMCPClosedFile)
			if _, statErr := os.Lstat(closedPath); errors.Is(statErr, fs.ErrNotExist) {
				if writeErr := publishBytes(closedPath, []byte("closed\n"), os.Rename); writeErr != nil {
					return writeErr
				}
			} else if statErr != nil {
				return statErr
			}
		}
		loaded, readErr := readSessionMCPProcsLocked(rtDir)
		if readErr != nil {
			return readErr
		}
		doc = loaded
		stopped = true
		return nil
	})
	if err != nil || !acquired || !stopped {
		return false
	}
	for _, proc := range doc.Processes {
		if proc.PID <= 0 || proc.ProcessGroupID <= 0 || proc.PID != proc.ProcessGroupID || proc.ProcessStartIdentity == "" {
			return false
		}
		record := RunRecord{
			PID:                  proc.PID,
			ProcessGroupID:       proc.ProcessGroupID,
			ProcessStartIdentity: proc.ProcessStartIdentity,
		}
		if providerProcessGroupReused(record) {
			continue
		}
		if !providerProcessGroupDead(proc.ProcessGroupID) {
			return false
		}
		if ownershipForIdentity(proc.PID, proc.ProcessStartIdentity) != ownershipDead {
			return false
		}
	}
	return true
}
