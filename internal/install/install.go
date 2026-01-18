package install

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nicholasjconn/agent-layer/internal/templates"
)

const (
	gitignoreStart = "# >>> agent-layer"
	gitignoreEnd   = "# <<< agent-layer"
)

const gitignoreHashPrefix = "# Template hash: "

// Options controls installer behavior.
type Options struct {
	Overwrite bool
}

type installer struct {
	root      string
	overwrite bool
	diffs     []string
}

// Run initializes the repository with the required Agent Layer structure.
func Run(root string, opts Options) error {
	if root == "" {
		return fmt.Errorf("root path is required")
	}

	inst := &installer{root: root, overwrite: opts.Overwrite}
	steps := []func() error{
		inst.createDirs,
		inst.writeTemplateFiles,
		inst.writeTemplateDirs,
		inst.updateGitignore,
	}

	if err := runSteps(steps); err != nil {
		return err
	}

	inst.warnDifferences()
	return nil
}

func runSteps(steps []func() error) error {
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

func (inst *installer) createDirs() error {
	root := inst.root
	dirs := []string{
		filepath.Join(root, ".agent-layer", "instructions"),
		filepath.Join(root, ".agent-layer", "slash-commands"),
		filepath.Join(root, ".agent-layer", "templates", "docs"),
		filepath.Join(root, "docs", "agent-layer"),
		filepath.Join(root, "tmp", "agent-layer", "runs"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return nil
}

func (inst *installer) writeTemplateFiles() error {
	root := inst.root
	files := []struct {
		path     string
		template string
		perm     fs.FileMode
	}{
		{filepath.Join(root, ".agent-layer", "config.toml"), "config.toml", 0o644},
		{filepath.Join(root, ".agent-layer", "commands.allow"), "commands.allow", 0o644},
		{filepath.Join(root, ".agent-layer", ".env"), "env", 0o600},
		{filepath.Join(root, ".agent-layer", "gitignore.block"), "gitignore.block", 0o644},
	}
	for _, file := range files {
		if file.template == "gitignore.block" {
			if err := writeGitignoreBlock(file.path, file.template, file.perm, inst.overwrite, inst.recordDiff); err != nil {
				return err
			}
			continue
		}
		if err := writeTemplateFile(file.path, file.template, file.perm, inst.overwrite, inst.recordDiff); err != nil {
			return err
		}
	}
	return nil
}

func (inst *installer) writeTemplateDirs() error {
	root := inst.root
	dirs := []struct {
		templateRoot string
		destRoot     string
	}{
		{"instructions", filepath.Join(root, ".agent-layer", "instructions")},
		{"slash-commands", filepath.Join(root, ".agent-layer", "slash-commands")},
		{"docs/agent-layer", filepath.Join(root, "docs", "agent-layer")},
		{"docs/agent-layer", filepath.Join(root, ".agent-layer", "templates", "docs")},
	}
	for _, dir := range dirs {
		if err := writeTemplateDir(dir.templateRoot, dir.destRoot, inst.overwrite, inst.recordDiff); err != nil {
			return err
		}
	}
	return nil
}

func (inst *installer) updateGitignore() error {
	root := inst.root
	blockPath := filepath.Join(root, ".agent-layer", "gitignore.block")
	blockBytes, err := os.ReadFile(blockPath)
	if err != nil {
		return fmt.Errorf("failed to read gitignore block %s: %w", blockPath, err)
	}
	return ensureGitignore(filepath.Join(root, ".gitignore"), string(blockBytes))
}

func (inst *installer) recordDiff(path string) {
	inst.diffs = append(inst.diffs, path)
}

func (inst *installer) warnDifferences() {
	if inst.overwrite || len(inst.diffs) == 0 {
		return
	}

	sort.Strings(inst.diffs)
	_, _ = fmt.Fprintln(os.Stderr, "Agent Layer install found existing files that differ from the templates:")
	for _, path := range inst.diffs {
		rel, err := filepath.Rel(inst.root, path)
		if err != nil {
			rel = path
		}
		_, _ = fmt.Fprintf(os.Stderr, "  - %s\n", rel)
	}
	_, _ = fmt.Fprintln(os.Stderr, "Re-run `./al install --overwrite` to replace them with template defaults.")
}

func writeTemplateIfMissing(path string, templatePath string, perm fs.FileMode) error {
	return writeTemplateFile(path, templatePath, perm, false, nil)
}

func writeTemplateDir(templateRoot string, destRoot string, overwrite bool, recordDiff func(string)) error {
	return templates.Walk(templateRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, templateRoot+"/")
		if rel == path {
			return fmt.Errorf("unexpected template path %s", path)
		}
		destPath := filepath.Join(destRoot, rel)
		return writeTemplateFile(destPath, path, 0o644, overwrite, recordDiff)
	})
}

func ensureGitignore(path string, block string) error {
	block = normalizeGitignoreBlock(block)
	contentBytes, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	if errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(path, []byte(block), 0o644)
	}

	content := normalizeGitignoreBlock(string(contentBytes))
	updated := updateGitignoreContent(content, block)
	return os.WriteFile(path, []byte(updated), 0o644)
}

