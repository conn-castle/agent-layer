package skillimport

import (
	"context"
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
	diffSideBase        = "base"
	diffSideLocal       = "local"
	diffSideUpstream    = "upstream"
	diffSideDestination = "destination"
)

// Diff compares two live sides of an imported skill and returns an ordinary
// Git unified diff. Identical trees produce no output.
func (s *Service) Diff(ctx context.Context, name string, from string, to string) ([]byte, error) {
	from = defaultDiffSide(from, diffSideLocal)
	to = defaultDiffSide(to, diffSideUpstream)
	if err := validateDiffSide(from); err != nil {
		return nil, err
	}
	if err := validateDiffSide(to); err != nil {
		return nil, err
	}

	var output []byte
	err := s.withLockedState(func(st *state) error {
		if err := failOnOrphans(st); err != nil {
			return err
		}
		entry, ok := st.lock.Entry(name)
		if !ok {
			return fmt.Errorf("imported skill %q has no lock entry", name)
		}
		diff, err := s.diffLocked(ctx, st, entry, from, to)
		if err != nil {
			return err
		}
		output = diff
		return nil
	})
	return output, err
}

func defaultDiffSide(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func validateDiffSide(side string) error {
	switch side {
	case diffSideBase, diffSideLocal, diffSideUpstream, diffSideDestination:
		return nil
	default:
		return fmt.Errorf("unsupported diff side %q; use base, local, upstream, or destination", side)
	}
}

func (s *Service) diffLocked(ctx context.Context, st *state, entry skilllock.Entry, from string, to string) ([]byte, error) {
	runner, err := s.newRunner(st.env)
	if err != nil {
		return nil, err
	}
	workRoot, err := os.MkdirTemp("", "al-skill-diff-")
	if err != nil {
		return nil, fmt.Errorf("failed to create a git working directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(workRoot) }()

	fromTree, err := s.resolveDiffSide(ctx, runner, workRoot, st, entry, from)
	if err != nil {
		return nil, err
	}
	toTree, err := s.resolveDiffSide(ctx, runner, workRoot, st, entry, to)
	if err != nil {
		return nil, err
	}
	return runner.DiffTrees(ctx, from, fromTree, to, toTree)
}

func (s *Service) resolveDiffSide(ctx context.Context, runner *gitrepo.Runner, workRoot string, st *state, entry skilllock.Entry, side string) (skilltree.Tree, error) {
	switch side {
	case diffSideLocal:
		observed := st.skill(entry.Name)
		if !observed.Present {
			return skilltree.Tree{}, fmt.Errorf("imported directory %s is missing", relativeTo(st.paths.Root, observed.Dir))
		}
		if observed.Err != nil {
			return skilltree.Tree{}, observed.Err
		}
		return observed.Tree, nil
	case diffSideBase:
		source, err := s.openLockedSource(ctx, runner, workRoot, entry.Repository)
		if err != nil {
			return skilltree.Tree{}, err
		}
		if err := source.Fetch(ctx, entry.Commit); err != nil {
			return skilltree.Tree{}, err
		}
		return source.ReadTree(ctx, entry.Commit, entry.SelectedPath)
	case diffSideUpstream:
		block, index, configured := st.configuredBlockForEntry(entry)
		if !configured {
			return skilltree.Tree{}, fmt.Errorf("imported skill %q is not in the current configuration", entry.Name)
		}
		blockCtx, err := s.openBlock(ctx, runner, workRoot, index, block)
		if err != nil {
			return skilltree.Tree{}, err
		}
		return blockCtx.Source.ReadTree(ctx, blockCtx.Resolution.Commit, entry.SelectedPath)
	case diffSideDestination:
		return s.readLiveDestinationTree(ctx, runner, workRoot, st, entry)
	default:
		return skilltree.Tree{}, fmt.Errorf("unhandled diff side %q", side)
	}
}

func (s *Service) openLockedSource(ctx context.Context, runner *gitrepo.Runner, workRoot string, repository string) (*gitrepo.Source, error) {
	dir := filepath.Join(workRoot, "lock-source")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create git working directory %s: %w", dir, err)
	}
	resolved, err := runner.Secrets().Resolve(config.NormalizeSkillRepository(repository))
	if err != nil {
		return nil, err
	}
	return gitrepo.OpenSource(ctx, runner, dir, resolved)
}

func (s *Service) readLiveDestinationTree(ctx context.Context, runner *gitrepo.Runner, workRoot string, st *state, entry skilllock.Entry) (skilltree.Tree, error) {
	block, _, configured := st.configuredBlockForEntry(entry)
	if !configured || !block.WriteEnabled() {
		return skilltree.Tree{}, fmt.Errorf("imported skill %q has no writable destination", entry.Name)
	}
	dir := filepath.Join(workRoot, "destination")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return skilltree.Tree{}, fmt.Errorf("failed to create git working directory %s: %w", dir, err)
	}
	destinationRef, err := runner.Secrets().Resolve(config.NormalizeSkillRepository(block.EffectivePushRepository()))
	if err != nil {
		return skilltree.Tree{}, err
	}
	destination, err := gitrepo.OpenDestination(ctx, runner, dir, destinationRef)
	if err != nil {
		return skilltree.Tree{}, err
	}
	branch, err := liveDestinationBranch(ctx, destination, destinationRef, block)
	if err != nil {
		return skilltree.Tree{}, err
	}
	head, exists, err := destination.Head(ctx, branch)
	if err != nil {
		return skilltree.Tree{}, err
	}
	if !exists {
		return skilltree.Tree{}, fmt.Errorf("destination %s has no branch %s", destinationRef, branch)
	}
	if err := destination.FetchCommit(ctx, destinationRef, head); err != nil {
		return skilltree.Tree{}, err
	}
	return destination.ReadTree(ctx, head, entry.SelectedPath)
}

func liveDestinationBranch(ctx context.Context, destination *gitrepo.Destination, destinationRef gitrepo.Repository, block config.SkillImport) (string, error) {
	defaultBranch, err := destination.DefaultBranch(ctx)
	if err != nil {
		return "", err
	}
	branch, _, err := pushBranchForDefault(block, destinationRef, defaultBranch)
	return branch, err
}
