package sync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestResolvePromptServerCommandUsesGlobalBinary(t *testing.T) {
	t.Parallel()
	sys := &MockSystem{
		LookPathFunc: func(file string) (string, error) {
			if file != "al" {
				return "", errors.New("unexpected lookup")
			}
			return "/usr/local/bin/al", nil
		},
	}

	command, args, err := resolvePromptServerCommand(sys, t.TempDir())
	if err != nil {
		t.Fatalf("resolvePromptServerCommand error: %v", err)
	}
	if command != "al" {
		t.Fatalf("expected al, got %q", command)
	}
	if len(args) != 1 || args[0] != "mcp-prompts" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolvePromptServerCommandExportedUsesGlobalBinary(t *testing.T) {
	t.Parallel()
	sys := &MockSystem{
		LookPathFunc: func(file string) (string, error) {
			if file != "al" {
				return "", errors.New("unexpected lookup")
			}
			return "/usr/local/bin/al", nil
		},
	}

	command, args, err := ResolvePromptServerCommand(sys, t.TempDir())
	if err != nil {
		t.Fatalf("ResolvePromptServerCommand error: %v", err)
	}
	if command != "al" {
		t.Fatalf("expected al, got %q", command)
	}
	if len(args) != 1 || args[0] != "mcp-prompts" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolvePromptServerCommandRequiresSystem(t *testing.T) {
	t.Parallel()

	_, _, err := ResolvePromptServerCommand(nil, t.TempDir())
	if err == nil {
		t.Fatalf("expected error for nil system")
	}
}

func TestResolvePromptServerCommandRequiresTypedNilSystem(t *testing.T) {
	t.Parallel()

	var sys *MockSystem
	_, _, err := ResolvePromptServerCommand(sys, t.TempDir())
	if err == nil {
		t.Fatalf("expected error for typed nil system")
	}
}

func TestResolvePromptServerCommandRootEmptyNoGlobalBinary(t *testing.T) {
	t.Parallel()
	sys := &MockSystem{
		LookPathFunc: func(file string) (string, error) {
			return "", errors.New("missing")
		},
	}

	_, _, err := resolvePromptServerCommand(sys, "")
	if err == nil {
		t.Fatalf("expected error for missing al")
	}
}

func TestResolvePromptServerCommandFallsBackToGoRun(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		LookPathFunc: func(file string) (string, error) {
			if file == "al" {
				return "", errors.New("missing")
			}
			if file != "go" {
				return "", errors.New("unexpected lookup")
			}
			return "/usr/bin/go", nil
		},
		StatFunc: func(name string) (os.FileInfo, error) {
			if name == filepath.Join(root, "cmd", "al") {
				return &mockFileInfo{isDir: true}, nil
			}
			return nil, os.ErrNotExist
		},
		ReadFileFunc: func(name string) ([]byte, error) {
			if name == filepath.Join(root, "go.mod") {
				return []byte("module github.com/conn-castle/agent-layer\n"), nil
			}
			return nil, os.ErrNotExist
		},
	}

	command, args, err := resolvePromptServerCommand(sys, root)
	if err != nil {
		t.Fatalf("resolvePromptServerCommand error: %v", err)
	}
	if command != "go" {
		t.Fatalf("expected go, got %q", command)
	}
	expectedSource := filepath.Join(root, "cmd", "al")
	if len(args) != 3 || args[0] != "run" || args[1] != expectedSource || args[2] != "mcp-prompts" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolvePromptServerCommandPrefersGoRunWhenSourceExists(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		LookPathFunc: func(file string) (string, error) {
			switch file {
			case "go":
				return "/usr/bin/go", nil
			case "al":
				return "/usr/local/bin/al", nil
			default:
				return "", errors.New("unexpected lookup")
			}
		},
		StatFunc: func(name string) (os.FileInfo, error) {
			if name == filepath.Join(root, "cmd", "al") {
				return &mockFileInfo{isDir: true}, nil
			}
			return nil, os.ErrNotExist
		},
		ReadFileFunc: func(name string) ([]byte, error) {
			if name == filepath.Join(root, "go.mod") {
				return []byte("module github.com/conn-castle/agent-layer\n"), nil
			}
			return nil, os.ErrNotExist
		},
	}

	command, args, err := resolvePromptServerCommand(sys, root)
	if err != nil {
		t.Fatalf("resolvePromptServerCommand error: %v", err)
	}
	if command != "go" {
		t.Fatalf("expected go, got %q", command)
	}
	expectedSource := filepath.Join(root, "cmd", "al")
	if len(args) != 3 || args[0] != "run" || args[1] != expectedSource || args[2] != "mcp-prompts" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolvePromptServerCommandFallsBackToAlWhenGoMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		LookPathFunc: func(file string) (string, error) {
			switch file {
			case "go":
				return "", errors.New("missing")
			case "al":
				return "/usr/local/bin/al", nil
			default:
				return "", errors.New("unexpected lookup")
			}
		},
		StatFunc: func(name string) (os.FileInfo, error) {
			if name == filepath.Join(root, "cmd", "al") {
				return &mockFileInfo{isDir: true}, nil
			}
			return nil, os.ErrNotExist
		},
		ReadFileFunc: func(name string) ([]byte, error) {
			if name == filepath.Join(root, "go.mod") {
				return []byte("module github.com/conn-castle/agent-layer\n"), nil
			}
			return nil, os.ErrNotExist
		},
	}

	command, args, err := resolvePromptServerCommand(sys, root)
	if err != nil {
		t.Fatalf("resolvePromptServerCommand error: %v", err)
	}
	if command != "al" {
		t.Fatalf("expected al, got %q", command)
	}
	if len(args) != 1 || args[0] != "mcp-prompts" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolvePromptServerCommandMissingGo(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		LookPathFunc: func(file string) (string, error) {
			return "", errors.New("missing")
		},
		StatFunc: func(name string) (os.FileInfo, error) {
			if name == filepath.Join(root, "cmd", "al") {
				return &mockFileInfo{isDir: true}, nil
			}
			return nil, os.ErrNotExist
		},
		ReadFileFunc: func(name string) ([]byte, error) {
			if name == filepath.Join(root, "go.mod") {
				return []byte("module github.com/conn-castle/agent-layer\n"), nil
			}
			return nil, os.ErrNotExist
		},
	}

	_, _, err := resolvePromptServerCommand(sys, root)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestResolvePromptServerCommandNonAgentLayerRootFallsBackToAl(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		LookPathFunc: func(file string) (string, error) {
			switch file {
			case "go":
				return "/usr/bin/go", nil
			case "al":
				return "/usr/local/bin/al", nil
			default:
				return "", errors.New("unexpected lookup")
			}
		},
		StatFunc: func(name string) (os.FileInfo, error) {
			if name == filepath.Join(root, "cmd", "al") {
				return &mockFileInfo{isDir: true}, nil
			}
			return nil, os.ErrNotExist
		},
		ReadFileFunc: func(name string) ([]byte, error) {
			if name == filepath.Join(root, "go.mod") {
				return []byte("module example.com/other\n"), nil
			}
			return nil, os.ErrNotExist
		},
	}

	command, args, err := resolvePromptServerCommand(sys, root)
	if err != nil {
		t.Fatalf("resolvePromptServerCommand error: %v", err)
	}
	if command != "al" {
		t.Fatalf("expected al, got %q", command)
	}
	if len(args) != 1 || args[0] != "mcp-prompts" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestResolvePromptServerCommandNonAgentLayerRootErrorsWhenAlMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		LookPathFunc: func(file string) (string, error) {
			return "", errors.New("missing")
		},
		StatFunc: func(name string) (os.FileInfo, error) {
			if name == filepath.Join(root, "cmd", "al") {
				return &mockFileInfo{isDir: true}, nil
			}
			return nil, os.ErrNotExist
		},
		ReadFileFunc: func(name string) ([]byte, error) {
			if name == filepath.Join(root, "go.mod") {
				return []byte("module example.com/other\n"), nil
			}
			return nil, os.ErrNotExist
		},
	}

	_, _, err := resolvePromptServerCommand(sys, root)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolvePromptServerEnv(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	env, err := resolvePromptServerEnv(root)
	if err != nil {
		t.Fatalf("resolvePromptServerEnv error: %v", err)
	}
	if got := env[config.BuiltinRepoRootEnvVar]; got != root {
		t.Fatalf("expected %s=%q, got %q", config.BuiltinRepoRootEnvVar, root, got)
	}
}

func TestResolvePromptServerEnvExported(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	env, err := ResolvePromptServerEnv(root)
	if err != nil {
		t.Fatalf("ResolvePromptServerEnv error: %v", err)
	}
	if got := env[config.BuiltinRepoRootEnvVar]; got != root {
		t.Fatalf("expected %s=%q, got %q", config.BuiltinRepoRootEnvVar, root, got)
	}
}

func TestResolvePromptServerEnvRootRequired(t *testing.T) {
	t.Parallel()

	_, err := resolvePromptServerEnv(" ")
	if err == nil {
		t.Fatal("expected error for empty repo root")
	}
}
