package grok

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/conn-castle/agent-layer/internal/clients"
	clientgrok "github.com/conn-castle/agent-layer/internal/clients/grok"
	probeantigravity "github.com/conn-castle/agent-layer/internal/probe/antigravity"
	"github.com/conn-castle/agent-layer/internal/tomlpatch"
)

const (
	probePrompt              = "Call the " + probeantigravity.FixtureToolName + " tool and report exactly what it returns."
	probeRunTimeout          = 45 * time.Second
	maxStreamingJSONLineSize = 16 * 1024 * 1024
)

// Probe runs a contained Grok capability probe under tmpRoot. When authHome
// contains Grok credentials, only auth.json is copied into the disposable
// home for the duration of the provider process.
func Probe(ctx context.Context, tmpRoot string, authHome string) (*Result, error) {
	if tmpRoot == "" {
		return nil, errors.New("grok probe requires a temporary root")
	}
	grokPath, err := exec.LookPath("grok")
	if err != nil {
		return nil, fmt.Errorf("grok probe requires grok on PATH: %w", err)
	}
	absTmpRoot, err := filepath.Abs(tmpRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve grok probe tmp root: %w", err)
	}
	if err := os.MkdirAll(absTmpRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create grok probe tmp root %s: %w", absTmpRoot, err)
	}

	probedAt := time.Now().UTC()
	probeDir, err := os.MkdirTemp(absTmpRoot, "probe-grok-"+probedAt.Format("20060102T150405Z")+"-")
	if err != nil {
		return nil, fmt.Errorf("create grok probe dir in %s: %w", absTmpRoot, err)
	}

	workspaceDir, grokHome, promptPath, markerPath, err := seedGrokProbeWorkspace(probeDir)
	if err != nil {
		return nil, err
	}
	authPath, err := seedProbeAuthentication(authHome, grokHome)
	if err != nil {
		return nil, err
	}
	if authPath != "" {
		defer func() { _ = os.Remove(authPath) }()
	}

	version := detectGrokVersion(ctx, grokPath)
	runCtx, cancel := context.WithTimeout(ctx, probeRunTimeout)
	defer cancel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	// #nosec G204 -- grokPath is resolved from exec.LookPath("grok") and is the explicit probe target.
	cmd := exec.CommandContext(runCtx, grokPath,
		"--no-auto-update",
		"--trust",
		"--sandbox", "workspace",
		"--always-approve",
		"--no-memory",
		"--no-subagents",
		"--output-format", "streaming-json",
		"--prompt-file", promptPath,
	)
	cmd.Dir = workspaceDir
	cmd.Env = clients.SetEnv(os.Environ(), clientgrok.EnvHome, grokHome)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)
	exitCode := commandExitCode(runCtx, runErr)
	if authPath != "" {
		if err := os.Remove(authPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("remove disposable grok probe credentials: %w", err)
		}
		authPath = ""
	}

	if err := os.WriteFile(filepath.Join(probeDir, "stdout.txt"), stdout.Bytes(), 0o600); err != nil {
		return nil, fmt.Errorf("write grok probe stdout: %w", err)
	}
	if err := os.WriteFile(filepath.Join(probeDir, "stderr.txt"), stderr.Bytes(), 0o600); err != nil {
		return nil, fmt.Errorf("write grok probe stderr: %w", err)
	}

	result := &Result{
		GrokVersion:      version,
		ProbedAt:         probedAt,
		ProbeDir:         probeDir,
		WorkspaceDir:     workspaceDir,
		GrokHomeDir:      grokHome,
		ExitCode:         exitCode,
		WallClockSeconds: int(elapsed.Round(time.Second).Seconds()),
		TimedOut:         errors.Is(runCtx.Err(), context.DeadlineExceeded),
	}
	if runErr != nil {
		result.Error = runErr.Error()
	}

	if streamErr := validateStreamingJSON(&stdout); streamErr == nil {
		result.Capabilities.StreamingJSONUsed = true
		result.Evidence = append(result.Evidence, "grok stdout contained a valid streaming-json sequence ending with a completed turn")
	} else {
		result.Evidence = append(result.Evidence, "invalid grok streaming-json output: "+streamErr.Error())
		if runErr == nil {
			result.Error = streamErr.Error()
		}
	}
	if invoked := inspectFixtureMarker(markerPath); invoked {
		result.Capabilities.MCPToolInvoked = true
		result.Evidence = append(result.Evidence, "mcp fixture: "+probeantigravity.FixtureToolName+" was invoked by the client")
	}
	return result, nil
}

// PreferredAuthHome finds an existing login without importing any other Grok
// home state. Explicit GROK_HOME wins, followed by this repo's managed home and
// the default user home.
func PreferredAuthHome(repoRoot string) string {
	candidates := make([]string, 0, 3)
	if value := os.Getenv(clientgrok.EnvHome); value != "" {
		candidates = append(candidates, value)
	}
	if repoRoot != "" {
		candidates = append(candidates, clientgrok.HomeDir(repoRoot))
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".grok"))
	}
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		clean := filepath.Clean(candidate)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		if info, err := os.Stat(filepath.Join(clean, "auth.json")); err == nil && info.Mode().IsRegular() {
			return clean
		}
	}
	return ""
}

