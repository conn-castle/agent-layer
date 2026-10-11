package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
	"github.com/conn-castle/agent-layer/internal/tomlpatch"
)

const localInstructionTier = ".agent-layer/instructions"

// InstructionsConfig declares one explicit position per local or imported file.
type InstructionsConfig struct {
	Imports []InstructionImport `toml:"imports"`
	Local   []LocalInstruction  `toml:"local"`
}

// InstructionImport adds a required projection order to the shared import policy.
type InstructionImport struct {
	SkillImport
	Order *int `toml:"order"`
}

// LocalInstruction positions one project-owned Markdown file.
type LocalInstruction struct {
	Selectors []string `toml:"selectors"`
	Order     *int     `toml:"order"`
}

// ValidateInstructionSelector requires one literal Markdown file path.
func ValidateInstructionSelector(selector string) error {
	if err := ValidateSkillSelectorPath(selector); err != nil {
		return err
	}
	if strings.ContainsAny(selector, "!*?[") || !strings.HasSuffix(selector, ".md") || strings.HasPrefix(path.Base(selector), ".") {
		return fmt.Errorf("instruction selector must name one exact regular Markdown file; wildcards and exclusions are unsupported")
	}
	return nil
}

func validateInstructions(source string, cfg InstructionsConfig) error {
	orders := map[int]bool{}
	names := map[string]bool{}
	check := func(selectors []string, order *int) error {
		if len(selectors) != 1 {
			return fmt.Errorf("%s: each instructions block requires exactly one selector", source)
		}
		if err := ValidateInstructionSelector(selectors[0]); err != nil {
			return fmt.Errorf("%s: %w", source, err)
		}
		if order == nil || *order < 0 {
			return fmt.Errorf("%s: each instructions block requires a nonnegative integer order (zero is valid)", source)
		}
		if orders[*order] {
			return fmt.Errorf("%s: duplicate instruction order %d", source, *order)
		}
		orders[*order] = true
		name := strings.ToLower(skilltree.NormalizeName(path.Base(selectors[0])))
		if names[name] {
			return fmt.Errorf("%s: duplicate instruction filename %s across local/imported tiers", source, name)
		}
		names[name] = true
		return nil
	}
	for i, imp := range cfg.Imports {
		if err := check(imp.Selectors, imp.Order); err != nil {
			return err
		}
		if err := validateSkillImportBlock(source, i, imp.SkillImport); err != nil {
			return err
		}
	}
	for _, local := range cfg.Local {
		if err := check(local.Selectors, local.Order); err != nil {
			return err
		}
		if path.Base(local.Selectors[0]) != local.Selectors[0] {
			return fmt.Errorf("local instruction selector must be a filename under .agent-layer/instructions")
		}
	}
	return nil
}

// LocalInstructionEntry selects active local Markdown sources; directories and
// auxiliary files retain the legacy reader's ignored-entry behavior.
func LocalInstructionEntry(entry fs.DirEntry) bool {
	return !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") && strings.HasSuffix(entry.Name(), ".md")
}

// UnconfiguredLocalInstruction explains how to establish explicit ownership.
func UnconfiguredLocalInstruction(source string, cfg InstructionsConfig) error {
	if len(cfg.Local)+len(cfg.Imports) == 0 {
		return fmt.Errorf("unconfigured instruction source %s: run al upgrade or al wizard to establish explicit ordering", source)
	}
	return fmt.Errorf("unconfigured local instruction %s: declare it in .agent-layer/config.toml with [[instructions.local]], selectors = [<filename.md>], and a unique order", source)
}

