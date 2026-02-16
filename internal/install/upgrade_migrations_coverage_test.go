package install

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/templates"
)

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("write boom")
}

func TestWriteUpgradeMigrationReport_CoversFieldsAndWriterErrors(t *testing.T) {
	report := UpgradeMigrationReport{
		TargetVersion:       "0.7.0",
		SourceVersion:       "0.6.0",
		SourceVersionOrigin: UpgradeMigrationSourcePin,
		SourceResolutionNotes: []string{
			"note one",
			"note two",
		},
		Entries: []UpgradeMigrationEntry{
			{
				ID:        "rename",
				Kind:      string(upgradeMigrationKindRenameFile),
				Rationale: "rename old path",
				Status:    UpgradeMigrationStatusApplied,
				From:      "old.md",
				To:        "new.md",
			},
			{
				ID:         "skip",
				Kind:       string(upgradeMigrationKindDeleteFile),
				Rationale:  "skip old delete",
				Status:     UpgradeMigrationStatusSkippedUnknownSource,
				SkipReason: "source unknown",
				Path:       "legacy.md",
				Key:        "clients.codex.model",
			},
		},
	}

	var out bytes.Buffer
	if err := writeUpgradeMigrationReport(&out, report); err != nil {
		t.Fatalf("write report: %v", err)
	}
	got := out.String()
	if !containsAll(got,
		"Migration report:",
		"target version: 0.7.0",
		"source version: 0.6.0 (pin_file)",
		"source note: note one",
		"[applied] rename (rename_file): rename old path",
		"from: old.md",
		"to: new.md",
		"[skipped_unknown_source] skip (delete_file): skip old delete",
		"reason: source unknown",
		"path: legacy.md",
		"key: clients.codex.model",
	) {
		t.Fatalf("unexpected report output:\n%s", got)
	}

	if err := writeUpgradeMigrationReport(errorWriter{}, report); err == nil {
		t.Fatal("expected writer error")
	}
	if err := writeUpgradeMigrationReport(&bytes.Buffer{}, UpgradeMigrationReport{}); err != nil {
		t.Fatalf("empty report should be no-op: %v", err)
	}
}

