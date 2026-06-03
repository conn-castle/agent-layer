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
	WizardApplyChangesPrompt                  = "Apply these config, secret, skills, instructions, memory-file, and statusline-source changes?"
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

	// WizardEnableAgentLayerPrompt asks whether to install or refresh the Agent
	// Layer workflow bundle. A "no" answer leaves existing files unchanged.
	WizardEnableAgentLayerPrompt = "Install or refresh the Agent Layer workflow bundle? (refreshes ~24 workflow skills and managed instruction files; creates missing memory docs/templates)" +
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

	// Per-feature checkbox labels for the model-step multi-selects. Each label is
	// the load-bearing option identity: MultiSelect uses the option string as both
	// label and returned value, and the wizard matches the returned selection on
	// these exact strings to invert checkbox state back into the disable-sense
	// Choices fields. Define each label ONCE here and reuse it on both the
	// option-list and contains-check sides so the identity cannot drift.
	WizardClaudeFeatureStatuslineLabel   = "Claude statusline"
	WizardClaudeFeatureIDEReadingLabel   = "IDE open-file reading"
	WizardClaudeFeatureMemoryLabel       = "Auto-memory"
	WizardClaudeFeatureConnectorsLabel   = "claude.ai connectors"
	WizardClaudeFeatureQuestionToolLabel = "AskUserQuestion tool"
	WizardCodexFeatureStatuslineLabel    = "Codex statusline"
	WizardCodexFeatureAppsLabel          = "Built-in apps (GitHub, Gmail, etc.)"
	WizardCodexFeatureBrowserLabel       = "Browser / computer-use"

	// WizardClaudeFeaturesTitle labels the Claude feature multi-select. Checked =
	// keep the feature enabled (Claude Code's native default); unchecking disables
	// it. The per-feature explanations live in the title because MultiSelect has no
	// per-option description field (same multi-line convention as the MCP/CLI-skills
	// titles above).
	WizardClaudeFeaturesTitle = "Claude features (checked = keep enabled; uncheck to disable)" +
		"\n  Claude statusline: creates .agent-layer/claude-statusline.sh if missing, then sync wires Claude Code statusLine." +
		"\n  IDE open-file reading: Claude Code otherwise auto-connects to your IDE and reads files open in the editor." +
		"\n  Auto-memory: Claude Code's auto-memory persists notes across sessions. (This does not affect CLAUDE.md.)" +
		"\n  claude.ai connectors: claude.ai app connectors load only under Claude.ai-subscription auth." +
		"\n  AskUserQuestion tool: Claude Code's structured clarification-question tool; disabling blocks it via permissions.deny and a PreToolUse hook (the hook also enforces it under YOLO)."
	// WizardCodexFeaturesTitle labels the Codex feature multi-select. Checked =
	// keep the feature enabled; unchecking disables it. Built-in apps default to
	// unchecked (Agent Layer disables Codex's app surface by default and always
	// writes an explicit features.apps).
	WizardCodexFeaturesTitle = "Codex features (checked = keep enabled; uncheck to disable)" +
		"\n  Codex statusline: creates .agent-layer/codex-statusline.toml if missing, then sync merges [tui].status_line." +
		"\n  Built-in apps (GitHub, Gmail, etc.): Codex's built-in app integrations add extra tools to every session." +
		"\n  Browser / computer-use: these tools let Codex drive a browser and control the screen."
)
