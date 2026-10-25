package vscode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/claude"
	"github.com/conn-castle/agent-layer/internal/clients/grok"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/vscodesettings"
)

var (
	lookPath = exec.LookPath
	readFile = os.ReadFile
)

// Launch starts VS Code, optionally setting CODEX_HOME, GROK_HOME, and/or
// CLAUDE_CONFIG_DIR based on the enabled agent extensions.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, passArgs []string) error {
	if err := runPreflight(cfg.Root); err != nil {
		return err
	}

	if config.IsAgentEnabled(cfg.Config.Agents.VSCode.Enabled) && config.CodexLocalConfigDirEnabled(cfg.Config.Agents.Codex) {
		codexHome := filepath.Join(cfg.Root, ".codex")
		env = clients.SetEnv(env, "CODEX_HOME", codexHome)
	}

	if config.IsAgentEnabled(cfg.Config.Agents.Grok.Enabled) {
		if err := grok.EnsureHome(cfg.Root); err != nil {
			return err
		}
		env = grok.ConfigureEnvironment(cfg.Root, env, cfg.Config.Agents.Grok, os.Stderr)
	} else {
		env = grok.ClearStaleHome(cfg.Root, env)
	}

	localConfigDir := cfg.Config.Agents.Claude.LocalConfigDir != nil && *cfg.Config.Agents.Claude.LocalConfigDir
	if config.IsAgentEnabled(cfg.Config.Agents.ClaudeVSCode.Enabled) && localConfigDir {
		claudeConfigDir := filepath.Join(cfg.Root, ".claude-config")
		env = clients.SetEnv(env, "CLAUDE_CONFIG_DIR", claudeConfigDir)
	} else {
		env = claude.ClearStaleConfigDir(cfg.Root, env)
	}

	args := append([]string{}, passArgs...)
	if !hasPositionalArg(passArgs) {
		args = append(args, ".")
	}
	cmd := exec.Command("code", args...)
	cmd.Dir = cfg.Root
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return fmt.Errorf(messages.ClientsVSCodeExitErrorFmt, err)
	}

	return nil
}

// hasPositionalArg reports whether passArgs already names a target: a file or
// folder path, stdin ("-"), or an operand after a bare "--". Separate values of
// the documented profile and storage options are not targets. Values of other
// options remain conservative and count as possible paths.
func hasPositionalArg(args []string) bool {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--":
			// Everything after bare "--" is positional.
			return i+1 < len(args)
		case "-":
			return true
		case "--user-data-dir", "--extensions-dir", "--profile", "--locale":
			// Like VS Code's parser, consume the next argument unless it looks like an option.
			if i+1 < len(args) && (args[i+1] == "-" || !strings.HasPrefix(args[i+1], "-")) {
				i++
			}
		default:
			if !strings.HasPrefix(a, "-") {
				return true
			}
		}
	}
	return false
}

func runPreflight(root string) error {
	if _, err := lookPath("code"); err != nil {
		return fmt.Errorf(messages.ClientsVSCodeCodeNotFoundFmt, err)
	}
	if err := checkManagedSettingsConflict(root); err != nil {
		return err
	}
	return nil
}

func checkManagedSettingsConflict(root string) error {
	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	content, err := readFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	// Recognize boundaries the same way sync does: complete root-level lines.
	text := strings.ReplaceAll(strings.ReplaceAll(string(content), "\r\n", "\n"), "\r", "\n")
	starts, ends := vscodesettings.ManagedMarkerLines(strings.Split(text, "\n"))
	startCount, endCount := len(starts), len(ends)
	if startCount == 0 && endCount == 0 {
		return nil
	}

	if startCount != 1 || endCount != 1 {
		reason := fmt.Sprintf("start markers=%d, end markers=%d", startCount, endCount)
		return fmt.Errorf(messages.ClientsVSCodeManagedBlockConflictFmt, settingsPath, reason)
	}

	if starts[0] > ends[0] {
		return fmt.Errorf(messages.ClientsVSCodeManagedBlockConflictFmt, settingsPath, "start marker appears after end marker")
	}

	return nil
}
