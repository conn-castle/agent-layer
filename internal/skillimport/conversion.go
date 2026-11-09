package skillimport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/gitrepo"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// UpgradeConverter converts unmodified shipped local copies to the Git imports
// a fresh setup creates. al upgrade passes it to the installer, which holds the
// project lock, has already recovered interrupted imports, and owns the upgrade
// snapshot. Nothing here takes the project lock or runs recovery: the lock is
// not reentrant.
type UpgradeConverter struct{}

// Prepare fetches the catalog repository once and validates every requested
// import at the resolved commit. It writes only to a temporary directory
// outside the project.
func (UpgradeConverter) Prepare(ctx context.Context, root string, items []install.PristineConversion) (install.PreparedConversion, error) {
	var skills, instructions []string
	for _, item := range items {
		if item.Kind == install.PristineConversionSkill {
			skills = append(skills, item.Selector)
		} else {
			instructions = append(instructions, item.Selector)
		}
	}
	prepared, err := PrepareConversion(ctx, root, skills, instructions)
	if err != nil {
		return nil, err
	}
	return prepared, nil
}

// ConversionSource is a preflighted catalog source pinned to one resolved
// commit, so every conversion imports exactly what the preflight validated.
type ConversionSource struct {
	root       string
	source     *gitrepo.Source
	resolution gitrepo.Resolution
	cleanup    func()
	// newTransaction lets tests interrupt or fail the journaled commits.
	newTransaction func(pathSet, *skilllock.File) *transaction
}

// PrepareConversion resolves the catalog repository's default branch and
// validates every selector there without writing to the project.
func PrepareConversion(ctx context.Context, root string, skillSelectors, instructionSelectors []string) (*ConversionSource, error) {
	env, err := config.LoadEnv(config.DefaultPaths(root).EnvPath)
	if err != nil {
		return nil, err
	}
	runner, err := gitrepo.NewRunner(env)
	if err != nil {
		return nil, err
	}
	workRoot, err := os.MkdirTemp("", "al-skill-convert-")
	if err != nil {
		return nil, fmt.Errorf("failed to create a git working directory: %w", err)
	}
	prepared := &ConversionSource{root: root, cleanup: func() { _ = os.RemoveAll(workRoot) }}
	if err := prepared.open(ctx, runner, workRoot, skillSelectors, instructionSelectors); err != nil {
		prepared.Close()
		return nil, err
	}
	return prepared, nil
}

func (c *ConversionSource) open(ctx context.Context, runner *gitrepo.Runner, workRoot string, skillSelectors, instructionSelectors []string) error {
	repository, err := runner.Secrets().Resolve(config.NormalizeSkillRepository(templates.GeneralSkillsRepository))
	if err != nil {
		return err
	}
	c.source, err = gitrepo.OpenSource(ctx, runner, filepath.Join(workRoot, "catalog"), repository)
	if err != nil {
		return err
	}
	c.resolution, err = c.source.Resolve(ctx, "")
	if err != nil {
		return err
	}
	blocks := []config.SkillImport{}
	if len(skillSelectors) > 0 {
		blocks = append(blocks, config.SkillImport{Repository: templates.GeneralSkillsRepository, Selectors: skillSelectors})
	}
	for _, selector := range instructionSelectors {
		blocks = append(blocks, config.SkillImport{Repository: templates.GeneralSkillsRepository, Selectors: []string{selector}, ExactFile: true})
	}
	for _, block := range blocks {
		desired, failures, err := resolveBlock(ctx, c.source, block, c.resolution.Commit)
		if err != nil {
			return err
		}
		if len(failures) > 0 {
			return candidateFailureError(failures)
		}
		if len(desired) != len(block.Selectors) {
			return fmt.Errorf("%d of %d selectors resolved at %s", len(desired), len(block.Selectors), shortCommit(c.resolution.Commit))
		}
	}
	return nil
}

// Commit is the resolved catalog commit every conversion imports.
func (c *ConversionSource) Commit() string { return c.resolution.Commit }

// Close removes the temporary Git workspace.
func (c *ConversionSource) Close() {
	if c.cleanup != nil {
		c.cleanup()
		c.cleanup = nil
	}
}

