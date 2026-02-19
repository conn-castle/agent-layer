package wizard

import "github.com/conn-castle/agent-layer/internal/config"

// approvalModeLabel returns the display label for an approval mode option.
func approvalModeLabel(option config.FieldOption) string {
	return option.Value + " - " + option.Description
}

// approvalModeLabels returns the display labels for all approval modes.
func approvalModeLabels() []string {
	options := ApprovalModeFieldOptions()
	labels := make([]string, 0, len(options))
	for _, option := range options {
		labels = append(labels, approvalModeLabel(option))
	}
	return labels
}

// approvalModeLabelForValue returns the display label for a canonical value.
func approvalModeLabelForValue(value string) (string, bool) {
	for _, option := range ApprovalModeFieldOptions() {
		if option.Value == value {
			return approvalModeLabel(option), true
		}
	}
	return "", false
}

// approvalModeValueForLabel returns the canonical value for a display label.
func approvalModeValueForLabel(label string) (string, bool) {
	for _, option := range ApprovalModeFieldOptions() {
		if approvalModeLabel(option) == label {
			return option.Value, true
		}
	}
	return "", false
}
