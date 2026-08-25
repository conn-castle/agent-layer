package install

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestScanUnknowns_UpgradeKeepListSuppressesFilesAndDirectories(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, ".agent-layer", "local", "nested.txt"),
		filepath.Join(root, "docs", "agent-layer", "NOTES.md"),
		filepath.Join(root, ".agent-layer", "unknown.txt"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte("local\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	keepPath := filepath.Join(root, ".agent-layer", UpgradeKeepListFileName)
	if err := os.WriteFile(keepPath, []byte("# intentional local content\n.agent-layer/local/\ndocs/agent-layer/NOTES.md\n"), 0o600); err != nil {
		t.Fatalf("write keep list: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.scanUnknowns(); err != nil {
		t.Fatalf("scanUnknowns: %v", err)
	}
	if got, want := inst.relativeUnknowns(), []string{".agent-layer/unknown.txt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unknowns = %v, want %v", got, want)
	}
}

func TestScanUnknowns_NestedKeptFileProtectsUnknownParentDirectory(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, ".agent-layer", "local", "nested.txt")
	if err := os.MkdirAll(filepath.Dir(nested), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(nested, []byte("local\n"), 0o600); err != nil {
		t.Fatalf("write nested file: %v", err)
	}
	keepPath := filepath.Join(root, ".agent-layer", UpgradeKeepListFileName)
	if err := os.WriteFile(keepPath, []byte(".agent-layer/local/nested.txt\n"), 0o600); err != nil {
		t.Fatalf("write keep list: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.scanUnknowns(); err != nil {
		t.Fatalf("scanUnknowns: %v", err)
	}
	if got := inst.relativeUnknowns(); len(got) != 0 {
		t.Fatalf("unknowns = %v, want parent directory protected by nested keep entry", got)
	}
}

func TestHandleUnknowns_NestedKeptFileCannotBeDeletedWithParent(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, ".agent-layer", "local", "nested.txt")
	if err := os.MkdirAll(filepath.Dir(nested), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(nested, []byte("local\n"), 0o600); err != nil {
		t.Fatalf("write nested file: %v", err)
	}
	keepPath := filepath.Join(root, ".agent-layer", UpgradeKeepListFileName)
	if err := os.WriteFile(keepPath, []byte(".agent-layer/local/nested.txt\n"), 0o600); err != nil {
		t.Fatalf("write keep list: %v", err)
	}

	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			SelectUnknownsToKeepFunc: func([]string) ([]string, error) {
				t.Fatal("protected parent should not be offered for keeping")
				return nil, nil
			},
			DeleteUnknownAllFunc: func([]string) (bool, error) {
				t.Fatal("protected parent should not be offered for deletion")
				return true, nil
			},
		},
	}
	if err := inst.handleUnknowns(); err != nil {
		t.Fatalf("handleUnknowns: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("nested kept file was removed with its parent: %v", err)
	}
}

func TestHandleUnknowns_SelectsKeepPathsThenUsesExistingDeletionFlow(t *testing.T) {
	root := t.TempDir()
	keptPath := filepath.Join(root, ".agent-layer", "keep-me.txt")
	remainingPath := filepath.Join(root, "docs", "agent-layer", "review-me.md")
	for _, path := range []string{keptPath, remainingPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte("local\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	keepListPath := filepath.Join(root, ".agent-layer", UpgradeKeepListFileName)
	if err := os.WriteFile(keepListPath, []byte("# keep local upgrade intent\n.agent-layer/temporarily-missing\n"), 0o600); err != nil {
		t.Fatalf("seed keep list: %v", err)
	}

	var offered, deletionPaths []string
	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			SelectUnknownsToKeepFunc: func(paths []string) ([]string, error) {
				offered = append([]string(nil), paths...)
				return []string{".agent-layer/keep-me.txt"}, nil
			},
			DeleteUnknownAllFunc: func(paths []string) (bool, error) {
				deletionPaths = append([]string(nil), paths...)
				return false, nil
			},
			DeleteUnknownFunc: func(string) (bool, error) { return false, nil },
		},
	}
	if err := inst.handleUnknowns(); err != nil {
		t.Fatalf("handleUnknowns: %v", err)
	}
	if want := []string{".agent-layer/keep-me.txt", "docs/agent-layer/review-me.md"}; !reflect.DeepEqual(offered, want) {
		t.Fatalf("keep options = %v, want %v", offered, want)
	}
	if want := []string{"docs/agent-layer/review-me.md"}; !reflect.DeepEqual(deletionPaths, want) {
		t.Fatalf("deletion paths = %v, want %v", deletionPaths, want)
	}
	data, err := os.ReadFile(keepListPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read keep list: %v", err)
	}
	if got, want := string(data), "# keep local upgrade intent\n.agent-layer/temporarily-missing\n.agent-layer/keep-me.txt\n"; got != want {
		t.Fatalf("keep list = %q, want %q", got, want)
	}
}

func TestHandleUnknowns_NoSelectionDoesNotCreateKeepList(t *testing.T) {
	inst, _ := setupUnknownFile(t)
	inst.prompter = PromptFuncs{
		SelectUnknownsToKeepFunc: func([]string) ([]string, error) { return nil, nil },
		DeleteUnknownAllFunc:     func([]string) (bool, error) { return false, nil },
		DeleteUnknownFunc:        func(string) (bool, error) { return false, nil },
	}
	if err := inst.handleUnknowns(); err != nil {
		t.Fatalf("handleUnknowns: %v", err)
	}
	if _, err := os.Stat(filepath.Join(inst.root, ".agent-layer", UpgradeKeepListFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("keep list should not exist, stat error = %v", err)
	}
}

func TestHandleUnknowns_AllPathsAlreadyKeptDoesNotPrompt(t *testing.T) {
	root := t.TempDir()
	unknownPath := filepath.Join(root, "docs", "agent-layer", "NOTES.md")
	if err := os.MkdirAll(filepath.Dir(unknownPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(unknownPath, []byte("local\n"), 0o600); err != nil {
		t.Fatalf("write unknown: %v", err)
	}
	keepPath := filepath.Join(root, ".agent-layer", UpgradeKeepListFileName)
	if err := os.MkdirAll(filepath.Dir(keepPath), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.WriteFile(keepPath, []byte("docs/agent-layer/NOTES.md\n"), 0o600); err != nil {
		t.Fatalf("write keep list: %v", err)
	}

	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			SelectUnknownsToKeepFunc: func([]string) ([]string, error) {
				t.Fatal("keep selection should not be prompted")
				return nil, nil
			},
			DeleteUnknownAllFunc: func([]string) (bool, error) {
				t.Fatal("bulk deletion should not be prompted")
				return false, nil
			},
			DeleteUnknownFunc: func(string) (bool, error) {
				t.Fatal("per-path deletion should not be prompted")
				return false, nil
			},
		},
	}
	if err := inst.handleUnknowns(); err != nil {
		t.Fatalf("handleUnknowns: %v", err)
	}
}

func TestLoadUpgradeKeepList_RejectsInvalidPaths(t *testing.T) {
	tests := []string{"../outside\n", "README.md\n", ".agent-layer/tmp/run.log\n", ".agent-layer/upgrade-keep-list\n"}
	for _, content := range tests {
		t.Run(strings.TrimSpace(content), func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".agent-layer", UpgradeKeepListFileName)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatalf("write keep list: %v", err)
			}
			inst := &installer{root: root, sys: RealSystem{}}
			if _, err := inst.loadUpgradeKeepList(); err == nil || !strings.Contains(err.Error(), "line 1") {
				t.Fatalf("loadUpgradeKeepList error = %v", err)
			}
		})
	}
}

func TestBuildUpgradePlan_SuppressesKeptOrphans(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	seedWorkflowBundleForTest(t, root)
	keptFiles := []string{
		filepath.Join(root, ".agent-layer", "skills", "local-skill", "SKILL.md"),
		filepath.Join(root, "docs", "agent-layer", "NOTES.md"),
	}
	for _, path := range keptFiles {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte("local\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	keepData := ".agent-layer/skills/local-skill\ndocs/agent-layer/NOTES.md\n"
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", UpgradeKeepListFileName), []byte(keepData), 0o600); err != nil {
		t.Fatalf("write keep list: %v", err)
	}

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	if err != nil {
		t.Fatalf("BuildUpgradePlan: %v", err)
	}
	for _, change := range plan.TemplateRemovalsOrOrphans {
		if change.Path == ".agent-layer/skills/local-skill/SKILL.md" || change.Path == "docs/agent-layer/NOTES.md" {
			t.Fatalf("kept path still appeared in plan: %s", change.Path)
		}
	}
}
