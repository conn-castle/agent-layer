package config

import "github.com/conn-castle/agent-layer/internal/messages"

// FieldType classifies the kind of value a config field accepts.
type FieldType string

const (
	// FieldBool accepts true or false.
	FieldBool FieldType = "bool"
	// FieldEnum accepts one of a fixed set of options.
	FieldEnum FieldType = "enum"
	// FieldFreetext accepts arbitrary string input.
	FieldFreetext FieldType = "freetext"
	// FieldPositiveInt accepts a positive integer.
	FieldPositiveInt FieldType = "positive_int"
)

// FieldOption describes a single selectable value for a field.
type FieldOption struct {
	Value       string
	Description string // empty for options without descriptions
}

// FieldDef describes a single config field's type, constraints, and valid options.
type FieldDef struct {
	Key         string
	Type        FieldType
	Required    bool
	Options     []FieldOption
	AllowCustom bool // when true, enum fields also accept freetext values
}

// fields is the canonical ordered registry of all config fields with constrained values.
// Order matches the wizard UI flow (approval → agents → models).
var fields = []FieldDef{
	{
		Key:      "approvals.mode",
		Type:     FieldEnum,
		Required: true,
		Options: []FieldOption{
			{Value: "all", Description: messages.WizardApprovalAllDescription},
			{Value: "mcp", Description: messages.WizardApprovalMCPDescription},
			{Value: "commands", Description: messages.WizardApprovalCommandsDescription},
			{Value: "none", Description: messages.WizardApprovalNoneDescription},
			{Value: "yolo", Description: messages.WizardApprovalYOLODescription},
		},
	},
	{Key: "agents.gemini.enabled", Type: FieldBool, Required: true},
	{
		Key:         "agents.gemini.model",
		Type:        FieldEnum,
		AllowCustom: true,
		Options: []FieldOption{
			{Value: "auto"},
			{Value: "auto-gemini-3.1"},
			{Value: "gemini-3.1-pro-preview"},
			{Value: "gemini-3.1-flash"},
			{Value: "gemini-2.5-pro"},
			{Value: "gemini-2.5-flash"},
			{Value: "gemini-2.0-pro"},
			{Value: "gemini-2.0-flash"},
		},
	},
	{Key: "agents.claude.enabled", Type: FieldBool, Required: true},
	{
		Key:         "agents.claude.model",
		Type:        FieldEnum,
		AllowCustom: true,
		Options: []FieldOption{
			{Value: "default"},
			{Value: "sonnet"},
			{Value: "opus"},
			{Value: "haiku"},
			{Value: "sonnet[1m]"},
			{Value: "opusplan"},
		},
	},
	{
		Key:  "agents.claude.reasoning_effort",
		Type: FieldEnum,
		Options: []FieldOption{
			{Value: "low"},
			{Value: "medium"},
			{Value: "high"},
		},
	},
	{Key: "agents.claude-vscode.enabled", Type: FieldBool, Required: true},
	{Key: "agents.codex.enabled", Type: FieldBool, Required: true},
	{
		Key:         "agents.codex.model",
		Type:        FieldEnum,
		AllowCustom: true,
		Options: []FieldOption{
			{Value: "gpt-5.3-codex-spark"},
			{Value: "gpt-5.3-codex"},
			{Value: "gpt-5.2"},
			{Value: "gpt-5.2-mini"},
			{Value: "gpt-5.1"},
			{Value: "gpt-5.1-mini"},
			{Value: "gpt-5"},
		},
	},
	{
		Key:         "agents.codex.reasoning_effort",
		Type:        FieldEnum,
		AllowCustom: true,
		Options: []FieldOption{
			{Value: "minimal"},
			{Value: "low"},
			{Value: "medium"},
			{Value: "high"},
			{Value: "xhigh"},
		},
	},
	{Key: "agents.vscode.enabled", Type: FieldBool, Required: true},
	{Key: "agents.antigravity.enabled", Type: FieldBool, Required: true},
}

// fieldIndex provides O(1) lookup by key.
var fieldIndex = buildFieldIndex()

func buildFieldIndex() map[string]int {
	idx := make(map[string]int, len(fields))
	for i, f := range fields {
		idx[f.Key] = i
	}
	return idx
}

// LookupField returns the field definition for the given config key.
// Returns false when the key is not in the catalog.
func LookupField(key string) (FieldDef, bool) {
	i, ok := fieldIndex[key]
	if !ok {
		return FieldDef{}, false
	}
	return copyFieldDef(fields[i]), true
}

// Fields returns a copy of all registered field definitions in catalog order.
func Fields() []FieldDef {
	out := make([]FieldDef, len(fields))
	for i, f := range fields {
		out[i] = copyFieldDef(f)
	}
	return out
}

// FieldOptionValues returns the option values for a field as a plain string slice.
// Returns nil when the key is not in the catalog or has no options.
func FieldOptionValues(key string) []string {
	f, ok := LookupField(key)
	if !ok || len(f.Options) == 0 {
		return nil
	}
	values := make([]string, len(f.Options))
	for i, opt := range f.Options {
		values[i] = opt.Value
	}
	return values
}

// copyFieldDef returns a deep copy of a FieldDef so callers cannot mutate the registry.
func copyFieldDef(f FieldDef) FieldDef {
	if len(f.Options) > 0 {
		opts := make([]FieldOption, len(f.Options))
		copy(opts, f.Options)
		f.Options = opts
	}
	return f
}
