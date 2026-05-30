package messages

// Wizard prompt and UI text.
const (
	// WizardInstallPrompt prompts to install Agent Layer.
	WizardInstallPrompt                       = "Agent Layer isn't installed in this repository. Run `al init` now? (recommended)"
	WizardExitWithoutChanges                  = "No changes made."
	WizardFirstStepEscapeExitPrompt           = "Escape pressed on the first wizard step. Exit without saving changes?"
	WizardInstallComplete                     = "Installation complete. Continuing the wizard..."
	WizardApprovalModeTitle                   = "Approval Mode"
	WizardEnableAgentsTitle                   = "Enable Agents"
	WizardClaudeModelTitle                    = "Claude Model"
	WizardClaudeReasoningEffortTitle          = "Claude Reasoning Effort"
	WizardClaudeLocalConfigDirPrompt          = "Isolate Claude settings and caches per repo? (auth remains shared globally — upstream limitation)"
	WizardCodexModelTitle                     = "Codex Model"
	WizardCodexReasoningEffortTitle           = "Codex Reasoning Effort"
	WizardCodexAppsPrompt                     = "Enable Codex built-in apps (Github, Gmail, etc.)? They add extra tools to every session."
	WizardCopilotCLIModelTitle                = "Copilot CLI Model"
	WizardSecretAlreadySetPromptFmt           = "Secret %s is already set. Overwrite?"
	WizardEnvSecretFoundPromptFmt             = "Found %s in your environment. Write it to .agent-layer/.env?"
	WizardSecretInputPromptFmt                = "Enter %s (leave blank to skip)"
	WizardSecretMissingDisablePromptFmt       = "No value provided for %s. Disable MCP server %s?"
	WizardEnableWarningsPrompt                = "Enable warnings for performance and usage issues?"
	WizardInstructionTokenThresholdTitle      = "Instruction token threshold"
	WizardMCPServerThresholdTitle             = "MCP server threshold"
	WizardMCPToolsTotalThresholdTitle         = "MCP tools total threshold"
	WizardMCPServerToolsThresholdTitle        = "MCP server tools threshold"
	WizardMCPSchemaTokensTotalThresholdTitle  = "MCP schema tokens total threshold"
	WizardMCPSchemaTokensServerThresholdTitle = "MCP schema tokens server threshold"
	WizardSummaryTitle                        = "Summary of Changes"
	WizardRewritePreviewTitle                 = "Rewrite Preview"
	WizardApplyChangesPrompt                  = "Save changes to .agent-layer/config.toml and .agent-layer/.env?"
	WizardCompleted                           = "Wizard completed."
	WizardRunningSync                         = "Running sync..."
	WizardWarningFmt                          = "Warning: %s\n"
	WizardProfilePreviewHeader                = "Profile rewrite preview (.agent-layer/config.toml):"
	WizardProfilePreviewOnly                  = "Profile preview only. Re-run with --yes to apply."
	WizardProfileNoConfigChanges              = "Profile matches current config; no config changes are required."
	WizardProfileExistingConfigInvalidWarnFmt = "Warning: existing .agent-layer/config.toml is invalid TOML and will be replaced by the profile: %v"
	WizardLeaveBlankOption                    = "Leave blank (use client default)"
	WizardCustomOption                        = "Custom..."
	WizardCustomPromptFmt                     = "Custom %s"

	// WizardEnableAgentLayerInstallPrompt prompts during the fresh-install confirm
	// sequence for whether to install the Agent Layer workflow bundle (instructions,
	// memory templates, and the ~24 bundled workflow skills). The opt-out result
	// produces a minimal layout with only a placeholder 00_instructions.md.
	WizardEnableAgentLayerInstallPrompt = "Enable Agent Layer workflow bundle? (bundles ~24 workflow skills, instruction files, and memory templates)" +
		"\n  See https://agent-layer.dev/best-practices for what each bundle includes."
	// WizardEnableAgentLayerPrompt asks the same question mid-flow during a wizard
	// rerun on an existing repo. A "no" answer prunes the bundle from .agent-layer/.
	WizardEnableAgentLayerPrompt = "Keep Agent Layer workflow bundle enabled? (bundles ~24 workflow skills, instruction files, and memory templates)" +
		"\n  See https://agent-layer.dev/best-practices for what each bundle includes."
	// WizardEnableCLISkillsTitle labels the catalog multiselect screen.
	WizardEnableCLISkillsTitle = "Enable CLI skills (some require a CLI on PATH; doctor reports missing binaries)"
	// WizardEnableDefaultMCPServersTitle labels the MCP server multiselect screen.
	// The warning steers users toward CLI command-based skills for ordinary
	// CLI-backed tools, where MCP servers add tool-schema overhead and config drift.
	WizardEnableDefaultMCPServersTitle = "Enable Default MCP Servers" +
		"\n  MCP servers are not the recommended default for ordinary CLI-backed tools; prefer CLI command-based skills." +
		"\n  See https://agent-layer.dev/cli-skill-design. Do not enable both an MCP server and a CLI skill for the same tool (for example, Tavily)." +
		"\n  Unselected defaults already in config.toml are set enabled = false (the entry is kept, not deleted); missing defaults are added only when selected."
	// WizardKeepCustomMCPServersTitle labels the multiselect for MCP servers found
	// in config.toml that are not part of Agent Layer's default catalog. Selected
	// servers stay enabled; unselected servers are set to enabled = false. The
	// entry is preserved in config.toml either way — disabling never deletes it.
	WizardKeepCustomMCPServersTitle = "Keep custom MCP servers (not part of Agent Layer's defaults)" +
		"\n  Selected = keep enabled. Unselected = set enabled = false (the entry stays in config.toml; it is not deleted)."
)
