package wizard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptedUIAppliesAnswersAndRequiresAllUsed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	data := `{
  "select": {"Mode": "none"},
  "multi_select": {"Agents": ["claude"]},
  "confirm": {"Apply": true},
  "input": {"Model": "sonnet"},
  "secret_input": {"Secret": "token"}
}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write answers: %v", err)
	}
	ui, err := NewScriptedUIFromFile(path)
	if err != nil {
		t.Fatalf("load answers: %v", err)
	}

	selected := "all"
	if err := ui.Select("Mode", []string{"all", "none"}, &selected); err != nil {
		t.Fatalf("select: %v", err)
	}
	if selected != "none" {
		t.Fatalf("selected = %q, want none", selected)
	}
	multi := []string{"codex"}
	if err := ui.MultiSelect("Agents", []string{"claude", "codex"}, &multi); err != nil {
		t.Fatalf("multi-select: %v", err)
	}
	if len(multi) != 1 || multi[0] != "claude" {
		t.Fatalf("multi = %#v, want [claude]", multi)
	}
	confirm := false
	if err := ui.Confirm("Apply", &confirm); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !confirm {
		t.Fatal("confirm = false, want true")
	}
	input := ""
	if err := ui.Input("Model", &input); err != nil {
		t.Fatalf("input: %v", err)
	}
	if input != "sonnet" {
		t.Fatalf("input = %q, want sonnet", input)
	}
	secret := ""
	if err := ui.SecretInput("Secret", &secret); err != nil {
		t.Fatalf("secret: %v", err)
	}
	if secret != "token" {
		t.Fatalf("secret = %q, want token", secret)
	}
	if err := ui.Note("Summary", "body"); err != nil {
		t.Fatalf("note: %v", err)
	}
	if err := ui.AssertComplete(); err != nil {
		t.Fatalf("answers should be complete: %v", err)
	}
}

func TestScriptedUIMissingAnswerFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(path, []byte(`{"confirm":{}}`), 0o600); err != nil {
		t.Fatalf("write answers: %v", err)
	}
	ui, err := NewScriptedUIFromFile(path)
	if err != nil {
		t.Fatalf("load answers: %v", err)
	}
	value := false
	err = ui.Confirm("Apply", &value)
	if err == nil || !strings.Contains(err.Error(), `missing confirm prompt "Apply"`) {
		t.Fatalf("expected missing answer error, got %v", err)
	}
}

func TestScriptedUIAcceptsFirstLineTitleForMultilinePrompts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(path, []byte(`{"multi_select":{"Features":["A"]}}`), 0o600); err != nil {
		t.Fatalf("write answers: %v", err)
	}
	ui, err := NewScriptedUIFromFile(path)
	if err != nil {
		t.Fatalf("load answers: %v", err)
	}
	selected := []string{}
	if err := ui.MultiSelect("Features\n  Details for humans", []string{"A", "B"}, &selected); err != nil {
		t.Fatalf("multi-select: %v", err)
	}
	if len(selected) != 1 || selected[0] != "A" {
		t.Fatalf("selected = %#v, want [A]", selected)
	}
	if err := ui.AssertComplete(); err != nil {
		t.Fatalf("answers should be complete: %v", err)
	}
}

func TestScriptedUIInvalidOptionFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(path, []byte(`{"select":{"Mode":"bad"}}`), 0o600); err != nil {
		t.Fatalf("write answers: %v", err)
	}
	ui, err := NewScriptedUIFromFile(path)
	if err != nil {
		t.Fatalf("load answers: %v", err)
	}
	value := ""
	err = ui.Select("Mode", []string{"all", "none"}, &value)
	if err == nil || !strings.Contains(err.Error(), "not one of") {
		t.Fatalf("expected invalid option error, got %v", err)
	}
}

func TestScriptedUIUnusedAnswerFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(path, []byte(`{"confirm":{"Apply":true,"Unused":false}}`), 0o600); err != nil {
		t.Fatalf("write answers: %v", err)
	}
	ui, err := NewScriptedUIFromFile(path)
	if err != nil {
		t.Fatalf("load answers: %v", err)
	}
	value := false
	if err := ui.Confirm("Apply", &value); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	err = ui.AssertComplete()
	if err == nil || !strings.Contains(err.Error(), "confirm: Unused") {
		t.Fatalf("expected unused answer error, got %v", err)
	}
}

func TestScriptedUILoadRejectsMultipleJSONValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(path, []byte(`{"confirm":{}} {"confirm":{}}`), 0o600); err != nil {
		t.Fatalf("write answers: %v", err)
	}
	_, err := NewScriptedUIFromFile(path)
	if err == nil || !strings.Contains(err.Error(), "multiple JSON values") {
		t.Fatalf("expected multiple JSON values error, got %v", err)
	}
}
