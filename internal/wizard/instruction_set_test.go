package wizard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestInstructionSetLabelRoundTrip(t *testing.T) {
	for _, option := range instructionSetOptions() {
		label, ok := instructionSetLabelForValue(option.value)
		require.True(t, ok, option.value)
		assert.Equal(t, option.label, label)
		got, ok := instructionSetValueForLabel(label)
		require.True(t, ok, label)
		assert.Equal(t, option.value, got)
	}
}

func TestInstructionSetLabelForValueDefaultsEmptyToNone(t *testing.T) {
	label, ok := instructionSetLabelForValue("")
	require.True(t, ok)
	assert.Equal(t, messages.WizardInstructionSetNone, label)
}

func TestInstructionSetUnknownValues(t *testing.T) {
	_, ok := instructionSetLabelForValue("not-a-set")
	assert.False(t, ok)
	_, ok = instructionSetValueForLabel("not a label")
	assert.False(t, ok)
}
