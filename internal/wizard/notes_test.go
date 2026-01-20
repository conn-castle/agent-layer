package wizard

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApprovalModeHelpTextIncludesModes(t *testing.T) {
	text := approvalModeHelpText()

	assert.Contains(t, text, "all")
	assert.Contains(t, text, "mcp")
	assert.Contains(t, text, "commands")
	assert.Contains(t, text, "none")
}

func TestHasPreviewModels(t *testing.T) {
	assert.True(t, hasPreviewModels([]string{"gemini-3-pro-preview"}))
	assert.False(t, hasPreviewModels([]string{"gemini-2.5-pro"}))
}

func TestPreviewModelWarningText(t *testing.T) {
	text := previewModelWarningText()
	assert.Contains(t, text, "pre-release")
}
