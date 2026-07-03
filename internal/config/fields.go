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

const (
	// AntigravityModelFieldKey is the canonical config path for agy's model display
	// string. Sync projects this typed Agent Layer setting into Antigravity's
	// generated settings.json.
	AntigravityModelFieldKey = "agents.antigravity.model"
	// ClaudeModelFieldKey is the canonical config path for Claude Code model aliases.
	ClaudeModelFieldKey = "agents.claude.model"
	// ClaudeReasoningEffortFieldKey is the canonical config path for Claude Code effort.
	ClaudeReasoningEffortFieldKey = "agents.claude.reasoning_effort"
	// CodexModelFieldKey is the canonical config path for Codex model selection.
	CodexModelFieldKey = "agents.codex.model"
	// CodexReasoningEffortFieldKey is the canonical config path for Codex reasoning effort.
	CodexReasoningEffortFieldKey = "agents.codex.reasoning_effort"
	// CopilotCLIModelFieldKey is the canonical config path for Copilot CLI model selection.
	CopilotCLIModelFieldKey = "agents.copilot_cli.model"
)

var (
	antigravityModelOptions = fieldOptions(
		"Gemini 3.5 Flash (Medium)",
		"Gemini 3.5 Flash (High)",
		"Gemini 3.5 Flash (Low)",
		"Gemini 3.1 Pro (Low)",
		"Gemini 3.1 Pro (High)",
		"Claude Sonnet 4.6 (Thinking)",
		"Claude Opus 4.6 (Thinking)",
		"GPT-OSS 120B (Medium)",
	)
	claudeModelOptions = fieldOptions(
		"default",
		"sonnet",
		"opus",
		"haiku",
		"fable",
		"sonnet[1m]",
		"opus[1m]",
		"opusplan",
	)
	claudeReasoningEffortOptions = fieldOptions("low", "medium", "high", "xhigh", "max")
	codexModelOptions            = fieldOptions("gpt-5.4", "gpt-5.3-codex-spark", "gpt-5.3-codex", "gpt-5.2", "gpt-5.2-mini")
	codexReasoningEffortOptions  = fieldOptions("minimal", "low", "medium", "high", "xhigh")
	copilotCLIModelOptions       = fieldOptions("claude-opus-4.6", "claude-sonnet-4.6", "claude-haiku-4.5", "gpt-5.4", "gpt-5.3-codex", "gemini-3-pro")
)

// fields is the canonical ordered registry of all config fields with constrained values.
// Order matches the wizard UI flow (approval → agents → models).
var fields = []FieldDef{
	{
		Key:      "approvals.mode",
		Type:     FieldEnum,
		Required: true,
		Options: []FieldOption{
			{Value: ApprovalModeAll, Description: messages.WizardApprovalAllDescription},
			{Value: ApprovalModeMCP, Description: messages.WizardApprovalMCPDescription},
			{Value: ApprovalModeCommands, Description: messages.WizardApprovalCommandsDescription},
			{Value: ApprovalModeNone, Description: messages.WizardApprovalNoneDescription},
			{Value: ApprovalModeYOLO, Description: messages.WizardApprovalYOLODescription},
		},
	},
	{Key: "dispatch.max_depth", Type: FieldPositiveInt},
	{Key: "notifications.chime", Type: FieldBool},
	{Key: "agents.antigravity.enabled", Type: FieldBool, Required: true},
	{
		Key:         AntigravityModelFieldKey,
		Type:        FieldEnum,
		AllowCustom: true,
		Options:     antigravityModelOptions,
	},
	{
		Key:     "agents.antigravity.dispatch.default_agent",
		Type:    FieldEnum,
		Options: dispatchDefaultAgentOptions(),
	},
	{Key: "agents.claude.enabled", Type: FieldBool, Required: true},
	{
		Key:         ClaudeModelFieldKey,
		Type:        FieldEnum,
		AllowCustom: true,
		Options:     claudeModelOptions,
	},
	{
		Key:         ClaudeReasoningEffortFieldKey,
		Type:        FieldEnum,
		AllowCustom: true,
		Options:     claudeReasoningEffortOptions,
	},
	{
		Key:     "agents.claude.dispatch.default_agent",
		Type:    FieldEnum,
		Options: dispatchDefaultAgentOptions(),
	},
	// statusline is explicit opt-in and is surfaced in the wizard. It remains in
	// the field catalog so upgrade migrations render clean true/false prompts.
	{Key: "agents.claude.statusline", Type: FieldBool},
	{Key: "agents.claude_vscode.enabled", Type: FieldBool, Required: true},
	{Key: "agents.codex.enabled", Type: FieldBool, Required: true},
	{
		Key:         CodexModelFieldKey,
		Type:        FieldEnum,
		AllowCustom: true,
		Options:     codexModelOptions,
	},
	{
		Key:         CodexReasoningEffortFieldKey,
		Type:        FieldEnum,
		AllowCustom: true,
		Options:     codexReasoningEffortOptions,
	},
	{
		Key:     "agents.codex.dispatch.default_agent",
		Type:    FieldEnum,
		Options: dispatchDefaultAgentOptions(),
	},
	{Key: "agents.codex.local_config_dir", Type: FieldBool},
	// statusline is explicit opt-in and is surfaced in the wizard. It remains in
	// the field catalog so upgrade migrations render clean true/false prompts.
	{Key: "agents.codex.statusline", Type: FieldBool},
	{Key: "agents.vscode.enabled", Type: FieldBool, Required: true},
	{Key: "agents.copilot_cli.enabled", Type: FieldBool, Required: true},
	{
		Key:         CopilotCLIModelFieldKey,
		Type:        FieldEnum,
		AllowCustom: true,
		Options:     copilotCLIModelOptions,
	},
}

func fieldOptions(values ...string) []FieldOption {
	options := make([]FieldOption, len(values))
	for i, value := range values {
		options[i] = FieldOption{Value: value}
	}
	return options
}

func dispatchDefaultAgentOptions() []FieldOption {
	return []FieldOption{
		{Value: "random"},
		{Value: "codex"},
		{Value: "claude"},
		{Value: "antigravity"},
	}
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