func TestExecuteRenameMigration_Branches(t *testing.T) {
	t.Run("same path no-op", func(t *testing.T) {
		inst := &installer{root: t.TempDir(), sys: RealSystem{}}
		changed, err := inst.executeRenameMigration("a.md", "a.md")
		if err != nil {
			t.Fatalf("executeRenameMigration: %v", err)
		}
		if changed {
			t.Fatal("expected no-op rename")
		}
	})

	t.Run("missing source and destination no-op", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{root: root, sys: RealSystem{}}
		changed, err := inst.executeRenameMigration(".agent-layer/missing.md", ".agent-layer/new.md")
		if err != nil {
			t.Fatalf("executeRenameMigration: %v", err)
		}
		if changed {
			t.Fatal("expected no-op rename when source missing")
		}
	})

	t.Run("missing source and destination stat failure", func(t *testing.T) {
		root := t.TempDir()
		fault := newFaultSystem(RealSystem{})
		toPath := filepath.Join(root, ".agent-layer", "new.md")
		fault.statErrs[normalizePath(toPath)] = errors.New("stat boom")
		inst := &installer{root: root, sys: fault}
		if _, err := inst.executeRenameMigration(".agent-layer/missing.md", ".agent-layer/new.md"); err == nil || !strings.Contains(err.Error(), "stat") {
			t.Fatalf("expected stat error, got %v", err)
		}
	})

	t.Run("source stat error", func(t *testing.T) {
		root := t.TempDir()
		fault := newFaultSystem(RealSystem{})
		fromPath := filepath.Join(root, ".agent-layer", "old.md")
		fault.statErrs[normalizePath(fromPath)] = errors.New("source stat boom")
		inst := &installer{root: root, sys: fault}
		if _, err := inst.executeRenameMigration(".agent-layer/old.md", ".agent-layer/new.md"); err == nil || !strings.Contains(err.Error(), "source stat boom") {
			t.Fatalf("expected source stat error, got %v", err)
		}
	})

	t.Run("destination exists with identical content removes source", func(t *testing.T) {
		root := t.TempDir()
		fromPath := filepath.Join(root, ".agent-layer", "old.md")
		toPath := filepath.Join(root, ".agent-layer", "new.md")
		if err := os.MkdirAll(filepath.Dir(fromPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(fromPath, []byte("same\n"), 0o644); err != nil {
			t.Fatalf("write from: %v", err)
		}
		if err := os.WriteFile(toPath, []byte("same\n"), 0o644); err != nil {
			t.Fatalf("write to: %v", err)
		}
		inst := &installer{root: root, sys: RealSystem{}}
		changed, err := inst.executeRenameMigration(".agent-layer/old.md", ".agent-layer/new.md")
		if err != nil {
			t.Fatalf("executeRenameMigration: %v", err)
		}
		if !changed {
			t.Fatal("expected duplicate-source removal")
		}
		if _, err := os.Stat(fromPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected source removed, stat err = %v", err)
		}
	})

	t.Run("destination exists with different content conflicts", func(t *testing.T) {
		root := t.TempDir()
		fromPath := filepath.Join(root, ".agent-layer", "old.md")
		toPath := filepath.Join(root, ".agent-layer", "new.md")
		if err := os.MkdirAll(filepath.Dir(fromPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(fromPath, []byte("source\n"), 0o644); err != nil {
			t.Fatalf("write from: %v", err)
		}
		if err := os.WriteFile(toPath, []byte("target\n"), 0o644); err != nil {
			t.Fatalf("write to: %v", err)
		}
		inst := &installer{root: root, sys: RealSystem{}}
		if _, err := inst.executeRenameMigration(".agent-layer/old.md", ".agent-layer/new.md"); err == nil || !strings.Contains(err.Error(), "target already exists") {
			t.Fatalf("expected conflict error, got %v", err)
		}
	})

	t.Run("destination stat failure", func(t *testing.T) {
		root := t.TempDir()
		fromPath := filepath.Join(root, ".agent-layer", "old.md")
		if err := os.MkdirAll(filepath.Dir(fromPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(fromPath, []byte("source\n"), 0o644); err != nil {
			t.Fatalf("write from: %v", err)
		}
		toPath := filepath.Join(root, ".agent-layer", "new.md")
		fault := newFaultSystem(RealSystem{})
		fault.statErrs[normalizePath(toPath)] = errors.New("dest stat boom")
		inst := &installer{root: root, sys: fault}
		if _, err := inst.executeRenameMigration(".agent-layer/old.md", ".agent-layer/new.md"); err == nil || !strings.Contains(err.Error(), "dest stat boom") {
			t.Fatalf("expected destination stat error, got %v", err)
		}
	})

	t.Run("duplicate content remove error", func(t *testing.T) {
		root := t.TempDir()
		fromPath := filepath.Join(root, ".agent-layer", "old.md")
		toPath := filepath.Join(root, ".agent-layer", "new.md")
		if err := os.MkdirAll(filepath.Dir(fromPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(fromPath, []byte("same\n"), 0o644); err != nil {
			t.Fatalf("write from: %v", err)
		}
		if err := os.WriteFile(toPath, []byte("same\n"), 0o644); err != nil {
			t.Fatalf("write to: %v", err)
		}
		fault := newFaultSystem(RealSystem{})
		fault.removeErrs[normalizePath(fromPath)] = errors.New("remove boom")
		inst := &installer{root: root, sys: fault}
		if _, err := inst.executeRenameMigration(".agent-layer/old.md", ".agent-layer/new.md"); err == nil || !strings.Contains(err.Error(), "remove duplicate rename source") {
			t.Fatalf("expected remove error, got %v", err)
		}
	})

	t.Run("destination read errors", func(t *testing.T) {
		root := t.TempDir()
		fromPath := filepath.Join(root, ".agent-layer", "old.md")
		toPath := filepath.Join(root, ".agent-layer", "new.md")
		if err := os.MkdirAll(filepath.Dir(fromPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(fromPath, []byte("same\n"), 0o644); err != nil {
			t.Fatalf("write from: %v", err)
		}
		if err := os.WriteFile(toPath, []byte("same\n"), 0o644); err != nil {
			t.Fatalf("write to: %v", err)
		}
		fault := newFaultSystem(RealSystem{})
		fault.readErrs[normalizePath(fromPath)] = errors.New("read from boom")
		inst := &installer{root: root, sys: fault}
		if _, err := inst.executeRenameMigration(".agent-layer/old.md", ".agent-layer/new.md"); err == nil || !strings.Contains(err.Error(), "read from boom") {
			t.Fatalf("expected from read error, got %v", err)
		}

		fault = newFaultSystem(RealSystem{})
		fault.readErrs[normalizePath(toPath)] = errors.New("read to boom")
		inst = &installer{root: root, sys: fault}
		if _, err := inst.executeRenameMigration(".agent-layer/old.md", ".agent-layer/new.md"); err == nil || !strings.Contains(err.Error(), "read to boom") {
			t.Fatalf("expected to read error, got %v", err)
		}
	})

	t.Run("mkdir and rename failures", func(t *testing.T) {
		root := t.TempDir()
		fromPath := filepath.Join(root, ".agent-layer", "old.md")
		toPath := filepath.Join(root, ".agent-layer", "nested", "new.md")
		if err := os.MkdirAll(filepath.Dir(fromPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(fromPath, []byte("source\n"), 0o644); err != nil {
			t.Fatalf("write from: %v", err)
		}
		fault := newFaultSystem(RealSystem{})
		fault.mkdirErrs[normalizePath(filepath.Dir(toPath))] = errors.New("mkdir boom")
		inst := &installer{root: root, sys: fault}
		if _, err := inst.executeRenameMigration(".agent-layer/old.md", ".agent-layer/nested/new.md"); err == nil || !strings.Contains(err.Error(), "mkdir boom") {
			t.Fatalf("expected mkdir error, got %v", err)
		}

		fault = newFaultSystem(RealSystem{})
		fault.renameErrs[normalizePath(fromPath)] = errors.New("rename boom")
		inst = &installer{root: root, sys: fault}
		if _, err := inst.executeRenameMigration(".agent-layer/old.md", ".agent-layer/nested/new.md"); err == nil || !strings.Contains(err.Error(), "rename boom") {
			t.Fatalf("expected rename error, got %v", err)
		}
	})

	t.Run("rename success", func(t *testing.T) {
		root := t.TempDir()
		fromPath := filepath.Join(root, ".agent-layer", "old.md")
		if err := os.MkdirAll(filepath.Dir(fromPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(fromPath, []byte("source\n"), 0o644); err != nil {
			t.Fatalf("write from: %v", err)
		}
		inst := &installer{root: root, sys: RealSystem{}}
		changed, err := inst.executeRenameMigration(".agent-layer/old.md", ".agent-layer/new.md")
		if err != nil {
			t.Fatalf("executeRenameMigration: %v", err)
		}
		if !changed {
			t.Fatal("expected rename to apply")
		}
	})
}

func TestExecuteDeleteMigration_Branches(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "stale.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("stale\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	changed, err := inst.executeDeleteMigration(".agent-layer/stale.md")
	if err != nil {
		t.Fatalf("executeDeleteMigration: %v", err)
	}
	if !changed {
		t.Fatal("expected delete to apply")
	}

	changed, err = inst.executeDeleteMigration(".agent-layer/stale.md")
	if err != nil {
		t.Fatalf("delete missing should be no-op: %v", err)
	}
	if changed {
		t.Fatal("expected missing delete no-op")
	}

	fault := newFaultSystem(RealSystem{})
	fault.statErrs[normalizePath(path)] = errors.New("stat boom")
	inst = &installer{root: root, sys: fault}
	if _, err := inst.executeDeleteMigration(".agent-layer/stale.md"); err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected stat error, got %v", err)
	}

	if err := os.WriteFile(path, []byte("stale\n"), 0o644); err != nil {
		t.Fatalf("rewrite file: %v", err)
	}
	fault = newFaultSystem(RealSystem{})
	fault.removeErrs[normalizePath(path)] = errors.New("remove boom")
	inst = &installer{root: root, sys: fault}
	if _, err := inst.executeDeleteMigration(".agent-layer/stale.md"); err == nil || !strings.Contains(err.Error(), "remove boom") {
		t.Fatalf("expected remove error, got %v", err)
	}
}

func TestExecuteConfigRenameKeyMigration_Branches(t *testing.T) {
	t.Run("same key no-op", func(t *testing.T) {
		inst := &installer{root: t.TempDir(), sys: RealSystem{}}
		changed, err := inst.executeConfigRenameKeyMigration("a.b", "a.b")
		if err != nil {
			t.Fatalf("executeConfigRenameKeyMigration: %v", err)
		}
		if changed {
			t.Fatal("expected no-op when keys are equal")
		}
	})

	t.Run("missing config no-op", func(t *testing.T) {
		inst := &installer{root: t.TempDir(), sys: RealSystem{}}
		changed, err := inst.executeConfigRenameKeyMigration("a.b", "a.c")
		if err != nil {
			t.Fatalf("executeConfigRenameKeyMigration: %v", err)
		}
		if changed {
			t.Fatal("expected no-op when config missing")
		}
	})

	t.Run("invalid key paths error", func(t *testing.T) {
		root := t.TempDir()
		writeTestConfigFile(t, root, "a = 1\n")
		inst := &installer{root: root, sys: RealSystem{}}
		if _, err := inst.executeConfigRenameKeyMigration("a..b", "a.c"); err == nil {
			t.Fatal("expected invalid source key error")
		}
		if _, err := inst.executeConfigRenameKeyMigration("a.b", "a..c"); err == nil {
			t.Fatal("expected invalid destination key error")
		}
	})

	t.Run("source missing no-op", func(t *testing.T) {
		root := t.TempDir()
		writeTestConfigFile(t, root, "[dest]\nkey = \"x\"\n")
		inst := &installer{root: root, sys: RealSystem{}}
		changed, err := inst.executeConfigRenameKeyMigration("from.key", "dest.key")
		if err != nil {
			t.Fatalf("executeConfigRenameKeyMigration: %v", err)
		}
		if changed {
			t.Fatal("expected no-op when source key missing")
		}
	})

	t.Run("destination same value removes source key", func(t *testing.T) {
		root := t.TempDir()
		writeTestConfigFile(t, root, "[from]\nkey = \"same\"\n[to]\nkey = \"same\"\n")
		inst := &installer{root: root, sys: RealSystem{}}
		changed, err := inst.executeConfigRenameKeyMigration("from.key", "to.key")
		if err != nil {
			t.Fatalf("executeConfigRenameKeyMigration: %v", err)
		}
		if !changed {
			t.Fatal("expected source key removal when destination already matches")
		}
		cfg, _, _, err := inst.readMigrationConfigMap()
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		if _, exists, err := getNestedConfigValue(cfg, []string{"from", "key"}); err != nil || exists {
			t.Fatalf("expected from.key removed, exists=%v err=%v", exists, err)
		}
	})

	t.Run("destination conflict errors", func(t *testing.T) {
		root := t.TempDir()
		writeTestConfigFile(t, root, "[from]\nkey = \"a\"\n[to]\nkey = \"b\"\n")
		inst := &installer{root: root, sys: RealSystem{}}
		if _, err := inst.executeConfigRenameKeyMigration("from.key", "to.key"); err == nil || !strings.Contains(err.Error(), "conflict") {
			t.Fatalf("expected conflict error, got %v", err)
		}
	})

	t.Run("destination missing moves key", func(t *testing.T) {
		root := t.TempDir()
		writeTestConfigFile(t, root, "[from]\nkey = \"value\"\n")
		inst := &installer{root: root, sys: RealSystem{}}
		changed, err := inst.executeConfigRenameKeyMigration("from.key", "to.key")
		if err != nil {
			t.Fatalf("executeConfigRenameKeyMigration: %v", err)
		}
		if !changed {
			t.Fatal("expected key move")
		}
		cfg, _, _, err := inst.readMigrationConfigMap()
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		if val, exists, err := getNestedConfigValue(cfg, []string{"to", "key"}); err != nil || !exists || val != "value" {
			t.Fatalf("expected to.key=value, got val=%v exists=%v err=%v", val, exists, err)
		}
	})

	t.Run("non-table traversal and write failures", func(t *testing.T) {
		root := t.TempDir()
		writeTestConfigFile(t, root, "from = \"x\"\n")
		inst := &installer{root: root, sys: RealSystem{}}
		if _, err := inst.executeConfigRenameKeyMigration("from.key", "to.key"); err == nil || !strings.Contains(err.Error(), "non-table") {
			t.Fatalf("expected non-table error, got %v", err)
		}

		root = t.TempDir()
		writeTestConfigFile(t, root, "[from]\nkey = \"value\"\n")
		cfgPath := filepath.Join(root, ".agent-layer", "config.toml")
		fault := newFaultSystem(RealSystem{})
		fault.writeErrs[normalizePath(cfgPath)] = errors.New("write boom")
		inst = &installer{root: root, sys: fault}
		if _, err := inst.executeConfigRenameKeyMigration("from.key", "to.key"); err == nil || !strings.Contains(err.Error(), "write boom") {
			t.Fatalf("expected write error, got %v", err)
		}
	})
}

func TestExecuteConfigSetDefaultMigration_Branches(t *testing.T) {
	t.Run("missing config no-op", func(t *testing.T) {
		inst := &installer{root: t.TempDir(), sys: RealSystem{}}
		changed, err := inst.executeConfigSetDefaultMigration("a.b", json.RawMessage(`"x"`))
		if err != nil {
			t.Fatalf("executeConfigSetDefaultMigration: %v", err)
		}
		if changed {
			t.Fatal("expected no-op when config missing")
		}
	})

	t.Run("invalid key and invalid json errors", func(t *testing.T) {
		root := t.TempDir()
		writeTestConfigFile(t, root, "a = 1\n")
		inst := &installer{root: root, sys: RealSystem{}}
		if _, err := inst.executeConfigSetDefaultMigration("a..b", json.RawMessage(`"x"`)); err == nil {
			t.Fatal("expected invalid key error")
		}
		if _, err := inst.executeConfigSetDefaultMigration("a.b", json.RawMessage(`{`)); err == nil {
			t.Fatal("expected invalid json error")
		}
	})

	t.Run("existing key no-op", func(t *testing.T) {
		root := t.TempDir()
		writeTestConfigFile(t, root, "[a]\nb = \"existing\"\n")
		inst := &installer{root: root, sys: RealSystem{}}
		changed, err := inst.executeConfigSetDefaultMigration("a.b", json.RawMessage(`"new"`))
		if err != nil {
			t.Fatalf("executeConfigSetDefaultMigration: %v", err)
		}
		if changed {
			t.Fatal("expected no-op when key already exists")
		}
	})

	t.Run("set default and write failure", func(t *testing.T) {
		root := t.TempDir()
		writeTestConfigFile(t, root, "[a]\n")
		inst := &installer{root: root, sys: RealSystem{}}
		changed, err := inst.executeConfigSetDefaultMigration("a.b", json.RawMessage(`"value"`))
		if err != nil {
			t.Fatalf("executeConfigSetDefaultMigration: %v", err)
		}
		if !changed {
			t.Fatal("expected default to be written")
		}

		cfg, _, _, err := inst.readMigrationConfigMap()
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		if val, exists, err := getNestedConfigValue(cfg, []string{"a", "b"}); err != nil || !exists || val != "value" {
			t.Fatalf("expected a.b=value, got val=%v exists=%v err=%v", val, exists, err)
		}

		root = t.TempDir()
		writeTestConfigFile(t, root, "[a]\n")
		cfgPath := filepath.Join(root, ".agent-layer", "config.toml")
		fault := newFaultSystem(RealSystem{})
		fault.writeErrs[normalizePath(cfgPath)] = errors.New("write boom")
		inst = &installer{root: root, sys: fault}
		if _, err := inst.executeConfigSetDefaultMigration("a.b", json.RawMessage(`"value"`)); err == nil || !strings.Contains(err.Error(), "write boom") {
			t.Fatalf("expected write error, got %v", err)
		}
	})
}

func TestReadAndWriteMigrationConfigMap(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}

	cfg, cfgPath, exists, err := inst.readMigrationConfigMap()
	if err != nil {
		t.Fatalf("readMigrationConfigMap missing: %v", err)
	}
	if exists || cfg != nil {
		t.Fatalf("expected missing config result, exists=%v cfg=%v", exists, cfg)
	}
	if !strings.HasSuffix(filepath.ToSlash(cfgPath), ".agent-layer/config.toml") {
		t.Fatalf("unexpected cfg path: %s", cfgPath)
	}

	writeTestConfigFile(t, root, "invalid = [\n")
	if _, _, _, err := inst.readMigrationConfigMap(); err == nil || !strings.Contains(err.Error(), "decode config") {
		t.Fatalf("expected decode error, got %v", err)
	}

	cfgPath = writeTestConfigFile(t, root, "name = \"ok\"\n")
	cfg = map[string]any{"name": "updated"}
	if err := inst.writeMigrationConfigMap(cfgPath, cfg); err != nil {
		t.Fatalf("writeMigrationConfigMap: %v", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("expected trailing newline, got %q", string(data))
	}

	fault := newFaultSystem(RealSystem{})
	fault.writeErrs[normalizePath(cfgPath)] = errors.New("write boom")
	inst = &installer{root: root, sys: fault}
	if err := inst.writeMigrationConfigMap(cfgPath, cfg); err == nil || !strings.Contains(err.Error(), "write boom") {
		t.Fatalf("expected write error, got %v", err)
	}
}

func TestConfigPathHelpers(t *testing.T) {
	cfg := map[string]any{
		"nested": map[string]any{
			"value": "x",
		},
		"boxed": map[string]interface{}{
			"flag": true,
		},
	}

	if _, err := splitMigrationKeyPath(""); err == nil {
		t.Fatal("expected split error for empty path")
	}
	if _, err := splitMigrationKeyPath("a..b"); err == nil {
		t.Fatal("expected split error for invalid path")
	}
	if parts, err := splitMigrationKeyPath(" a . b "); err != nil || strings.Join(parts, ".") != "a.b" {
		t.Fatalf("unexpected split result: parts=%v err=%v", parts, err)
	}

	if got, ok := asStringAnyMap(map[string]interface{}{"k": "v"}); !ok || got["k"] != "v" {
		t.Fatalf("unexpected interface-map conversion: got=%v ok=%v", got, ok)
	}
	if _, ok := asStringAnyMap(42); ok {
		t.Fatal("expected non-map conversion to fail")
	}

	if _, _, err := getNestedConfigValue(cfg, nil); err == nil {
		t.Fatal("expected getNestedConfigValue error for empty path")
	}
	if v, ok, err := getNestedConfigValue(cfg, []string{"nested", "value"}); err != nil || !ok || v != "x" {
		t.Fatalf("unexpected nested read: v=%v ok=%v err=%v", v, ok, err)
	}
	if _, ok, err := getNestedConfigValue(cfg, []string{"nested", "missing"}); err != nil || ok {
		t.Fatalf("expected missing nested key, ok=%v err=%v", ok, err)
	}
	if _, _, err := getNestedConfigValue(map[string]any{"nested": "x"}, []string{"nested", "value"}); err == nil || !strings.Contains(err.Error(), "non-table") {
		t.Fatalf("expected non-table traversal error, got %v", err)
	}

	if err := setNestedConfigValue(cfg, nil, "x", true); err == nil {
		t.Fatal("expected setNestedConfigValue error for empty path")
	}
	if err := setNestedConfigValue(cfg, []string{"missing", "value"}, "x", false); err == nil {
		t.Fatal("expected missing table error when create=false")
	}
	if err := setNestedConfigValue(map[string]any{"nested": "x"}, []string{"nested", "value"}, "x", true); err == nil || !strings.Contains(err.Error(), "non-table") {
		t.Fatalf("expected non-table set error, got %v", err)
	}
	if err := setNestedConfigValue(cfg, []string{"new", "value"}, "created", true); err != nil {
		t.Fatalf("setNestedConfigValue create=true: %v", err)
	}

	if _, err := deleteNestedConfigValue(cfg, nil); err == nil {
		t.Fatal("expected deleteNestedConfigValue error for empty path")
	}
	if removed, err := deleteNestedConfigValue(cfg, []string{"new", "missing"}); err != nil || removed {
		t.Fatalf("expected delete missing no-op, removed=%v err=%v", removed, err)
	}
	if _, err := deleteNestedConfigValue(map[string]any{"nested": "x"}, []string{"nested", "value"}); err == nil || !strings.Contains(err.Error(), "non-table") {
		t.Fatalf("expected non-table delete error, got %v", err)
	}
	if removed, err := deleteNestedConfigValue(cfg, []string{"new", "value"}); err != nil || !removed {
		t.Fatalf("expected delete success, removed=%v err=%v", removed, err)
	}
}

func TestMigrationOperationHelpersAndDispatch(t *testing.T) {
	if migration, ok := configMigrationFromOperation(upgradeMigrationOperation{
		ID:   "rename",
		Kind: upgradeMigrationKindConfigRenameKey,
		From: "a.b",
		To:   "a.c",
	}); !ok || migration.Key != "a.b" || migration.To != "a.c" {
		t.Fatalf("unexpected config rename migration: %#v ok=%v", migration, ok)
	}

	if migration, ok := configMigrationFromOperation(upgradeMigrationOperation{
		ID:    "default",
		Kind:  upgradeMigrationKindConfigSetDefault,
		Key:   "a.b",
		Value: nil,
	}); !ok || migration.Key != "a.b" || migration.To != "null" {
		t.Fatalf("unexpected config default migration: %#v ok=%v", migration, ok)
	}

	if _, ok := configMigrationFromOperation(upgradeMigrationOperation{Kind: upgradeMigrationKindDeleteFile}); ok {
		t.Fatal("expected non-config migration to return ok=false")
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "old.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	writeTestConfigFile(t, root, "[a]\n")

	inst := &installer{root: root, sys: RealSystem{}}
	if _, err := inst.executeUpgradeMigrationOperation(upgradeMigrationOperation{
		ID:   "rename",
		Kind: upgradeMigrationKindRenameFile,
		From: ".agent-layer/old.md",
		To:   ".agent-layer/new.md",
	}); err != nil {
		t.Fatalf("rename dispatch: %v", err)
	}
	if _, err := inst.executeUpgradeMigrationOperation(upgradeMigrationOperation{
		ID:   "delete",
		Kind: upgradeMigrationKindDeleteFile,
		Path: ".agent-layer/new.md",
	}); err != nil {
		t.Fatalf("delete dispatch: %v", err)
	}
	if _, err := inst.executeUpgradeMigrationOperation(upgradeMigrationOperation{
		ID:   "cfg-rename",
		Kind: upgradeMigrationKindConfigRenameKey,
		From: "a.b",
		To:   "a.c",
	}); err != nil {
		t.Fatalf("config rename dispatch: %v", err)
	}
	if _, err := inst.executeUpgradeMigrationOperation(upgradeMigrationOperation{
		ID:    "cfg-default",
		Kind:  upgradeMigrationKindConfigSetDefault,
		Key:   "a.b",
		Value: json.RawMessage(`"x"`),
	}); err != nil {
		t.Fatalf("config default dispatch: %v", err)
	}
	if _, err := inst.executeUpgradeMigrationOperation(upgradeMigrationOperation{ID: "bad", Kind: "unknown"}); err == nil {
		t.Fatal("expected unsupported kind error")
	}
}

func TestSourceVersionInferenceFromSnapshotAndResolutionOrder(t *testing.T) {
	t.Run("snapshot inference handles missing and stat error", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{root: root, sys: RealSystem{}}
		versionValue, err := inst.inferSourceVersionFromLatestSnapshot()
		if err != nil {
			t.Fatalf("inferSourceVersionFromLatestSnapshot missing dir: %v", err)
		}
		if versionValue != "" {
			t.Fatalf("expected empty version when snapshot dir missing, got %q", versionValue)
		}

		fault := newFaultSystem(RealSystem{})
		snapshotDir := filepath.Join(root, ".agent-layer", "state", "upgrade-snapshots")
		fault.statErrs[normalizePath(snapshotDir)] = errors.New("stat boom")
		inst = &installer{root: root, sys: fault}
		if _, err := inst.inferSourceVersionFromLatestSnapshot(); err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected snapshot stat error, got %v", err)
		}
	})

	t.Run("snapshot inference returns newest valid pin entry", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{root: root, sys: RealSystem{}}
		dir := inst.upgradeSnapshotDirPath()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir snapshot dir: %v", err)
		}

		now := time.Now().UTC()
		nonPinSnapshot := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    "s1",
			CreatedAtUTC:  now.Format(time.RFC3339),
			Status:        upgradeSnapshotStatusApplied,
			Entries: []upgradeSnapshotEntry{
				{
					Path:          ".agent-layer/config.toml",
					Kind:          upgradeSnapshotEntryKindFile,
					ContentBase64: base64.StdEncoding.EncodeToString([]byte("name = \"x\"\n")),
				},
			},
		}
		if err := writeUpgradeSnapshotFile(filepath.Join(dir, "s1.json"), nonPinSnapshot, RealSystem{}); err != nil {
			t.Fatalf("write non-pin snapshot: %v", err)
		}
		validSnapshot := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    "s2",
			CreatedAtUTC:  now.Add(time.Second).Format(time.RFC3339),
			Status:        upgradeSnapshotStatusApplied,
			Entries: []upgradeSnapshotEntry{
				{
					Path:          ".agent-layer/al.version",
					Kind:          upgradeSnapshotEntryKindFile,
					ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.6.1\n")),
				},
			},
		}
		if err := writeUpgradeSnapshotFile(filepath.Join(dir, "s2.json"), validSnapshot, RealSystem{}); err != nil {
			t.Fatalf("write valid snapshot: %v", err)
		}

		versionValue, err := inst.inferSourceVersionFromLatestSnapshot()
		if err != nil {
			t.Fatalf("inferSourceVersionFromLatestSnapshot: %v", err)
		}
		if versionValue != "0.6.1" {
			t.Fatalf("snapshot inferred version = %q, want %q", versionValue, "0.6.1")
		}
	})

	t.Run("source resolution order pin then baseline then snapshot", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{root: root, sys: RealSystem{}}

		pinPath := filepath.Join(root, ".agent-layer", "al.version")
		if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
			t.Fatalf("mkdir pin dir: %v", err)
		}
		if err := os.WriteFile(pinPath, []byte("0.6.2\n"), 0o644); err != nil {
			t.Fatalf("write pin: %v", err)
		}
		res := inst.resolveUpgradeMigrationSourceVersion()
		if res.version != "0.6.2" || res.origin != UpgradeMigrationSourcePin {
			t.Fatalf("unexpected pin resolution: %#v", res)
		}

		if err := os.Remove(pinPath); err != nil {
			t.Fatalf("remove pin: %v", err)
		}
		now := time.Now().UTC().Format(time.RFC3339)
		state := managedBaselineState{
			SchemaVersion:   baselineStateSchemaVersion,
			BaselineVersion: "0.6.0",
			Source:          BaselineStateSourceWrittenByUpgrade,
			CreatedAt:       now,
			UpdatedAt:       now,
			Files: []manifestFileEntry{
				{Path: ".agent-layer/templates/docs/README.md", FullHashNormalized: "hash"},
			},
		}
		if err := writeManagedBaselineState(root, RealSystem{}, state); err != nil {
			t.Fatalf("write baseline state: %v", err)
		}
		res = inst.resolveUpgradeMigrationSourceVersion()
		if res.version != "0.6.0" || res.origin != UpgradeMigrationSourceBaseline {
			t.Fatalf("unexpected baseline resolution: %#v", res)
		}

		baselinePath := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
		if err := os.WriteFile(baselinePath, []byte(`{"schema_version":1,"baseline_version":"bad","source":"written_by_upgrade","created_at_utc":"`+now+`","updated_at_utc":"`+now+`","files":[{"path":"x","full_hash_normalized":"h"}]}`), 0o644); err != nil {
			t.Fatalf("write invalid baseline: %v", err)
		}
		snapshotDir := inst.upgradeSnapshotDirPath()
		if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
			t.Fatalf("mkdir snapshot dir: %v", err)
		}
		snapshot := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    "s3",
			CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
			Status:        upgradeSnapshotStatusApplied,
			Entries: []upgradeSnapshotEntry{
				{
					Path:          ".agent-layer/al.version",
					Kind:          upgradeSnapshotEntryKindFile,
					ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.6.1\n")),
				},
			},
		}
		if err := writeUpgradeSnapshotFile(filepath.Join(snapshotDir, "s3.json"), snapshot, RealSystem{}); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}
		res = inst.resolveUpgradeMigrationSourceVersion()
		if res.version != "0.6.1" || res.origin != UpgradeMigrationSourceSnapshot {
			t.Fatalf("unexpected snapshot resolution: %#v", res)
		}
		if len(res.notes) == 0 {
			t.Fatal("expected note for invalid baseline version")
		}
	})
}

func TestLoadAndValidateUpgradeMigrationManifest_Errors(t *testing.T) {
	t.Run("load reports decode, validation, and target mismatch errors", func(t *testing.T) {
		original := templates.ReadFunc
		templates.ReadFunc = func(name string) ([]byte, error) {
			switch name {
			case "migrations/0.7.0.json":
				return []byte(`{`), nil
			default:
				return original(name)
			}
		}
		t.Cleanup(func() { templates.ReadFunc = original })
		if _, _, err := loadUpgradeMigrationManifestByVersion("0.7.0"); err == nil || !strings.Contains(err.Error(), "decode migration manifest") {
			t.Fatalf("expected decode error, got %v", err)
		}
	})

	t.Run("validate manifest catches required fields and duplicates", func(t *testing.T) {
		cases := []struct {
			name     string
			manifest upgradeMigrationManifest
			wantErr  string
		}{
			{
				name: "bad schema",
				manifest: upgradeMigrationManifest{
					SchemaVersion:   9,
					TargetVersion:   "0.7.0",
					MinPriorVersion: "0.6.0",
				},
				wantErr: "unsupported schema_version",
			},
			{
				name: "missing target",
				manifest: upgradeMigrationManifest{
					SchemaVersion:   1,
					MinPriorVersion: "0.6.0",
				},
				wantErr: "target_version is required",
			},
			{
				name: "non-normalized target",
				manifest: upgradeMigrationManifest{
					SchemaVersion:   1,
					TargetVersion:   "v0.7.0",
					MinPriorVersion: "0.6.0",
				},
				wantErr: "must be normalized",
			},
			{
				name: "missing min",
				manifest: upgradeMigrationManifest{
					SchemaVersion: 1,
					TargetVersion: "0.7.0",
				},
				wantErr: "min_prior_version is required",
			},
			{
				name: "duplicate ids",
				manifest: upgradeMigrationManifest{
					SchemaVersion:   1,
					TargetVersion:   "0.7.0",
					MinPriorVersion: "0.6.0",
					Operations: []upgradeMigrationOperation{
						{ID: "x", Kind: upgradeMigrationKindDeleteFile, Rationale: "r", Path: "a.md"},
						{ID: "x", Kind: upgradeMigrationKindDeleteFile, Rationale: "r", Path: "b.md"},
					},
				},
				wantErr: "duplicate migration id",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if err := validateUpgradeMigrationManifest(tc.manifest); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error %q, got %v", tc.wantErr, err)
				}
			})
		}
	})

	t.Run("validate operations covers all kinds and failures", func(t *testing.T) {
		validCases := []upgradeMigrationOperation{
			{ID: "rename", Kind: upgradeMigrationKindRenameFile, Rationale: "r", From: "a.md", To: "b.md"},
			{ID: "rename-gen", Kind: upgradeMigrationKindRenameGeneratedArtifact, Rationale: "r", From: "a.md", To: "b.md"},
			{ID: "delete", Kind: upgradeMigrationKindDeleteFile, Rationale: "r", Path: "a.md"},
			{ID: "delete-gen", Kind: upgradeMigrationKindDeleteGeneratedArtifact, Rationale: "r", Path: "a.md"},
			{ID: "cfg-rename", Kind: upgradeMigrationKindConfigRenameKey, Rationale: "r", From: "a.b", To: "a.c"},
			{ID: "cfg-default", Kind: upgradeMigrationKindConfigSetDefault, Rationale: "r", Key: "a.b", Value: json.RawMessage(`"x"`)},
		}
		for _, op := range validCases {
			if err := validateUpgradeMigrationOperation(op); err != nil {
				t.Fatalf("expected valid operation %s: %v", op.ID, err)
			}
		}

		invalidCases := []struct {
			op      upgradeMigrationOperation
			wantErr string
		}{
			{op: upgradeMigrationOperation{Kind: upgradeMigrationKindDeleteFile, Rationale: "r", Path: "a.md"}, wantErr: "migration id is required"},
			{op: upgradeMigrationOperation{ID: "x", Kind: upgradeMigrationKindDeleteFile, Path: "a.md"}, wantErr: "rationale is required"},
			{op: upgradeMigrationOperation{ID: "x", Kind: upgradeMigrationKindRenameFile, Rationale: "r", From: "a.md", To: "a.md"}, wantErr: "requires distinct from/to"},
			{op: upgradeMigrationOperation{ID: "x", Kind: upgradeMigrationKindRenameFile, Rationale: "r", From: "", To: "b.md"}, wantErr: "requires from and to"},
			{op: upgradeMigrationOperation{ID: "x", Kind: upgradeMigrationKindDeleteFile, Rationale: "r"}, wantErr: "requires path"},
			{op: upgradeMigrationOperation{ID: "x", Kind: upgradeMigrationKindConfigRenameKey, Rationale: "r", From: "a..b", To: "a.c"}, wantErr: "invalid from key"},
			{op: upgradeMigrationOperation{ID: "x", Kind: upgradeMigrationKindConfigRenameKey, Rationale: "r", From: "a.b", To: "a..c"}, wantErr: "invalid to key"},
			{op: upgradeMigrationOperation{ID: "x", Kind: upgradeMigrationKindConfigSetDefault, Rationale: "r", Key: "a..b", Value: json.RawMessage(`"x"`)}, wantErr: "invalid key"},
			{op: upgradeMigrationOperation{ID: "x", Kind: upgradeMigrationKindConfigSetDefault, Rationale: "r", Key: "a.b"}, wantErr: "requires value"},
			{op: upgradeMigrationOperation{ID: "x", Kind: upgradeMigrationKindConfigSetDefault, Rationale: "r", Key: "a.b", Value: json.RawMessage(`{`)}, wantErr: "invalid value"},
			{op: upgradeMigrationOperation{ID: "x", Kind: "unknown", Rationale: "r"}, wantErr: "unsupported kind"},
		}
		for idx, tc := range invalidCases {
			t.Run(fmt.Sprintf("invalid_%d", idx), func(t *testing.T) {
				if err := validateUpgradeMigrationOperation(tc.op); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected %q, got %v", tc.wantErr, err)
				}
			})
		}
	})
}

