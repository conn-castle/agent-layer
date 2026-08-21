package skillimport

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/gitrepo"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

const (
	conflictKindPull = "pull"
	conflictKindPush = "push"
	conflictMetaFile = "agent-layer.json"
)

type conflictState struct {
	Kind                   string          `json:"kind"`
	Lock                   skilllock.Entry `json:"lock"`
	LocalTreeHash          string          `json:"local_tree_hash"`
	Next                   skilllock.Entry `json:"next"`
	DestinationRepository  string          `json:"destination_repository,omitempty"`
	DestinationBranch      string          `json:"destination_branch,omitempty"`
	DestinationHead        string          `json:"destination_head,omitempty"`
	DestinationTreeHash    string          `json:"destination_tree_hash,omitempty"`
	DestinationWritePolicy string          `json:"destination_write_policy,omitempty"`
}

// Resolve applies the staged Git index of a skill conflict workspace.
func (s *Service) Resolve(ctx context.Context, name string) (*Report, error) {
	report := &Report{}
	err := s.withLockedState(func(st *state) error {
		if err := failOnOrphans(st); err != nil {
			return err
		}
		entry, ok := st.lock.Entry(name)
		if !ok {
			return fmt.Errorf("imported skill %q has no lock entry", name)
		}
		state, workspace, err := readConflictMetadata(st, name)
		if err != nil {
			return err
		}
		if !conflictMatches(st, entry, state) {
			return fmt.Errorf("conflict workspace for %q is stale; run the original %s again", name, conflictRetryCommand(state.Kind))
		}

		runner, err := s.newRunner(st.env)
		if err != nil {
			return err
		}
		tree, err := runner.ReadConflictIndex(ctx, workspace)
		if err != nil {
			return err
		}
		if _, err := skilltree.ValidateSkill(tree, entry.SelectedPath); err != nil {
			return fmt.Errorf("resolved tree is not a valid skill: %w", err)
		}

		txn := newTransaction(pathSetFor(st), st.lock)
		txn.WriteSkill(name, tree)
		next := entry
		detail := ""
		switch state.Kind {
		case conflictKindPull:
			next = preservePublication(state.Next, entry)
		case conflictKindPush:
			next.Publication = &skilllock.Publication{
				Repository: state.DestinationRepository,
				Branch:     state.DestinationBranch,
				Commit:     state.DestinationHead,
				TreeHash:   state.DestinationTreeHash,
			}
			detail = "rerun 'al skills push'"
		default:
			return fmt.Errorf("conflict workspace for %q has unknown kind %q", name, state.Kind)
		}
		txn.SetLockEntry(next)
		if err := txn.Commit(); err != nil {
			return err
		}
		if err := os.RemoveAll(workspace); err != nil {
			return fmt.Errorf("resolved skill but could not remove conflict workspace: %w", err)
		}
		report.Add(SkillResult{
			Name:         name,
			Repository:   entry.Repository,
			SelectedPath: entry.SelectedPath,
			Outcome:      OutcomeResolved,
			Detail:       detail,
		})
		s.project(report)
		return nil
	})
	report.Sort()
	return report, err
}

func conflictRetryCommand(kind string) string {
	if kind == conflictKindPush {
		return "'al skills push'"
	}
	return "'al skills pull'"
}

func conflictWorkspace(root, name string) (string, error) {
	if err := skilltree.ValidateRelativePath(name); err != nil {
		return "", fmt.Errorf("invalid skill name %q: %w", name, err)
	}
	return filepath.Join(root, ".agent-layer", "tmp", "skill-conflicts", name), nil
}

func writePullConflictWorkspace(ctx context.Context, runner *gitrepo.Runner, st *state, name string, local, baseTree, upstream skilltree.Tree, lock, next skilllock.Entry) (string, error) {
	next.Publication = nil
	return writeConflictWorkspace(ctx, runner, st, name, gitrepo.ConflictWorkspaceSpec{
		Base:         baseTree,
		Local:        local,
		Theirs:       upstream,
		TheirsBranch: gitrepo.ConflictBranchUpstream,
	}, conflictState{Kind: conflictKindPull, Lock: lock, Next: next})
}

func writePushConflictWorkspace(ctx context.Context, runner *gitrepo.Runner, st *state, name string, local, baseTree, destination skilltree.Tree, lock skilllock.Entry, group *pushGroup, head string) (string, error) {
	block, _, configured := st.configuredBlockForEntry(lock)
	writePolicy := ""
	if configured {
		writePolicy = block.EffectiveWritePolicy()
	}
	return writeConflictWorkspace(ctx, runner, st, name, gitrepo.ConflictWorkspaceSpec{
		Base:         baseTree,
		Local:        local,
		Theirs:       destination,
		TheirsBranch: gitrepo.ConflictBranchDestination,
	}, conflictState{
		Kind:                   conflictKindPush,
		Lock:                   lock,
		DestinationRepository:  group.Repository.String(),
		DestinationBranch:      group.Branch,
		DestinationHead:        head,
		DestinationTreeHash:    destination.Hash(),
		DestinationWritePolicy: writePolicy,
	})
}

