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
	f, ok := LookupField("agents.gemini.enabled")
	if !ok {
		t.Fatal("expected agents.gemini.enabled to be in catalog")
	}
	if f.Type != FieldBool {
		t.Errorf("expected FieldBool, got %s", f.Type)
	}
	if !f.Required {
		t.Error("expected agents.gemini.enabled to be required")
	}
}

func TestLookupField_EnumWithCustom(t *testing.T) {
	f, ok := LookupField("agents.gemini.model")
	if !ok {
		t.Fatal("expected agents.gemini.model to be in catalog")
	}
	if f.Type != FieldEnum {
		t.Errorf("expected FieldEnum, got %s", f.Type)
	}
	if f.Required {
		t.Error("expected agents.gemini.model to not be required")
	}
	if !f.AllowCustom {
		t.Error("expected agents.gemini.model to allow custom values")
	}
	if len(f.Options) == 0 {
		t.Error("expected agents.gemini.model to have options")
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
	values := FieldOptionValues("agents.gemini.enabled")
	if values != nil {
		t.Errorf("expected nil for bool field with no options, got %v", values)
	}
}

func TestFieldOptionValues_ClaudeModelCatalog(t *testing.T) {
	values := FieldOptionValues("agents.claude.model")
	want := []string{"default", "sonnet", "opus", "haiku", "sonnet[1m]", "opus[1m]", "opusplan"}
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
	values := FieldOptionValues("agents.claude.reasoning_effort")
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

func TestFieldOptionValues_GeminiModelCatalog(t *testing.T) {
	values := FieldOptionValues("agents.gemini.model")
	want := []string{"auto", "auto-gemini-3.1", "gemini-3.1-pro-preview", "gemini-3.1-flash", "gemini-3.1-flash-lite", "gemini-2.5-pro", "gemini-2.5-flash"}
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
	values := FieldOptionValues("agents.codex.model")
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

func TestFieldOptionValues_CopilotCliModelCatalog(t *testing.T) {
	values := FieldOptionValues("agents.copilot_cli.model")
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