// LoadOrderedInstructionsFS validates completeness before reading any projected output.
func LoadOrderedInstructionsFS(fsys fs.FS, root string, cfg InstructionsConfig) ([]InstructionFile, error) {
	if err := ValidateInstructionRootsFS(fsys, root); err != nil {
		return nil, err
	}
	if err := validateInstructions("instructions", cfg); err != nil {
		return nil, err
	}
	paths := DefaultPaths(root)
	type positioned struct {
		name, dir string
		order     int
	}
	var files []positioned
	localNames, importedNames := map[string]bool{}, map[string]bool{}
	for _, local := range cfg.Local {
		name := local.Selectors[0]
		localNames[name] = true
		files = append(files, positioned{name, paths.InstructionsDir, *local.Order})
	}
	var lock *skilllock.File
	lockData, err := readFileFS(fsys, root, paths.InstructionsLockPath)
	if err == nil {
		lock, err = skilllock.ParseInstructions(lockData, paths.InstructionsLockPath)
	}
	if err != nil && !isNotExist(err) {
		return nil, err
	}
	for _, imp := range cfg.Imports {
		name := path.Base(imp.Selectors[0])
		importedNames[name] = true
		if lock == nil {
			return nil, fmt.Errorf("instruction %s has no lock; run al instructions add or pull", name)
		}
		entry, ok := lock.Entry(name)
		if !ok || entry.Repository != NormalizeSkillRepository(imp.Repository) || entry.Selector != imp.Selectors[0] {
			return nil, fmt.Errorf("instruction %s has no matching lock ownership", name)
		}
		files = append(files, positioned{name, paths.ImportedInstructionsDir, *imp.Order})
	}
	for _, tier := range []struct {
		dir   string
		names map[string]bool
	}{{paths.InstructionsDir, localNames}, {paths.ImportedInstructionsDir, importedNames}} {
		entries, err := readDirFS(fsys, root, tier.dir)
		if err != nil {
			if isNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			if tier.dir == paths.InstructionsDir && !LocalInstructionEntry(entry) && !tier.names[entry.Name()] {
				continue
			}
			if !tier.names[entry.Name()] {
				source := filepath.Join(tier.dir, entry.Name())
				if tier.dir == paths.InstructionsDir {
					return nil, UnconfiguredLocalInstruction(source, cfg)
				}
				return nil, fmt.Errorf("unconfigured imported instruction source %s: restore its [[instructions.imports]] block or remove the unowned file", source)
			}
			if !entry.Type().IsRegular() {
				return nil, fmt.Errorf("instruction %s must be a regular unlinked Markdown file", entry.Name())
			}
		}
	}
	if lock != nil {
		for _, entry := range lock.Skills {
			if !importedNames[entry.Name] {
				return nil, fmt.Errorf("instruction lock entry %s is not configured", entry.Name)
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].order < files[j].order })
	out := make([]InstructionFile, 0, len(files))
	for _, file := range files {
		data, err := readFileFS(fsys, root, filepath.Join(file.dir, file.name))
		if err != nil {
			return nil, fmt.Errorf("missing instructions file %s: %w", filepath.Join(file.dir, file.name), err)
		}
		out = append(out, InstructionFile{Name: file.name, Content: strings.TrimPrefix(string(data), "\ufeff")})
	}
	return out, nil
}
func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

// SetInstructionImport edits one exact-file block using the shared TOML editor.
func SetInstructionImport(content string, block SkillImport, order *int, remove bool) (string, error) {
	if len(block.Selectors) != 1 {
		return "", fmt.Errorf("instruction import requires exactly one selector")
	}
	block.ExactFile = true
	selectors := block.Selectors
	if remove {
		selectors = nil
	}
	return setImportSelectors(content, block.Identity(), selectors, &InstructionImport{SkillImport: block, Order: order})
}

// AddInstructionImport optionally adopts a legacy local file's explicit order.
func AddInstructionImport(content string, block SkillImport, order *int, localName string) (string, error) {
	if localName != "" {
		var err error
		content, order, err = RemoveLocalInstruction(content, localName)
		if err != nil {
			return "", err
		}
		if order == nil {
			return "", fmt.Errorf("legacy %s needs explicit ordering; run al upgrade or al wizard", localName)
		}
	}
	return SetInstructionImport(content, block, order, false)
}