func seedProbeAuthentication(sourceHome string, disposableHome string) (string, error) {
	if sourceHome == "" {
		return "", nil
	}
	source := filepath.Join(sourceHome, "auth.json")
	info, err := os.Stat(source)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect grok probe authentication: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("grok probe authentication must be a regular file: %s", source)
	}
	data, err := os.ReadFile(source) // #nosec G304 -- sourceHome is an explicitly selected Grok home.
	if err != nil {
		return "", fmt.Errorf("read grok probe authentication: %w", err)
	}
	destination := filepath.Join(disposableHome, "auth.json")
	if err := os.WriteFile(destination, data, 0o600); err != nil { // #nosec G703 -- destination is inside the probe-owned disposable home.
		return "", fmt.Errorf("write disposable grok probe authentication: %w", err)
	}
	return destination, nil
}

func validateStreamingJSON(reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxStreamingJSONLineSize)
	seenEvent := false
	seenEnd := false
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if seenEnd {
			return fmt.Errorf("streaming-json event found after terminal end event on line %d", lineNumber)
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			return fmt.Errorf("decode streaming-json line %d: %w", lineNumber, err)
		}
		eventType, ok := event["type"].(string)
		if !ok || eventType == "" {
			return fmt.Errorf("streaming-json line %d has no string type", lineNumber)
		}
		seenEvent = true
		switch eventType {
		case "error":
			return fmt.Errorf("grok streaming-json reported an error event")
		case "end":
			stopReason, _ := event["stopReason"].(string)
			if !clientgrok.IsSuccessfulStopReason(stopReason) {
				return fmt.Errorf("grok streaming-json ended with incompatible stop reason %q", stopReason)
			}
			sessionID, _ := event["sessionId"].(string)
			if sessionID == "" {
				return errors.New("grok streaming-json end event omitted sessionId")
			}
			seenEnd = true
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read grok streaming-json: %w", err)
	}
	if !seenEvent {
		return errors.New("grok streaming-json output was empty")
	}
	if !seenEnd {
		return errors.New("grok streaming-json output omitted a terminal end event")
	}
	return nil
}

func seedGrokProbeWorkspace(probeDir string) (workspaceDir, grokHome, promptPath, markerPath string, err error) {
	workspaceDir = filepath.Join(probeDir, "workspace")
	grokHome = filepath.Join(probeDir, "grok-home")
	markerPath = filepath.Join(workspaceDir, "mcp-tool-invoked.txt")
	promptPath = filepath.Join(probeDir, "prompt.txt")
	for _, dir := range []string{workspaceDir, grokHome, filepath.Join(workspaceDir, ".grok")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", "", "", "", fmt.Errorf("create grok probe workspace: %w", err)
		}
	}

	executable, err := os.Executable()
	if err != nil {
		return "", "", "", "", fmt.Errorf("resolve grok probe MCP fixture command: %w", err)
	}
	config := fmt.Sprintf("[mcp_servers.probe]\ncommand = %q\nargs = [%q]\nenv = { %q = %q }\n",
		executable, "__probe-mcp-fixture", probeantigravity.FixtureMarkerEnvVar, markerPath)
	if err := os.WriteFile(filepath.Join(workspaceDir, ".grok", "config.toml"), []byte(config), 0o600); err != nil {
		return "", "", "", "", fmt.Errorf("write grok probe project config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "AGENTS.md"), []byte("Probe workspace.\n"), 0o600); err != nil {
		return "", "", "", "", fmt.Errorf("write grok probe AGENTS.md: %w", err)
	}
	if err := os.WriteFile(promptPath, []byte(probePrompt+"\n"), 0o600); err != nil {
		return "", "", "", "", fmt.Errorf("write grok probe prompt: %w", err)
	}

	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return "", "", "", "", fmt.Errorf("resolve grok probe workspace: %w", err)
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(absWorkspace)
	if err != nil {
		return "", "", "", "", fmt.Errorf("resolve grok probe workspace symlinks: %w", err)
	}
	trust := fmt.Sprintf("[folders.%s]\ntrusted = true\ndecided_at = %d\n", tomlpatch.FormatKey(canonicalWorkspace), time.Now().Unix())
	if err := os.WriteFile(filepath.Join(grokHome, "trusted_folders.toml"), []byte(trust), 0o600); err != nil {
		return "", "", "", "", fmt.Errorf("write grok probe trust file: %w", err)
	}
	return workspaceDir, grokHome, promptPath, markerPath, nil
}

func inspectFixtureMarker(markerPath string) bool {
	data, err := os.ReadFile(markerPath) // #nosec G304 -- probe-owned path.
	if err != nil {
		return false
	}
	return string(bytes.TrimSpace(data)) == probeantigravity.FixtureToolReply
}

func detectGrokVersion(ctx context.Context, grokPath string) string {
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// #nosec G204 -- grokPath is resolved from exec.LookPath("grok") and is the explicit probe target.
	output, err := exec.CommandContext(runCtx, grokPath, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(output))
}

func commandExitCode(ctx context.Context, err error) int {
	if ctx.Err() != nil {
		return 124
	}
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
