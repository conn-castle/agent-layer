package messages

// Wizard summary output strings.
const (
	WizardSummaryApprovalsFmt                    = "Approval mode: %s\n"
	WizardSummaryEnabledAgentsHeader             = "\nEnabled Agents:\n"
	WizardSummaryEnabledMCPServersHeader         = "\nEnabled MCP Servers:\n"
	WizardSummaryNoneLoaded                      = "(none loaded)\n"
	WizardSummaryNone                            = "(none)\n"
	WizardSummaryRestoredMCPServersHeader        = "\nRestored Default MCP Servers:\n"
	WizardSummaryDisabledMCPServersHeader        = "\nDisabled MCP Servers (missing secrets):\n"
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
	WizardSummaryCodexModelReasoningFmt          = "%s (%s)"
	WizardSummaryCodexReasoningFmt               = "reasoning: %s"
	WizardSummaryClaudeLocalConfigDir            = "\nClaude credential isolation: enabled (per-repo login)\n"
)
