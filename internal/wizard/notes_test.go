package wizard

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasPreviewModels(t *testing.T) {
	assert.True(t, hasPreviewModels([]string{"gemini-3.1-pro-preview"}))
	assert.False(t, hasPreviewModels([]string{"gemini-3.1-pro"}))
}

func TestPreviewModelWarningText(t *testing.T) {
	text := previewModelWarningText()
	assert.Contains(t, text, "pre-release")
}
