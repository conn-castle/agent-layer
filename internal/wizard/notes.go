package wizard

import (
	"fmt"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// approvalModeHelpText returns explanatory text for approval modes.
func approvalModeHelpText() string {
	options := ApprovalModeFieldOptions()
	lines := make([]string, 0, len(options)+2)
	lines = append(lines, messages.WizardApprovalModeHelpIntro)
	for _, option := range options {
		lines = append(lines, fmt.Sprintf(messages.WizardApprovalModeHelpLineFmt, option.Value, option.Description))
	}
	lines = append(lines, messages.WizardApprovalModeHelpSupportNote)
	return strings.Join(lines, "\n")
}

// previewModelWarningText returns the warning text shown before preview model selection.
func previewModelWarningText() string {
	return messages.WizardPreviewModelWarningText
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
