package skillimports

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
)

// Projector publishes the current local skill sources into the client
// directories. It is injected so import operations stay testable without
// running the whole sync pipeline, and so ordinary sync keeps owning
// projection.
type Projector func(root string) error

// Service runs skill import operations for one repository root.
type Service struct {
	// Root is the consuming project's repo root.
	Root string
	// Runner executes git. Nil is not allowed; New sets the real runner.
	Runner GitRunner
	// Project projects local sources into clients after source state changes.
	Project Projector
	// Out receives the human-readable report.
	Out io.Writer
	// WithProjectLock serializes the source publish against ordinary projection.
	// Nil uses the real project sync lock.
	WithProjectLock func(root string, fn func() error) error
}

// New builds a Service with the real git runner and the given projector.
func New(root string, project Projector, out io.Writer) *Service {
	return &Service{Root: root, Runner: ExecGitRunner{}, Project: project, Out: out}
}

// projectState is one consistent snapshot of everything an operation reads.
type projectState struct {
	configPath string
	configText string
	config     *config.Config
	lockPath   string
	lock       *config.SkillImportLock
	// sourceFingerprint captures imported trees and user-managed skill names so
	// remote work cannot overwrite a local edit or a newly-created collision.
	sourceFingerprint string
}

// loadState recovers any interrupted publish, then reads configuration and lock
// state as one snapshot.
func (s *Service) loadState() (*projectState, error) {
	var state *projectState
	err := s.lockProject(s.Root, func() error {
		loaded, loadErr := s.loadStateLocked()
		state = loaded
		return loadErr
	})
	return state, err
}

func (s *Service) loadStateLocked() (*projectState, error) {
	if _, err := RecoverTransaction(s.Root); err != nil {
		return nil, err
	}
	paths := config.DefaultPaths(s.Root)
	raw, err := os.ReadFile(paths.ConfigPath) // #nosec G304 -- paths.ConfigPath is derived from the resolved repo root.
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", paths.ConfigPath, err)
	}
	cfg, err := config.ParseConfig(raw, paths.ConfigPath)
	if err != nil {
		return nil, err
	}
	lock, err := config.LoadSkillImportLock(paths.SkillImportLockPath)
	if err != nil {
		return nil, err
	}
	fingerprint, err := s.skillSourceFingerprint(lock)
	if err != nil {
		return nil, err
	}
	return &projectState{
		configPath:        paths.ConfigPath,
		configText:        string(raw),
		config:            cfg,
		lockPath:          paths.SkillImportLockPath,
		lock:              lock,
		sourceFingerprint: fingerprint,
	}, nil
}

func (s *Service) skillSourceFingerprint(lock *config.SkillImportLock) (string, error) {
	var parts []string
	for _, entry := range lock.Entries {
		local := s.readLocalSkill(entry)
		value := "missing"
		if local.Err != nil {
			value = "error:" + local.Err.Error()
		} else if local.Present {
			value = local.Tree.Hash()
		}
		parts = append(parts, entry.SkillName+"="+value)
	}
	userNames, err := s.userManagedSkillNames()
	if err != nil {
		return "", err
	}
	for name := range userNames {
		parts = append(parts, "user:"+name)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n"), nil
}

// localSkillDir returns the managed directory for one imported skill.
func (s *Service) localSkillDir(name string) string {
	return filepath.Join(ImportedSkillsRoot(s.Root), name)
}

// userManagedSkillNames lists the normalized names of user-managed skills. An
// existing user-managed skill blocks an import with the same name.
func (s *Service) userManagedSkillNames() (map[string]string, error) {
	dir := filepath.Join(s.Root, ".agent-layer", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	names := make(map[string]string, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names[config.NormalizeSkillImportName(entry.Name())] = filepath.Join(dir, entry.Name())
	}
	return names, nil
}

// importedSkillDirs lists the managed root's top-level directories.
func (s *Service) importedSkillDirs() ([]string, error) {
	root := ImportedSkillsRoot(s.Root)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// localTreeState is the on-disk state of one imported skill relative to its lock.
type localTreeState struct {
	// Present reports whether the local directory exists.
	Present bool
	// Tree is the local canonical tree, nil when the directory is absent.
	Tree *Tree
	// Modified reports whether local content differs from the locked upstream hash.
	Modified bool
	// Err records why the local tree could not be read at all.
	Err error
}

// readLocalSkill inspects one imported skill directory against its lock entry.
func (s *Service) readLocalSkill(entry config.SkillImportLockEntry) localTreeState {
	dir := s.localSkillDir(entry.SkillName)
	info, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return localTreeState{}
		}
		return localTreeState{Err: fmt.Errorf("inspect %s: %w", dir, err)}
	}
	if !info.IsDir() {
		return localTreeState{Present: true, Err: fmt.Errorf("%s is not a directory", dir)}
	}
	tree, err := ReadLocalTree(dir)
	if err != nil {
		return localTreeState{Present: true, Err: err}
	}
	return localTreeState{
		Present:  true,
		Tree:     tree,
		Modified: tree.Hash() != entry.UpstreamTreeHash,
	}
}

// sourceLabel renders a block's ref for reporting.
func sourceLabel(imp config.SkillImport) string {
	if ref := strings.TrimSpace(imp.Ref); ref != "" {
		return ref
	}
	return "(default branch)"
}

// runProjection projects local sources into the clients and records a failure
// without discarding already-published valid source state.
func (s *Service) runProjection(report *Report) {
	if s.Project == nil {
		return
	}
	if err := s.Project(s.Root); err != nil {
		report.ProjectionErr = err
	}
}

// finish writes the report and returns the aggregate error.
func (s *Service) finish(report *Report) error {
	if s.Out != nil {
		report.Write(s.Out)
	}
	return report.Err()
}

// withWorkspace runs fn with a disposable git repository, always cleaning up.
func (s *Service) withWorkspace(ctx context.Context, label string, fn func(*workspace) error) error {
	space, err := newWorkspace(ctx, s.Runner, s.Root, label)
	if err != nil {
		return err
	}
	runErr := fn(space)
	closeErr := space.close()
	if runErr != nil {
		return runErr
	}
	return closeErr
}