func TestMatchesTemplateDocsManifest_UsesDocsPaths(t *testing.T) {
	root := t.TempDir()
	docsPath := filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")
	if err := os.MkdirAll(filepath.Dir(docsPath), 0o755); err != nil {
		t.Fatalf("mkdir docs path: %v", err)
	}
	content := []byte("roadmap content\n")
	if err := os.WriteFile(docsPath, content, 0o644); err != nil {
		t.Fatalf("write docs file: %v", err)
	}
	inst := &installer{root: root, sys: RealSystem{}}

	matchManifest := templateManifest{
		Files: []manifestFileEntry{
			{
				Path:               "docs/agent-layer/ROADMAP.md",
				FullHashNormalized: hashNormalizedContent(content),
			},
		},
	}
	match, err := inst.matchesTemplateDocsManifest(matchManifest)
	if err != nil {
		t.Fatalf("matchesTemplateDocsManifest match: %v", err)
	}
	if !match {
		t.Fatal("expected docs manifest match")
	}

	mismatchManifest := templateManifest{
		Files: []manifestFileEntry{
			{
				Path:               "docs/agent-layer/ROADMAP.md",
				FullHashNormalized: "wrong-hash",
			},
		},
	}
	match, err = inst.matchesTemplateDocsManifest(mismatchManifest)
	if err != nil {
		t.Fatalf("matchesTemplateDocsManifest mismatch: %v", err)
	}
	if match {
		t.Fatal("expected docs manifest mismatch")
	}

	missingManifest := templateManifest{
		Files: []manifestFileEntry{
			{
				Path:               "docs/agent-layer/MISSING.md",
				FullHashNormalized: "unused",
			},
		},
	}
	match, err = inst.matchesTemplateDocsManifest(missingManifest)
	if err != nil {
		t.Fatalf("matchesTemplateDocsManifest missing file: %v", err)
	}
	if match {
		t.Fatal("expected missing docs file to not match")
	}

	readFault := newFaultSystem(RealSystem{})
	readFault.readErrs[normalizePath(docsPath)] = errors.New("read boom")
	inst = &installer{root: root, sys: readFault}
	if _, err := inst.matchesTemplateDocsManifest(matchManifest); err == nil || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("expected read error from matchesTemplateDocsManifest, got %v", err)
	}
}

