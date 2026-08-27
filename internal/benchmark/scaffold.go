package benchmark

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
	destination, err := filepath.Abs(options.Directory)
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
		{filepath.Join(options.RepoRoot, ".agent-layer", "instructions"), filepath.Join(destination, "treatment", "instructions")},
		{filepath.Join(options.RepoRoot, ".agents", "skills"), filepath.Join(destination, "treatment", "skills")},
	} {
		if err := copyScaffoldTree(item.source, item.target); err != nil {
			return "", err
		}
	}
	config := scaffoldConfig(model.Adapter, modelNameForPublished(model.PublishedIdentifier), effort)
	if err := os.WriteFile(filepath.Join(destination, "treatment", "config.toml"), []byte(config), 0o600); err != nil {
		return "", err
	}
	prompt := "Complete the following task using the available Agent Layer instructions and skills.\n\n{{task}}\n"
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
instructions = "treatment/instructions"
skills = "treatment/skills"
entry_prompt = "treatment/prompt.md"
required_dispatch_roles = []
`, modelNameForPublished(model.PublishedIdentifier), effort, modelNameForPublished(model.PublishedIdentifier), effort)
	path := filepath.Join(destination, "study.toml")
	if err := os.WriteFile(path, []byte(study), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func scaffoldConfig(adapter, model, effort string) string {
	agent := adapter
	if agent == adapterClaudeCode {
		agent = "claude"
	}
	return fmt.Sprintf("[approvals]\nmode = \"yolo\"\n\n[agents.%s]\nenabled = true\nmodel = %q\nreasoning_effort = %q\n\n[notifications]\nchime = false\n", agent, model, effort)
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
	if err != nil || !info.IsDir() {
		return fmt.Errorf("snapshot benchmark input %s: %w", source, err)
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
