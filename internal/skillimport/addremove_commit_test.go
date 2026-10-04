package skillimport

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// TestAddRemoveCommitFailureRollsBackAndDiscardsResults exercises the public
// operations through all three commit-error branches. Each fault occurs after
// the trees have changed, with both pre-publication and post-publication file
// failures, so passing requires actual rollback as well as honest reporting.
func TestAddRemoveCommitFailureRollsBackAndDiscardsResults(t *testing.T) {
	source := newGitRepo(t, "main")
	for _, name := range []string{"alpha", "beta"} {
		source.WriteSkill("skills/"+name, name, name+" body")
		source.WriteFile("skills/"+name+"/reference.md", name+" reference\n", 0o644)
	}
	source.Commit("add skills")

	cases := []struct {
		name             string
		initialSelectors []string
		addSelectors     []string
		removeSelector   string
		pruneBeta        bool
		publishedSkills  map[string]bool
	}{
		{
			name:            "add-new-block",
			addSelectors:    []string{"skills/*"},
			publishedSkills: map[string]bool{"alpha": true, "beta": true},
		},
		{
			name:             "add-to-existing-block",
			initialSelectors: []string{"skills/alpha"},
			addSelectors:     []string{"skills/beta"},
			publishedSkills:  map[string]bool{"alpha": true, "beta": true},
		},
		{
			name:             "remove-entire-block",
			initialSelectors: []string{"skills/*"},
			removeSelector:   "skills/*",
			pruneBeta:        true,
			publishedSkills:  map[string]bool{"alpha": false, "beta": false},
		},
		{
			name:             "remove-positive-retain-block",
			initialSelectors: []string{"skills/alpha", "skills/beta"},
			removeSelector:   "skills/alpha",
			publishedSkills:  map[string]bool{"alpha": false, "beta": true},
		},
		{
			name:             "remove-exclusion-retain-block",
			initialSelectors: []string{"skills/*", "!skills/beta"},
			removeSelector:   "!skills/beta",
			publishedSkills:  map[string]bool{"alpha": true, "beta": true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, failFile := range []string{"config", "lock"} {
				t.Run(failFile, func(t *testing.T) {
					for _, afterPublishing := range []bool{false, true} {
						phase := "before-publication"
						if afterPublishing {
							phase = "after-publication"
						}
						t.Run(phase, func(t *testing.T) {
							proj := newProject(t)
							service := proj.Service()
							if len(tc.initialSelectors) > 0 {
								if _, err := service.Add(context.Background(), AddOptions{Repository: source.URL(), Selectors: tc.initialSelectors}); err != nil {
									t.Fatalf("initial add: %v", err)
								}
							}
							if tc.pruneBeta {
								if err := os.RemoveAll(filepath.Join(proj.paths.ImportedSkillsDir, "beta")); err != nil {
									t.Fatalf("remove beta: %v", err)
								}
							}
							proj.WriteUserSkill("local")
							before := snapshotAddRemoveState(t, proj)
							failPath := proj.paths.ConfigPath
							if failFile == "lock" {
								failPath = proj.paths.SkillsLockPath
							}
							injected := false
							service.newTransaction = func(paths pathSet, lock *skilllock.File) *transaction {
								txn := newTransaction(paths, lock)
								write := txn.writeFile
								txn.writeFile = func(path string, data []byte, perm os.FileMode) error {
									if path != failPath || injected {
										return write(path, data, perm)
									}
									injected = true
									// Prove preflight completed and publication changed the live
									// trees before injecting the error that triggers rollback.
									for name, want := range tc.publishedSkills {
										if got := proj.ImportedExists(name); got != want {
											t.Fatalf("at commit failure, %s present = %v, want %v", name, got, want)
										}
									}
									if failFile == "lock" && proj.ConfigContent() == before.config {
										t.Fatal("lock failure was not preceded by configuration publication")
									}
									err := failingWriter(failPath, afterPublishing)(path, data, perm)
									if afterPublishing {
										got, readErr := os.ReadFile(path) // #nosec G304 -- test project path.
										if readErr != nil || !bytes.Equal(got, data) {
											t.Fatalf("injected failure did not publish requested bytes: %v", readErr)
										}
									}
									return err
								}
								return txn
							}

							var report *Report
							var err error
							if tc.removeSelector != "" {
								report, err = service.Remove(context.Background(), source.URL(), tc.removeSelector)
							} else {
								report, err = service.Add(context.Background(), AddOptions{Repository: source.URL(), Selectors: tc.addSelectors})
							}
							if !injected || err == nil || !strings.Contains(err.Error(), "injected durability failure") {
								t.Fatalf("expected injected commit error, injected = %v, err = %v", injected, err)
							}
							if strings.Contains(err.Error(), "rolling the change back also failed") {
								t.Fatalf("rollback failed: %v", err)
							}
							if after := snapshotAddRemoveState(t, proj); !reflect.DeepEqual(after, before) {
								t.Fatalf("rollback changed config, lock, or skills:\nbefore: %+v\nafter: %+v", before, after)
							}
							if _, statErr := os.Stat(skilljournal.StagingRoot(proj.paths.ImportedSkillsDir)); !os.IsNotExist(statErr) {
								t.Fatalf("settled rollback left staging behind: %v", statErr)
							}
							// Commit errors are operation errors, not fabricated skill or
							// source failures. The CLI uses err and omits this empty report.
							if report == nil || len(report.Skills) != 0 || len(report.Sources) != 0 || report.ProjectionErr != nil {
								t.Fatalf("report contains unapplied results: %+v", report)
							}
							if report.Succeeded() != 0 || report.Partial() {
								t.Fatalf("rolled-back work reports success: %+v", report)
							}
						})
					}
				})
			}
		})
	}
}

type addRemoveState struct {
	config      string
	lock        string
	lockPresent bool
	skills      map[string]string
}

// snapshotAddRemoveState records exact config/lock bytes and all imported,
// user-managed, and projected skill tree hashes, including absent paths.
func snapshotAddRemoveState(t *testing.T, proj *project) addRemoveState {
	t.Helper()
	state := addRemoveState{config: proj.ConfigContent(), skills: map[string]string{}}
	lock, err := os.ReadFile(proj.paths.SkillsLockPath) // #nosec G304 -- test project path.
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read lock: %v", err)
	}
	state.lock, state.lockPresent = string(lock), err == nil
	for _, dir := range []string{proj.paths.ImportedSkillsDir, proj.paths.SkillsDir, filepath.Join(proj.root, ".claude", "skills")} {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("read skills: %v", err)
		}
		for _, entry := range entries {
			tree, err := skilltree.Read(skilltree.OSFS{}, filepath.Join(dir, entry.Name()))
			if err != nil {
				t.Fatalf("read skill %s: %v", entry.Name(), err)
			}
			state.skills[filepath.Join(dir, entry.Name())] = tree.Hash()
		}
	}
	return state
}
