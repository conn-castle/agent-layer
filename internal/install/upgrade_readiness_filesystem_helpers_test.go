package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestCountMarkdownFiles_ErrorBranches(t *testing.T) {
	root := t.TempDir()
	markdownRoot := filepath.Join(root, ".agent-layer", "slash-commands")
	if err := os.MkdirAll(markdownRoot, 0o755); err != nil {
		t.Fatalf("mkdir markdown root: %v", err)
	}

	t.Run("stat error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.statErrs[normalizePath(markdownRoot)] = errors.New("stat boom")
		inst := &installer{root: root, sys: sys}

		_, err := countMarkdownFiles(inst, markdownRoot)
		if err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected stat error, got %v", err)
		}
	})

	t.Run("walk error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.walkErrs[normalizePath(markdownRoot)] = errors.New("walk boom")
		inst := &installer{root: root, sys: sys}

		_, err := countMarkdownFiles(inst, markdownRoot)
		if err == nil || !strings.Contains(err.Error(), "walk boom") {
			t.Fatalf("expected walk error, got %v", err)
		}
	})

	t.Run("missing root", func(t *testing.T) {
		inst := &installer{root: root, sys: RealSystem{}}
		count, err := countMarkdownFiles(inst, filepath.Join(root, "does-not-exist"))
		if err != nil {
			t.Fatalf("countMarkdownFiles: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected zero count for missing root, got %d", count)
		}
	})
}

func TestListGeneratedFilesWithSuffix_ErrorBranches(t *testing.T) {
	root := t.TempDir()
	promptRoot := filepath.Join(root, ".vscode", "prompts")
	if err := os.MkdirAll(promptRoot, 0o755); err != nil {
		t.Fatalf("mkdir prompts root: %v", err)
	}
	promptPath := filepath.Join(promptRoot, "alpha.prompt.md")
	if err := os.WriteFile(promptPath, []byte("<!--\n  "+generatedFileMarker+"\n-->\n"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	t.Run("read error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.readErrs[normalizePath(promptPath)] = errors.New("read boom")
		inst := &installer{root: root, sys: sys}

		_, _, err := listGeneratedFilesWithSuffix(inst, promptRoot, ".prompt.md")
		if err == nil || !strings.Contains(err.Error(), "read boom") {
			t.Fatalf("expected read error, got %v", err)
		}
	})

	t.Run("stat error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.statErrs[normalizePath(promptPath)] = errors.New("stat boom")
		inst := &installer{root: root, sys: sys}

		_, _, err := listGeneratedFilesWithSuffix(inst, promptRoot, ".prompt.md")
		if err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected stat error, got %v", err)
		}
	})

	t.Run("walk error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.walkErrs[normalizePath(promptRoot)] = errors.New("walk boom")
		inst := &installer{root: root, sys: sys}

		_, _, err := listGeneratedFilesWithSuffix(inst, promptRoot, ".prompt.md")
		if err == nil || !strings.Contains(err.Error(), "walk boom") {
			t.Fatalf("expected walk error, got %v", err)
		}
	})

	t.Run("missing root", func(t *testing.T) {
		inst := &installer{root: root, sys: RealSystem{}}
		paths, latest, err := listGeneratedFilesWithSuffix(inst, filepath.Join(root, ".vscode", "missing-prompts"), ".prompt.md")
		if err != nil {
			t.Fatalf("listGeneratedFilesWithSuffix: %v", err)
		}
		if len(paths) != 0 {
			t.Fatalf("expected no paths for missing root, got %#v", paths)
		}
		if !latest.IsZero() {
			t.Fatalf("expected zero latest time for missing root, got %s", latest)
		}
	})

	t.Run("root stat error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.statErrs[normalizePath(promptRoot)] = errors.New("stat boom")
		inst := &installer{root: root, sys: sys}
		_, _, err := listGeneratedFilesWithSuffix(inst, promptRoot, ".prompt.md")
		if err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected root stat error, got %v", err)
		}
	})

	t.Run("ignores non-generated files", func(t *testing.T) {
		manualPromptPath := filepath.Join(promptRoot, "manual.prompt.md")
		otherPath := filepath.Join(promptRoot, "notes.txt")
		if err := os.WriteFile(manualPromptPath, []byte("manual\n"), 0o644); err != nil {
			t.Fatalf("write manual prompt: %v", err)
		}
		if err := os.WriteFile(otherPath, []byte("notes\n"), 0o644); err != nil {
			t.Fatalf("write notes: %v", err)
		}

		if err := os.Remove(promptPath); err != nil {
			t.Fatalf("remove generated prompt: %v", err)
		}

		inst := &installer{root: root, sys: RealSystem{}}
		paths, latest, err := listGeneratedFilesWithSuffix(inst, promptRoot, ".prompt.md")
		if err != nil {
			t.Fatalf("listGeneratedFilesWithSuffix: %v", err)
		}
		if len(paths) != 0 {
			t.Fatalf("expected no generated prompt paths, got %#v", paths)
		}
		if !latest.IsZero() {
			t.Fatalf("expected zero latest time for non-generated prompts, got %s", latest)
		}
	})
}

func TestExactTemplateMatcher_Branches(t *testing.T) {
	matcher := exactTemplateMatcher("launchers/open-vscode.command")
	templateData, err := templates.Read("launchers/open-vscode.command")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	matched, err := matcher(templateData)
	if err != nil {
		t.Fatalf("matcher error: %v", err)
	}
	if !matched {
		t.Fatal("expected exact template match")
	}
	matched, err = matcher([]byte("not-template"))
	if err != nil {
		t.Fatalf("matcher error: %v", err)
	}
	if matched {
		t.Fatal("expected non-template content to fail match")
	}
}

func TestSortedMapKeys_Sorts(t *testing.T) {
	keys := sortedMapKeys(map[string]string{
		"beta":  "2",
		"alpha": "1",
	})
	if len(keys) != 2 || keys[0] != "alpha" || keys[1] != "beta" {
		t.Fatalf("unexpected key order: %#v", keys)
	}
}
