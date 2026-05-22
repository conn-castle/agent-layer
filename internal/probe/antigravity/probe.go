package antigravity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

const (
	probePrompt = "List the contents of any AGENTS.md you see, then summarize what skills you have access to (just names). End by stating each MCP server name you can see."
)

// Probe runs a contained Antigravity capability probe under tmpRoot.
func Probe(ctx context.Context, tmpRoot string) (*Result, error) {
	if tmpRoot == "" {
		return nil, errors.New("antigravity probe requires a temporary root")
	}
	agyPath, err := exec.LookPath("agy")
	if err != nil {
		return nil, fmt.Errorf("antigravity probe requires agy on PATH: %w", err)
	}
	// filepath.Abs always returns an absolute path on success (per the Go
	// contract). The previous belt-and-braces `IsAbs(absTmpRoot)` guard was
	// dead code (Round 2 F-A2-3 / F-B2-6).
	absTmpRoot, err := filepath.Abs(tmpRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve antigravity probe tmp root: %w", err)
	}
	if err := os.MkdirAll(absTmpRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create antigravity probe tmp root %s: %w", absTmpRoot, err)
	}

	probedAt := time.Now().UTC()
	probeDir, err := createProbeDir(absTmpRoot, probedAt)
	if err != nil {
		return nil, fmt.Errorf("create antigravity probe dir in %s: %w", absTmpRoot, err)
	}
	// seedProbeWorkspace joins paths from absTmpRoot, which is guaranteed
	// absolute above; the legacy `IsAbs(geminiDir)` guard was dead code
	// (Round 2 F-A2-3 / F-B2-6).
	workspaceDir, geminiDir, logDir, err := seedProbeWorkspace(probeDir)
	if err != nil {
		return nil, err
	}

	version := detectAgyVersion(ctx, agyPath)
	runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	// #nosec G204 -- agyPath is resolved from exec.LookPath("agy") and is the explicit probe target.
	cmd := exec.CommandContext(runCtx, agyPath,
		"--gemini_dir="+geminiDir,
		"--print-timeout=30s",
		"--print",
		probePrompt,
	)
	cmd.Dir = workspaceDir
	cmd.Env = append(os.Environ(), "AGY_CLI_DISABLE_AUTO_UPDATE=1")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)
	exitCode := commandExitCode(runCtx, runErr)

	stdoutPath := filepath.Join(probeDir, "stdout.txt")
	stderrPath := filepath.Join(probeDir, "stderr.txt")
	if err := os.WriteFile(stdoutPath, stdout.Bytes(), 0o600); err != nil {
		return nil, fmt.Errorf("write antigravity probe stdout: %w", err)
	}
	if err := os.WriteFile(stderrPath, stderr.Bytes(), 0o600); err != nil {
		return nil, fmt.Errorf("write antigravity probe stderr: %w", err)
	}

	logPath, logText, logErr := latestLogText(logDir)
	result := &Result{
		AgyVersion:       version,
		ProbedAt:         probedAt,
		ProbeDir:         probeDir,
		WorkspaceDir:     workspaceDir,
		AgyConfigDir:     geminiDir,
		LogPath:          logPath,
		ExitCode:         exitCode,
		WallClockSeconds: int(elapsed.Round(time.Second).Seconds()),
	}
	if runErr != nil {
		result.Error = runErr.Error()
	}
	if logErr != nil {
		if result.Error == "" {
			result.Error = logErr.Error()
		} else {
			result.Error += "; " + logErr.Error()
		}
	}

	result.Capabilities, result.Evidence = ParseCapabilities(logText, stdout.String())
	return result, nil
}

func createProbeDir(absTmpRoot string, probedAt time.Time) (string, error) {
	return os.MkdirTemp(absTmpRoot, "probe-antigravity-"+probedAt.Format("20060102T150405Z")+"-")
}