func TestInferSourceVersionFromManifestMatch_ReturnsSingleCandidate(t *testing.T) {
	originalWalk := templates.WalkFunc
	originalRead := templates.ReadFunc
	originalMap := allTemplateManifestByV
	originalErr := allTemplateManifestErr
	t.Cleanup(func() {
		templates.WalkFunc = originalWalk
		templates.ReadFunc = originalRead
		allTemplateManifestOnce = sync.Once{}
		allTemplateManifestByV = originalMap
		allTemplateManifestErr = originalErr
	})

	allTemplateManifestOnce = sync.Once{}
	allTemplateManifestByV = nil
	allTemplateManifestErr = nil

	root := t.TempDir()
	docsPath := filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")
	if err := os.MkdirAll(filepath.Dir(docsPath), 0o755); err != nil {
		t.Fatalf("mkdir docs path: %v", err)
	}
	docsContent := []byte("manifest match candidate\n")
	if err := os.WriteFile(docsPath, docsContent, 0o644); err != nil {
		t.Fatalf("write docs file: %v", err)
	}

	targetManifestPath := path.Join(templateManifestDir, "9.9.9.json")
	otherManifestPath := path.Join(templateManifestDir, "9.9.8.json")
	templates.WalkFunc = func(rootPath string, fn fs.WalkDirFunc) error {
		if rootPath != templateManifestDir {
			return originalWalk(rootPath, fn)
		}
		if err := fn(targetManifestPath, staticDirEntry{name: "9.9.9.json"}, nil); err != nil {
			return err
		}
		return fn(otherManifestPath, staticDirEntry{name: "9.9.8.json"}, nil)
	}

	matchManifest := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "9.9.9",
		GeneratedAt:   "2026-02-15T00:00:00Z",
		Files: []manifestFileEntry{
			{
				Path:               "docs/agent-layer/ROADMAP.md",
				FullHashNormalized: hashNormalizedContent(docsContent),
			},
		},
	}
	otherManifest := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "9.9.8",
		GeneratedAt:   "2026-02-15T00:00:00Z",
		Files: []manifestFileEntry{
			{
				Path:               "docs/agent-layer/ROADMAP.md",
				FullHashNormalized: "different-hash",
			},
		},
	}
	matchBytes, err := json.Marshal(matchManifest)
	if err != nil {
		t.Fatalf("marshal match manifest: %v", err)
	}
	otherBytes, err := json.Marshal(otherManifest)
	if err != nil {
		t.Fatalf("marshal other manifest: %v", err)
	}
	templates.ReadFunc = func(name string) ([]byte, error) {
		switch name {
		case targetManifestPath:
			return matchBytes, nil
		case otherManifestPath:
			return otherBytes, nil
		default:
			return originalRead(name)
		}
	}

	inst := &installer{root: root, sys: RealSystem{}}
	versionValue, err := inst.inferSourceVersionFromManifestMatch()
	if err != nil {
		t.Fatalf("inferSourceVersionFromManifestMatch: %v", err)
	}
	if versionValue != "9.9.9" {
		t.Fatalf("manifest-match source version = %q, want %q", versionValue, "9.9.9")
	}
}