// RemoveLocalInstruction retires only the explicitly configured legacy block.
func RemoveLocalInstruction(content, name string) (string, *int, error) {
	lines := strings.Split(content, "\n")
	for _, span := range findTableBlocks(lines, "instructions.local") {
		var cfg Config
		if err := toml.Unmarshal([]byte(strings.Join(lines[span.start:span.end], "\n")), &cfg); err != nil {
			return "", nil, err
		}
		if len(cfg.Instructions.Local) != 1 {
			return "", nil, fmt.Errorf("invalid local instruction block")
		}
		local := cfg.Instructions.Local[0]
		if len(local.Selectors) == 1 && local.Selectors[0] == name {
			return strings.Join(removeSkillImportBlockLines(lines, span), "\n"), local.Order, nil
		}
	}
	var cfg Config
	if err := toml.Unmarshal([]byte(content), &cfg); err != nil {
		return "", nil, err
	}
	for _, local := range cfg.Instructions.Local {
		if len(local.Selectors) == 1 && local.Selectors[0] == name {
			return "", nil, fmt.Errorf("local instruction requires its own [[instructions.local]] header before editing")
		}
	}
	return content, nil, nil
}

// MigrateInstructionOrderFS is an offline, read-only preview of legacy ordering.
// Callers publish its result only inside their accepted upgrade/wizard transaction.
func MigrateInstructionOrderFS(fsys fs.FS, root, raw string) (string, error) {
	if err := ValidateInstructionRootsFS(fsys, root); err != nil {
		return "", err
	}
	cfg, err := ParseConfigLenient([]byte(raw), "config.toml")
	if err != nil {
		return "", err
	}
	if len(cfg.Instructions.Local)+len(cfg.Instructions.Imports) > 0 {
		return migrateInstructionOrder(raw, nil, cfg.Instructions)
	}
	entries, err := readDirFS(fsys, root, DefaultPaths(root).InstructionsDir)
	if isNotExist(err) {
		return raw, nil
	}
	if err != nil {
		return "", err
	}
	var names []string
	for _, entry := range entries {
		if !LocalInstructionEntry(entry) {
			continue
		}
		if !entry.Type().IsRegular() {
			return "", fmt.Errorf("instruction %s must be a regular unlinked file before migration", entry.Name())
		}
		names = append(names, entry.Name())
	}
	return migrateInstructionOrder(raw, names, cfg.Instructions)
}

// MigrateInstructionOrder records legacy lexical positions without touching content.
func MigrateInstructionOrder(raw string, names []string) (string, error) {
	cfg, err := ParseConfigLenient([]byte(raw), "config.toml")
	if err != nil {
		return "", err
	}
	return migrateInstructionOrder(raw, names, cfg.Instructions)
}

func migrateInstructionOrder(raw string, names []string, cfg InstructionsConfig) (string, error) {
	if len(cfg.Local)+len(cfg.Imports) == 0 {
		sort.Strings(names)
		for i, name := range names {
			order := 10 * i
			cfg.Local = append(cfg.Local, LocalInstruction{Selectors: []string{name}, Order: &order})
			literal, err := tomlpatch.FormatString(name)
			if err != nil {
				return "", err
			}
			raw = strings.TrimRight(raw, "\n") + fmt.Sprintf("\n\n[[instructions.local]]\nselectors = [%s]\norder = %d\n", literal, order)
		}
	}
	if err := validateInstructions("config.toml", cfg); err != nil {
		return "", err
	}
	return raw, nil
}

// ValidateInstructionRootsFS refuses linked instruction sources before writes.
func ValidateInstructionRootsFS(fsys fs.FS, root string) error {
	tree := skillTreeFS{fsys: fsys, root: root}
	for _, dir := range []string{".agent-layer", localInstructionTier, ".agent-layer/instructions-imported"} {
		info, err := tree.Lstat(dir)
		if isNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%s must be a real unlinked directory before instruction writes", filepath.Join(root, dir))
		}
	}
	for _, tier := range []string{localInstructionTier, ".agent-layer/instructions-imported"} {
		entries, err := fs.ReadDir(fsys, tier)
		if isNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			if tier == localInstructionTier && !LocalInstructionEntry(entry) {
				continue
			}
			if entry.Type()&fs.ModeSymlink != 0 || (tier == localInstructionTier && !entry.Type().IsRegular()) {
				return fmt.Errorf("instruction %s/%s must be a regular unlinked file before writes", tier, entry.Name())
			}
		}
	}

	return nil
}
