package messages

// Wizard help and descriptive text.
const (
	WizardApprovalModeHelpIntro       = "Approval modes control what runs without prompts:"
	WizardApprovalModeHelpLineFmt     = "- %s: %s"
	WizardApprovalModeHelpSupportNote = "Support varies by client; Agent Layer applies the closest available behavior."
	WizardPreviewModelWarningText     = "Preview models are pre-release and can change or be removed without notice."
	WizardApprovalAllDescription      = "Auto-approve shell commands and MCP tool calls (where supported)."
	WizardApprovalMCPDescription      = "Auto-approve MCP tool calls only; commands still prompt."
	WizardApprovalCommandsDescription = "Auto-approve shell commands only; MCP tools still prompt."
	WizardApprovalNoneDescription     = "Prompt for everything."
	WizardApprovalYOLODescription     = "YOLO: skip ALL permission prompts (use only in sandboxed/ephemeral environments)."
)
