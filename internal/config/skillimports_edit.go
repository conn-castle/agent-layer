package config

import (
	"fmt"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/tomlpatch"
)

// skillImportsTableName is the array-of-tables name that holds import blocks.
const skillImportsTableName = "skills.imports"

// skillImportBlockSpan locates one array table. Import scans also decode policy.
type skillImportBlockSpan struct {
	start  int // index of the array-table header line
	end    int // exclusive index of the first line after the block
	parsed SkillImport
}

// SetSkillImportSelectors returns config TOML content whose `[[skills.imports]]`
// block matching identity declares exactly selectors, preserving every
// unrelated line, comment, and formatting choice.
//
// An empty selectors slice removes the matching block. A non-empty slice with
// no matching block appends a new block built from identity. Selector order is
// preserved as given so callers control the recorded configuration order.
// A matching import declared without its own `[[skills.imports]]` header is
// rejected rather than edited.
func SetSkillImportSelectors(content string, identity SkillImportBlockIdentity, selectors []string) (string, error) {
	return setImportSelectors(content, identity, selectors, nil)
}

// setImportSelectors shares comment-preserving edits between skills and instructions.
// Instructions declare one file per block and reject additions to an existing block.
func setImportSelectors(content string, identity SkillImportBlockIdentity, selectors []string, instruction *InstructionImport) (string, error) {
	table := skillImportsTableName
	if instruction != nil {
		table = "instructions.imports"
	}
	lines := strings.Split(content, "\n")
	spans, err := findImportBlocks(lines, table)
	if err != nil {
		return "", err
	}
	for _, span := range spans {
		if span.parsed.Identity() != identity {
			continue
		}
		if len(selectors) == 0 {
			return strings.Join(removeSkillImportBlockLines(lines, span), "\n"), nil
		}
		if instruction != nil {
			return "", fmt.Errorf("instruction %s is already configured", identity.FileSelector)
		}
		replaced, err := replaceSelectorsInBlock(lines, span, selectors)
		if err != nil {
			return "", err
		}
		return strings.Join(replaced, "\n"), nil
	}
	var cfg Config
	if err := toml.Unmarshal([]byte(content), &cfg); err != nil {
		return "", fmt.Errorf(messages.ConfigSkillImportsDocumentUnparsableFmt, err)
	}
	for _, imp := range importBlocks(cfg, table) {
		if imp.Identity() == identity {
			return "", fmt.Errorf("import requires its own [[%s]] header before editing", table)
		}
	}
	if len(selectors) == 0 {
		return content, nil
	}
	if instruction != nil {
		if instruction.Order == nil {
			return "", fmt.Errorf("instruction add requires --order")
		}
		cfg.Instructions.Imports = append(cfg.Instructions.Imports, *instruction)
		if err := validateInstructions("config.toml", cfg.Instructions); err != nil {
			return "", err
		}
	}
	block, err := renderSkillImportBlock(identity, selectors)
	if err != nil {
		return "", err
	}
	if instruction != nil {
		block[0] = "[[" + table + "]]"
		block = append(block, fmt.Sprintf("order = %d", *instruction.Order))
	}
	return appendImportBlock(content, block), nil
}

func importBlocks(cfg Config, table string) []SkillImport {
	if table == skillImportsTableName {
		return cfg.Skills.Imports
	}
	imports := make([]SkillImport, 0, len(cfg.Instructions.Imports))
	for _, imp := range cfg.Instructions.Imports {
		imp.ExactFile = true
		imports = append(imports, imp.SkillImport)
	}
	return imports
}

// findTableBlocks locates visible array tables, ignoring headers inside strings.
// Every table header closes the preceding block, including quoted headers.
func findTableBlocks(lines []string, table string) []skillImportBlockSpan {
	var spans []skillImportBlockSpan
	tomlpatch.WalkLinesOutsideMultiline(lines, func(i int, line string, _ tomlpatch.StringState) tomlpatch.LineWalkResult {
		if name, isArray, ok := tomlpatch.ParseHeader(line); ok {
			if len(spans) > 0 && spans[len(spans)-1].end == len(lines) {
				spans[len(spans)-1].end = i
			}
			if isArray && name == table {
				spans = append(spans, skillImportBlockSpan{start: i, end: len(lines)})
			}
		}
		return tomlpatch.LineWalkResult{}
	})
	return spans
}

// findImportBlocks decodes each visible import with the configuration schema.
func findImportBlocks(lines []string, table string) ([]skillImportBlockSpan, error) {
	spans := findTableBlocks(lines, table)
	for i, span := range spans {
		var cfg Config
		if err := toml.Unmarshal([]byte(strings.Join(lines[span.start:span.end], "\n")), &cfg); err != nil {
			return nil, fmt.Errorf(messages.ConfigSkillImportBlockUnparsableFmt, err)
		}
		imports := importBlocks(cfg, table)
		if len(imports) != 1 {
			return nil, fmt.Errorf("expected one %s block, found %d", table, len(imports))
		}
		spans[i].parsed = imports[0]
	}
	return spans, nil
}

