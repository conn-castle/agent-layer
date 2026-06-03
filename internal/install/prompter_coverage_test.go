package install

import (
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestOverwriteAll_NilCallback(t *testing.T) {
	p := PromptFuncs{}
	_, err := p.OverwriteAll(nil)
	if err == nil || !strings.Contains(err.Error(), messages.InstallOverwritePromptRequired) {
		t.Fatalf("expected overwrite prompt required error, got %v", err)
	}
}

func TestOverwriteAll_CallbackCalled(t *testing.T) {
	called := false
	p := PromptFuncs{
		OverwriteAllPreviewFunc: func(previews []DiffPreview) (bool, error) {
			called = true
			return true, nil
		},
	}
	approved, err := p.OverwriteAll([]DiffPreview{{Path: "a.md"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected callback to be called")
	}
	if !approved {
		t.Fatal("expected approval")
	}
}

func TestOverwriteAllMemory_NilCallback(t *testing.T) {
	p := PromptFuncs{}
	_, err := p.OverwriteAllMemory(nil)
	if err == nil || !strings.Contains(err.Error(), messages.InstallOverwritePromptRequired) {
		t.Fatalf("expected overwrite prompt required error, got %v", err)
	}
}

func TestOverwriteAllUnified_NilCallback(t *testing.T) {
	p := PromptFuncs{}
	_, _, err := p.OverwriteAllUnified(nil, nil)
	if err == nil || !strings.Contains(err.Error(), messages.InstallOverwritePromptRequired) {
		t.Fatalf("expected overwrite prompt required error, got %v", err)
	}
}

func TestStatuslineSource_NilCallbackNeverOverwrites(t *testing.T) {
	// With no StatuslineSource callback wired (headless/non-interactive), the
	// prompter must report "do not overwrite" rather than silently replacing a
	// user-owned statusline source. A defect that defaulted this to true would
	// clobber customized sources on upgrade.
	overwrite, err := PromptFuncs{}.StatuslineSource(DiffPreview{})
	if err != nil {
		t.Fatalf("StatuslineSource nil callback returned error: %v", err)
	}
	if overwrite {
		t.Fatal("StatuslineSource with no callback must not authorize overwrite")
	}

	called := false
	p := PromptFuncs{StatuslineSourcePreviewFunc: func(DiffPreview) (bool, error) {
		called = true
		return true, nil
	}}
	overwrite, err = p.StatuslineSource(DiffPreview{})
	if err != nil || !overwrite || !called {
		t.Fatalf("StatuslineSource should delegate to the callback: called=%v overwrite=%v err=%v", called, overwrite, err)
	}
}
