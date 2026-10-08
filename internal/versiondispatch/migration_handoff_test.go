package versiondispatch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestDispatchRefusesUnsafeReaderBeforeLookingForBinary(t *testing.T) {
	for _, future := range []bool{true, false} {
		root := t.TempDir()
		paths := config.DefaultPaths(root)
		require.NoError(t, os.MkdirAll(paths.ImportedSkillsDir, 0o750))
		if future {
			stage := filepath.Join(paths.ImportedSkillsDir, ".staging")
			require.NoError(t, os.Mkdir(stage, 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(stage, "journal.json"), []byte("{\"version\":3}"), 0o600))
		} else {
			require.NoError(t, os.Mkdir(filepath.Join(paths.ImportedSkillsDir, "ship-pr"), 0o750))
			lock := skilllock.New()
			lock.Upsert(skilllock.Entry{Name: "ship-pr", Repository: templates.GeneralSkillsRepository, Selector: "skills/development/ship-pr", SelectedPath: "skills/development/ship-pr", ResolvedRef: "main", RefKind: "branch", Tracking: "tracked", Commit: strings.Repeat("a", 40), TreeHash: "sha256:" + strings.Repeat("b", 64)})
			require.NoError(t, lock.Save(paths.SkillsLockPath))
		}
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "sync.lock"), nil, 0o600))
		before := testutil.SnapshotEvidence(t, root)
		t.Setenv(EnvCacheDir, t.TempDir())
		t.Setenv(EnvVersionOverride, "0.99.0")
		t.Setenv(EnvShimActive, "")
		sys := &testSystem{ExecBinaryFunc: func(string, []string, []string) error { t.Fatal("unsafe reader dispatched"); return nil }}
		err := maybeExec(context.Background(), sys, []string{"al", "sync"}, "1.0.0", root)
		if future {
			require.ErrorContains(t, err, "unsupported schema version 3")
		} else {
			require.ErrorContains(t, err, "cannot select pre-migration CLI")
		}
		require.Equal(t, before, testutil.SnapshotEvidence(t, root))
	}
}
