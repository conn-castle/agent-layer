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

// TestScriptedUILoadRejectsUnknownFields guards the DisallowUnknownFields decoder
// setting: a typo'd top-level key must fail loudly rather than be silently ignored.
// Would fail if the decoder stopped rejecting unknown fields.
func TestScriptedUILoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(path, []byte(`{"selct":{"Mode":"none"}}`), 0o600); err != nil {
		t.Fatalf("write answers: %v", err)
	}
	_, err := NewScriptedUIFromFile(path)
	if err == nil || !strings.Contains(err.Error(), "decode wizard answers") {
		t.Fatalf("expected unknown-field decode error, got %v", err)
	}
}

// TestScriptedUILoadRejectsMalformedJSON guards the Decode error path: malformed
// JSON must surface a decode error rather than yield an empty answer set.
// Would fail if the decode error were swallowed.
func TestScriptedUILoadRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(path, []byte(`{"confirm": `), 0o600); err != nil {
		t.Fatalf("write answers: %v", err)
	}
	_, err := NewScriptedUIFromFile(path)
	if err == nil || !strings.Contains(err.Error(), "decode wizard answers") {
		t.Fatalf("expected malformed JSON decode error, got %v", err)
	}
}

// TestScriptedUILoadMissingFileFails guards the os.ReadFile error path: a missing
// answer file must propagate a not-exist error, not be treated as empty answers.
func TestScriptedUILoadMissingFileFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	_, err := NewScriptedUIFromFile(path)
	if err == nil || !os.IsNotExist(err) {
		t.Fatalf("expected not-exist error, got %v", err)
	}
}

// TestScriptedUIMissingAnswersFailPerKind verifies each prompt kind reports a
// missing-answer error naming its own kind. Would fail if a handler returned a
// zero value (e.g. empty select) instead of erroring on an absent answer.
func TestScriptedUIMissingAnswersFailPerKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write answers: %v", err)
	}
	ui, err := NewScriptedUIFromFile(path)
	if err != nil {
		t.Fatalf("load answers: %v", err)
	}

	selectValue := ""
	if err := ui.Select("Mode", []string{"a"}, &selectValue); err == nil ||
		!strings.Contains(err.Error(), `missing select prompt "Mode"`) {
		t.Fatalf("expected missing select error, got %v", err)
	}
	multi := []string{}
	if err := ui.MultiSelect("Agents", []string{"a"}, &multi); err == nil ||
		!strings.Contains(err.Error(), `missing multi_select prompt "Agents"`) {
		t.Fatalf("expected missing multi_select error, got %v", err)
	}
	input := ""
	if err := ui.Input("Model", &input); err == nil ||
		!strings.Contains(err.Error(), `missing input prompt "Model"`) {
		t.Fatalf("expected missing input error, got %v", err)
	}
	secret := ""
	if err := ui.SecretInput("Secret", &secret); err == nil ||
		!strings.Contains(err.Error(), `missing secret_input prompt "Secret"`) {
		t.Fatalf("expected missing secret_input error, got %v", err)
	}
}

// TestScriptedUIMultiSelectRejectsOptionNotAllowed verifies a multi-select value
// outside the offered options is rejected, naming the offending value. Would fail
// if MultiSelect applied arbitrary values without validating against options.
func TestScriptedUIMultiSelectRejectsOptionNotAllowed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(path, []byte(`{"multi_select":{"Agents":["claude","ghost"]}}`), 0o600); err != nil {
		t.Fatalf("write answers: %v", err)
	}
	ui, err := NewScriptedUIFromFile(path)
	if err != nil {
		t.Fatalf("load answers: %v", err)
	}
	selected := []string{}
	err = ui.MultiSelect("Agents", []string{"claude", "codex"}, &selected)
	if err == nil || !strings.Contains(err.Error(), `"ghost"`) || !strings.Contains(err.Error(), "not one of") {
		t.Fatalf("expected not-one-of error naming ghost, got %v", err)
	}
}

// TestScriptedUIInputAndSecretAcceptFirstLineTitle covers the short-title fallback
// in the string-answer lookup helper for Input and SecretInput: a multi-line prompt
// title must resolve to the answer keyed by its first line. Would fail if the
// fallback to firstScriptedTitleLine were removed.
func TestScriptedUIInputAndSecretAcceptFirstLineTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	data := `{"input":{"Model":"sonnet"},"secret_input":{"API key":"tok"}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write answers: %v", err)
	}
	ui, err := NewScriptedUIFromFile(path)
	if err != nil {
		t.Fatalf("load answers: %v", err)
	}

	input := ""
	if err := ui.Input("Model\n  Which model to use", &input); err != nil {
		t.Fatalf("input: %v", err)
	}
	if input != "sonnet" {
		t.Fatalf("input = %q, want sonnet", input)
	}
	secret := ""
	if err := ui.SecretInput("API key\n  Paste it here", &secret); err != nil {
		t.Fatalf("secret: %v", err)
	}
	if secret != "tok" {
		t.Fatalf("secret = %q, want tok", secret)
	}
	if err := ui.AssertComplete(); err != nil {
		t.Fatalf("answers should be complete: %v", err)
	}
}

// TestScriptedUIUnusedAnswersDetectedAcrossAllKinds verifies AssertComplete reports
// untouched answers for every prompt kind, sorted. Would fail if a kind were dropped
// from the unused-prompt scan (e.g. select or input answers never flagged).
func TestScriptedUIUnusedAnswersDetectedAcrossAllKinds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answers.json")
	data := `{
  "select": {"Sel": "a"},
  "multi_select": {"Multi": ["a"]},
  "confirm": {"Conf": true},
  "input": {"In": "x"},
  "secret_input": {"Sec": "y"}
}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write answers: %v", err)
	}
	ui, err := NewScriptedUIFromFile(path)
	if err != nil {
		t.Fatalf("load answers: %v", err)
	}
	err = ui.AssertComplete()
	if err == nil {
		t.Fatal("expected unused-prompt error, got nil")
	}
	for _, want := range []string{
		"select: Sel", "multi_select: Multi", "confirm: Conf", "input: In", "secret_input: Sec",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected unused error to mention %q, got %v", want, err)
		}
	}
}