func writeConflictWorkspace(ctx context.Context, runner *gitrepo.Runner, st *state, name string, spec gitrepo.ConflictWorkspaceSpec, state conflictState) (string, error) {
	dir, err := conflictWorkspace(st.paths.Root, name)
	if err != nil {
		return "", err
	}
	if _, statErr := os.Stat(dir); statErr == nil {
		existing, _, readErr := readConflictMetadata(st, name)
		if readErr != nil {
			return "", fmt.Errorf("existing conflict workspace %s is unreadable; move or remove it before retrying: %w", relativeTo(st.paths.Root, dir), readErr)
		}
		if conflictMatches(st, state.Lock, existing) {
			return "", fmt.Errorf("active %s conflict workspace %s already exists; finish it with git and run 'al skills resolve %s'", existing.Kind, relativeTo(st.paths.Root, dir), name)
		}
		return "", fmt.Errorf("existing conflict workspace %s is stale; move or remove it before retrying", relativeTo(st.paths.Root, dir))
	} else if !os.IsNotExist(statErr) {
		return "", fmt.Errorf("inspect conflict workspace %s: %w", relativeTo(st.paths.Root, dir), statErr)
	}
	if err := runner.CreateConflictWorkspace(ctx, dir, spec); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	state.LocalTreeHash = spec.Local.Hash()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", conflictMetaFile), append(data, '\n'), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

func validateConflictWorkspaceMetadata(st *state, name string) error {
	dir, err := conflictWorkspace(st.paths.Root, name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect conflict workspace %s: %w", relativeTo(st.paths.Root, dir), err)
	}
	if _, _, err := readConflictMetadata(st, name); err != nil {
		return fmt.Errorf("conflict workspace %s is unreadable; move or remove it before retrying: %w", relativeTo(st.paths.Root, dir), err)
	}
	return nil
}

func readConflictMetadata(st *state, name string) (conflictState, string, error) {
	dir, err := conflictWorkspace(st.paths.Root, name)
	if err != nil {
		return conflictState{}, "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, ".git", conflictMetaFile)) // #nosec G304 -- name is validated above.
	if err != nil {
		if os.IsNotExist(err) {
			return conflictState{}, "", fmt.Errorf("imported skill %q has no conflict workspace; run 'al skills pull' or 'al skills push' first", name)
		}
		return conflictState{}, "", err
	}
	var state conflictState
	if err := json.Unmarshal(data, &state); err != nil {
		return conflictState{}, "", fmt.Errorf("invalid conflict workspace for %q: %w", name, err)
	}
	if state.Kind != conflictKindPull && state.Kind != conflictKindPush {
		return conflictState{}, "", fmt.Errorf("invalid conflict workspace for %q: unknown kind %q", name, state.Kind)
	}
	return state, dir, nil
}

func matchingConflictWorkspace(st *state, entry skilllock.Entry) (string, bool) {
	state, dir, err := readConflictMetadata(st, entry.Name)
	if err != nil {
		return "", false
	}
	if !conflictMatches(st, entry, state) {
		return "", false
	}
	return dir, true
}

func conflictMatches(st *state, entry skilllock.Entry, state conflictState) bool {
	if !conflictLockMatches(state.Lock, entry, state.Kind) {
		return false
	}
	local := st.skill(entry.Name)
	if !local.Present || local.Err != nil || local.Tree.Hash() != state.LocalTreeHash {
		return false
	}
	block, _, configured := st.configuredBlockForEntry(entry)
	if !configured {
		return false
	}
	switch state.Kind {
	case conflictKindPull:
		return pullNextMatchesConfig(st, entry, state.Next)
	case conflictKindPush:
		if !block.WriteEnabled() {
			return false
		}
		if config.NormalizeSkillRepository(block.EffectivePushRepository()) != state.DestinationRepository {
			return false
		}
		if block.EffectiveWritePolicy() != state.DestinationWritePolicy {
			return false
		}
		if state.DestinationWritePolicy == config.SkillWritePolicyBranch {
			return strings.TrimSpace(block.PushBranch) == state.DestinationBranch
		}
		// Direct writes follow the live default branch, which cannot be rechecked
		// by this deliberately offline workspace validation.
		return true
	default:
		return false
	}
}

func conflictLockMatches(recorded, current skilllock.Entry, kind string) bool {
	if kind == conflictKindPull {
		// Publication checkpoints affect only destination-side push merges. A push
		// completed while a pull conflict is being resolved does not invalidate the
		// pull workspace's source base or result.
		recorded.Publication = nil
		current.Publication = nil
	}
	return recorded.Equal(current)
}

// pullNextMatchesConfig reports whether current configuration still owns the
// recorded next source selection, so resolve cannot apply an obsolete lock
// after a ref or selector edit.
func pullNextMatchesConfig(st *state, entry skilllock.Entry, next skilllock.Entry) bool {
	if next.Name != entry.Name || next.SelectedPath != entry.SelectedPath {
		return false
	}
	block, _, configured := st.configuredBlockForEntry(next)
	if !configured {
		return false
	}
	if config.NormalizeSkillRepository(block.Repository) != next.Repository {
		return false
	}
	if block.Ref != next.ConfiguredRef {
		return false
	}
	if configuredTracking(block, next.RefKind) != next.Tracking {
		return false
	}
	_, selected := selectingPositiveSelector(block, next.SelectedPath)
	return selected
}

// configuredTracking is the tracking mode resolve must compare against the
// recorded lock. An omitted config value is derived from the recorded ref kind
// the same way the first networked pull would: branches track, tags and
// commits pin.
func configuredTracking(block config.SkillImport, recordedRefKind string) string {
	if tracking := strings.TrimSpace(block.Tracking); tracking != "" {
		return tracking
	}
	if recordedRefKind == skilllock.RefKindBranch {
		return config.SkillTrackingTracked
	}
	return config.SkillTrackingPinned
}
