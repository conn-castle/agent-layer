package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestNewUpgradePlanCmd_NilDiffLines(t *testing.T) {
	root := prepareUpgradeTestRepo(t)
	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradePlanCmd(nil)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error for nil diff-lines pointer")
		}
		if !strings.Contains(err.Error(), "invalid value for --diff-lines") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestBuildUpgradePrompter_ErrorPaths(t *testing.T) {
	t.Run("ConfigSetDefault write error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		out := &errorWriter{failAfter: 0}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetIn(strings.NewReader(""))

		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		_, err := p.ConfigSetDefault("new.required", "value", "because", nil)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("ConfigSetDefault prompt read error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(errorReader{})

		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		_, err := p.ConfigSetDefault("new.required", "value", "because", nil)
		if err == nil || !strings.Contains(err.Error(), "read failed") {
			t.Fatalf("expected read failure, got %v", err)
		}
	})

	t.Run("OverwriteAll preview print error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		out := &errorWriter{failAfter: 0}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetIn(strings.NewReader("y\n"))

		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		_, err := p.OverwriteAll([]install.DiffPreview{{Path: "a.txt"}})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("OverwriteAllUnified managed prompt error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("maybe"))

		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		_, _, err := p.OverwriteAllUnified([]install.DiffPreview{{Path: "a.txt"}}, nil)
		if err == nil || !strings.Contains(err.Error(), "invalid response") {
			t.Fatalf("expected invalid response error, got %v", err)
		}
	})

	t.Run("OverwriteAllUnified memory prompt error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("maybe"))

		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		_, _, err := p.OverwriteAllUnified(nil, []install.DiffPreview{{Path: "docs/agent-layer/ROADMAP.md"}})
		if err == nil || !strings.Contains(err.Error(), "invalid response") {
			t.Fatalf("expected invalid response error, got %v", err)
		}
	})

	t.Run("OverwritePreview print error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		out := &errorWriter{failAfter: 0}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetIn(strings.NewReader("y\n"))

		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		_, err := p.Overwrite(install.DiffPreview{Path: "x.txt"})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("DeleteUnknownAll list print error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		out := &errorWriter{failAfter: 0}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetIn(strings.NewReader("n\n"))

		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		_, err := p.DeleteUnknownAll([]string{"unknown.txt"})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("DeleteUnknown prompt error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		out := &errorWriter{failAfter: 0}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetIn(strings.NewReader("n\n"))

		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		_, err := p.DeleteUnknown("unknown.txt")
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})
}

func TestPromptUnifiedUpgradeReview_Branches(t *testing.T) {
	t.Run("prompted returns immediately", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		state := &upgradeReviewState{prompted: true}
		if err := promptUnifiedUpgradeReview(cmd, strings.NewReader(""), state); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("managed preview print error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		out := &errorWriter{failAfter: 0}
		cmd.SetOut(out)
		cmd.SetErr(out)
		state := &upgradeReviewState{managedPreviews: []install.DiffPreview{{Path: "a"}}}
		err := promptUnifiedUpgradeReview(cmd, strings.NewReader(""), state)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("managed prompt error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		state := &upgradeReviewState{managedPreviews: []install.DiffPreview{{Path: "a"}}}
		err := promptUnifiedUpgradeReview(cmd, strings.NewReader("maybe"), state)
		if err == nil || !strings.Contains(err.Error(), "invalid response") {
			t.Fatalf("expected invalid response error, got %v", err)
		}
	})

	t.Run("memory prompt error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		state := &upgradeReviewState{memoryPreviews: []install.DiffPreview{{Path: "b"}}}
		err := promptUnifiedUpgradeReview(cmd, strings.NewReader("maybe"), state)
		if err == nil || !strings.Contains(err.Error(), "invalid response") {
			t.Fatalf("expected invalid response error, got %v", err)
		}
	})

	t.Run("no previews still marks prompted", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		state := &upgradeReviewState{}
		if err := promptUnifiedUpgradeReview(cmd, strings.NewReader(""), state); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !state.prompted {
			t.Fatal("expected state to be marked prompted")
		}
	})
}

