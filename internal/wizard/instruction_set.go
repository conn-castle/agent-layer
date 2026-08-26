package wizard

import "github.com/conn-castle/agent-layer/internal/messages"

// InstructionSet is the wizard's always-on instruction choice.
type InstructionSet string

// Canonical instruction-set values. None leaves files unchanged; Rules seeds
// 00_rules.md; RulesAndMemory also seeds 01_memory.md and memory docs.
const (
	InstructionSetNone           InstructionSet = "none"
	InstructionSetRules          InstructionSet = "rules"
	InstructionSetRulesAndMemory InstructionSet = "rules_and_memory"
)

type instructionSetOption struct {
	value InstructionSet
	label string
}

func instructionSetOptions() []instructionSetOption {
	return []instructionSetOption{
		{value: InstructionSetNone, label: messages.WizardInstructionSetNone},
		{value: InstructionSetRules, label: messages.WizardInstructionSetRules},
		{value: InstructionSetRulesAndMemory, label: messages.WizardInstructionSetRulesAndMemory},
	}
}

func instructionSetLabels() []string {
	options := instructionSetOptions()
	labels := make([]string, 0, len(options))
	for _, option := range options {
		labels = append(labels, option.label)
	}
	return labels
}

func instructionSetLabelForValue(value InstructionSet) (string, bool) {
	if value == "" {
		value = InstructionSetNone
	}
	for _, option := range instructionSetOptions() {
		if option.value == value {
			return option.label, true
		}
	}
	return "", false
}

func instructionSetValueForLabel(label string) (InstructionSet, bool) {
	for _, option := range instructionSetOptions() {
		if option.label == label {
			return option.value, true
		}
	}
	return "", false
}
