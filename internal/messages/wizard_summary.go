package messages

// Wizard summary output strings.
const (
	WizardSummaryApprovalsFmt                    = "Approval mode: %s\n"
	WizardSummaryEnabledAgentsHeader             = "\nEnabled Agents:\n"
	WizardSummaryEnabledMCPServersHeader         = "\nEnabled MCP Servers:\n"
	WizardSummaryNoneLoaded                      = "(none loaded)\n"
	WizardSummaryNone                            = "(none)\n"
	WizardSummaryDisabledMCPServersHeader        = "\nDisabled MCP Servers (missing secrets):\n"
	WizardSummaryDisabledCustomMCPServersHeader  = "\nDisabled Custom MCP Servers (entry kept, enabled = false):\n"
	WizardSummarySecretsHeader                   = "\nSecrets to Update:\n"
	WizardSummaryWarningsHeader                  = "\nWarnings:\n"
	WizardSummaryWarningsDisabled                = "(disabled)\n"
	WizardSummaryListItemFmt                     = "- %s\n"
	WizardSummaryWarningInstructionTokenFmt      = "- instruction_token_threshold = %d\n"
	WizardSummaryWarningMCPServerFmt             = "- mcp_server_threshold = %d\n"
	WizardSummaryWarningMCPToolsTotalFmt         = "- mcp_tools_total_threshold = %d\n"
	WizardSummaryWarningMCPServerToolsFmt        = "- mcp_server_tools_threshold = %d\n"
	WizardSummaryWarningMCPSchemaTokensTotalFmt  = "- mcp_schema_tokens_total_threshold = %d\n"
	WizardSummaryWarningMCPSchemaTokensServerFmt = "- mcp_schema_tokens_server_threshold = %d\n"
	WizardSummaryAgentFmt                        = "- %s"
	WizardSummaryAgentModelFmt                   = "- %s: %s"
	WizardSummaryModelReasoningFmt               = "%s (%s)"
	WizardSummaryReasoningFmt                    = "reasoning: %s"
	WizardSummaryClaudeLocalConfigDir            = "\nClaude config isolation: enabled (per-repo settings and caches; auth shared globally — upstream limitation)\n"
	WizardSummaryCodexAppsDisabled               = "\nCodex built-in apps: disabled (suppresses Github/Gmail/etc. tool surface)\n"
	WizardSummaryCodexAppsEnabled                = "\nCodex built-in apps: enabled\n"
	// Disable-toggle summary lines. Each is emitted only when the toggle is on
	// (the feature is disabled); leaving a toggle off keeps the client default
	// and prints nothing.
	WizardSummaryCodexBrowserDisabled       = "\nCodex browser/computer-use: disabled\n"
	WizardSummaryClaudeIDEReadingDisabled   = "\nClaude IDE open-file reading: disabled\n"
	WizardSummaryClaudeMemoryDisabled       = "\nClaude memory: disabled (auto-memory off; does not affect CLAUDE.md)\n"
	WizardSummaryClaudeConnectorsDisabled   = "\nClaude connectors: disabled\n"
	WizardSummaryClaudeQuestionToolDisabled = "\nClaude AskUserQuestion tool: disabled (permissions.deny + PreToolUse hook)\n"
)
