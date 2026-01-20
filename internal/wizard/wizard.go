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

const leaveBlankOption = "Leave blank (use client default)"

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
	agentConfigs := []agentEnabledConfig{
		{id: AgentGemini, enabled: cfg.Config.Agents.Gemini.Enabled},
		{id: AgentClaude, enabled: cfg.Config.Agents.Claude.Enabled},
		{id: AgentCodex, enabled: cfg.Config.Agents.Codex.Enabled},
		{id: AgentVSCode, enabled: cfg.Config.Agents.VSCode.Enabled},
		{id: AgentAntigravity, enabled: cfg.Config.Agents.Antigravity.Enabled},
	}
	setEnabledAgentsFromConfig(choices.EnabledAgents, agentConfigs)

	// Models
	choices.GeminiModel = cfg.Config.Agents.Gemini.Model
	choices.ClaudeModel = cfg.Config.Agents.Claude.Model
	choices.CodexModel = cfg.Config.Agents.Codex.Model
	choices.CodexReasoning = cfg.Config.Agents.Codex.ReasoningEffort

	// MCP Servers
	for _, srv := range cfg.Config.MCP.Servers {
		if srv.Enabled != nil && *srv.Enabled {
			choices.EnabledMCPServers[srv.ID] = true
		}
	}

	// 5. UI Flow

	// Approvals
	if err := ui.Note("Approval Modes", approvalModeHelpText()); err != nil {
		return err
	}
	if err := ui.Select("Approval Mode", ApprovalModes, &choices.ApprovalMode); err != nil {
		return err
	}
	choices.ApprovalModeTouched = true

	// Agents
	enabledAgents := enabledAgentIDs(choices.EnabledAgents)
	if err := ui.MultiSelect("Enable Agents", SupportedAgents, &enabledAgents); err != nil {
		return err
	}
	// Update map
	choices.EnabledAgents = agentIDSet(enabledAgents)
	choices.EnabledAgentsTouched = true

	// Models (for enabled agents)
	if choices.EnabledAgents[AgentGemini] {
		if hasPreviewModels(GeminiModels) {
			if err := ui.Note("Preview Model Warning", previewModelWarningText()); err != nil {
				return err
			}
		}
		if err := selectOptionalValue(ui, "Gemini Model", GeminiModels, &choices.GeminiModel); err != nil {
			return err
		}
		choices.GeminiModelTouched = true
	}
	if choices.EnabledAgents[AgentClaude] {
		if err := selectOptionalValue(ui, "Claude Model", ClaudeModels, &choices.ClaudeModel); err != nil {
			return err
		}
		choices.ClaudeModelTouched = true
	}
	if choices.EnabledAgents[AgentCodex] {
		if err := selectOptionalValue(ui, "Codex Model", CodexModels, &choices.CodexModel); err != nil {
			return err
		}
		choices.CodexModelTouched = true

		if err := selectOptionalValue(ui, "Codex Reasoning Effort", CodexReasoningEfforts, &choices.CodexReasoning); err != nil {
			return err
		}
		choices.CodexReasoningTouched = true
	}

	// MCP Servers
	missingDefaults := missingDefaultMCPServers(cfg.Config.MCP.Servers)
	if len(missingDefaults) > 0 {
		choices.MissingDefaultMCPServers = missingDefaults
		restore := true
		if err := ui.Confirm(fmt.Sprintf("Default MCP server entries are missing from config.toml: %s. Restore them before selection?", strings.Join(missingDefaults, ", ")), &restore); err != nil {
			return err
		}
		choices.RestoreMissingMCPServers = restore
	}
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
	envValues := make(map[string]string)
	if b, err := os.ReadFile(envPath); err == nil {
		parsed, err := ParseEnv(string(b))
		if err != nil {
			return err
		}
		envValues = parsed
	} else if !os.IsNotExist(err) {
		return err
	}

	for _, srv := range KnownDefaultMCPServers {
		if choices.EnabledMCPServers[srv.ID] {
			key := srv.RequiredEnv

			// Check if already in file
			if _, ok := envValues[key]; ok {
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
				choices.DisabledMCPServers[srv.ID] = true
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

	agents := agentSummaryLines(c)
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

	restoredMCP := restoredMCPServers(c)
	if len(restoredMCP) > 0 {
		sb.WriteString("\nRestored Default MCP Servers:\n")
		for _, m := range restoredMCP {
			sb.WriteString(fmt.Sprintf("- %s\n", m))
		}
	}

	disabledMCP := disabledMCPServers(c)
	sb.WriteString("\nDisabled MCP Servers (missing secrets):\n")
	if len(disabledMCP) > 0 {
		for _, m := range disabledMCP {
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

type agentEnabledConfig struct {
	id      string
	enabled *bool
}

func setEnabledAgentsFromConfig(dest map[string]bool, configs []agentEnabledConfig) {
	for _, cfg := range configs {
		if cfg.enabled != nil && *cfg.enabled {
			dest[cfg.id] = true
		}
	}
}

func enabledAgentIDs(enabled map[string]bool) []string {
	ids := make([]string, 0, len(enabled))
	for id, isEnabled := range enabled {
		if isEnabled {
			ids = append(ids, id)
		}
	}
	return ids
}

func agentIDSet(ids []string) map[string]bool {
	enabled := make(map[string]bool, len(ids))
	for _, id := range ids {
		enabled[id] = true
	}
	return enabled
}

// selectOptionalValue prompts for an optional selection and updates value.
// title and options define the prompt; value holds the current selection.
func selectOptionalValue(ui UI, title string, options []string, value *string) error {
	selection := *value
	if selection == "" {
		selection = leaveBlankOption
	}
	opts := append([]string{leaveBlankOption}, options...)
	if err := ui.Select(title, opts, &selection); err != nil {
		return err
	}
	if selection == leaveBlankOption {
		*value = ""
		return nil
	}
	*value = selection
	return nil
}

func agentSummaryLines(c *Choices) []string {
	var agents []string
	for _, agent := range SupportedAgents {
		if !c.EnabledAgents[agent] {
			continue
		}
		modelSummary := agentModelSummary(agent, c)
		if modelSummary == "" {
			agents = append(agents, fmt.Sprintf("- %s", agent))
			continue
		}
		agents = append(agents, fmt.Sprintf("- %s: %s", agent, modelSummary))
	}
	return agents
}

func agentModelSummary(agent string, c *Choices) string {
	switch agent {
	case AgentGemini:
		return c.GeminiModel
	case AgentClaude:
		return c.ClaudeModel
	case AgentCodex:
		return codexModelSummary(c)
	default:
		return ""
	}
}

func codexModelSummary(c *Choices) string {
	if c.CodexModel != "" && c.CodexReasoning != "" {
		return fmt.Sprintf("%s (%s)", c.CodexModel, c.CodexReasoning)
	}
	if c.CodexModel != "" {
		return c.CodexModel
	}
	if c.CodexReasoning != "" {
		return fmt.Sprintf("reasoning: %s", c.CodexReasoning)
	}
	return ""
}

// disabledMCPServers returns sorted IDs of servers disabled due to missing secrets.
// c is the current wizard choices; returns nil when none are disabled.
func disabledMCPServers(c *Choices) []string {
	if len(c.DisabledMCPServers) == 0 {
		return nil
	}
	ids := make([]string, 0, len(c.DisabledMCPServers))
	for _, srv := range KnownDefaultMCPServers {
		if c.DisabledMCPServers[srv.ID] {
			ids = append(ids, srv.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// restoredMCPServers returns IDs of default servers being restored to config.toml.
// c is the current wizard choices; returns nil when no restoration is requested.
func restoredMCPServers(c *Choices) []string {
	if !c.RestoreMissingMCPServers || len(c.MissingDefaultMCPServers) == 0 {
		return nil
	}
	ids := make([]string, 0, len(c.MissingDefaultMCPServers))
	ids = append(ids, c.MissingDefaultMCPServers...)
	return ids
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
	if err := writeFileAtomic(configPath, []byte(newConfig), 0644); err != nil {
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
	if err := writeFileAtomic(envPath, []byte(newEnv), 0600); err != nil {
		return fmt.Errorf("failed to write .env: %w", err)
	}

	// Sync
	fmt.Println("Running sync...")
	return sync.Run(root)
}