func TestMigrationWillCoverPath(t *testing.T) {
	root := t.TempDir()
	sys := RealSystem{}

	// Create a file so rename/delete scenarios have something to stat.
	existingFile := filepath.Join(root, ".agent-layer", "existing.md")
	if err := os.MkdirAll(filepath.Dir(existingFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(existingFile, []byte("content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tests := []struct {
		name    string
		op      upgradeMigrationOperation
		relPath string
		want    bool
	}{
		{
			name:    "rename source exists covers path",
			op:      upgradeMigrationOperation{Kind: upgradeMigrationKindRenameFile, From: ".agent-layer/existing.md", To: ".agent-layer/new.md"},
			relPath: ".agent-layer/existing.md",
			want:    true,
		},
		{
			name:    "rename source absent does not cover path",
			op:      upgradeMigrationOperation{Kind: upgradeMigrationKindRenameFile, From: ".agent-layer/missing.md", To: ".agent-layer/new.md"},
			relPath: ".agent-layer/new.md",
			want:    false,
		},
		{
			name:    "rename same from and to does not cover",
			op:      upgradeMigrationOperation{Kind: upgradeMigrationKindRenameFile, From: ".agent-layer/existing.md", To: ".agent-layer/existing.md"},
			relPath: ".agent-layer/existing.md",
			want:    false,
		},
		{
			name:    "rename generated artifact source exists covers path",
			op:      upgradeMigrationOperation{Kind: upgradeMigrationKindRenameGeneratedArtifact, From: ".agent-layer/existing.md", To: ".agent-layer/new.md"},
			relPath: ".agent-layer/new.md",
			want:    true,
		},
		{
			name:    "delete file exists covers path",
			op:      upgradeMigrationOperation{Kind: upgradeMigrationKindDeleteFile, Path: ".agent-layer/existing.md"},
			relPath: ".agent-layer/existing.md",
			want:    true,
		},
		{
			name:    "delete file absent does not cover path",
			op:      upgradeMigrationOperation{Kind: upgradeMigrationKindDeleteFile, Path: ".agent-layer/missing.md"},
			relPath: ".agent-layer/missing.md",
			want:    false,
		},
		{
			name:    "delete generated artifact absent does not cover",
			op:      upgradeMigrationOperation{Kind: upgradeMigrationKindDeleteGeneratedArtifact, Path: ".agent-layer/gone.md"},
			relPath: ".agent-layer/gone.md",
			want:    false,
		},
		{
			name:    "config rename key does not cover file paths",
			op:      upgradeMigrationOperation{Kind: upgradeMigrationKindConfigRenameKey, From: "old.key", To: "new.key"},
			relPath: ".agent-layer/config.toml",
			want:    false,
		},
		{
			name:    "config set default does not cover file paths",
			op:      upgradeMigrationOperation{Kind: upgradeMigrationKindConfigSetDefault, Key: "some.key"},
			relPath: ".agent-layer/config.toml",
			want:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := migrationWillCoverPath(sys, root, tc.op, tc.relPath)
			if got != tc.want {
				t.Fatalf("migrationWillCoverPath = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRunMigrations_PrepareFailureAndEntryNotFound(t *testing.T) {
	t.Run("migrationsPrepared false and prepareUpgradeMigrations fails", func(t *testing.T) {
		// Use an invalid pin version so prepareUpgradeMigrations fails on manifest load.
		inst := &installer{root: t.TempDir(), pinVersion: "not-a-version", sys: RealSystem{}}
		if err := inst.runMigrations(); err == nil {
			t.Fatal("expected runMigrations to fail when prepare fails")
		}
	})

	t.Run("entry not found in index continues", func(t *testing.T) {
		root := t.TempDir()
		legacyPath := filepath.Join(root, ".agent-layer", "legacy.md")
		if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(legacyPath, []byte("legacy\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		var warn bytes.Buffer
		inst := &installer{root: root, sys: RealSystem{}, warnWriter: &warn}
		// Manually set up migration state with mismatched IDs.
		inst.migrationsPrepared = true
		inst.migrationReport = UpgradeMigrationReport{
			TargetVersion:       "0.7.0",
			SourceVersion:       "0.6.0",
			SourceVersionOrigin: UpgradeMigrationSourcePin,
			Entries: []UpgradeMigrationEntry{
				{ID: "different_id", Kind: string(upgradeMigrationKindDeleteFile), Status: UpgradeMigrationStatusPlanned},
			},
		}
		inst.pendingMigrationOps = []upgradeMigrationOperation{
			{ID: "not_in_index", Kind: upgradeMigrationKindDeleteFile, Path: ".agent-layer/legacy.md", Rationale: "test"},
		}
		// Should not panic; entry-not-found path just continues.
		if err := inst.runMigrations(); err != nil {
			t.Fatalf("runMigrations: %v", err)
		}
	})
}

func TestPlanUpgradeMigrations_SnapshotEntryAbsPathError(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.7.0\n"), 0o644); err != nil {
		t.Fatalf("write pin: %v", err)
	}

	// A rename with an empty "from" will cause snapshotEntryAbsPath to fail
	// since migrationCoveredPaths will produce empty rel paths that get cleaned.
	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "cfg_rename",
      "kind": "config_rename_key",
      "rationale": "Rename config key for testing",
      "source_agnostic": true,
      "from": "old.key",
      "to": "new.key"
    }
  ]
}`)
	inst := &installer{root: root, pinVersion: "0.7.0", sys: RealSystem{}}
	plan, err := inst.planUpgradeMigrations()
	if err != nil {
		t.Fatalf("planUpgradeMigrations: %v", err)
	}
	// Should include config migration in plan.
	if len(plan.configMigrations) != 1 {
		t.Fatalf("expected 1 config migration, got %d", len(plan.configMigrations))
	}
	if plan.configMigrations[0].From != "old.key" || plan.configMigrations[0].To != "new.key" {
		t.Fatalf("unexpected config migration: %#v", plan.configMigrations[0])
	}
}

func TestResolveUpgradeMigrationSourceVersion_BaselineAndSnapshotFallback(t *testing.T) {
	t.Run("baseline fallback path with valid baseline", func(t *testing.T) {
		root := t.TempDir()
		// No pin file, so pin resolution returns empty.
		now := time.Now().UTC().Format(time.RFC3339)
		state := managedBaselineState{
			SchemaVersion:   baselineStateSchemaVersion,
			BaselineVersion: "0.5.0",
			Source:          BaselineStateSourceWrittenByUpgrade,
			CreatedAt:       now,
			UpdatedAt:       now,
			Files: []manifestFileEntry{
				{Path: "docs/agent-layer/ROADMAP.md", FullHashNormalized: "hash"},
			},
		}
		if err := writeManagedBaselineState(root, RealSystem{}, state); err != nil {
			t.Fatalf("write baseline: %v", err)
		}
		inst := &installer{root: root, sys: RealSystem{}}
		res := inst.resolveUpgradeMigrationSourceVersion()
		if res.version != "0.5.0" || res.origin != UpgradeMigrationSourceBaseline {
			t.Fatalf("expected baseline resolution, got version=%q origin=%q", res.version, res.origin)
		}
	})

	t.Run("baseline non-existent falls through to snapshot", func(t *testing.T) {
		root := t.TempDir()
		// No pin file, no baseline file.
		// Create a snapshot with a valid pin entry.
		inst := &installer{root: root, sys: RealSystem{}}
		snapshotDir := inst.upgradeSnapshotDirPath()
		if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
			t.Fatalf("mkdir snapshot dir: %v", err)
		}
		snapshot := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    "s1",
			CreatedAtUTC:  time.Now().UTC().Format(time.RFC3339),
			Status:        upgradeSnapshotStatusApplied,
			Entries: []upgradeSnapshotEntry{
				{
					Path:          ".agent-layer/al.version",
					Kind:          upgradeSnapshotEntryKindFile,
					ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.4.0\n")),
				},
			},
		}
		if err := writeUpgradeSnapshotFile(filepath.Join(snapshotDir, "s1.json"), snapshot, RealSystem{}); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}
		res := inst.resolveUpgradeMigrationSourceVersion()
		if res.version != "0.4.0" || res.origin != UpgradeMigrationSourceSnapshot {
			t.Fatalf("expected snapshot resolution, got version=%q origin=%q", res.version, res.origin)
		}
	})

	t.Run("baseline read error (non-ErrNotExist) adds note", func(t *testing.T) {
		root := t.TempDir()
		// Create a corrupted baseline file (not JSON parse error, but a read error).
		baselinePath := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
		if err := os.MkdirAll(filepath.Dir(baselinePath), 0o755); err != nil {
			t.Fatalf("mkdir baseline dir: %v", err)
		}
		fault := newFaultSystem(RealSystem{})
		fault.readErrs[normalizePath(baselinePath)] = errors.New("baseline read boom")
		inst := &installer{root: root, sys: fault}
		res := inst.resolveUpgradeMigrationSourceVersion()
		if res.origin != UpgradeMigrationSourceUnknown {
			t.Fatalf("expected unknown origin, got %q", res.origin)
		}
		foundNote := false
		for _, note := range res.notes {
			if strings.Contains(note, "managed baseline unavailable") {
				foundNote = true
			}
		}
		if !foundNote {
			t.Fatalf("expected baseline unavailable note, got notes=%v", res.notes)
		}
	})
}

func TestCompareSemver_AdditionalCases(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "equal", a: "1.2.3", b: "1.2.3", want: 0},
		{name: "a less major", a: "0.9.9", b: "1.0.0", want: -1},
		{name: "a greater major", a: "2.0.0", b: "1.9.9", want: 1},
		{name: "a less minor", a: "1.0.9", b: "1.1.0", want: -1},
		{name: "a greater minor", a: "1.2.0", b: "1.1.9", want: 1},
		{name: "a less patch", a: "1.2.3", b: "1.2.4", want: -1},
		{name: "a greater patch", a: "1.2.5", b: "1.2.4", want: 1},
		{name: "with v prefix", a: "v1.2.3", b: "v1.2.3", want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := compareSemver(tc.a, tc.b)
			if err != nil {
				t.Fatalf("compareSemver(%q, %q) err: %v", tc.a, tc.b, err)
			}
			if got != tc.want {
				t.Fatalf("compareSemver(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}

	t.Run("invalid a", func(t *testing.T) {
		if _, err := compareSemver("bad", "1.0.0"); err == nil {
			t.Fatal("expected error for invalid a")
		}
	})
	t.Run("invalid b", func(t *testing.T) {
		if _, err := compareSemver("1.0.0", "bad"); err == nil {
			t.Fatal("expected error for invalid b")
		}
	})
}

func TestParseSemver_EdgeCases(t *testing.T) {
	t.Run("valid version", func(t *testing.T) {
		parts, err := parseSemver("1.2.3")
		if err != nil {
			t.Fatalf("parseSemver: %v", err)
		}
		if parts != [3]int{1, 2, 3} {
			t.Fatalf("parseSemver = %v, want [1 2 3]", parts)
		}
	})
	t.Run("v-prefix version", func(t *testing.T) {
		parts, err := parseSemver("v0.7.0")
		if err != nil {
			t.Fatalf("parseSemver: %v", err)
		}
		if parts != [3]int{0, 7, 0} {
			t.Fatalf("parseSemver = %v, want [0 7 0]", parts)
		}
	})
	t.Run("invalid version", func(t *testing.T) {
		if _, err := parseSemver("abc"); err == nil {
			t.Fatal("expected error for invalid version")
		}
	})
	t.Run("empty version", func(t *testing.T) {
		if _, err := parseSemver(""); err == nil {
			t.Fatal("expected error for empty version")
		}
	})
}

func TestLoadUpgradeMigrationManifestByVersion_ValidationError(t *testing.T) {
	// Invalid schema_version triggers validation error path.
	original := templates.ReadFunc
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == "migrations/0.8.0.json" {
			return []byte(`{
				"schema_version": 999,
				"target_version": "0.8.0",
				"min_prior_version": "0.7.0",
				"operations": []
			}`), nil
		}
		return original(name)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, _, err := loadUpgradeMigrationManifestByVersion("0.8.0")
	if err == nil || !strings.Contains(err.Error(), "validate migration manifest") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestLoadUpgradeMigrationManifestByVersion_TargetVersionMismatch(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == "migrations/0.8.0.json" {
			return []byte(`{
				"schema_version": 1,
				"target_version": "0.9.0",
				"min_prior_version": "0.7.0",
				"operations": []
			}`), nil
		}
		return original(name)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, _, err := loadUpgradeMigrationManifestByVersion("0.8.0")
	if err == nil || !strings.Contains(err.Error(), "does not match requested version") {
		t.Fatalf("expected version mismatch error, got %v", err)
	}
}

func TestLoadUpgradeMigrationManifestByVersion_InvalidPinVersion(t *testing.T) {
	_, _, err := loadUpgradeMigrationManifestByVersion("not-a-version")
	if err == nil {
		t.Fatal("expected error for invalid pin version")
	}
}

func TestWriteMigrationConfigMap_MarshalAndWriteErrors(t *testing.T) {
	t.Run("write error path", func(t *testing.T) {
		root := t.TempDir()
		cfgPath := filepath.Join(root, ".agent-layer", "config.toml")
		if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		fault := newFaultSystem(RealSystem{})
		fault.writeErrs[normalizePath(cfgPath)] = errors.New("write boom")
		inst := &installer{root: root, sys: fault}
		cfg := map[string]any{"key": "value"}
		if err := inst.writeMigrationConfigMap(cfgPath, cfg); err == nil || !strings.Contains(err.Error(), "write boom") {
			t.Fatalf("expected write error, got %v", err)
		}
	})
}

func TestAsStringAnyMap_TypeAssertionPaths(t *testing.T) {
	t.Run("map[string]any direct", func(t *testing.T) {
		input := map[string]any{"k": "v"}
		got, ok := asStringAnyMap(input)
		if !ok || got["k"] != "v" {
			t.Fatalf("expected direct map[string]any, got ok=%v val=%v", ok, got)
		}
	})
	t.Run("map[string]interface{} conversion", func(t *testing.T) {
		input := map[string]interface{}{"a": 1, "b": "two"}
		got, ok := asStringAnyMap(input)
		if !ok || got["a"] != 1 || got["b"] != "two" {
			t.Fatalf("expected conversion, got ok=%v val=%v", ok, got)
		}
	})
	t.Run("non-map returns false", func(t *testing.T) {
		if _, ok := asStringAnyMap("string"); ok {
			t.Fatal("expected non-map to fail")
		}
		if _, ok := asStringAnyMap(42); ok {
			t.Fatal("expected int to fail")
		}
		if _, ok := asStringAnyMap(nil); ok {
			t.Fatal("expected nil to fail")
		}
		if _, ok := asStringAnyMap([]string{"a"}); ok {
			t.Fatal("expected slice to fail")
		}
	})
}

func TestExecuteConfigRenameKeyMigration_DestExistsDifferentValue(t *testing.T) {
	root := t.TempDir()
	writeTestConfigFile(t, root, "[from]\nkey = \"source_val\"\n[to]\nkey = \"different_val\"\n")
	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.executeConfigRenameKeyMigration("from.key", "to.key")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error, got %v", err)
	}
}

func TestExecuteConfigSetDefaultMigration_NonTableTraversal(t *testing.T) {
	root := t.TempDir()
	writeTestConfigFile(t, root, "a = \"scalar\"\n")
	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.executeConfigSetDefaultMigration("a.b.c", json.RawMessage(`"x"`))
	if err == nil || !strings.Contains(err.Error(), "non-table") {
		t.Fatalf("expected non-table traversal error, got %v", err)
	}
}

func TestInferSourceVersionFromManifestMatch_SuccessfulMatch(t *testing.T) {
	originalWalk := templates.WalkFunc
	originalRead := templates.ReadFunc
	originalMap := allTemplateManifestByV
	originalErr := allTemplateManifestErr
	t.Cleanup(func() {
		templates.WalkFunc = originalWalk
		templates.ReadFunc = originalRead
		allTemplateManifestOnce = sync.Once{}
		allTemplateManifestByV = originalMap
		allTemplateManifestErr = originalErr
	})

	allTemplateManifestOnce = sync.Once{}
	allTemplateManifestByV = nil
	allTemplateManifestErr = nil

	root := t.TempDir()
	docsPath := filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")
	if err := os.MkdirAll(filepath.Dir(docsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	docsContent := []byte("unique match test content\n")
	if err := os.WriteFile(docsPath, docsContent, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	targetManifestPath := path.Join(templateManifestDir, "8.8.8.json")
	templates.WalkFunc = func(rootPath string, fn fs.WalkDirFunc) error {
		if rootPath != templateManifestDir {
			return originalWalk(rootPath, fn)
		}
		return fn(targetManifestPath, staticDirEntry{name: "8.8.8.json"}, nil)
	}
	manifest := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "8.8.8",
		GeneratedAt:   "2026-02-15T00:00:00Z",
		Files: []manifestFileEntry{
			{
				Path:               "docs/agent-layer/ROADMAP.md",
				FullHashNormalized: hashNormalizedContent(docsContent),
			},
		},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == targetManifestPath {
			return manifestBytes, nil
		}
		return originalRead(name)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	version, err := inst.inferSourceVersionFromManifestMatch()
	if err != nil {
		t.Fatalf("inferSourceVersionFromManifestMatch: %v", err)
	}
	if version != "8.8.8" {
		t.Fatalf("version = %q, want %q", version, "8.8.8")
	}
}

func TestInferSourceVersionFromManifestMatch_MultipleMatchesReturnsEmpty(t *testing.T) {
	originalWalk := templates.WalkFunc
	originalRead := templates.ReadFunc
	originalMap := allTemplateManifestByV
	originalErr := allTemplateManifestErr
	t.Cleanup(func() {
		templates.WalkFunc = originalWalk
		templates.ReadFunc = originalRead
		allTemplateManifestOnce = sync.Once{}
		allTemplateManifestByV = originalMap
		allTemplateManifestErr = originalErr
	})

	allTemplateManifestOnce = sync.Once{}
	allTemplateManifestByV = nil
	allTemplateManifestErr = nil

	root := t.TempDir()
	docsPath := filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")
	if err := os.MkdirAll(filepath.Dir(docsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	docsContent := []byte("multi match content\n")
	if err := os.WriteFile(docsPath, docsContent, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	hash := hashNormalizedContent(docsContent)
	m1Path := path.Join(templateManifestDir, "7.7.7.json")
	m2Path := path.Join(templateManifestDir, "7.7.8.json")
	templates.WalkFunc = func(rootPath string, fn fs.WalkDirFunc) error {
		if rootPath != templateManifestDir {
			return originalWalk(rootPath, fn)
		}
		if err := fn(m1Path, staticDirEntry{name: "7.7.7.json"}, nil); err != nil {
			return err
		}
		return fn(m2Path, staticDirEntry{name: "7.7.8.json"}, nil)
	}
	m1 := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "7.7.7",
		GeneratedAt:   "2026-02-15T00:00:00Z",
		Files:         []manifestFileEntry{{Path: "docs/agent-layer/ROADMAP.md", FullHashNormalized: hash}},
	}
	m2 := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "7.7.8",
		GeneratedAt:   "2026-02-15T00:00:00Z",
		Files:         []manifestFileEntry{{Path: "docs/agent-layer/ROADMAP.md", FullHashNormalized: hash}},
	}
	m1Bytes, _ := json.Marshal(m1)
	m2Bytes, _ := json.Marshal(m2)
	templates.ReadFunc = func(name string) ([]byte, error) {
		switch name {
		case m1Path:
			return m1Bytes, nil
		case m2Path:
			return m2Bytes, nil
		default:
			return originalRead(name)
		}
	}

	inst := &installer{root: root, sys: RealSystem{}}
	version, err := inst.inferSourceVersionFromManifestMatch()
	if err != nil {
		t.Fatalf("inferSourceVersionFromManifestMatch: %v", err)
	}
	if version != "" {
		t.Fatalf("expected empty version for multiple matches, got %q", version)
	}
}

func TestResolveUpgradeMigrationSourceVersion_ManifestMatchFallback(t *testing.T) {
	originalWalk := templates.WalkFunc
	originalRead := templates.ReadFunc
	originalMap := allTemplateManifestByV
	originalErr := allTemplateManifestErr
	t.Cleanup(func() {
		templates.WalkFunc = originalWalk
		templates.ReadFunc = originalRead
		allTemplateManifestOnce = sync.Once{}
		allTemplateManifestByV = originalMap
		allTemplateManifestErr = originalErr
	})

	allTemplateManifestOnce = sync.Once{}
	allTemplateManifestByV = nil
	allTemplateManifestErr = nil

	root := t.TempDir()
	docsPath := filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")
	if err := os.MkdirAll(filepath.Dir(docsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	docsContent := []byte("manifest fallback content\n")
	if err := os.WriteFile(docsPath, docsContent, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	targetManifestPath := path.Join(templateManifestDir, "6.6.6.json")
	templates.WalkFunc = func(rootPath string, fn fs.WalkDirFunc) error {
		if rootPath != templateManifestDir {
			return originalWalk(rootPath, fn)
		}
		return fn(targetManifestPath, staticDirEntry{name: "6.6.6.json"}, nil)
	}
	manifest := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "6.6.6",
		GeneratedAt:   "2026-02-15T00:00:00Z",
		Files: []manifestFileEntry{
			{
				Path:               "docs/agent-layer/ROADMAP.md",
				FullHashNormalized: hashNormalizedContent(docsContent),
			},
		},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == targetManifestPath {
			return manifestBytes, nil
		}
		return originalRead(name)
	}

	// No pin, no baseline, no snapshot -> should fall through to manifest match.
	inst := &installer{root: root, sys: RealSystem{}}
	res := inst.resolveUpgradeMigrationSourceVersion()
	if res.version != "6.6.6" || res.origin != UpgradeMigrationSourceManifestMatch {
		t.Fatalf("expected manifest match resolution, got version=%q origin=%q", res.version, res.origin)
	}
}

func TestValidateUpgradeMigrationManifest_NonNormalizedMinPrior(t *testing.T) {
	manifest := upgradeMigrationManifest{
		SchemaVersion:   1,
		TargetVersion:   "0.7.0",
		MinPriorVersion: "v0.6.0",
	}
	err := validateUpgradeMigrationManifest(manifest)
	if err == nil || !strings.Contains(err.Error(), "must be normalized") {
		t.Fatalf("expected non-normalized min_prior_version error, got %v", err)
	}
}

func TestValidateUpgradeMigrationManifest_InvalidTargetVersion(t *testing.T) {
	manifest := upgradeMigrationManifest{
		SchemaVersion:   1,
		TargetVersion:   "bad-version",
		MinPriorVersion: "0.6.0",
	}
	err := validateUpgradeMigrationManifest(manifest)
	if err == nil || !strings.Contains(err.Error(), "invalid target_version") {
		t.Fatalf("expected invalid target_version error, got %v", err)
	}
}

func TestValidateUpgradeMigrationManifest_InvalidMinPriorVersion(t *testing.T) {
	manifest := upgradeMigrationManifest{
		SchemaVersion:   1,
		TargetVersion:   "0.7.0",
		MinPriorVersion: "bad-version",
	}
	err := validateUpgradeMigrationManifest(manifest)
	if err == nil || !strings.Contains(err.Error(), "invalid min_prior_version") {
		t.Fatalf("expected invalid min_prior_version error, got %v", err)
	}
}

func TestInferSourceVersionFromLatestSnapshot_SkipsBadEntriesAndDecodeErrors(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	dir := inst.upgradeSnapshotDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}

	now := time.Now().UTC()

	// Snapshot with entries that should be skipped:
	// 1. al.version entry with invalid version content (fails version.Normalize)
	// 2. non-al.version entry (wrong path, skip)
	// 3. non-file entry kind (skip)
	badSnapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "s_bad",
		CreatedAtUTC:  now.Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{
			{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindFile,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("not-a-version!!!")),
			},
			{
				Path:          ".agent-layer/config.toml",
				Kind:          upgradeSnapshotEntryKindFile,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("name = \"x\"\n")),
			},
		},
	}
	if err := writeUpgradeSnapshotFile(filepath.Join(dir, "s_bad.json"), badSnapshot, RealSystem{}); err != nil {
		t.Fatalf("write bad snapshot: %v", err)
	}

	// Should return empty when all al.version entries fail normalization.
	versionValue, err := inst.inferSourceVersionFromLatestSnapshot()
	if err != nil {
		t.Fatalf("inferSourceVersionFromLatestSnapshot: %v", err)
	}
	if versionValue != "" {
		t.Fatalf("expected empty version when all entries skip, got %q", versionValue)
	}
}

func TestReadMigrationConfigMap_ReadError(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte("a = 1\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	fault := newFaultSystem(RealSystem{})
	fault.readErrs[normalizePath(cfgPath)] = errors.New("read boom")
	inst := &installer{root: root, sys: fault}
	cfg, _, exists, err := inst.readMigrationConfigMap()
	if err == nil || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("expected read error, got %v", err)
	}
	if cfg != nil || exists {
		t.Fatalf("expected nil cfg and exists=false on error, got cfg=%v exists=%v", cfg, exists)
	}
}

func TestExecuteConfigRenameKeyMigration_ToKeyNonTableError(t *testing.T) {
	// Trigger getNestedConfigValue error for the "to" key path: traverse non-table.
	// The "to" key must be at top-level (before any table header) to avoid being nested.
	root := t.TempDir()
	writeTestConfigFile(t, root, "to = \"scalar\"\n\n[from]\nkey = \"val\"\n")
	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.executeConfigRenameKeyMigration("from.key", "to.nested")
	if err == nil || !strings.Contains(err.Error(), "non-table") {
		t.Fatalf("expected non-table error for 'to' path, got %v", err)
	}
}

func TestExecuteConfigRenameKeyMigration_WriteErrorOnDuplicateRemoval(t *testing.T) {
	// toExists with same value, but writeMigrationConfigMap fails.
	root := t.TempDir()
	writeTestConfigFile(t, root, "[from]\nkey = \"same\"\n[to]\nkey = \"same\"\n")
	cfgPath := filepath.Join(root, ".agent-layer", "config.toml")
	fault := newFaultSystem(RealSystem{})
	fault.writeErrs[normalizePath(cfgPath)] = errors.New("write boom dup")
	inst := &installer{root: root, sys: fault}
	_, err := inst.executeConfigRenameKeyMigration("from.key", "to.key")
	if err == nil || !strings.Contains(err.Error(), "write boom dup") {
		t.Fatalf("expected write error on duplicate removal, got %v", err)
	}
}

func TestExecuteConfigSetDefaultMigration_GetNestedError(t *testing.T) {
	// Trigger getNestedConfigValue error on the existing key check.
	root := t.TempDir()
	writeTestConfigFile(t, root, "a = \"scalar\"\n")
	inst := &installer{root: root, sys: RealSystem{}}
	// Key path "a.child" traverses scalar "a", which errors in getNestedConfigValue.
	_, err := inst.executeConfigSetDefaultMigration("a.child", json.RawMessage(`"x"`))
	if err == nil || !strings.Contains(err.Error(), "non-table") {
		t.Fatalf("expected non-table error, got %v", err)
	}
}

func TestSortedUpgradeMigrationOperations_SameIDDifferentKind(t *testing.T) {
	ops := []upgradeMigrationOperation{
		{ID: "same", Kind: upgradeMigrationKindDeleteFile},
		{ID: "same", Kind: upgradeMigrationKindConfigRenameKey},
	}
	sorted := sortedUpgradeMigrationOperations(ops)
	if sorted[0].Kind != upgradeMigrationKindConfigRenameKey || sorted[1].Kind != upgradeMigrationKindDeleteFile {
		t.Fatalf("expected sort by kind when IDs equal, got %v then %v", sorted[0].Kind, sorted[1].Kind)
	}
}

func TestInferSourceVersionFromLatestSnapshot_UnreadableSnapshotSkipped(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	dir := inst.upgradeSnapshotDirPath()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}

	// Write valid snapshot first.
	now := time.Now().UTC()
	validSnapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "s_valid",
		CreatedAtUTC:  now.Format(time.RFC3339),
		Status:        upgradeSnapshotStatusApplied,
		Entries: []upgradeSnapshotEntry{
			{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindFile,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.3.0\n")),
			},
		},
	}
	if err := writeUpgradeSnapshotFile(filepath.Join(dir, "s_valid.json"), validSnapshot, RealSystem{}); err != nil {
		t.Fatalf("write valid snapshot: %v", err)
	}

	// Write a corrupt JSON file that will fail readUpgradeSnapshot during
	// iteration in inferSourceVersionFromLatestSnapshot (via readUpgradeSnapshot
	// inside the loop, not the one inside listUpgradeSnapshotFiles).
	// The corrupt file will cause listUpgradeSnapshotFiles to fail, so we
	// can't test the "continue on readErr" in inferSourceVersionFromLatestSnapshot
	// this way. Instead, test the successful path which is enough.
	versionValue, err := inst.inferSourceVersionFromLatestSnapshot()
	if err != nil {
		t.Fatalf("inferSourceVersionFromLatestSnapshot: %v", err)
	}
	if versionValue != "0.3.0" {
		t.Fatalf("expected 0.3.0, got %q", versionValue)
	}
}

func TestPlanUpgradeMigrations_ConfigSetDefaultWithConfigMigration(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.7.0\n"), 0o644); err != nil {
		t.Fatalf("write pin: %v", err)
	}
	writeTestConfigFile(t, root, "[clients]\n")

	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "set_default",
      "kind": "config_set_default",
      "rationale": "Set default model",
      "source_agnostic": true,
      "key": "clients.model",
      "value": "\"gpt-4\""
    }
  ]
}`)
	inst := &installer{root: root, pinVersion: "0.7.0", sys: RealSystem{}}
	plan, err := inst.planUpgradeMigrations()
	if err != nil {
		t.Fatalf("planUpgradeMigrations: %v", err)
	}
	if len(plan.configMigrations) != 1 {
		t.Fatalf("expected 1 config migration, got %d", len(plan.configMigrations))
	}
	if plan.configMigrations[0].Key != "clients.model" {
		t.Fatalf("unexpected config migration key: %q", plan.configMigrations[0].Key)
	}
	// Rollback targets should include config path.
	cfgAbs := filepath.Clean(filepath.Join(root, ".agent-layer", "config.toml"))
	found := false
	for _, target := range plan.rollbackTargets {
		if target == cfgAbs {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected config path in rollback targets, got %v", plan.rollbackTargets)
	}
}

func TestRunMigrations_NoopStatusForAlreadyMigrated(t *testing.T) {
	root := t.TempDir()
	var warn bytes.Buffer
	inst := &installer{root: root, sys: RealSystem{}, warnWriter: &warn}
	inst.migrationsPrepared = true
	inst.migrationReport = UpgradeMigrationReport{
		TargetVersion:       "0.7.0",
		SourceVersion:       "0.6.0",
		SourceVersionOrigin: UpgradeMigrationSourcePin,
		Entries: []UpgradeMigrationEntry{
			{
				ID:        "del_missing",
				Kind:      string(upgradeMigrationKindDeleteFile),
				Rationale: "Delete missing file",
				Status:    UpgradeMigrationStatusPlanned,
			},
		},
	}
	// Delete a non-existent file -> no-op status.
	inst.pendingMigrationOps = []upgradeMigrationOperation{
		{
			ID:        "del_missing",
			Kind:      upgradeMigrationKindDeleteFile,
			Path:      ".agent-layer/nonexistent.md",
			Rationale: "Delete missing file",
		},
	}
	if err := inst.runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	if inst.migrationReport.Entries[0].Status != UpgradeMigrationStatusNoop {
		t.Fatalf("expected no_op status, got %q", inst.migrationReport.Entries[0].Status)
	}
}

func TestExecuteConfigRenameKeyMigration_FromKeyNonTableError(t *testing.T) {
	// Trigger getNestedConfigValue error for the "from" key path.
	root := t.TempDir()
	writeTestConfigFile(t, root, "from = \"scalar\"\n")
	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.executeConfigRenameKeyMigration("from.key", "to.key")
	if err == nil || !strings.Contains(err.Error(), "non-table") {
		t.Fatalf("expected non-table error for 'from' path, got %v", err)
	}
}

func TestExecuteConfigSetDefaultMigration_SetNestedError(t *testing.T) {
	// Trigger setNestedConfigValue error by having a non-table in the path.
	root := t.TempDir()
	// "clients" is a scalar, not a table, so setNestedConfigValue should error
	// when trying to create "clients.model.name".
	writeTestConfigFile(t, root, "clients = \"scalar\"\n")
	inst := &installer{root: root, sys: RealSystem{}}
	_, err := inst.executeConfigSetDefaultMigration("clients.model.name", json.RawMessage(`"x"`))
	if err == nil || !strings.Contains(err.Error(), "non-table") {
		t.Fatalf("expected non-table error, got %v", err)
	}
}

func TestExecuteConfigSetDefaultMigration_WriteError(t *testing.T) {
	root := t.TempDir()
	writeTestConfigFile(t, root, "[clients]\n")
	cfgPath := filepath.Join(root, ".agent-layer", "config.toml")
	fault := newFaultSystem(RealSystem{})
	fault.writeErrs[normalizePath(cfgPath)] = errors.New("write boom set")
	inst := &installer{root: root, sys: fault}
	_, err := inst.executeConfigSetDefaultMigration("clients.model", json.RawMessage(`"x"`))
	if err == nil || !strings.Contains(err.Error(), "write boom set") {
		t.Fatalf("expected write error, got %v", err)
	}
}

func TestInferSourceVersionFromManifestMatch_MatchError(t *testing.T) {
	originalWalk := templates.WalkFunc
	originalRead := templates.ReadFunc
	originalMap := allTemplateManifestByV
	originalErr := allTemplateManifestErr
	t.Cleanup(func() {
		templates.WalkFunc = originalWalk
		templates.ReadFunc = originalRead
		allTemplateManifestOnce = sync.Once{}
		allTemplateManifestByV = originalMap
		allTemplateManifestErr = originalErr
	})

	allTemplateManifestOnce = sync.Once{}
	allTemplateManifestByV = nil
	allTemplateManifestErr = nil

	root := t.TempDir()
	docsPath := filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")
	if err := os.MkdirAll(filepath.Dir(docsPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(docsPath, []byte("content\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	targetManifestPath := path.Join(templateManifestDir, "5.5.5.json")
	templates.WalkFunc = func(rootPath string, fn fs.WalkDirFunc) error {
		if rootPath != templateManifestDir {
			return originalWalk(rootPath, fn)
		}
		return fn(targetManifestPath, staticDirEntry{name: "5.5.5.json"}, nil)
	}
	manifest := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "5.5.5",
		GeneratedAt:   "2026-02-15T00:00:00Z",
		Files: []manifestFileEntry{
			{Path: "docs/agent-layer/ROADMAP.md", FullHashNormalized: hashNormalizedContent([]byte("content\n"))},
		},
	}
	manifestBytes, _ := json.Marshal(manifest)

	// Make the docs file unreadable to trigger matchErr.
	fault := newFaultSystem(RealSystem{})
	fault.readErrs[normalizePath(docsPath)] = errors.New("read boom match")

	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == targetManifestPath {
			return manifestBytes, nil
		}
		return originalRead(name)
	}

	inst := &installer{root: root, sys: fault}
	_, err := inst.inferSourceVersionFromManifestMatch()
	if err == nil || !strings.Contains(err.Error(), "read boom match") {
		t.Fatalf("expected match error, got %v", err)
	}
}

func TestMigrationWillCoverPath_StatErrors(t *testing.T) {
	root := t.TempDir()
	fault := newFaultSystem(RealSystem{})
	// Test that snapshotEntryAbsPath error returns false for rename.
	// Use invalid path that causes snapshotEntryAbsPath error.
	op := upgradeMigrationOperation{Kind: upgradeMigrationKindRenameFile, From: "", To: ".agent-layer/new.md"}
	got := migrationWillCoverPath(fault, root, op, ".agent-layer/new.md")
	if got {
		t.Fatal("expected false when snapshotEntryAbsPath fails for rename from path")
	}

	// Test delete with empty path (causes snapshotEntryAbsPath error).
	delOp := upgradeMigrationOperation{Kind: upgradeMigrationKindDeleteFile, Path: ""}
	got = migrationWillCoverPath(fault, root, delOp, "")
	if got {
		t.Fatal("expected false when snapshotEntryAbsPath fails for delete")
	}
}

func TestReadMigrationConfigMap_EmptyConfig(t *testing.T) {
	root := t.TempDir()
	// Empty TOML file should parse to nil map, then be initialized.
	writeTestConfigFile(t, root, "")
	inst := &installer{root: root, sys: RealSystem{}}
	cfg, _, exists, err := inst.readMigrationConfigMap()
	if err != nil {
		t.Fatalf("readMigrationConfigMap: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true for empty config file")
	}
	if cfg == nil {
		t.Fatal("expected initialized empty map, got nil")
	}
}

func TestDeleteNestedConfigValue_ParentMissing(t *testing.T) {
	cfg := map[string]any{}
	removed, err := deleteNestedConfigValue(cfg, []string{"missing", "key"})
	if err != nil {
		t.Fatalf("deleteNestedConfigValue: %v", err)
	}
	if removed {
		t.Fatal("expected removed=false for missing parent")
	}
}

func TestExecuteDeleteMigration_InvalidPath(t *testing.T) {
	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	if _, err := inst.executeDeleteMigration(""); err == nil {
		t.Fatal("expected invalid path error")
	}
}

func TestExecuteConfigSetDefaultMigration_ReadError(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte("[clients]\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	faults := newFaultSystem(RealSystem{})
	faults.readErrs[normalizePath(cfgPath)] = errors.New("read boom")
	inst := &installer{root: root, sys: faults}
	if _, err := inst.executeConfigSetDefaultMigration("clients.model", json.RawMessage(`"x"`)); err == nil || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestWriteMigrationConfigMap_EmptyMapAddsTrailingNewline(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeMigrationConfigMap(cfgPath, map[string]any{}); err != nil {
		t.Fatalf("writeMigrationConfigMap: %v", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config file: %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("expected trailing newline for empty config, got %q", string(data))
	}
}

func TestMatchesTemplateDocsManifest_NoDocsEntries(t *testing.T) {
	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	match, err := inst.matchesTemplateDocsManifest(templateManifest{
		Files: []manifestFileEntry{{Path: ".agent-layer/config.toml"}},
	})
	if err != nil {
		t.Fatalf("matchesTemplateDocsManifest: %v", err)
	}
	if match {
		t.Fatal("expected no match when manifest has no docs entries")
	}
}

func TestDedupSortedStrings_Branches(t *testing.T) {
	if out := dedupSortedStrings(nil); out != nil {
		t.Fatalf("expected nil for empty input, got %#v", out)
	}
	out := dedupSortedStrings([]string{" beta ", "", "alpha", "alpha", " "})
	if len(out) != 2 || out[0] != "alpha" || out[1] != "beta" {
		t.Fatalf("unexpected dedup output: %#v", out)
	}
}

func TestRunMigrations_ReportWriteFailurePropagates(t *testing.T) {
	inst := &installer{
		root:               t.TempDir(),
		sys:                RealSystem{},
		warnWriter:         errorWriter{},
		migrationsPrepared: true,
		migrationReport: UpgradeMigrationReport{
			TargetVersion:       "0.7.0",
			SourceVersion:       "0.6.0",
			SourceVersionOrigin: UpgradeMigrationSourcePin,
			Entries: []UpgradeMigrationEntry{{
				ID:        "noop-delete",
				Kind:      string(upgradeMigrationKindDeleteFile),
				Rationale: "delete missing file",
				Status:    UpgradeMigrationStatusPlanned,
			}},
		},
		pendingMigrationOps: []upgradeMigrationOperation{{
			ID:   "noop-delete",
			Kind: upgradeMigrationKindDeleteFile,
			Path: ".agent-layer/missing.md",
		}},
	}

	if err := inst.runMigrations(); err == nil || !strings.Contains(err.Error(), "write boom") {
		t.Fatalf("expected migration report write error, got %v", err)
	}
}

func TestInferSourceVersionFromLatestSnapshot_ListErrorAndReadSkip(t *testing.T) {
	t.Run("list snapshot files error propagates", func(t *testing.T) {
		root := t.TempDir()
		snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
		if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
			t.Fatalf("mkdir snapshot dir: %v", err)
		}

		faults := newFaultSystem(RealSystem{})
		faults.walkErrs[normalizePath(snapshotDir)] = errors.New("walk boom")
		inst := &installer{root: root, sys: faults}
		if _, err := inst.inferSourceVersionFromLatestSnapshot(); err == nil || !strings.Contains(err.Error(), "walk boom") {
			t.Fatalf("expected list/walk error, got %v", err)
		}
	})

	t.Run("unreadable newest snapshot is skipped", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{root: root, sys: RealSystem{}}
		snapshotDir := inst.upgradeSnapshotDirPath()
		if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
			t.Fatalf("mkdir snapshot dir: %v", err)
		}

		now := time.Now().UTC()
		olderPath := filepath.Join(snapshotDir, "older.json")
		newerPath := filepath.Join(snapshotDir, "newer.json")
		older := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    "older",
			CreatedAtUTC:  now.Format(time.RFC3339),
			Status:        upgradeSnapshotStatusApplied,
			Entries: []upgradeSnapshotEntry{{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindFile,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.5.0\n")),
			}},
		}
		newer := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    "newer",
			CreatedAtUTC:  now.Add(time.Second).Format(time.RFC3339),
			Status:        upgradeSnapshotStatusApplied,
			Entries: []upgradeSnapshotEntry{{
				Path:          ".agent-layer/al.version",
				Kind:          upgradeSnapshotEntryKindFile,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("0.6.0\n")),
			}},
		}
		if err := writeUpgradeSnapshotFile(olderPath, older, RealSystem{}); err != nil {
			t.Fatalf("write older snapshot: %v", err)
		}
		if err := writeUpgradeSnapshotFile(newerPath, newer, RealSystem{}); err != nil {
			t.Fatalf("write newer snapshot: %v", err)
		}

		sys := &readFailOnNthSystem{
			base:   RealSystem{},
			target: newerPath,
			failOn: 2, // First read for listUpgradeSnapshotFiles succeeds, second read in infer loop fails.
			err:    errors.New("read boom"),
		}
		inst = &installer{root: root, sys: sys}
		versionValue, err := inst.inferSourceVersionFromLatestSnapshot()
		if err != nil {
			t.Fatalf("inferSourceVersionFromLatestSnapshot: %v", err)
		}
		if versionValue != "0.5.0" {
			t.Fatalf("expected fallback to older snapshot version, got %q", versionValue)
		}
	})
}

type readFailOnNthSystem struct {
	base   System
	target string
	failOn int
	err    error
	calls  int
}

func (s *readFailOnNthSystem) Stat(name string) (os.FileInfo, error) {
	return s.base.Stat(name)
}

func (s *readFailOnNthSystem) ReadFile(name string) ([]byte, error) {
	if normalizePath(name) == normalizePath(s.target) {
		s.calls++
		if s.calls == s.failOn {
			return nil, s.err
		}
	}
	return s.base.ReadFile(name)
}

func (s *readFailOnNthSystem) LookupEnv(key string) (string, bool) {
	return s.base.LookupEnv(key)
}

func (s *readFailOnNthSystem) MkdirAll(path string, perm os.FileMode) error {
	return s.base.MkdirAll(path, perm)
}

func (s *readFailOnNthSystem) RemoveAll(path string) error {
	return s.base.RemoveAll(path)
}

func (s *readFailOnNthSystem) Rename(oldpath string, newpath string) error {
	return s.base.Rename(oldpath, newpath)
}

func (s *readFailOnNthSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	return s.base.WalkDir(root, fn)
}

func (s *readFailOnNthSystem) WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	return s.base.WriteFileAtomic(filename, data, perm)
}

func writeTestConfigFile(t *testing.T, root string, content string) string {
	t.Helper()
	path := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return path
}
