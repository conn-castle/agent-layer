package wizard

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/term"

	"github.com/nicholasjconn/agent-layer/internal/config"
	"github.com/nicholasjconn/agent-layer/internal/install"
	"github.com/nicholasjconn/agent-layer/internal/sync"
)

// Run starts the interactive wizard.
func Run(ctx context.Context, root string) error {
	// 1. Interactive check
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("al wizard is interactive-only. Run it from a terminal")
	}

	ui := NewHuhUI()
	configPath := filepath.Join(root, ".agent-layer", "config.toml")

	// 2. Install gating
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		confirm := true
		err := ui.Confirm("Agent Layer is not installed in this repo. Run 'al install' now? (recommended)", &confirm)
		if err != nil {
			return err
		}
		if !confirm {
			fmt.Println("Exiting without changes.")
			return nil
		}

		// Run install
		if err := install.Run(root, install.Options{Overwrite: false}); err != nil {
			return fmt.Errorf("install failed: %w", err)
		}
		fmt.Println("Installation complete. Continuing wizard...")
	}

	// 3. Load config
	cfg, err := config.LoadProjectConfig(root)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// 4. Initialize choices from config
	choices := NewChoices()

	// Approvals
	choices.ApprovalMode = cfg.Config.Approvals.Mode
	if choices.ApprovalMode == "" {
		choices.ApprovalMode = ApprovalAll
	}

	// Agents
	if cfg.Config.Agents.Gemini.Enabled != nil && *cfg.Config.Agents.Gemini.Enabled {
		choices.EnabledAgents[AgentGemini] = true
	}
	if cfg.Config.Agents.Claude.Enabled != nil && *cfg.Config.Agents.Claude.Enabled {
		choices.EnabledAgents[AgentClaude] = true
	}
	if cfg.Config.Agents.Codex.Enabled != nil && *cfg.Config.Agents.Codex.Enabled {
		choices.EnabledAgents[AgentCodex] = true
	}
	if cfg.Config.Agents.VSCode.Enabled != nil && *cfg.Config.Agents.VSCode.Enabled {
		choices.EnabledAgents[AgentVSCode] = true
	}
	if cfg.Config.Agents.Antigravity.Enabled != nil && *cfg.Config.Agents.Antigravity.Enabled {
		choices.EnabledAgents[AgentAntigravity] = true
	}

	// Models
	choices.GeminiModel = cfg.Config.Agents.Gemini.Model
	if choices.GeminiModel == "" {
		choices.GeminiModel = "auto"
	}
	choices.ClaudeModel = cfg.Config.Agents.Claude.Model
	if choices.ClaudeModel == "" {
		choices.ClaudeModel = "default"
	}
	choices.CodexModel = cfg.Config.Agents.Codex.Model
	if choices.CodexModel == "" {
		choices.CodexModel = "gpt-5.2-codex"
	}
	choices.CodexReasoning = cfg.Config.Agents.Codex.ReasoningEffort
	if choices.CodexReasoning == "" {
		choices.CodexReasoning = "xhigh"
	}

	// MCP Servers
	for _, srv := range cfg.Config.MCP.Servers {
		if srv.Enabled != nil && *srv.Enabled {
			choices.EnabledMCPServers[srv.ID] = true
		}
	}

	// 5. UI Flow

	// Approvals
	if err := ui.Select("Approval Mode", ApprovalModes, &choices.ApprovalMode); err != nil {
		return err
	}
	choices.ApprovalModeTouched = true

	// Agents
	var enabledAgents []string
	for a, enabled := range choices.EnabledAgents {
		if enabled {
			enabledAgents = append(enabledAgents, a)
		}
	}
	if err := ui.MultiSelect("Enable Agents", SupportedAgents, &enabledAgents); err != nil {
		return err
	}
	// Update map
	choices.EnabledAgents = make(map[string]bool)
	for _, a := range enabledAgents {
		choices.EnabledAgents[a] = true
	}
	choices.EnabledAgentsTouched = true

	// Models (for enabled agents)
	if choices.EnabledAgents[AgentGemini] {
		if err := ui.Select("Gemini Model", GeminiModels, &choices.GeminiModel); err != nil {
			return err
		}
		choices.GeminiModelTouched = true
	}
	if choices.EnabledAgents[AgentClaude] {
		if err := ui.Select("Claude Model", ClaudeModels, &choices.ClaudeModel); err != nil {
			return err
		}
		choices.ClaudeModelTouched = true
	}
	if choices.EnabledAgents[AgentCodex] {
		if err := ui.Select("Codex Model", CodexModels, &choices.CodexModel); err != nil {
			return err
		}
		choices.CodexModelTouched = true

		if err := ui.Select("Codex Reasoning Effort", CodexReasoningEfforts, &choices.CodexReasoning); err != nil {
			return err
		}
		choices.CodexReasoningTouched = true
	}

	// MCP Servers
	var defaultServerIDs []string
	var enabledDefaultServers []string
	for _, s := range KnownDefaultMCPServers {
		defaultServerIDs = append(defaultServerIDs, s.ID)
		if choices.EnabledMCPServers[s.ID] {
			enabledDefaultServers = append(enabledDefaultServers, s.ID)
		}
	}
	if err := ui.MultiSelect("Enable Default MCP Servers", defaultServerIDs, &enabledDefaultServers); err != nil {
		return err
	}
	// Only update known defaults in the map
	for _, s := range KnownDefaultMCPServers {
		choices.EnabledMCPServers[s.ID] = false // Reset known ones
	}
	for _, id := range enabledDefaultServers {
		choices.EnabledMCPServers[id] = true
	}
	choices.EnabledMCPServersTouched = true

	// Secrets
	// Load existing env to know what's set
	envPath := filepath.Join(root, ".agent-layer", ".env")
	envContent := ""
	if b, err := os.ReadFile(envPath); err == nil {
		envContent = string(b)
	}

	for _, srv := range KnownDefaultMCPServers {
		if choices.EnabledMCPServers[srv.ID] {
			key := srv.RequiredEnv

			// Check if already in file
			if strings.Contains(envContent, key+"=") || strings.Contains(envContent, key+" =") {
				override := false
				if err := ui.Confirm(fmt.Sprintf("Secret %s is already set. Override?", key), &override); err != nil {
					return err
				}
				if !override {
					continue
				}
			} else {
				// Check environment
				if val := os.Getenv(key); val != "" {
					useEnv := false
					if err := ui.Confirm(fmt.Sprintf("%s found in your environment. Write to .agent-layer/.env?", key), &useEnv); err != nil {
						return err
					}
					if useEnv {
						choices.Secrets[key] = val
						continue
					}
				}
			}

			// Prompt input
			var val string
			if err := ui.SecretInput(fmt.Sprintf("Enter %s (leave blank to skip)", key), &val); err != nil {
				return err
			}
			if val != "" {
				choices.Secrets[key] = val
			} else {
				// Warn and disable
				choices.EnabledMCPServers[srv.ID] = false
				// We don't have a simple way to show a warning without pausing, but we can note it in summary
			}
		}
	}

	// 6. Summary
	summary := buildSummary(choices)
	confirmApply := true
	if err := ui.Note("Summary of Changes", summary); err != nil {
		return err
	}
	if err := ui.Confirm("Apply these changes?", &confirmApply); err != nil {
		return err
	}
	if !confirmApply {
		fmt.Println("Exiting without changes.")
		return nil
	}

	// 7. Apply
	if err := applyChanges(root, configPath, envPath, choices); err != nil {
		return err
	}

	fmt.Println("Wizard completed successfully.")
	return nil
}