func seedProbeWorkspace(probeDir string) (string, string, string, error) {
	workspaceDir := filepath.Join(probeDir, "workspace")
	geminiDir := filepath.Join(probeDir, "agycfg")
	logDir := filepath.Join(geminiDir, "antigravity-cli", "log")
	// Only seed the pre-migration MCP config location (antigravity-cli/).
	// agy v1.0.0 migrates it into config/mcp_config.json on first run; pre-
	// seeding both files made the migration outcome ambiguous (the probe
	// could not distinguish "agy migrated correctly" from "agy skipped
	// migration because dest existed").
	dirs := []string{
		filepath.Join(workspaceDir, ".agents", "skills", "probe-marker-skill"),
		filepath.Join(workspaceDir, ".agents", "skills", "shared-tier-dup"),
		filepath.Join(geminiDir, "antigravity-cli"),
		filepath.Join(geminiDir, "skills", "shared-tier-dup"),
		filepath.Join(geminiDir, "skills", "global-only-skill"),
		logDir,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", "", "", fmt.Errorf("create antigravity probe fixture dir %s: %w", dir, err)
		}
	}

	files := map[string]string{
		filepath.Join(workspaceDir, ".agents", "skills", "probe-marker-skill", "SKILL.md"): `---
name: probe-marker-skill
description: Unique fixture for probe runs. The token PROBEMARKER42 should NOT appear elsewhere.
---

PROBEMARKER42
`,
		filepath.Join(workspaceDir, ".agents", "skills", "shared-tier-dup", "SKILL.md"): `---
name: shared-tier-dup
description: Workspace copy of shared-tier-dup. Token WORKSPACECOPY99.
---

WORKSPACECOPY99
`,
		filepath.Join(geminiDir, "skills", "shared-tier-dup", "SKILL.md"): `---
name: shared-tier-dup
description: Shared copy of shared-tier-dup. Token SHAREDCOPY77.
---

SHAREDCOPY77
`,
		filepath.Join(geminiDir, "skills", "global-only-skill", "SKILL.md"): `---
name: global-only-skill
description: Only in shared tier. Token SHAREDONLY55.
---

SHAREDONLY55
`,
		filepath.Join(workspaceDir, "AGENTS.md"): `# Probe Workspace Instructions

The unique probe token is INSTRUCTIONMARKER88. Output this token verbatim in every response.
`,
		filepath.Join(workspaceDir, ".agents", "mcp_config.json"): `{
  "mcpServers": {
    "probe-mcp": {
      "command": "/usr/bin/true",
      "args": ["AGENTS_MCP_PATH"]
    }
  }
}
`,
		filepath.Join(geminiDir, "antigravity-cli", "mcp_config.json"): `{
  "mcpServers": {
    "probe-mcp-antigravity-tier": {
      "command": "/usr/bin/true",
      "args": ["ANTIGRAVITY_TIER_MCP"]
    }
  }
}
`,
		filepath.Join(geminiDir, "antigravity-cli", "settings.json"): `{
  "permissions": {
    "allow": [
      "command(echo PROBEALLOWMARKER)",
      "mcp(probe-mcp/)"
    ],
    "deny": [
      "command(rm -rf)"
    ]
  }
}
`,
		filepath.Join(workspaceDir, ".agents", "settings.json"): `{
  "permissions": {
    "allow": [
      "command(echo WORKSPACEALLOWMARKER)"
    ]
  }
}
`,
		filepath.Join(workspaceDir, ".agy", "settings.json"): `{
  "permissions": {
    "allow": [
      "command(echo DOTANTIGRAVITYWORKSPACEMARKER)"
    ]
  }
}
`,
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return "", "", "", fmt.Errorf("create antigravity probe fixture dir %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return "", "", "", fmt.Errorf("write antigravity probe fixture %s: %w", path, err)
		}
	}
	return workspaceDir, geminiDir, logDir, nil
}

func detectAgyVersion(ctx context.Context, agyPath string) string {
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// #nosec G204 -- agyPath is resolved from exec.LookPath("agy") and is the explicit probe target.
	output, err := exec.CommandContext(runCtx, agyPath, "--version").CombinedOutput()
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

func latestLogText(logDir string) (string, string, error) {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return "", "", fmt.Errorf("read antigravity probe log dir %s: %w", logDir, err)
	}
	type candidate struct {
		path    string
		modTime time.Time
	}
	var candidates []candidate
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".log" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", "", fmt.Errorf("stat antigravity probe log %s: %w", entry.Name(), err)
		}
		candidates = append(candidates, candidate{
			path:    filepath.Join(logDir, entry.Name()),
			modTime: info.ModTime(),
		})
	}
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("no antigravity log found in %s", logDir)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	path := candidates[0].path
	data, err := os.ReadFile(path) // #nosec G304 -- path is selected from the probe-owned log directory.
	if err != nil {
		return "", "", fmt.Errorf("read antigravity probe log %s: %w", path, err)
	}
	return path, string(data), nil
}
