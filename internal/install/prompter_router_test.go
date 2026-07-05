package install

import (
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// tmpOnlyNoValidatorPrompter satisfies the base Prompter and the optional
// grouped tmp deletion capability, but NOT promptValidator. It models a legacy
// Prompter implementation that predates the promptValidator probe: under the
// tmp capability rule such a prompter still gets grouped tmp deletion (the
// validator step is skipped when absent). This is the distinct half of the tmp
// rule that differs from the unified-overwrite rule.
type tmpOnlyNoValidatorPrompter struct {
	plainPrompter
	called *bool
	answer bool
}

func (p tmpOnlyNoValidatorPrompter) DeleteUnknownTmpAll([]string) (bool, error) {
	if p.called != nil {
		*p.called = true
	}
	return p.answer, nil
}

func TestPromptRouter_UnifiedOverwrite_RequiresValidator(t *testing.T) {
	// Unified overwrite must be selected ONLY when the prompter implements both
	// the unified interface and promptValidator (reporting the callback wired).
	// A prompter that implements the interface but not promptValidator keeps the
	// separate managed/memory prompts — treating it as unified would change the
	// prompt flow.
	if newPromptRouter(unifiedOnlyPrompter{}).hasUnifiedOverwrite() {
		t.Fatal("unified interface without promptValidator must not select unified overwrite")
	}
	if newPromptRouter(PromptFuncs{}).hasUnifiedOverwrite() {
		t.Fatal("PromptFuncs without a wired unified callback must not select unified overwrite")
	}

	router := newPromptRouter(PromptFuncs{
		OverwriteAllUnifiedPreviewFunc: func([]DiffPreview, []DiffPreview) (bool, bool, error) {
			return true, false, nil
		},
	})
	if !router.hasUnifiedOverwrite() {
		t.Fatal("PromptFuncs with a wired unified callback must select unified overwrite")
	}
	resp, err := router.route(promptRequest{kind: promptKindOverwriteAllUnified})
	if err != nil {
		t.Fatalf("route unified: %v", err)
	}
	if !resp.approved || resp.approvedMemory {
		t.Fatalf("unified route must surface both decisions: managed=%v memory=%v", resp.approved, resp.approvedMemory)
	}
}

func TestPromptRouter_DeleteUnknownTmpAll_UnavailableLeavesUntouched(t *testing.T) {
	// A PromptFuncs without DeleteUnknownTmpAllFunc must not delete tmp: the
	// router reports the grouped capability as unavailable and route returns
	// "not approved" without invoking any callback. Tmp deletion is destructive
	// and only ever allowed through an affirmative grouped answer.
	router := newPromptRouter(PromptFuncs{
		DeleteUnknownTmpAllFunc: nil,
	})
	resp, err := router.route(promptRequest{kind: promptKindDeleteUnknownTmpAll, paths: []string{".agent-layer/tmp/x"}})
	if err != nil {
		t.Fatalf("route tmp: %v", err)
	}
	if resp.approved {
		t.Fatal("tmp deletion must not be approved when the grouped callback is unwired")
	}
}

func TestPromptRouter_DeleteUnknownTmpAll_LegacyWithoutValidatorRoutes(t *testing.T) {
	// The tmp rule differs from the unified rule: a tmp-capable prompter that
	// does NOT implement promptValidator still gets grouped tmp deletion.
	called := false
	router := newPromptRouter(tmpOnlyNoValidatorPrompter{called: &called, answer: true})
	resp, err := router.route(promptRequest{kind: promptKindDeleteUnknownTmpAll, paths: []string{".agent-layer/tmp/x"}})
	if err != nil {
		t.Fatalf("route tmp: %v", err)
	}
	if !called {
		t.Fatal("grouped tmp callback must be invoked for a legacy tmp-capable prompter without promptValidator")
	}
	if !resp.approved {
		t.Fatal("expected the grouped tmp answer to be surfaced")
	}
}

func TestPromptRouter_StatuslineSource_FallbackNeverOverwrites(t *testing.T) {
	// A prompter that does not implement the statusline capability must never
	// authorize overwriting a customized statusline source. A zero PromptFuncs
	// also has the method set, but the router must not advertise the capability
	// unless the callback is wired; callers use that gate to avoid building the
	// statusline diff preview.
	router := newPromptRouter(plainPrompter{})
	if router.hasStatuslineSource() {
		t.Fatal("plain prompter must not advertise the statusline capability")
	}
	if newPromptRouter(PromptFuncs{}).hasStatuslineSource() {
		t.Fatal("PromptFuncs without a wired statusline callback must not advertise the capability")
	}
	wired := newPromptRouter(PromptFuncs{
		StatuslineSourcePreviewFunc: func(DiffPreview) (bool, error) { return true, nil },
	})
	if !wired.hasStatuslineSource() {
		t.Fatal("PromptFuncs with a wired statusline callback must advertise the capability")
	}
	resp, err := router.route(promptRequest{kind: promptKindStatuslineSource})
	if err != nil {
		t.Fatalf("route statusline: %v", err)
	}
	if resp.approved {
		t.Fatal("missing statusline prompt must keep the existing source (not approved)")
	}
	resp, err = newPromptRouter(PromptFuncs{}).route(promptRequest{kind: promptKindStatuslineSource})
	if err != nil {
		t.Fatalf("route zero-value PromptFuncs statusline: %v", err)
	}
	if resp.approved {
		t.Fatal("zero-value PromptFuncs statusline prompt must keep the existing source (not approved)")
	}
}

func TestPromptRouter_ConfigSetDefault_FallbackUsesManifestValue(t *testing.T) {
	// When the prompter cannot confirm a config default, the migration manifest
	// value must be used verbatim.
	router := newPromptRouter(plainPrompter{})
	resp, err := router.route(promptRequest{kind: promptKindConfigSetDefault, manifestValue: "manifest"})
	if err != nil {
		t.Fatalf("route config default: %v", err)
	}
	if resp.value != "manifest" {
		t.Fatalf("expected manifest value fallback, got %v", resp.value)
	}
	resp, err = newPromptRouter(PromptFuncs{}).route(promptRequest{kind: promptKindConfigSetDefault, manifestValue: "manifest"})
	if err != nil {
		t.Fatalf("route zero-value PromptFuncs config default: %v", err)
	}
	if resp.value != "manifest" {
		t.Fatalf("zero-value PromptFuncs expected manifest value fallback, got %v", resp.value)
	}
}

func TestPromptRouter_ConfigSetDefault_WiredReturnsPromptedValue(t *testing.T) {
	router := newPromptRouter(PromptFuncs{
		ConfigSetDefaultFunc: func(string, any, string, *config.FieldDef) (any, error) {
			return "chosen", nil
		},
	})
	resp, err := router.route(promptRequest{kind: promptKindConfigSetDefault, manifestValue: "manifest"})
	if err != nil {
		t.Fatalf("route config default: %v", err)
	}
	if resp.value != "chosen" {
		t.Fatalf("expected wired prompt value, got %v", resp.value)
	}
}

func TestPromptRouter_ConfirmSkillsMigration_FallbackProceeds(t *testing.T) {
	// A prompter without the skills-migration capability proceeds automatically
	// (headless default) once preflight has passed.
	router := newPromptRouter(plainPrompter{})
	resp, err := router.route(promptRequest{kind: promptKindConfirmSkillsMigration})
	if err != nil {
		t.Fatalf("route skills migration: %v", err)
	}
	if !resp.approved {
		t.Fatal("missing skills-migration prompt must proceed (approved)")
	}
	resp, err = newPromptRouter(PromptFuncs{}).route(promptRequest{kind: promptKindConfirmSkillsMigration})
	if err != nil {
		t.Fatalf("route zero-value PromptFuncs skills migration: %v", err)
	}
	if !resp.approved {
		t.Fatal("zero-value PromptFuncs skills-migration prompt must proceed (approved)")
	}

	declined := newPromptRouter(PromptFuncs{
		ConfirmSkillsMigrationFunc: func([]string, []SkillsMigrationConflict) (bool, error) {
			return false, nil
		},
	})
	resp, err = declined.route(promptRequest{kind: promptKindConfirmSkillsMigration})
	if err != nil {
		t.Fatalf("route skills migration declined: %v", err)
	}
	if resp.approved {
		t.Fatal("a wired skills-migration prompt that declines must not be approved")
	}
}

func TestPromptRouter_ValidateRequiredOverwrite(t *testing.T) {
	if err := newPromptRouter(nil).validateRequiredOverwrite(); err == nil ||
		!strings.Contains(err.Error(), messages.InstallOverwritePromptRequired) {
		t.Fatalf("nil prompter must be rejected in overwrite mode, got %v", err)
	}
	// A non-PromptFuncs Prompter (no promptValidator) is trusted to implement
	// the required methods and must pass validation.
	if err := newPromptRouter(plainPrompter{}).validateRequiredOverwrite(); err != nil {
		t.Fatalf("plain Prompter without promptValidator must pass validation, got %v", err)
	}
	// A PromptFuncs missing required callbacks must fail with the delete error
	// once the overwrite callbacks are satisfied.
	partial := PromptFuncs{
		OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { return false, nil },
		OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { return false, nil },
		OverwritePreviewFunc:          func(DiffPreview) (bool, error) { return false, nil },
	}
	if err := newPromptRouter(partial).validateRequiredOverwrite(); err == nil ||
		!strings.Contains(err.Error(), messages.InstallDeleteUnknownPromptRequired) {
		t.Fatalf("missing delete callbacks must be rejected, got %v", err)
	}
}

func TestPromptRouter_UnknownKind_ReturnsError(t *testing.T) {
	// The router must reject an unrecognized prompt kind rather than silently
	// returning a zero decision that a caller could act on.
	_, err := newPromptRouter(PromptFuncs{}).route(promptRequest{kind: promptKind(-1)})
	if err == nil {
		t.Fatal("expected an error for an unknown prompt kind")
	}
}
