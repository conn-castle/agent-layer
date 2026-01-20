package wizard

import "strings"

// approvalModeHelpText returns explanatory text for approval modes.
func approvalModeHelpText() string {
	lines := []string{
		"Approval modes control what runs without prompts:",
		"- all: auto-approve shell commands and MCP tool calls (where supported).",
		"- mcp: auto-approve MCP tool calls only; commands still prompt.",
		"- commands: auto-approve shell commands only; MCP tools still prompt.",
		"- none: prompt for everything.",
		"Support varies by client; Agent Layer applies the closest available behavior.",
	}
	return strings.Join(lines, "\n")
}

// previewModelWarningText returns the warning text shown before preview model selection.
func previewModelWarningText() string {
	return "Preview models are pre-release and can change or be removed without notice."
}

// hasPreviewModels reports whether any model option looks like a preview release.
func hasPreviewModels(options []string) bool {
	for _, option := range options {
		if strings.Contains(option, "preview") {
			return true
		}
	}
	return false
}