func buildSummary(c *Choices) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Approvals Mode: %s\n", c.ApprovalMode))

	var agents []string
	for a, e := range c.EnabledAgents {
		if e {
			mod := ""
			if a == AgentGemini {
				mod = c.GeminiModel
			}
			if a == AgentClaude {
				mod = c.ClaudeModel
			}
			if a == AgentCodex {
				mod = fmt.Sprintf("%s (%s)", c.CodexModel, c.CodexReasoning)
			}
			if mod != "" {
				agents = append(agents, fmt.Sprintf("- %s: %s", a, mod))
			} else {
				agents = append(agents, fmt.Sprintf("- %s", a))
			}
		}
	}
	sort.Strings(agents)
	sb.WriteString("\nEnabled Agents:\n")
	for _, a := range agents {
		sb.WriteString(a + "\n")
	}

	var mcp []string
	for _, s := range KnownDefaultMCPServers {
		if c.EnabledMCPServers[s.ID] {
			mcp = append(mcp, s.ID)
		}
	}
	sb.WriteString("\nEnabled MCP Servers:\n")
	if len(mcp) > 0 {
		for _, m := range mcp {
			sb.WriteString(fmt.Sprintf("- %s\n", m))
		}
	} else {
		sb.WriteString("(none)\n")
	}

	sb.WriteString("\nSecrets to Update:\n")
	if len(c.Secrets) > 0 {
		for k := range c.Secrets {
			sb.WriteString(fmt.Sprintf("- %s\n", k))
		}
	} else {
		sb.WriteString("(none)\n")
	}

	return sb.String()
}

func applyChanges(root, configPath, envPath string, c *Choices) error {
	// Config
	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	// Backup
	if err := os.WriteFile(configPath+".bak", rawConfig, 0644); err != nil {
		return fmt.Errorf("failed to backup config: %w", err)
	}
	// Patch
	newConfig, err := PatchConfig(string(rawConfig), c)
	if err != nil {
		return fmt.Errorf("failed to patch config: %w", err)
	}
	if err := os.WriteFile(configPath, []byte(newConfig), 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// Env
	// Backup if exists
	rawEnv, err := os.ReadFile(envPath)
	if err == nil {
		if err := os.WriteFile(envPath+".bak", rawEnv, 0600); err != nil {
			return fmt.Errorf("failed to backup .env: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	// Patch
	newEnv := PatchEnv(string(rawEnv), c.Secrets)
	if err := os.WriteFile(envPath, []byte(newEnv), 0600); err != nil {
		return fmt.Errorf("failed to write .env: %w", err)
	}

	// Sync
	fmt.Println("Running sync...")
	return sync.Run(root)
}
