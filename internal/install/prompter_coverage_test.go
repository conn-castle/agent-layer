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
