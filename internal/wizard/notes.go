package wizard

import (
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

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
