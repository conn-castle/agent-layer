package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/gitrepo"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// InitStudyOptions configures a self-contained benchmark study scaffold.
type InitStudyOptions struct {
	RepoRoot      string
	SelectionPath string
	Directory     string
	SkillsRef     string
}

// InitStudy creates a reproducible bare-versus-current-Agent-Layer study.
func InitStudy(options InitStudyOptions) (string, error) {
	if options.RepoRoot == "" || options.SelectionPath == "" || options.Directory == "" {
		return "", fmt.Errorf("benchmark init requires a selection and destination directory")
	}
	directory := options.Directory
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(options.RepoRoot, directory)
	}
	destination, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	if entries, readErr := os.ReadDir(destination); readErr == nil && len(entries) > 0 {
		return "", fmt.Errorf("benchmark study directory %s is not empty", destination)
	} else if readErr != nil && !os.IsNotExist(readErr) {
		return "", readErr
	}
	selection, _, err := loadMatrixSelection(options.SelectionPath, nil)
	if err != nil {
		return "", err
	}
	model, effort, err := ParseModelSelection(modelNameForPublished(selection.Selector.Model) + ":" + selection.Selector.Reasoning)
	if err != nil {
		return "", err
	}
	// Fetch and validate all external content before changing the requested path.
	frozen, provenance, err := freezeDevelopmentSkills(context.Background(), options.SkillsRef)
	if err != nil {
		return "", fmt.Errorf("snapshot external development skills: %w", err)
	}
	requestedDestination := destination
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".al-study-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	destination = stage
	if err := os.MkdirAll(filepath.Join(destination, "treatment"), 0o700); err != nil {
		return "", err
	}
	if err := copyScaffoldFile(options.SelectionPath, filepath.Join(destination, "selection.json")); err != nil {
		return "", err
	}
	for _, item := range []struct{ source, target string }{
		{filepath.Join(options.RepoRoot, ".agent-layer", "instructions"), filepath.Join(destination, "treatment", "project-instructions")},
		{filepath.Join(options.RepoRoot, ".agents", "skills"), filepath.Join(destination, "treatment", "project-skills")},
	} {
		if err := copyScaffoldTree(item.source, item.target); err != nil {
			return "", err
		}
	}
	for name, tree := range frozen {
		target := filepath.Join(destination, "treatment", "official-skills", name)
		if err := os.MkdirAll(target, 0o750); err != nil {
			return "", err
		}
		if err := skilltree.Materialize(tree, target); err != nil {
			return "", err
		}
	}
	data, err := json.MarshalIndent(provenance, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(destination, "treatment", "skills-source.json"), append(data, '\n'), 0o600); err != nil {
		return "", err
	}
	if err := copyEmbeddedScaffoldFile("instructions/00_rules.md", filepath.Join(destination, "treatment", "official-instructions", "00_rules.md")); err != nil {
		return "", fmt.Errorf("snapshot official Agent Layer instructions: %w", err)
	}
	config := scaffoldConfig(model.Adapter, dispatchModel(model), effort)
	if err := os.WriteFile(filepath.Join(destination, "treatment", "config.toml"), []byte(config), 0o600); err != nil {
		return "", err
	}
	prompt := `Complete the following task using the Agent Layer implement skill.

This study requires completed Agent Dispatch plan-reviewer, implementer, and code-reviewer roles. The benchmark runtime supplies their exact named targets and dispatch role fields in the mandatory workflow contract.

{{task}}
`
	if err := os.WriteFile(filepath.Join(destination, "treatment", "prompt.md"), []byte(prompt), 0o600); err != nil {
		return "", err
	}
	study := fmt.Sprintf(`selection = "selection.json"

[[experiments]]
name = "bare"
model = %q
reasoning = %q

[[experiments]]
name = "agent-layer"
model = %q
reasoning = %q
config = "treatment/config.toml"
instructions = "treatment/official-instructions"
skills = "treatment/official-skills"
skills_source = "treatment/skills-source.json"
entry_prompt = "treatment/prompt.md"
required_dispatch_roles = ["plan-reviewer", "implementer", "code-reviewer"]
`, modelNameForPublished(model.PublishedIdentifier), effort, modelNameForPublished(model.PublishedIdentifier), effort)
	path := filepath.Join(destination, "study.toml")
	if err := os.WriteFile(path, []byte(study), 0o600); err != nil {
		return "", err
	}
	// A destination that was originally empty must still be empty. Rename publishes
	// the complete scaffold; failure leaves any pre-existing content intact.
	if entries, err := os.ReadDir(requestedDestination); err == nil {
		if len(entries) > 0 {
			return "", fmt.Errorf("study destination changed during setup: %s", requestedDestination)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	// os.Rename rejects an existing directory in Go. The native rename atomically
	// replaces an empty directory on supported Unix hosts and refuses nonempty ones.
	if err := syscall.Rename(stage, requestedDestination); err != nil {
		return "", fmt.Errorf("publish benchmark study %s: %w", requestedDestination, err)
	}
	return filepath.Join(requestedDestination, "study.toml"), nil
}

// SkillSnapshotSource records genuine Git and canonical tree evidence for frozen content.
type SkillSnapshotSource struct {
	Repository    string            `json:"repository"`
	ConfiguredRef string            `json:"configured_ref,omitempty"`
	ResolvedRef   string            `json:"resolved_ref"`
	Commit        string            `json:"commit"`
	Trees         map[string]string `json:"trees"`
}

func freezeDevelopmentSkills(ctx context.Context, ref string) (map[string]skilltree.Tree, SkillSnapshotSource, error) {
	result := SkillSnapshotSource{Repository: templates.GeneralSkillsRepository, ConfiguredRef: ref, Trees: map[string]string{}}
	catalog, err := templates.LoadCLISkillCatalog()
	if err != nil {
		return nil, result, err
	}
	var selectors []string
	for _, entry := range catalog {
		if entry.ID == "development-skills" {
			selectors = entry.Selectors
		}
	}
	if len(selectors) != 7 {
		return nil, result, fmt.Errorf("development catalog must contain seven skills")
	}
	runner, err := gitrepo.NewRunner(nil)
	if err != nil {
		return nil, result, err
	}
	root, err := os.MkdirTemp("", "al-benchmark-skills-")
	if err != nil {
		return nil, result, err
	}
	defer func() { _ = os.RemoveAll(root) }()
	repository, err := runner.Secrets().Resolve(result.Repository)
	if err != nil {
		return nil, result, err
	}
	source, err := gitrepo.OpenSource(ctx, runner, root, repository)
	if err != nil {
		return nil, result, err
	}
	resolved, err := source.Resolve(ctx, ref)
	if err != nil {
		return nil, result, err
	}
	result.ResolvedRef = resolved.Ref
	result.Commit = resolved.Commit
	trees := map[string]skilltree.Tree{}
	for _, selector := range selectors {
		tree, err := source.ReadTree(ctx, resolved.Commit, selector)
		if err != nil {
			return nil, result, err
		}
		info, err := skilltree.ValidateSkill(tree, selector)
		if err != nil {
			return nil, result, err
		}
		trees[info.Name] = tree
		result.Trees[selector] = tree.Hash()
	}
	return trees, result, nil
}

func copyEmbeddedScaffoldFile(source, destination string) error {
	data, err := templates.Read(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o600)
}

func scaffoldConfig(adapter, model, effort string) string {
	agent := adapter
	if agent == adapterClaudeCode {
		agent = providerClaude
	}
	var config strings.Builder
	config.WriteString("[approvals]\nmode = \"yolo\"\n")
	for _, name := range []string{adapterAntigravity, providerClaude, "claude_vscode", adapterCodex, "vscode", "copilot_cli", adapterGrok} {
		fmt.Fprintf(&config, "\n[agents.%s]\nenabled = %t\n", name, name == agent)
		if name != agent {
			continue
		}
		fmt.Fprintf(&config, "model = %q\n", model)
		if name != adapterAntigravity {
			fmt.Fprintf(&config, "reasoning_effort = %q\n", effort)
		}
	}
	config.WriteString("\n[notifications]\nchime = false\n")
	return config.String()
}

func copyScaffoldFile(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("benchmark input %s is not a regular file", source)
	}
	data, err := os.ReadFile(source) // #nosec G304 -- explicit scaffold source.
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o600) // #nosec G703 -- destination is below the validated new scaffold root.
}

