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
