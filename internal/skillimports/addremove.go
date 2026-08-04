package skillimports

import (
	"context"
	"fmt"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
)

// AddOptions describes one `al skills add` invocation. Omitted policy fields
// resolve to their documented constants and never inherit from another block.
type AddOptions struct {
	Repository     string
	Selectors      []string
	Ref            string
	Tracking       string
	Write          string
	PushRepository string
	PushBranch     string
}

// importBlock renders the options as a configuration block.
func (o AddOptions) importBlock() config.SkillImport {
	return config.SkillImport{
		Repository:     config.NormalizeSkillRepository(o.Repository),
		Selectors:      o.Selectors,
		Ref:            strings.TrimSpace(o.Ref),
		Tracking:       strings.TrimSpace(o.Tracking),
		Write:          strings.TrimSpace(o.Write),
		PushRepository: config.NormalizeSkillRepository(o.PushRepository),
		PushBranch:     strings.TrimSpace(o.PushBranch),
	}
}

// Add validates explicit selectors, creates or extends the one block whose
// policy matches exactly, resolves the whole old-to-new desired-set change, and
// publishes configuration, imported skills, and lock state atomically.
//
// It never searches, recommends, or previews skills.
func (s *Service) Add(ctx context.Context, options AddOptions) error {
	state, err := s.loadState()
	if err != nil {
		return err
	}
	if len(options.Selectors) == 0 {
		return fmt.Errorf("at least one selector is required")
	}

	normalized := make([]string, 0, len(options.Selectors))
	positives := 0
	for _, selector := range options.Selectors {
		body, exclusion, parseErr := config.ParseSkillSelector(selector)
		if parseErr != nil {
			return parseErr
		}
		if exclusion {
			normalized = append(normalized, config.SkillSelectorExclusionPrefix+body)
			continue
		}
		positives++
		normalized = append(normalized, body)
	}
	options.Selectors = normalized

	block := options.importBlock()
	blockIndex, found := findPolicyBlock(state.config.Skills, block.PolicyKey())
	if !found && positives == 0 {
		return fmt.Errorf(
			"an exclusion-only add must extend an existing import block with the same repository, ref, tracking, write, and push policy; none matched, and an exclusion never imports a skill by itself",
		)
	}
	if found && len(state.config.Skills.Imports[blockIndex].PositiveSelectors()) == 0 {
		return fmt.Errorf("skills.imports[%d] has no positive selector to extend", blockIndex)
	}
	for _, selector := range normalized {
		if _, exists := config.FindSkillImportSelector(state.config.Skills, block.Repository, selector); exists {
			return fmt.Errorf(
				"selector %q is already configured for %s; each repository and selector pair must be unique",
				selector, RedactSecrets(block.Repository),
			)
		}
	}

	newConfigText := state.configText
	targetIndex := blockIndex
	if found {
		for _, selector := range normalized {
			newConfigText, err = AddSelectorToBlock(newConfigText, blockIndex, selector)
			if err != nil {
				return err
			}
		}
	} else {
		newConfigText = AppendImportBlock(newConfigText, block)
		targetIndex = len(state.config.Skills.Imports)
	}

	newState, err := reparse(state, newConfigText)
	if err != nil {
		return err
	}

	report := &Report{}
	err = s.applyDesiredSetChange(ctx, "add", newState, newConfigText, report, func(plan *reconcilePlan) error {
		return requireSelectorMatches(plan, targetIndex, normalized)
	})
	if err != nil {
		if s.Out != nil {
			report.Write(s.Out)
		}
		return err
	}
	s.runProjection(report)
	return s.finish(report)
}

