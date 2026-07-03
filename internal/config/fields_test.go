package config

import (
	"strings"
	"testing"
)

func TestLookupField_KnownKey(t *testing.T) {
	f, ok := LookupField("approvals.mode")
	if !ok {
		t.Fatal("expected approvals.mode to be in catalog")
	}
	if f.Type != FieldEnum {
		t.Errorf("expected FieldEnum, got %s", f.Type)
	}
	if !f.Required {
		t.Error("expected approvals.mode to be required")
	}
	if len(f.Options) == 0 {
		t.Error("expected approvals.mode to have options")
	}
}

func TestLookupField_UnknownKey(t *testing.T) {
	_, ok := LookupField("nonexistent.field")
	if ok {
		t.Error("expected unknown key to return false")
	}
}

func TestLookupField_BoolField(t *testing.T) {
	f, ok := LookupField("agents.antigravity.enabled")
	if !ok {
		t.Fatal("expected agents.antigravity.enabled to be in catalog")
	}
	if f.Type != FieldBool {
		t.Errorf("expected FieldBool, got %s", f.Type)
	}
	if !f.Required {
		t.Error("expected agents.antigravity.enabled to be required")
	}
}

func TestLookupField_CodexLocalConfigDirOptionalBool(t *testing.T) {
	f, ok := LookupField("agents.codex.local_config_dir")
	if !ok {
		t.Fatal("expected agents.codex.local_config_dir to be in catalog")
	}
	if f.Type != FieldBool {
		t.Errorf("expected FieldBool, got %s", f.Type)
	}
	if f.Required {
		t.Error("expected agents.codex.local_config_dir to be optional")
	}
}

func TestLookupField_NotificationsChimeOptionalBool(t *testing.T) {
	f, ok := LookupField("notifications.chime")
	if !ok {
		t.Fatal("expected notifications.chime to be in catalog")
	}
	if f.Type != FieldBool {
		t.Errorf("expected FieldBool, got %s", f.Type)
	}
	if f.Required {
		t.Error("expected notifications.chime to be optional")
	}
}

func TestLookupField_EnumWithCustom(t *testing.T) {
	f, ok := LookupField(ClaudeModelFieldKey)
	if !ok {
		t.Fatal("expected agents.claude.model to be in catalog")
	}
	if f.Type != FieldEnum {
		t.Errorf("expected FieldEnum, got %s", f.Type)
	}
	if f.Required {
		t.Error("expected agents.claude.model to not be required")
	}
	if !f.AllowCustom {
		t.Error("expected agents.claude.model to allow custom values")
	}
	if len(f.Options) == 0 {
		t.Error("expected agents.claude.model to have options")
	}
}

func TestFieldsCoversValidApprovals(t *testing.T) {
	// Ensure the catalog's approvals.mode options exactly match the old
	// validApprovals map that was used in validate.go.
	expected := map[string]struct{}{
		ApprovalModeAll:      {},
		ApprovalModeMCP:      {},
		ApprovalModeCommands: {},
		ApprovalModeNone:     {},
		ApprovalModeYOLO:     {},
	}
	f, ok := LookupField("approvals.mode")
	if !ok {
		t.Fatal("approvals.mode not in catalog")
	}
	got := make(map[string]struct{}, len(f.Options))
	for _, opt := range f.Options {
		got[opt.Value] = struct{}{}
	}
	for k := range expected {
		if _, ok := got[k]; !ok {
			t.Errorf("catalog missing approval mode %q", k)
		}
	}
	for k := range got {
		if _, ok := expected[k]; !ok {
			t.Errorf("catalog has unexpected approval mode %q", k)
		}
	}
}

func TestFieldOptionValues(t *testing.T) {
	values := FieldOptionValues("approvals.mode")
	if len(values) != 5 {
		t.Fatalf("expected 5 approval mode values, got %d", len(values))
	}
	if values[0] != ApprovalModeAll {
		t.Errorf("expected first value to be %q, got %q", ApprovalModeAll, values[0])
	}
}

func TestFieldOptionValues_UnknownKey(t *testing.T) {
	values := FieldOptionValues("nonexistent.key")
	if values != nil {
		t.Errorf("expected nil for unknown key, got %v", values)
	}
}

func TestFieldOptionValues_BoolField(t *testing.T) {
	values := FieldOptionValues("agents.antigravity.enabled")
	if values != nil {
		t.Errorf("expected nil for bool field with no options, got %v", values)
	}
}

func TestLookupField_DispatchMaxDepth(t *testing.T) {
	f, ok := LookupField("dispatch.max_depth")
	if !ok {
		t.Fatal("expected dispatch.max_depth to be in catalog")
	}
	if f.Type != FieldPositiveInt {
		t.Fatalf("dispatch.max_depth type = %s, want %s", f.Type, FieldPositiveInt)
	}
	if f.Required {
		t.Fatal("dispatch.max_depth should not be required")
	}
}

func TestFieldOptionValues_ClaudeModelCatalog(t *testing.T) {
	values := FieldOptionValues(ClaudeModelFieldKey)
	want := []string{"default", "sonnet", "opus", "haiku", "fable"}
	if len(values) != len(want) {
		t.Fatalf("expected %d values, got %d (%v)", len(want), len(values), values)
	}
	for i, expected := range want {
		if values[i] != expected {
			t.Fatalf("value at index %d = %q, want %q", i, values[i], expected)
		}
	}
}

