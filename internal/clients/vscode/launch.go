package vscode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
)

const (
	vscodeSettingsManagedStart = "// >>> agent-layer"
	vscodeSettingsManagedEnd   = "// <<< agent-layer"
)

var (
	lookPath = exec.LookPath
	readFile = os.ReadFile
)

// Launch starts VS Code with CODEX_HOME set for the repo.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, passArgs []string) error {
	if err := runPreflight(cfg.Root); err != nil {
		return err
	}

	codexHome := filepath.Join(cfg.Root, ".codex")
	env = clients.SetEnv(env, "CODEX_HOME", codexHome)

	args := append([]string{}, passArgs...)
	args = append(args, ".")
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

	text := string(content)
	startCount := strings.Count(text, vscodeSettingsManagedStart)
	endCount := strings.Count(text, vscodeSettingsManagedEnd)
	if startCount == 0 && endCount == 0 {
		return nil
	}

	if startCount != 1 || endCount != 1 {
		reason := fmt.Sprintf("start markers=%d, end markers=%d", startCount, endCount)
		return fmt.Errorf(messages.ClientsVSCodeManagedBlockConflictFmt, settingsPath, reason)
	}

	startIndex := strings.Index(text, vscodeSettingsManagedStart)
	endIndex := strings.Index(text, vscodeSettingsManagedEnd)
	if startIndex > endIndex {
		return fmt.Errorf(messages.ClientsVSCodeManagedBlockConflictFmt, settingsPath, "start marker appears after end marker")
	}

	return nil
}
