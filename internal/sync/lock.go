package sync

import (
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/projectlock"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
)

// ErrPostWriteLockCleanup identifies a fatal lock cleanup failure after sync
// has already written all generated outputs successfully. It aliases the
// projectlock sentinel so existing `errors.Is` callers keep working after the
// lock moved into its own package.
var ErrPostWriteLockCleanup = projectlock.ErrPostWriteCleanup

// RecoverInterruptedImport rolls back a skill import transaction an earlier
// process left in flight, so the caller reads one coherent generation of
// configuration, imported trees, and lock state. It is a no-op when no
// transaction was interrupted. Callers must already hold the project lock.
func RecoverInterruptedImport(root string) error {
	paths := config.DefaultPaths(root)
	return skilljournal.Recover(skilljournal.Targets{
		ImportedSkillsDir: paths.ImportedSkillsDir,
		ConfigPath:        paths.ConfigPath,
		SkillsLockPath:    paths.SkillsLockPath,
	})
}

// withProjectSyncLock runs fn inside the shared project lock that serializes
// projection with skill import mutations.
//
// Recovery of an interrupted import runs first: ordinary projection must never
// build its snapshot from a half-applied import transaction.
func withProjectSyncLock(sys System, root string, fn func() (*Result, error)) (*Result, error) {
	var result *Result
	err := projectlock.With(sys, root, func() error {
		if recoverErr := RecoverInterruptedImport(root); recoverErr != nil {
			return recoverErr
		}
		var runErr error
		result, runErr = fn()
		return runErr
	})
	// The result is returned even when err is non-nil: a post-write lock cleanup
	// failure must not discard a sync that already generated all of its outputs.
	return result, err
}