func TestFieldOptionValues_AntigravityModelCatalog(t *testing.T) {
	values := FieldOptionValues("agents.antigravity.model")
	want := []string{
		"Gemini 3.5 Flash (Medium)",
		"Gemini 3.5 Flash (High)",
		"Gemini 3.5 Flash (Low)",
		"Gemini 3.1 Pro (Low)",
		"Gemini 3.1 Pro (High)",
		"Claude Sonnet 4.6 (Thinking)",
		"Claude Opus 4.6 (Thinking)",
		"GPT-OSS 120B (Medium)",
	}
	if len(values) != len(want) {
		t.Fatalf("expected %d values, got %d (%v)", len(want), len(values), values)
	}
	for i, expected := range want {
		if values[i] != expected {
			t.Fatalf("value at index %d = %q, want %q", i, values[i], expected)
		}
	}
}

func TestFieldOptionValues_ClaudeReasoningCatalog(t *testing.T) {
	values := FieldOptionValues(ClaudeReasoningEffortFieldKey)
	want := []string{"low", "medium", "high", "xhigh", "max"}
	if len(values) != len(want) {
		t.Fatalf("expected %d values, got %d (%v)", len(want), len(values), values)
	}
	for i, expected := range want {
		if values[i] != expected {
			t.Fatalf("value at index %d = %q, want %q", i, values[i], expected)
		}
	}
}

func TestFieldOptionValues_CodexModelCatalog(t *testing.T) {
	values := FieldOptionValues(CodexModelFieldKey)
	want := []string{"gpt-5.4", "gpt-5.3-codex-spark", "gpt-5.3-codex", "gpt-5.2", "gpt-5.2-mini"}
	if len(values) != len(want) {
		t.Fatalf("expected %d values, got %d (%v)", len(want), len(values), values)
	}
	for i, expected := range want {
		if values[i] != expected {
			t.Fatalf("value at index %d = %q, want %q", i, values[i], expected)
		}
	}
}

func TestFieldOptionValues_DispatchDefaultAgentCatalog(t *testing.T) {
	keys := []string{
		"agents.antigravity.dispatch.default_agent",
		"agents.claude.dispatch.default_agent",
		"agents.codex.dispatch.default_agent",
	}
	want := []string{"random", "codex", "claude", "antigravity"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			values := FieldOptionValues(key)
			if len(values) != len(want) {
				t.Fatalf("expected %d values, got %d (%v)", len(want), len(values), values)
			}
			for i, expected := range want {
				if values[i] != expected {
					t.Fatalf("value at index %d = %q, want %q", i, values[i], expected)
				}
			}
		})
	}
}

func TestFieldOptionValues_CopilotCliModelCatalog(t *testing.T) {
	values := FieldOptionValues(CopilotCLIModelFieldKey)
	want := []string{"claude-opus-4.6", "claude-sonnet-4.6", "claude-haiku-4.5", "gpt-5.4", "gpt-5.3-codex", "gemini-3-pro"}
	if len(values) != len(want) {
		t.Fatalf("expected %d values, got %d (%v)", len(want), len(values), values)
	}
	for i, expected := range want {
		if values[i] != expected {
			t.Fatalf("value at index %d = %q, want %q", i, values[i], expected)
		}
	}
}

func TestFieldsCopySemantics(t *testing.T) {
	all := Fields()
	if len(all) == 0 {
		t.Fatal("Fields() returned empty")
	}
	// Mutate the returned slice; verify the registry is unaffected.
	all[0].Key = "mutated"
	original, ok := LookupField("approvals.mode")
	if !ok {
		t.Fatal("LookupField failed after mutation")
	}
	if original.Key == "mutated" {
		t.Error("mutation of Fields() result affected the registry")
	}
}

func TestFieldsCopySemantics_Options(t *testing.T) {
	f, _ := LookupField("approvals.mode")
	if len(f.Options) == 0 {
		t.Fatal("no options to test")
	}
	f.Options[0].Value = "mutated"
	f2, _ := LookupField("approvals.mode")
	if f2.Options[0].Value == "mutated" {
		t.Error("mutation of LookupField result affected the registry")
	}
}

func TestAllRequiredBoolFieldsAreAgentEnabled(t *testing.T) {
	for _, f := range Fields() {
		if f.Type == FieldBool && f.Required {
			if !strings.HasSuffix(f.Key, ".enabled") {
				t.Errorf("required bool field %q does not end with .enabled", f.Key)
			}
			if !strings.HasPrefix(f.Key, "agents.") {
				t.Errorf("required bool field %q does not start with agents.", f.Key)
			}
		}
	}
}

func TestFieldsRegistryConsistency(t *testing.T) {
	seen := make(map[string]struct{})
	for _, f := range fields {
		if f.Key == "" {
			t.Error("field with empty key")
		}
		if _, dup := seen[f.Key]; dup {
			t.Errorf("duplicate field key %q", f.Key)
		}
		seen[f.Key] = struct{}{}
		if f.Type == "" {
			t.Errorf("field %q has empty type", f.Key)
		}
	}
}