// removeSkillImportBlockLines drops a block and the blank separator lines that
// immediately precede it, so removing a block never leaves a growing run of
// blank lines behind.
func removeSkillImportBlockLines(lines []string, span skillImportBlockSpan) []string {
	start := span.start
	for start > 0 && strings.TrimSpace(lines[start-1]) == "" {
		start--
	}
	if start == 0 {
		// Keep the document from starting with the blank lines that trailed the
		// removed block.
		start = span.start
	}
	out := make([]string, 0, len(lines))
	out = append(out, lines[:start]...)
	out = append(out, lines[span.end:]...)
	// Splitting on "\n" represents a document's final newline as a trailing
	// empty element. Removing the block that ends the document consumes it, so
	// it is restored to keep the rewritten file's final newline.
	if span.end == len(lines) && lines[len(lines)-1] == "" {
		out = append(out, "")
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// replaceSelectorsInBlock rewrites the block's `selectors` assignment in place.
func replaceSelectorsInBlock(lines []string, span skillImportBlockSpan, selectors []string) ([]string, error) {
	keyStart, keyEnd, indent, err := findSelectorsAssignment(lines, span)
	if err != nil {
		return nil, err
	}
	rendered, err := renderSelectorsAssignment(indent, selectors)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(lines)+len(rendered))
	out = append(out, lines[:keyStart]...)
	out = append(out, rendered...)
	out = append(out, lines[keyEnd:]...)
	return out, nil
}

// findSelectorsAssignment returns the half-open line range covering the block's
// `selectors = [...]` assignment along with its indentation.
func findSelectorsAssignment(lines []string, span skillImportBlockSpan) (start int, end int, indent string, err error) {
	state := tomlpatch.StateNone
	for i := span.start; i < span.end; i++ {
		line := lines[i]
		if tomlpatch.StateInMultiline(state) {
			_, state = tomlpatch.ScanLineForComment(line, state)
			continue
		}
		if _, _, ok := tomlpatch.ParseKeyValueWithState(line, "selectors", state); ok {
			indent = line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			end, err := selectorsArrayEndIndex(lines, i, span.end)
			if err != nil {
				return 0, 0, "", err
			}
			return i, end, indent, nil
		}
		_, state = tomlpatch.ScanLineForComment(line, state)
	}
	return 0, 0, "", fmt.Errorf(messages.ConfigSkillImportSelectorsAssignmentMissing)
}

// selectorsArrayEndIndex returns the exclusive line index that closes the array
// literal beginning on line startIdx, counting only brackets outside strings
// and comments.
func selectorsArrayEndIndex(lines []string, startIdx int, limit int) (int, error) {
	depth := 0
	for i := startIdx; i < limit; i++ {
		line := lines[i]
		commentPos, _ := tomlpatch.ScanLineForComment(line, tomlpatch.StateNone)
		scan := line
		if commentPos >= 0 {
			scan = line[:commentPos]
		}
		inBasic := false
		inLiteral := false
		for pos := 0; pos < len(scan); pos++ {
			ch := scan[pos]
			switch {
			case inBasic:
				if ch == '\\' {
					pos++
					continue
				}
				if ch == '"' {
					inBasic = false
				}
			case inLiteral:
				if ch == '\'' {
					inLiteral = false
				}
			case ch == '"':
				inBasic = true
			case ch == '\'':
				inLiteral = true
			case ch == '[':
				depth++
			case ch == ']':
				depth--
			}
		}
		if depth <= 0 {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf(messages.ConfigSkillImportSelectorsAssignmentUnterminated)
}

// renderSelectorsAssignment renders a deterministic multi-line selectors array.
func renderSelectorsAssignment(indent string, selectors []string) ([]string, error) {
	out := make([]string, 0, len(selectors)+2)
	out = append(out, indent+"selectors = [")
	for _, selector := range selectors {
		literal, err := tomlpatch.FormatString(selector)
		if err != nil {
			return nil, fmt.Errorf("selector %q cannot be written to config.toml: %w", selector, err)
		}
		out = append(out, indent+"  "+literal+",")
	}
	out = append(out, indent+"]")
	return out, nil
}

// appendImportBlock keeps the document's final-newline convention.
func appendImportBlock(content string, block []string) string {
	hadFinalNewline := strings.HasSuffix(content, "\n")
	trimmed := strings.TrimRight(content, "\n")
	suffix := ""
	if hadFinalNewline {
		suffix = "\n"
	}
	if trimmed == "" {
		return strings.Join(block, "\n") + suffix
	}
	return trimmed + "\n\n" + strings.Join(block, "\n") + suffix
}

// renderSkillImportBlock renders a new `[[skills.imports]]` block, omitting
// every optional field the caller left at its default.
func renderSkillImportBlock(identity SkillImportBlockIdentity, selectors []string) ([]string, error) {
	repository, err := tomlpatch.FormatString(identity.Repository)
	if err != nil {
		return nil, fmt.Errorf("repository cannot be written to config.toml: %w", err)
	}
	lines := []string{
		"[[" + skillImportsTableName + "]]",
		"repository = " + repository,
	}
	renderedSelectors, err := renderSelectorsAssignment("", selectors)
	if err != nil {
		return nil, err
	}
	lines = append(lines, renderedSelectors...)
	appendOptional := func(key string, value string) error {
		if strings.TrimSpace(value) == "" {
			return nil
		}
		literal, err := tomlpatch.FormatString(value)
		if err != nil {
			return fmt.Errorf("%s cannot be written to config.toml: %w", key, err)
		}
		lines = append(lines, key+" = "+literal)
		return nil
	}
	if err := appendOptional("ref", identity.Ref); err != nil {
		return nil, err
	}
	if err := appendOptional("tracking", identity.Tracking); err != nil {
		return nil, err
	}
	if identity.WritePolicy != SkillWritePolicyNone {
		if err := appendOptional("write_policy", identity.WritePolicy); err != nil {
			return nil, err
		}
	}
	if NormalizeSkillRepository(identity.PushRepository) != NormalizeSkillRepository(identity.Repository) {
		if err := appendOptional("push_repository", identity.PushRepository); err != nil {
			return nil, err
		}
	}
	if err := appendOptional("push_branch", identity.PushBranch); err != nil {
		return nil, err
	}
	return lines, nil
}
