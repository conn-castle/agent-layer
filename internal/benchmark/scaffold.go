package benchmark

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/templates"
)

// InitStudyOptions configures a self-contained benchmark study scaffold.
type InitStudyOptions struct {
	RepoRoot      string
	SelectionPath string
	Directory     string
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
	if err := copyEmbeddedScaffoldTree("skills", filepath.Join(destination, "treatment", "official-skills")); err != nil {
		return "", fmt.Errorf("snapshot official Agent Layer skills: %w", err)
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
entry_prompt = "treatment/prompt.md"
required_dispatch_roles = ["plan-reviewer", "implementer", "code-reviewer"]
`, modelNameForPublished(model.PublishedIdentifier), effort, modelNameForPublished(model.PublishedIdentifier), effort)
	path := filepath.Join(destination, "study.toml")
	if err := os.WriteFile(path, []byte(study), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func copyEmbeddedScaffoldTree(source, destination string) error {
	found := false
	err := templates.Walk(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || strings.HasPrefix(relative, "..") {
			return fmt.Errorf("snapshot embedded benchmark input %s", path)
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("embedded benchmark input contains non-regular file %s", path)
		}
		data, err := templates.Read(path)
		if err != nil {
			return err
		}
		found = true
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("embedded benchmark input %s is empty", source)
	}
	return nil
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