func TestWriteUpgradeSkippedCategoryNotes_WriteErrors(t *testing.T) {
	t.Run("managed note write error", func(t *testing.T) {
		err := writeUpgradeSkippedCategoryNotes(&errorWriter{failAfter: 0}, upgradeApplyPolicy{
			explicitCategory: true,
			applyManaged:     false,
			applyMemory:      true,
			applyDeletions:   true,
		})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("memory note write error", func(t *testing.T) {
		err := writeUpgradeSkippedCategoryNotes(&errorWriter{failAfter: 0}, upgradeApplyPolicy{
			explicitCategory: true,
			applyManaged:     true,
			applyMemory:      false,
			applyDeletions:   true,
		})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("deletion note write error", func(t *testing.T) {
		err := writeUpgradeSkippedCategoryNotes(&errorWriter{failAfter: 0}, upgradeApplyPolicy{
			explicitCategory: true,
			applyManaged:     true,
			applyMemory:      true,
			applyDeletions:   false,
		})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})
}

func TestUpgradeRenderHelpers_ErrorPaths(t *testing.T) {
	t.Run("renderUpgradePlanText write header error", func(t *testing.T) {
		err := renderUpgradePlanText(&errorWriter{failAfter: 0}, install.UpgradePlan{}, nil)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("writeUpgradeChangeSection empty list write error", func(t *testing.T) {
		err := writeUpgradeChangeSection(&errorWriter{failAfter: 1}, "Changes", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("writeUpgradeRenameSection empty list write error", func(t *testing.T) {
		err := writeUpgradeRenameSection(&errorWriter{failAfter: 1}, "Renames", nil)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("writeConfigMigrationSection empty list write error", func(t *testing.T) {
		err := writeConfigMigrationSection(&errorWriter{failAfter: 1}, "Config", nil)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("errWriter short-circuits after first failure", func(t *testing.T) {
		out := &errorWriter{failAfter: 0}
		ew := &errWriter{w: out}
		ew.printf("one")
		ew.println("two")
		if ew.err == nil || !strings.Contains(ew.err.Error(), "write failed") {
			t.Fatalf("expected first write failure, got %v", ew.err)
		}
		if out.writes != 1 {
			t.Fatalf("expected one attempted write after short-circuit, got %d", out.writes)
		}
	})
}

func TestWriteUnifiedDiff_ErrorPaths(t *testing.T) {
	t.Run("no trailing newline fprint error", func(t *testing.T) {
		err := writeUnifiedDiff(&errorWriter{failAfter: 0}, "-old", false, "")
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("trailing newline fprintf error", func(t *testing.T) {
		err := writeUnifiedDiff(&errorWriter{failAfter: 0}, "-old\n", false, "")
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})
}

func TestWriteReadinessSection_ErrorPaths(t *testing.T) {
	t.Run("empty checks", func(t *testing.T) {
		var out bytes.Buffer
		if err := writeReadinessSection(&out, nil); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !strings.Contains(out.String(), "(none)") {
			t.Fatalf("expected empty marker, got %q", out.String())
		}
	})

	t.Run("recommendation write error", func(t *testing.T) {
		err := writeReadinessSection(&errorWriter{failAfter: 2}, []install.UpgradeReadinessCheck{{
			ID:      "unrecognized_config_keys",
			Summary: "ignored",
		}})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("detail write error", func(t *testing.T) {
		err := writeReadinessSection(&errorWriter{failAfter: 2}, []install.UpgradeReadinessCheck{{
			ID:      "unknown",
			Summary: "summary",
			Details: []string{"detail"},
		}})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("truncation write error", func(t *testing.T) {
		err := writeReadinessSection(&errorWriter{failAfter: 5}, []install.UpgradeReadinessCheck{{
			ID:      "unknown",
			Summary: "summary",
			Details: []string{"a", "b", "c", "d"},
		}})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})
}

func TestWriteUpgradeSummary_Branches(t *testing.T) {
	t.Run("needs review true", func(t *testing.T) {
		var out bytes.Buffer
		err := writeUpgradeSummary(&out, install.UpgradePlan{
			ReadinessChecks: []install.UpgradeReadinessCheck{{ID: "warn"}},
		})
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !strings.Contains(out.String(), "needs review before apply: yes") {
			t.Fatalf("expected yes review line, got %q", out.String())
		}
	})

	t.Run("write error", func(t *testing.T) {
		err := writeUpgradeSummary(&errorWriter{failAfter: 0}, install.UpgradePlan{})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})
}

func TestPromptConfigChoice_DefaultBranchErrors(t *testing.T) {
	unknown := config.FieldDef{Key: "foo.bar", Type: "string"}

	t.Run("value write error", func(t *testing.T) {
		_, err := promptConfigChoice(strings.NewReader(""), &errorWriter{failAfter: 0}, "foo.bar", "x", unknown)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("prompt read error", func(t *testing.T) {
		_, err := promptConfigChoice(errorReader{}, &bytes.Buffer{}, "foo.bar", "x", unknown)
		if err == nil || !strings.Contains(err.Error(), "read failed") {
			t.Fatalf("expected read failure, got %v", err)
		}
	})

	t.Run("decline default value", func(t *testing.T) {
		_, err := promptConfigChoice(strings.NewReader("n\n"), &bytes.Buffer{}, "foo.bar", "x", unknown)
		if err == nil || !strings.Contains(err.Error(), "user declined default value") {
			t.Fatalf("expected decline error, got %v", err)
		}
	})
}

func TestPromptNumberedChoice_ErrorPaths(t *testing.T) {
	t.Run("invalid choice at EOF", func(t *testing.T) {
		_, err := promptNumberedChoice(strings.NewReader("abc"), &bytes.Buffer{}, []string{"one"}, 0)
		if err == nil || !strings.Contains(err.Error(), "invalid choice") {
			t.Fatalf("expected invalid choice error, got %v", err)
		}
	})

	t.Run("retry write error", func(t *testing.T) {
		_, err := promptNumberedChoice(strings.NewReader("abc\n1\n"), &errorWriter{failAfter: 3}, []string{"one"}, 0)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})
}

func TestWriteSinglePreviewBlock_WhitespaceDiffNoOp(t *testing.T) {
	var out bytes.Buffer
	err := writeSinglePreviewBlock(&out, install.DiffPreview{UnifiedDiff: "   \n\t"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no output for whitespace diff, got %q", out.String())
	}
}

func TestWritePinVersionSection_WriteError(t *testing.T) {
	err := writePinVersionSection(&errorWriter{failAfter: 0}, install.UpgradePinVersionDiff{})
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected write failure, got %v", err)
	}
}

func TestPromptUnifiedUpgradeReview_ManagedThenMemory(t *testing.T) {
	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	state := &upgradeReviewState{
		managedPreviews: []install.DiffPreview{{Path: "managed.txt"}},
		memoryPreviews:  []install.DiffPreview{{Path: "docs/agent-layer/ROADMAP.md"}},
	}

	err := promptUnifiedUpgradeReview(cmd, strings.NewReader("y\nn\n"), state)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !state.applyManaged {
		t.Fatal("expected applyManaged=true")
	}
	if state.applyMemory {
		t.Fatal("expected applyMemory=false")
	}
	if !state.prompted {
		t.Fatal("expected prompted=true")
	}
}

func TestPromptConfigChoice_DefaultAccept(t *testing.T) {
	unknown := config.FieldDef{Key: "foo.bar", Type: "string"}
	val, err := promptConfigChoice(strings.NewReader("y\n"), io.Discard, "foo.bar", "x", unknown)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if val != "x" {
		t.Fatalf("expected accepted value 'x', got %v", val)
	}
}

func TestUpgradeCmd_ErrorBranches(t *testing.T) {
	t.Run("write skipped notes error", func(t *testing.T) {
		root := prepareUpgradeTestRepo(t)
		origIsTerminal := isTerminal
		origInstallRun := installRun
		t.Cleanup(func() {
			isTerminal = origIsTerminal
			installRun = origInstallRun
		})
		isTerminal = func() bool { return false }
		installRun = func(string, install.Options) error { return nil }

		testutil.WithWorkingDir(t, root, func() {
			cmd := newUpgradeCmd()
			cmd.SetArgs([]string{"--yes", "--apply-managed-updates"})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&errorWriter{failAfter: 0})
			cmd.SetIn(strings.NewReader(""))

			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "write failed") {
				t.Fatalf("expected write failure, got %v", err)
			}
		})
	})

	t.Run("resolve latest pin error", func(t *testing.T) {
		root := prepareUpgradeTestRepo(t)
		origIsTerminal := isTerminal
		origInstallRun := installRun
		origResolveLatest := resolveLatestPinVersion
		t.Cleanup(func() {
			isTerminal = origIsTerminal
			installRun = origInstallRun
			resolveLatestPinVersion = origResolveLatest
		})
		isTerminal = func() bool { return false }
		installRun = func(string, install.Options) error { return nil }
		resolveLatestPinVersion = func(context.Context, string) (string, error) {
			return "", errors.New("latest unavailable")
		}

		testutil.WithWorkingDir(t, root, func() {
			cmd := newUpgradeCmd()
			cmd.SetArgs([]string{"--yes", "--apply-managed-updates", "--version", "latest"})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetIn(strings.NewReader(""))

			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "latest unavailable") {
				t.Fatalf("expected latest-resolution error, got %v", err)
			}
		})
	})
}

func TestUpgradeSubcommands_ErrorBranches(t *testing.T) {
	t.Run("rollback resolve root error", func(t *testing.T) {
		root := t.TempDir()
		testutil.WithWorkingDir(t, root, func() {
			cmd := newUpgradeCmd()
			cmd.SetArgs([]string{"rollback", "snapshot-123"})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			if err == nil || err.Error() != messages.RootMissingAgentLayer {
				t.Fatalf("expected missing root error, got %v", err)
			}
		})
	})

	t.Run("rollback list snapshots error", func(t *testing.T) {
		if os.Getenv("SKIP_CHMOD_TESTS") != "" {
			t.Skip("chmod-restricted test skipped by env")
		}
		root := t.TempDir()
		snapshotDir := filepath.Join(root, ".agent-layer", "state", "upgrade-snapshots")
		if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
			t.Fatalf("mkdir snapshots dir: %v", err)
		}
		if err := os.Chmod(snapshotDir, 0o000); err != nil {
			t.Fatalf("chmod snapshots dir: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chmod(snapshotDir, 0o755)
		})

		testutil.WithWorkingDir(t, root, func() {
			cmd := newUpgradeCmd()
			cmd.SetArgs([]string{"rollback", "--list"})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected list error from unreadable snapshot dir")
			}
		})
	})

	t.Run("prefetch resolve latest error", func(t *testing.T) {
		origResolveLatest := resolveLatestPinVersion
		t.Cleanup(func() { resolveLatestPinVersion = origResolveLatest })
		resolveLatestPinVersion = func(context.Context, string) (string, error) {
			return "", errors.New("latest unavailable")
		}

		cmd := newUpgradeCmd()
		cmd.SetArgs([]string{"prefetch", "--version", "latest"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "latest unavailable") {
			t.Fatalf("expected latest-resolution error, got %v", err)
		}
	})

	t.Run("prefetch dispatch error", func(t *testing.T) {
		origPrefetch := dispatchPrefetchVersion
		t.Cleanup(func() { dispatchPrefetchVersion = origPrefetch })
		dispatchPrefetchVersion = func(string, io.Writer) error { return errors.New("dispatch failed") }

		cmd := newUpgradeCmd()
		cmd.SetArgs([]string{"prefetch", "--version", "1.2.3"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		err := cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "dispatch failed") {
			t.Fatalf("expected dispatch error, got %v", err)
		}
	})

	t.Run("repair resolve root error", func(t *testing.T) {
		root := t.TempDir()
		testutil.WithWorkingDir(t, root, func() {
			cmd := newUpgradeCmd()
			cmd.SetArgs([]string{"repair-gitignore-block"})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			if err == nil || err.Error() != messages.RootMissingAgentLayer {
				t.Fatalf("expected missing root error, got %v", err)
			}
		})
	})

	t.Run("repair install error", func(t *testing.T) {
		root := prepareUpgradeTestRepo(t)
		origRepair := installRepairGitignoreBlock
		t.Cleanup(func() { installRepairGitignoreBlock = origRepair })
		installRepairGitignoreBlock = func(string, install.RepairGitignoreBlockOptions) error {
			return errors.New("repair failed")
		}
		testutil.WithWorkingDir(t, root, func() {
			cmd := newUpgradeCmd()
			cmd.SetArgs([]string{"repair-gitignore-block"})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "repair failed") {
				t.Fatalf("expected repair error, got %v", err)
			}
		})
	})
}

func TestUpgradePlanCmd_ErrorBranches(t *testing.T) {
	t.Run("resolve root error", func(t *testing.T) {
		root := t.TempDir()
		testutil.WithWorkingDir(t, root, func() {
			diffLines := install.DefaultDiffMaxLines
			cmd := newUpgradePlanCmd(&diffLines)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			if err == nil || err.Error() != messages.RootMissingAgentLayer {
				t.Fatalf("expected missing root error, got %v", err)
			}
		})
	})

	t.Run("resolve latest error", func(t *testing.T) {
		root := prepareUpgradeTestRepo(t)
		origResolveLatest := resolveLatestPinVersion
		t.Cleanup(func() { resolveLatestPinVersion = origResolveLatest })
		resolveLatestPinVersion = func(context.Context, string) (string, error) {
			return "", errors.New("latest unavailable")
		}
		testutil.WithWorkingDir(t, root, func() {
			diffLines := install.DefaultDiffMaxLines
			cmd := newUpgradePlanCmd(&diffLines)
			cmd.SetArgs([]string{"--version", "latest"})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "latest unavailable") {
				t.Fatalf("expected latest-resolution error, got %v", err)
			}
		})
	})
}

func TestRenderUpgradePlanText_SectionPropagationBranches(t *testing.T) {
	plan := install.UpgradePlan{}
	tests := []struct {
		name      string
		failAfter int
	}{
		{name: "summary error", failAfter: 1},
		{name: "additions section error", failAfter: 10},
		{name: "updates section error", failAfter: 12},
		{name: "rename section error", failAfter: 14},
		{name: "removals section error", failAfter: 16},
		{name: "config section error", failAfter: 18},
		{name: "migration report section error", failAfter: 20},
		{name: "pin section error", failAfter: 22},
		{name: "readiness section error", failAfter: 26},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := renderUpgradePlanText(&errorWriter{failAfter: tt.failAfter}, plan, map[string]install.DiffPreview{})
			if err == nil || !strings.Contains(err.Error(), "write failed") {
				t.Fatalf("expected write failure, got %v", err)
			}
		})
	}
}

func TestWriteUpgradeSummary_ErrorBranches(t *testing.T) {
	plan := install.UpgradePlan{}
	for _, failAfter := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
		t.Run("failAfter="+strconv.Itoa(failAfter), func(t *testing.T) {
			err := writeUpgradeSummary(&errorWriter{failAfter: failAfter}, plan)
			if err == nil || !strings.Contains(err.Error(), "write failed") {
				t.Fatalf("expected write failure, got %v", err)
			}
		})
	}
}

func TestWriteUpgradeSections_LoopErrorBranches(t *testing.T) {
	t.Run("writeUpgradeChangeSection path write error", func(t *testing.T) {
		err := writeUpgradeChangeSection(
			&errorWriter{failAfter: 1},
			"Changes",
			[]install.UpgradeChange{{Path: "file.txt"}},
			map[string]install.DiffPreview{},
		)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("writeUpgradeChangeSection preview write error", func(t *testing.T) {
		err := writeUpgradeChangeSection(
			&errorWriter{failAfter: 3},
			"Changes",
			[]install.UpgradeChange{{Path: "file.txt"}},
			map[string]install.DiffPreview{
				"file.txt": {Path: "file.txt", UnifiedDiff: "-old\n"},
			},
		)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("writeUpgradeRenameSection loop write error", func(t *testing.T) {
		err := writeUpgradeRenameSection(
			&errorWriter{failAfter: 1},
			"Renames",
			[]install.UpgradeRename{{From: "a", To: "b"}},
		)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("writeConfigMigrationSection loop write error", func(t *testing.T) {
		err := writeConfigMigrationSection(
			&errorWriter{failAfter: 1},
			"Config",
			[]install.ConfigKeyMigration{{Key: "a.b", From: "x", To: "y"}},
		)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("writeMigrationReportSection source-note write error", func(t *testing.T) {
		err := writeMigrationReportSection(
			&errorWriter{failAfter: 3},
			"Migrations",
			install.UpgradeMigrationReport{
				TargetVersion:         "1.2.3",
				SourceVersion:         "1.2.2",
				SourceVersionOrigin:   "pin",
				SourceResolutionNotes: []string{"note-a"},
				Entries: []install.UpgradeMigrationEntry{
					{ID: "m1", Kind: "rename_file", Rationale: "because", Status: install.UpgradeMigrationStatusPlanned},
				},
			},
		)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})
}

func TestWriteSingleAndPrintDiffPreview_Branches(t *testing.T) {
	t.Run("writeSinglePreviewBlock header write error", func(t *testing.T) {
		err := writeSinglePreviewBlock(&errorWriter{failAfter: 0}, install.DiffPreview{UnifiedDiff: "-old\n"})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("writeSinglePreviewBlock diff write error", func(t *testing.T) {
		err := writeSinglePreviewBlock(&errorWriter{failAfter: 1}, install.DiffPreview{UnifiedDiff: "-old\n"})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	tests := []struct {
		name      string
		failAfter int
	}{
		{name: "header write error", failAfter: 1},
		{name: "path write error", failAfter: 2},
		{name: "post-list newline write error", failAfter: 3},
		{name: "diff header write error", failAfter: 4},
		{name: "diff body write error", failAfter: 5},
		{name: "diff trailing newline write error", failAfter: 6},
	}
	for _, tt := range tests {
		t.Run("printDiffPreviews_"+tt.name, func(t *testing.T) {
			err := printDiffPreviews(
				&errorWriter{failAfter: tt.failAfter},
				"header",
				[]install.DiffPreview{{Path: "a.txt", UnifiedDiff: "-old\n"}},
			)
			if err == nil || !strings.Contains(err.Error(), "write failed") {
				t.Fatalf("expected write failure, got %v", err)
			}
		})
	}
}

func TestWriteUnifiedDiff_NoTrailingNewlineSuccess(t *testing.T) {
	var out bytes.Buffer
	err := writeUnifiedDiff(&out, "-old", false, "")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if out.String() != "-old" {
		t.Fatalf("unexpected diff output: %q", out.String())
	}
}

func TestWriteReadinessSection_HeaderWriteError(t *testing.T) {
	err := writeReadinessSection(&errorWriter{failAfter: 0}, nil)
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected write failure, got %v", err)
	}
}

func TestPromptChoiceAdditionalErrorBranches(t *testing.T) {
	t.Run("promptBoolChoice uses true default", func(t *testing.T) {
		val, err := promptBoolChoice(strings.NewReader("\n"), &bytes.Buffer{}, true)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		got, ok := val.(bool)
		if !ok || !got {
			t.Fatalf("expected true selection, got %v", val)
		}
	})

	t.Run("promptBoolChoice prompt error", func(t *testing.T) {
		_, err := promptBoolChoice(errorReader{}, &bytes.Buffer{}, false)
		if err == nil || !strings.Contains(err.Error(), "read failed") {
			t.Fatalf("expected read failure, got %v", err)
		}
	})

	t.Run("promptEnumChoice prompt error", func(t *testing.T) {
		field := config.FieldDef{
			Key:  "k",
			Type: config.FieldEnum,
			Options: []config.FieldOption{
				{Value: "a"},
				{Value: "b"},
			},
		}
		_, err := promptEnumChoice(errorReader{}, &bytes.Buffer{}, "a", field)
		if err == nil || !strings.Contains(err.Error(), "read failed") {
			t.Fatalf("expected read failure, got %v", err)
		}
	})

	t.Run("promptNumberedChoice read error", func(t *testing.T) {
		_, err := promptNumberedChoice(errorReader{}, &bytes.Buffer{}, []string{"a"}, 0)
		if err == nil || !strings.Contains(err.Error(), "read failed") {
			t.Fatalf("expected read failure, got %v", err)
		}
	})
}

func TestBuildUpgradePrompter_AdditionalBranches(t *testing.T) {
	t.Run("ConfigSetDefault with field catalog", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("\n"))
		field := config.FieldDef{
			Key:  "agents.codex.enabled",
			Type: config.FieldBool,
		}
		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		value, err := p.ConfigSetDefault("agents.codex.enabled", true, "needed", &field)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		gotBool, ok := value.(bool)
		if !ok || !gotBool {
			t.Fatalf("expected bool true, got %v", value)
		}
	})

	t.Run("OverwriteAllUnified no previews", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader(""))
		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		managed, memory, err := p.OverwriteAllUnified(nil, nil)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if managed || memory {
			t.Fatalf("expected both false for empty previews, got managed=%v memory=%v", managed, memory)
		}
	})

	t.Run("Overwrite preview prompt path", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("y\n"))
		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		overwrite, err := p.Overwrite(install.DiffPreview{Path: "file.txt"})
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !overwrite {
			t.Fatal("expected overwrite approval")
		}
	})

	t.Run("DeleteUnknownAll prompt path", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("y\n"))
		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		deleteAll, err := p.DeleteUnknownAll([]string{"a.txt"})
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !deleteAll {
			t.Fatal("expected delete-all approval")
		}
	})

	t.Run("OverwriteAll review-state prompt error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("maybe"))
		state := &upgradeReviewState{
			enabled:         true,
			managedPreviews: []install.DiffPreview{{Path: "m.txt"}},
		}
		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, state)
		_, err := p.OverwriteAll([]install.DiffPreview{{Path: "m.txt"}})
		if err == nil || !strings.Contains(err.Error(), "invalid response") {
			t.Fatalf("expected invalid response, got %v", err)
		}
	})

	t.Run("OverwriteAll fallback prompt path", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("y\n"))
		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		apply, err := p.OverwriteAll([]install.DiffPreview{{Path: "m.txt"}})
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !apply {
			t.Fatal("expected managed apply approval")
		}
	})

	t.Run("OverwriteAllMemory review-state prompt error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("maybe"))
		state := &upgradeReviewState{
			enabled:        true,
			memoryPreviews: []install.DiffPreview{{Path: "docs/agent-layer/ROADMAP.md"}},
		}
		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, state)
		_, err := p.OverwriteAllMemory([]install.DiffPreview{{Path: "docs/agent-layer/ROADMAP.md"}})
		if err == nil || !strings.Contains(err.Error(), "invalid response") {
			t.Fatalf("expected invalid response, got %v", err)
		}
	})

	t.Run("OverwriteAllMemory fallback prompt path", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("n\n"))
		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		apply, err := p.OverwriteAllMemory([]install.DiffPreview{{Path: "docs/agent-layer/ROADMAP.md"}})
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if apply {
			t.Fatal("expected memory apply rejection")
		}
	})

	t.Run("OverwriteAllUnified explicit category returns policy", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader(""))
		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{
			explicitCategory: true,
			applyManaged:     true,
			applyMemory:      false,
		}, nil)
		managed, memory, err := p.OverwriteAllUnified([]install.DiffPreview{{Path: "m.txt"}}, []install.DiffPreview{{Path: "docs/agent-layer/ROADMAP.md"}})
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !managed || memory {
			t.Fatalf("unexpected explicit-category result: managed=%v memory=%v", managed, memory)
		}
	})

	t.Run("OverwriteAllUnified review-state prompt error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader("maybe"))
		state := &upgradeReviewState{
			enabled:         true,
			managedPreviews: []install.DiffPreview{{Path: "m.txt"}},
		}
		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, state)
		_, _, err := p.OverwriteAllUnified([]install.DiffPreview{{Path: "m.txt"}}, nil)
		if err == nil || !strings.Contains(err.Error(), "invalid response") {
			t.Fatalf("expected invalid response, got %v", err)
		}
	})

	t.Run("OverwriteAllUnified managed preview print error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		out := &errorWriter{failAfter: 0}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetIn(strings.NewReader("y\n"))
		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		_, _, err := p.OverwriteAllUnified([]install.DiffPreview{{Path: "m.txt"}}, nil)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("OverwriteAllUnified memory preview print error", func(t *testing.T) {
		cmd := newUpgradeCmd()
		out := &errorWriter{failAfter: 3}
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetIn(strings.NewReader("y\n"))
		p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
		_, _, err := p.OverwriteAllUnified([]install.DiffPreview{{Path: "m.txt"}}, []install.DiffPreview{{Path: "docs/agent-layer/ROADMAP.md"}})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})
}

func TestPromptUnifiedUpgradeReview_MemoryPreviewPrintError(t *testing.T) {
	cmd := newUpgradeCmd()
	out := &errorWriter{failAfter: 3}
	cmd.SetOut(out)
	cmd.SetErr(out)
	state := &upgradeReviewState{
		managedPreviews: []install.DiffPreview{{Path: "m.txt"}},
		memoryPreviews:  []install.DiffPreview{{Path: "docs/agent-layer/ROADMAP.md"}},
	}
	err := promptUnifiedUpgradeReview(cmd, strings.NewReader("y\n"), state)
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected write failure, got %v", err)
	}
}

func TestUpgradePlanCmd_BuildAndPreviewErrorBranches(t *testing.T) {
	t.Run("BuildUpgradePlan error", func(t *testing.T) {
		if os.Getenv("SKIP_CHMOD_TESTS") != "" {
			t.Skip("chmod-restricted test skipped by env")
		}
		root := prepareUpgradeTestRepo(t)
		agentLayerDir := filepath.Join(root, ".agent-layer")
		if err := os.Chmod(agentLayerDir, 0o000); err != nil {
			t.Fatalf("chmod .agent-layer: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(agentLayerDir, 0o755) })

		testutil.WithWorkingDir(t, root, func() {
			diffLines := install.DefaultDiffMaxLines
			cmd := newUpgradePlanCmd(&diffLines)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected build plan error")
			}
		})
	})

	t.Run("BuildUpgradePlanDiffPreviews error", func(t *testing.T) {
		if os.Getenv("SKIP_CHMOD_TESTS") != "" {
			t.Skip("chmod-restricted test skipped by env")
		}
		root := prepareUpgradeTestRepo(t)
		orphanPath := filepath.Join(root, "docs", "agent-layer", "UNTRACKED.md")
		if err := os.WriteFile(orphanPath, []byte("orphan"), 0o644); err != nil {
			t.Fatalf("write orphan file: %v", err)
		}
		if err := os.Chmod(orphanPath, 0o000); err != nil {
			t.Fatalf("chmod orphan file: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(orphanPath, 0o644) })

		testutil.WithWorkingDir(t, root, func() {
			diffLines := install.DefaultDiffMaxLines
			cmd := newUpgradePlanCmd(&diffLines)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected diff preview error")
			}
		})
	})
}

func TestWriteReadinessSection_SummaryWriteError(t *testing.T) {
	err := writeReadinessSection(&errorWriter{failAfter: 1}, []install.UpgradeReadinessCheck{{
		ID:      "unknown",
		Summary: "summary",
	}})
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected write failure, got %v", err)
	}
}

func TestPromptNumberedChoice_WriteBranches(t *testing.T) {
	t.Run("header write error", func(t *testing.T) {
		_, err := promptNumberedChoice(strings.NewReader(""), &errorWriter{failAfter: 0}, []string{"one"}, 0)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("option write error", func(t *testing.T) {
		_, err := promptNumberedChoice(strings.NewReader(""), &errorWriter{failAfter: 1}, []string{"one"}, 0)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("enter choice write error", func(t *testing.T) {
		_, err := promptNumberedChoice(strings.NewReader(""), &errorWriter{failAfter: 2}, []string{"one"}, 0)
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})
}
