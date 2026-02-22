package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestUpgradeCmd_HelpShowsApplyFlagsWithoutForce(t *testing.T) {
	cmd := newUpgradeCmd()
	cmd.SetArgs([]string{"--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute upgrade --help: %v", err)
	}
	help := out.String()
	if strings.Contains(help, "--force") {
		t.Fatalf("expected --force to be removed from help output:\n%s", help)
	}
	for _, flag := range []string{
		"--yes",
		"--apply-managed-updates",
		"--apply-memory-updates",
		"--apply-deletions",
	} {
		if !strings.Contains(help, flag) {
			t.Fatalf("expected help output to include %s:\n%s", flag, help)
		}
	}
}

func TestUpgradeRollbackCmd_RequiresSnapshotID(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		cmd.SetArgs([]string{"rollback"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error")
		}
		if err.Error() != messages.UpgradeRollbackRequiresSnapshotID {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestUpgradeRollbackCmd_InvokesInstallRollback(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	origRollback := installRollbackUpgradeSnapshot
	called := false
	installRollbackUpgradeSnapshot = func(gotRoot string, snapshotID string, opts install.RollbackUpgradeSnapshotOptions) error {
		called = true
		if canonicalPath(gotRoot) != canonicalPath(root) {
			t.Fatalf("rollback root = %q, want %q", gotRoot, root)
		}
		if snapshotID != "snapshot-123" {
			t.Fatalf("snapshot id = %q, want snapshot-123", snapshotID)
		}
		if opts.System == nil {
			t.Fatal("opts.System = nil, want non-nil")
		}
		return nil
	}
	t.Cleanup(func() { installRollbackUpgradeSnapshot = origRollback })

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		var out bytes.Buffer
		cmd.SetArgs([]string{"rollback", "snapshot-123"})
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade rollback: %v", err)
		}
		if !called {
			t.Fatal("expected installRollbackUpgradeSnapshot to be called")
		}
		if !strings.Contains(out.String(), "snapshot-123") {
			t.Fatalf("expected success output with snapshot id, got %q", out.String())
		}
	})
}

func TestUpgradeRollbackCmd_PropagatesInstallErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	sentinel := errors.New("rollback failed")
	origRollback := installRollbackUpgradeSnapshot
	installRollbackUpgradeSnapshot = func(string, string, install.RollbackUpgradeSnapshotOptions) error {
		return sentinel
	}
	t.Cleanup(func() { installRollbackUpgradeSnapshot = origRollback })

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		cmd.SetArgs([]string{"rollback", "snapshot-123"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		err := cmd.Execute()
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected sentinel error, got %v", err)
		}
	})
}

func TestUpgradeRollbackCmd_ListNoSnapshots(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		var out bytes.Buffer
		cmd.SetArgs([]string{"rollback", "--list"})
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade rollback --list: %v", err)
		}
		if !strings.Contains(out.String(), messages.UpgradeRollbackNoSnapshots) {
			t.Fatalf("expected no snapshots message, got %q", out.String())
		}
	})
}

func TestUpgradeRollbackCmd_ListPrintsSnapshots(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	snapshotDir := filepath.Join(root, ".agent-layer", "state", "upgrade-snapshots")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshots: %v", err)
	}
	snapshotJSON := fmt.Sprintf(`{"schema_version":1,"snapshot_id":"snapshot-123","created_at_utc":"%s","status":"applied","entries":[]}`, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(snapshotDir, "snapshot-123.json"), []byte(snapshotJSON), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		var out bytes.Buffer
		cmd.SetArgs([]string{"rollback", "--list"})
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade rollback --list: %v", err)
		}
		output := out.String()
		if !strings.Contains(output, messages.UpgradeRollbackListHeader) {
			t.Fatalf("expected list header, got %q", output)
		}
		if !strings.Contains(output, "snapshot-123") {
			t.Fatalf("expected snapshot entry, got %q", output)
		}
		if !strings.Contains(output, "status: applied") {
			t.Fatalf("expected snapshot status, got %q", output)
		}
	})
}

func TestUpgradeRollbackCmd_ListRejectsPositionalArgs(t *testing.T) {
	cmd := newUpgradeRollbackCmd()
	if err := cmd.Flags().Set("list", "true"); err != nil {
		t.Fatalf("set --list: %v", err)
	}
	err := cmd.Args(cmd, []string{"snapshot-123"})
	if err == nil {
		t.Fatal("expected positional args to be rejected with --list")
	}
	if !strings.Contains(err.Error(), `unknown command "snapshot-123"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpgradePrefetchCmd_UsesVersionFlagAndCallsDispatch(t *testing.T) {
	origPrefetch := dispatchPrefetchVersion
	var gotVersion string
	dispatchPrefetchVersion = func(versionInput string, progressOut io.Writer) error {
		gotVersion = versionInput
		return nil
	}
	t.Cleanup(func() { dispatchPrefetchVersion = origPrefetch })

	cmd := newUpgradeCmd()
	var out bytes.Buffer
	cmd.SetArgs([]string{"prefetch", "--version", "1.2.3"})
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString(""))

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute upgrade prefetch: %v", err)
	}
	if gotVersion != "1.2.3" {
		t.Fatalf("prefetch version = %q, want 1.2.3", gotVersion)
	}
	if !strings.Contains(out.String(), "1.2.3") {
		t.Fatalf("expected success output to include version, got %q", out.String())
	}
}

func TestUpgradePrefetchCmd_DevBuildRequiresVersion(t *testing.T) {
	origVersion := Version
	Version = "dev"
	t.Cleanup(func() { Version = origVersion })

	cmd := newUpgradeCmd()
	cmd.SetArgs([]string{"prefetch"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString(""))

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing prefetch --version in dev builds")
	}
	if err.Error() != messages.UpgradePrefetchVersionRequired {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpgradeRepairGitignoreBlockCmd_InvokesRepair(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	origRepair := installRepairGitignoreBlock
	called := false
	installRepairGitignoreBlock = func(gotRoot string, opts install.RepairGitignoreBlockOptions) error {
		called = true
		if canonicalPath(gotRoot) != canonicalPath(root) {
			t.Fatalf("repair root = %q, want %q", gotRoot, root)
		}
		if opts.System == nil {
			t.Fatal("opts.System = nil, want non-nil")
		}
		return nil
	}
	t.Cleanup(func() { installRepairGitignoreBlock = origRepair })

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		var out bytes.Buffer
		cmd.SetArgs([]string{"repair-gitignore-block"})
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade repair-gitignore-block: %v", err)
		}
		if !called {
			t.Fatal("expected installRepairGitignoreBlock to be called")
		}
		if !strings.Contains(out.String(), "Repaired") {
			t.Fatalf("expected repair success output, got %q", out.String())
		}
	})
}