// Remove removes exactly one configured positive or exclusion selector and
// recomputes the desired set. Skills still matched by another selector stay
// managed. A block left without a positive selector is removed.
func (s *Service) Remove(ctx context.Context, repository string, selector string) error {
	state, err := s.loadState()
	if err != nil {
		return err
	}
	normalizedRepository := config.NormalizeSkillRepository(repository)
	ref, found := config.FindSkillImportSelector(state.config.Skills, normalizedRepository, selector)
	if !found {
		return fmt.Errorf(
			"no configured selector %q for repository %s; run 'al skills status --all' to see what is configured",
			selector, RedactSecrets(normalizedRepository),
		)
	}

	block := state.config.Skills.Imports[ref.BlockIndex]
	remainingPositives := len(block.PositiveSelectors())
	if !ref.Exclusion {
		remainingPositives--
	}

	stored := block.Selectors[ref.SelectorIndex]
	var newConfigText string
	if remainingPositives == 0 {
		newConfigText, err = RemoveImportBlock(state.configText, ref.BlockIndex)
	} else {
		newConfigText, err = RemoveSelectorFromBlock(state.configText, ref.BlockIndex, stored)
	}
	if err != nil {
		return err
	}

	newState, err := reparse(state, newConfigText)
	if err != nil {
		return err
	}

	report := &Report{}
	err = s.applyDesiredSetChange(ctx, "remove", newState, newConfigText, report, nil)
	if err != nil {
		if s.Out != nil {
			report.Write(s.Out)
		}
		return err
	}
	s.runProjection(report)
	return s.finish(report)
}

// applyDesiredSetChange reconciles a proposed configuration without advancing
// any block, then publishes configuration, skills, and lock state together.
// Nothing is published unless every part of the change succeeded, so a failure
// leaves the prior state exactly as it was.
func (s *Service) applyDesiredSetChange(
	ctx context.Context,
	label string,
	state *projectState,
	newConfigText string,
	report *Report,
	check func(*reconcilePlan) error,
) error {
	return s.withWorkspace(ctx, label, func(space *workspace) error {
		plan, err := s.reconcile(ctx, space, state, reconcileOptions{AdvanceTracked: false}, report)
		if err != nil {
			report.Note("no local state was changed")
			return err
		}
		if check != nil {
			if err := check(plan); err != nil {
				report.Note("no local state was changed")
				return err
			}
		}
		if aggregate := report.Err(); aggregate != nil {
			report.Note("no local state was changed")
			return aggregate
		}
		return s.applyPlan(state, plan, newConfigText)
	})
}

// requireSelectorMatches fails an add whose new positive selectors resolved to
// no valid skill. A wildcard that matches nothing is an actionable error rather
// than a silently empty import.
func requireSelectorMatches(plan *reconcilePlan, blockIndex int, selectors []string) error {
	block, ok := plan.blockByIndex(blockIndex)
	if !ok {
		return nil
	}
	for _, selector := range selectors {
		if strings.HasPrefix(selector, config.SkillSelectorExclusionPrefix) {
			continue
		}
		matched := false
		for _, resolvedPath := range block.paths {
			if matchSelectorPath(selector, resolvedPath) {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		for _, exclusion := range block.exclusions {
			if matchSelectorPath(exclusion, selector) || matchSelectorPath(selector, exclusion) {
				return fmt.Errorf(
					"selector %q is cancelled by exclusion %q in the same import block; nothing was added",
					selector, config.SkillSelectorExclusionPrefix+exclusion,
				)
			}
		}
		return fmt.Errorf(
			"selector %q resolved to no valid skills at %s; nothing was added",
			selector, shortCommit(block.commit),
		)
	}
	return nil
}

// findPolicyBlock returns the index of the block whose full policy tuple matches.
func findPolicyBlock(skills config.SkillsConfig, key config.SkillImportPolicyKey) (int, bool) {
	for i, imp := range skills.Imports {
		if imp.PolicyKey() == key {
			return i, true
		}
	}
	return 0, false
}

// reparse validates a proposed configuration document before any state changes,
// so an edit that would produce an invalid config fails before it is published.
func reparse(state *projectState, configText string) (*projectState, error) {
	cfg, err := config.ParseConfig([]byte(configText), state.configPath)
	if err != nil {
		return nil, err
	}
	return &projectState{
		configPath:        state.configPath,
		configText:        state.configText,
		config:            cfg,
		lockPath:          state.lockPath,
		lock:              state.lock,
		sourceFingerprint: state.sourceFingerprint,
	}, nil
}