func writeGitignoreBlock(path string, templatePath string, perm fs.FileMode, overwrite bool, recordDiff func(string)) error {
	templateBytes, err := templates.Read(templatePath)
	if err != nil {
		return fmt.Errorf("failed to read template %s: %w", templatePath, err)
	}
	templateBlock := normalizeGitignoreBlock(string(templateBytes))
	rendered := renderGitignoreBlock(templateBlock)

	existingBytes, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to read %s: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", path, err)
		}
		if err := os.WriteFile(path, []byte(rendered), perm); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
		return nil
	}

	existing := normalizeGitignoreBlock(string(existingBytes))
	if existing == templateBlock || gitignoreBlockMatchesHash(existing) || overwrite {
		if err := os.WriteFile(path, []byte(rendered), perm); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
		return nil
	}

	if recordDiff != nil {
		recordDiff(path)
	}
	return nil
}

func renderGitignoreBlock(block string) string {
	hashLine := gitignoreHashPrefix + gitignoreBlockHash(block)
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	if len(lines) == 0 {
		return hashLine + "\n"
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[0], hashLine)
	out = append(out, lines[1:]...)
	return strings.Join(out, "\n") + "\n"
}

func normalizeGitignoreBlock(block string) string {
	block = strings.ReplaceAll(block, "\r\n", "\n")
	block = strings.ReplaceAll(block, "\r", "\n")
	return strings.TrimRight(block, "\n") + "\n"
}

func gitignoreBlockHash(block string) string {
	sum := sha256.Sum256([]byte(block))
	return hex.EncodeToString(sum[:])
}

func gitignoreBlockMatchesHash(block string) bool {
	hash, stripped := stripGitignoreHash(block)
	if hash == "" {
		return false
	}
	return gitignoreBlockHash(stripped) == hash
}

func stripGitignoreHash(block string) (string, string) {
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	var hash string
	remaining := make([]string, 0, len(lines))
	for _, line := range lines {
		if hash == "" && strings.HasPrefix(line, gitignoreHashPrefix) {
			hash = strings.TrimSpace(strings.TrimPrefix(line, gitignoreHashPrefix))
			continue
		}
		remaining = append(remaining, line)
	}
	return hash, strings.Join(remaining, "\n") + "\n"
}

func updateGitignoreContent(content string, block string) string {
	lines := splitLines(content)
	blockLines := splitLines(block)

	start, end := findGitignoreBlock(lines)
	if start == -1 || end == -1 || end < start {
		if content == "" {
			return strings.Join(blockLines, "\n") + "\n"
		}
		separator := ""
		if !strings.HasSuffix(content, "\n") {
			separator = "\n"
		}
		return content + separator + strings.Join(blockLines, "\n") + "\n"
	}

	pre := append([]string{}, lines[:start]...)
	post := append([]string{}, lines[end+1:]...)
	post = trimLeadingBlankLines(post)

	updated := append(pre, blockLines...)
	if len(post) > 0 {
		updated = append(updated, "")
		updated = append(updated, post...)
	}

	return strings.Join(updated, "\n") + "\n"
}

func splitLines(input string) []string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	input = strings.TrimRight(input, "\n")
	if input == "" {
		return []string{}
	}
	return strings.Split(input, "\n")
}

func findGitignoreBlock(lines []string) (int, int) {
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == gitignoreStart {
			start = i
			break
		}
	}
	if start == -1 {
		return -1, -1
	}
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == gitignoreEnd {
			return start, i
		}
	}
	return start, -1
}

func trimLeadingBlankLines(lines []string) []string {
	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) != "" {
			break
		}
		i++
	}
	return lines[i:]
}

func writeTemplateFile(path string, templatePath string, perm fs.FileMode, overwrite bool, recordDiff func(string)) error {
	_, err := os.Stat(path)
	if err == nil {
		matches, err := fileMatchesTemplate(path, templatePath)
		if err != nil {
			return err
		}
		if matches {
			return nil
		}
		if !overwrite {
			if recordDiff != nil {
				recordDiff(path)
			}
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}

	data, err := templates.Read(templatePath)
	if err != nil {
		return fmt.Errorf("failed to read template %s: %w", templatePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

func fileMatchesTemplate(path string, templatePath string) (bool, error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("failed to read %s: %w", path, err)
	}
	template, err := templates.Read(templatePath)
	if err != nil {
		return false, fmt.Errorf("failed to read template %s: %w", templatePath, err)
	}
	return normalizeTemplateContent(string(existing)) == normalizeTemplateContent(string(template)), nil
}

func normalizeTemplateContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return strings.TrimRight(content, "\n") + "\n"
}
