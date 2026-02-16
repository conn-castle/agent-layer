package install

import (
	"fmt"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// Prompter provides user prompts for overwrite and delete decisions.
type Prompter interface {
	OverwriteAll(previews []DiffPreview) (bool, error)
	OverwriteAllMemory(previews []DiffPreview) (bool, error)
	Overwrite(preview DiffPreview) (bool, error)
	DeleteUnknownAll(paths []string) (bool, error)
	DeleteUnknown(path string) (bool, error)
}

// PromptOverwriteAllPreviewFunc asks whether to overwrite all paths in a diff preview batch.
type PromptOverwriteAllPreviewFunc func(previews []DiffPreview) (bool, error)

// PromptOverwriteAllUnifiedPreviewFunc asks whether to apply managed and memory updates in one pass.
type PromptOverwriteAllUnifiedPreviewFunc func(managed []DiffPreview, memory []DiffPreview) (bool, bool, error)

// PromptOverwritePreviewFunc asks whether to overwrite a single diff preview path.
type PromptOverwritePreviewFunc func(preview DiffPreview) (bool, error)

// PromptFuncs adapts optional prompt callbacks into a Prompter.
type PromptFuncs struct {
	OverwriteAllPreviewFunc        PromptOverwriteAllPreviewFunc
	OverwriteAllMemoryPreviewFunc  PromptOverwriteAllPreviewFunc
	OverwriteAllUnifiedPreviewFunc PromptOverwriteAllUnifiedPreviewFunc
	OverwritePreviewFunc           PromptOverwritePreviewFunc
	DeleteUnknownAllFunc           PromptDeleteUnknownAllFunc
	DeleteUnknownFunc              PromptDeleteUnknownFunc
}

// OverwriteAll prompts the user to confirm overwriting all given paths.
// Returns an error if no overwrite callback is configured.
func (p PromptFuncs) OverwriteAll(previews []DiffPreview) (bool, error) {
	if p.OverwriteAllPreviewFunc == nil {
		return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	return p.OverwriteAllPreviewFunc(previews)
}

// OverwriteAllMemory prompts the user to confirm overwriting all memory file paths.
// Returns an error if no OverwriteAll callback is configured.
func (p PromptFuncs) OverwriteAllMemory(previews []DiffPreview) (bool, error) {
	if p.OverwriteAllMemoryPreviewFunc == nil {
		return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	return p.OverwriteAllMemoryPreviewFunc(previews)
}

// OverwriteAllUnified prompts for managed and memory overwrite-all decisions in one pass.
// Returns an error if no unified callback is configured.
func (p PromptFuncs) OverwriteAllUnified(managed []DiffPreview, memory []DiffPreview) (bool, bool, error) {
	if p.OverwriteAllUnifiedPreviewFunc == nil {
		return false, false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	return p.OverwriteAllUnifiedPreviewFunc(managed, memory)
}

// Overwrite prompts the user to confirm overwriting a single path.
// Returns an error if no Overwrite callback is configured.
func (p PromptFuncs) Overwrite(preview DiffPreview) (bool, error) {
	if p.OverwritePreviewFunc == nil {
		return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	return p.OverwritePreviewFunc(preview)
}

// DeleteUnknownAll prompts the user to confirm deleting all unknown paths.
// Returns an error if no DeleteUnknownAllFunc is configured.
func (p PromptFuncs) DeleteUnknownAll(paths []string) (bool, error) {
	if p.DeleteUnknownAllFunc == nil {
		return false, fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
	}
	return p.DeleteUnknownAllFunc(paths)
}

// DeleteUnknown prompts the user to confirm deleting a single unknown path.
// Returns an error if no DeleteUnknownFunc is configured.
func (p PromptFuncs) DeleteUnknown(path string) (bool, error) {
	if p.DeleteUnknownFunc == nil {
		return false, fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
	}
	return p.DeleteUnknownFunc(path)
}

type promptValidator interface {
	hasOverwriteAll() bool
	hasOverwriteAllMemory() bool
	hasOverwriteAllUnified() bool
	hasOverwrite() bool
	hasDeleteUnknownAll() bool
	hasDeleteUnknown() bool
}

func (p PromptFuncs) hasOverwriteAll() bool {
	return p.OverwriteAllPreviewFunc != nil
}

func (p PromptFuncs) hasOverwriteAllMemory() bool {
	return p.OverwriteAllMemoryPreviewFunc != nil
}

func (p PromptFuncs) hasOverwriteAllUnified() bool {
	return p.OverwriteAllUnifiedPreviewFunc != nil
}

func (p PromptFuncs) hasOverwrite() bool {
	return p.OverwritePreviewFunc != nil
}

func (p PromptFuncs) hasDeleteUnknownAll() bool {
	return p.DeleteUnknownAllFunc != nil
}

func (p PromptFuncs) hasDeleteUnknown() bool {
	return p.DeleteUnknownFunc != nil
}