func copyScaffoldTree(source, destination string) error {
	info, err := os.Stat(source)
	if os.IsNotExist(err) {
		return os.MkdirAll(destination, 0o700)
	}
	if err != nil {
		return fmt.Errorf("snapshot benchmark input %s: %w", source, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("snapshot benchmark input %s: not a directory", source)
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || strings.HasPrefix(relative, "..") {
			return fmt.Errorf("snapshot benchmark input %s", path)
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("benchmark input contains non-regular file %s", path)
		}
		return copyScaffoldFile(path, target)
	})
}

// validateFrozenSkillsSource binds provenance to the exact frozen plain trees.
func validateFrozenSkillsSource(path, skills string) error {
	_, err := readFrozenSkillsSource(path, skills)
	return err
}

// Read and validate once so copying uses the exact trees bound by provenance.
func readFrozenSkillsSource(path, skills string) (map[string]skilltree.Tree, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the resolved study input snapshot.
	if err != nil {
		return nil, err
	}
	var source SkillSnapshotSource
	if err := json.Unmarshal(data, &source); err != nil {
		return nil, err
	}
	if source.Repository == "" || strings.TrimSpace(source.Repository) != source.Repository || strings.Contains(source.Repository, "${") || skilllock.ValidateRepository(source.Repository) != nil || !gitrepo.IsCommitID(source.Commit) || !validFrozenRef(source.ResolvedRef) || (source.ConfiguredRef != "" && !validFrozenRef(source.ConfiguredRef)) || len(source.Trees) == 0 {
		return nil, fmt.Errorf("invalid external skill source provenance")
	}
	if err := validateSnapshotDirectory(skills); err != nil {
		return nil, err
	}
	expected := map[string]string{}
	for selector := range source.Trees {
		if selector == "" || selector != config.NormalizeSkillSelector(selector) || config.ValidateSkillSelectorPath(selector) != nil || strings.ContainsAny(selector, "*?[!") {
			return nil, fmt.Errorf("invalid exact external skill path %q", selector)
		}
		name := filepath.Base(selector)
		if skilltree.NormalizeName(name) != name || skilltree.IsIgnoredName(name) {
			return nil, fmt.Errorf("invalid external skill name %q", name)
		}
		if _, duplicate := expected[name]; duplicate {
			return nil, fmt.Errorf("duplicate external skill name %s", name)
		}
		expected[name] = selector
	}
	entries, err := os.ReadDir(skills)
	if err != nil {
		return nil, err
	}
	trees := map[string]skilltree.Tree{}
	for _, entry := range entries {
		if skilltree.IsIgnoredName(entry.Name()) {
			continue
		}
		selector, ok := expected[entry.Name()]
		if !ok {
			return nil, fmt.Errorf("external skill snapshot membership differs from provenance: unexpected %s", entry.Name())
		}
		dir := filepath.Join(skills, entry.Name())
		tree, err := skilltree.ReadStrict(skilltree.OSFS{}, dir)
		if err != nil {
			return nil, err
		}
		_, err = skilltree.ValidateSkill(tree, selector)
		if err != nil {
			return nil, err
		}
		if tree.Hash() != source.Trees[selector] {
			return nil, fmt.Errorf("external skill snapshot %s differs from frozen provenance", selector)
		}
		trees[entry.Name()] = tree
	}
	if len(trees) != len(expected) {
		return nil, fmt.Errorf("external skill snapshot membership differs from provenance")
	}
	return trees, nil
}

func validateSnapshotDirectory(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("external skill snapshot %s must be a real directory", dir)
	}
	return nil
}

// Frozen refs are evidence, never resolved against today's remote. Apply Git's
// ref syntax rules offline, also allowing recorded object IDs.
func validFrozenRef(ref string) bool {
	if ref == "" || ref == "@" || strings.HasPrefix(ref, "-") || strings.HasPrefix(ref, "/") || strings.HasSuffix(ref, "/") || strings.HasSuffix(ref, ".") || strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.Contains(ref, "//") {
		return false
	}
	for _, r := range ref {
		if r <= 0x20 || r == 0x7f || strings.ContainsRune("~^:?*[\\", r) {
			return false
		}
	}
	for _, part := range strings.Split(ref, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// Freeze only the view bound by provenance. Inspect all trees before creating
// the skills destination; never traverse or copy root Git/private metadata.
func copyFrozenSkillsSource(provenance, source, destination string) error {
	trees, err := readFrozenSkillsSource(provenance, source)
	if err != nil {
		return err
	}
	for name, tree := range trees {
		if err := skilltree.Materialize(tree, filepath.Join(destination, name)); err != nil {
			return err
		}
	}
	return nil
}
