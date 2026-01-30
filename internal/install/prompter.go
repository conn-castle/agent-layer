package install

import (
	"fmt"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// Prompter provides user prompts for overwrite and delete decisions.
type Prompter interface {
	OverwriteAll(paths []string) (bool, error)
	OverwriteAllMemory(paths []string) (bool, error)
	Overwrite(path string) (bool, error)
	DeleteUnknownAll(paths []string) (bool, error)
	DeleteUnknown(path string) (bool, error)
}

// PromptFuncs adapts optional prompt callbacks into a Prompter.
type PromptFuncs struct {
	OverwriteAllFunc       PromptOverwriteAllFunc
	OverwriteAllMemoryFunc PromptOverwriteAllFunc
	OverwriteFunc          PromptOverwriteFunc
	DeleteUnknownAllFunc   PromptDeleteUnknownAllFunc
	DeleteUnknownFunc      PromptDeleteUnknownFunc
}

// OverwriteAll prompts the user to confirm overwriting all given paths.
// Returns an error if no OverwriteAllFunc is configured.
func (p PromptFuncs) OverwriteAll(paths []string) (bool, error) {
	if p.OverwriteAllFunc == nil {
		return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	return p.OverwriteAllFunc(paths)
}

// OverwriteAllMemory prompts the user to confirm overwriting all memory file paths.
// Returns an error if no OverwriteAllMemoryFunc is configured.
func (p PromptFuncs) OverwriteAllMemory(paths []string) (bool, error) {
	if p.OverwriteAllMemoryFunc == nil {
		return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	return p.OverwriteAllMemoryFunc(paths)
}

// Overwrite prompts the user to confirm overwriting a single path.
// Returns an error if no OverwriteFunc is configured.
func (p PromptFuncs) Overwrite(path string) (bool, error) {
	if p.OverwriteFunc == nil {
		return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	return p.OverwriteFunc(path)
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
	hasOverwrite() bool
	hasDeleteUnknownAll() bool
	hasDeleteUnknown() bool
}

func (p PromptFuncs) hasOverwriteAll() bool {
	return p.OverwriteAllFunc != nil
}

func (p PromptFuncs) hasOverwriteAllMemory() bool {
	return p.OverwriteAllMemoryFunc != nil
}

func (p PromptFuncs) hasOverwrite() bool {
	return p.OverwriteFunc != nil
}

func (p PromptFuncs) hasDeleteUnknownAll() bool {
	return p.DeleteUnknownAllFunc != nil
}

func (p PromptFuncs) hasDeleteUnknown() bool {
	return p.DeleteUnknownFunc != nil
}