// Import commits the skills in one journaled transaction and each instruction
// in its own, calling removeLocal for every item right after its import
// commits. A failure stops before any later local copy is removed.
func (c *ConversionSource) Import(ctx context.Context, items []install.PristineConversion, removeLocal func(install.PristineConversion) error) error {
	var skills []install.PristineConversion
	var selectors []string
	for _, item := range items {
		if item.Kind == install.PristineConversionSkill {
			skills = append(skills, item)
			selectors = append(selectors, item.Selector)
		}
	}
	if len(skills) > 0 {
		if err := c.ImportSkillsLocked(ctx, selectors); err != nil {
			return err
		}
		for _, item := range skills {
			if err := removeLocal(item); err != nil {
				return err
			}
		}
	}
	for _, item := range items {
		if item.Kind != install.PristineConversionInstruction {
			continue
		}
		if err := c.ImportInstructionLocked(ctx, item.Selector, item.LocalName); err != nil {
			return err
		}
		if err := removeLocal(item); err != nil {
			return err
		}
	}
	return nil
}

// ImportSkillsLocked adds exact catalog selectors to the default-policy block at
// the prepared commit. Same-name local skill directories are the caller's
// verified unmodified shipped copies, which it removes after this commits; they
// do not block the import. The caller holds the project lock.
func (c *ConversionSource) ImportSkillsLocked(ctx context.Context, selectors []string) error {
	s := c.service(NewSourceOnly(c.root))
	st, err := loadState(c.root, false)
	if err != nil {
		return err
	}
	if err := failOnOrphans(st); err != nil {
		return err
	}
	if err := requireCatalogPolicy(st); err != nil {
		return err
	}
	for _, selector := range selectors {
		delete(st.userSkills, collisionName(path.Base(selector)))
	}
	for _, selector := range selectors {
		member := catalogMember(st, templates.GeneralSkillsRepository, selector)
		if member.Problem != "" {
			return fmt.Errorf(messages.CatalogMemberProblemFmt, member.Name, member.Problem)
		}
		if member.Configured {
			return fmt.Errorf("catalog %s is already configured; its local copy is preserved", member.Name)
		}
	}
	report := &Report{}
	return withReportDetail(s.addLocked(ctx, st, AddOptions{Repository: templates.GeneralSkillsRepository, Selectors: selectors}, report), report)
}

// ImportInstructionLocked replaces the [[instructions.local]] block selecting
// localName with an import of selector at the same order, in one journaled
// transaction. The caller holds the project lock and removes the local file
// after this commits.
func (c *ConversionSource) ImportInstructionLocked(ctx context.Context, selector, localName string) error {
	s := c.service(NewInstructionsSourceOnly(c.root))
	if err := config.ValidateInstructionRootsFS(os.DirFS(c.root), c.root); err != nil {
		return err
	}
	st, err := loadState(c.root, true)
	if err != nil {
		return err
	}
	if err := failOnOrphans(st); err != nil {
		return err
	}
	_, order, err := config.RemoveLocalInstruction(st.configRaw, localName)
	if err != nil {
		return err
	}
	if order == nil {
		return fmt.Errorf("local instruction %s has no [[instructions.local]] block; its file is preserved", localName)
	}
	report := &Report{instructions: true}
	opts := AddOptions{Repository: templates.GeneralSkillsRepository, Selectors: []string{selector}, Order: order, replaceLocal: localName}
	return withReportDetail(s.addLocked(ctx, st, opts, report), report)
}

func (c *ConversionSource) service(s *Service) *Service {
	s.prepared = c
	if c.newTransaction != nil {
		s.newTransaction = c.newTransaction
	}
	return s
}

// blockContext reuses the prepared source for a catalog block, so the commit
// needs no network and cannot drift from the preflighted commit.
func (c *ConversionSource) blockContext(block config.SkillImport) (*blockContext, error) {
	if config.NormalizeSkillRepository(block.Repository) != config.NormalizeSkillRepository(templates.GeneralSkillsRepository) || block.Ref != "" {
		return nil, errors.New("conversion imports only the default catalog source")
	}
	tracking, err := ensureTrackedRefKind(block, c.resolution)
	if err != nil {
		return nil, err
	}
	return &blockContext{Block: block, Source: c.source, Resolution: c.resolution, Tracking: tracking}, nil
}

func withReportDetail(err error, report *Report) error {
	if err == nil || !report.Failed() {
		return err
	}
	return fmt.Errorf("%w:\n%s", err, report.Render("add"))
}
