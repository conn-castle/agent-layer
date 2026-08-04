package skillimports

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/tomlpatch"
)

// skillImportsTableName is the array-of-tables name for one import block.
const skillImportsTableName = "skills.imports"

// selectorsKey is the key holding a block's selectors.
const selectorsKey = "selectors"

// singleLineSelectorsBudget is the rendered width above which a rewritten
// selectors array is split one element per line.
const singleLineSelectorsBudget = 100

// blockSpan is the inclusive line range of one `[[skills.imports]]` block,
// including its header line.
type blockSpan struct {
	start int
	end   int
}

// skillImportBlockSpans locates every `[[skills.imports]]` block in document
// order. It mirrors the tokenizer used by the shared TOML patch helpers, so a
// header-looking line inside a multiline string is correctly treated as string
// content rather than a block boundary.
func skillImportBlockSpans(lines []string) []blockSpan {
	var spans []blockSpan
	state := tomlpatch.StateNone
	current := -1
	for i, line := range lines {
		if tomlpatch.StateInMultiline(state) {
			_, state = tomlpatch.ScanLineForComment(line, state)
			continue
		}
		name, isArray, ok := tomlpatch.ParseHeader(line)
		if ok {
			if current >= 0 {
				spans = append(spans, blockSpan{start: current, end: i - 1})
				current = -1
			}
			if isArray && name == skillImportsTableName {
				current = i
			}
		}
		_, state = tomlpatch.ScanLineForComment(line, state)
	}
	if current >= 0 {
		spans = append(spans, blockSpan{start: current, end: len(lines) - 1})
	}
	return spans
}

// RenderImportBlock renders a new `[[skills.imports]]` block. Only fields the
// user actually chose are written: an omitted ref, tracking mode, or write
// policy stays omitted so the documented defaults keep applying.
func RenderImportBlock(imp config.SkillImport) []string {
	lines := []string{"[[" + skillImportsTableName + "]]"}
	lines = append(lines, "repository = "+strconv.Quote(config.NormalizeSkillRepository(imp.Repository)))
	lines = append(lines, renderSelectorsArray("", imp.Selectors, "")...)
	if ref := strings.TrimSpace(imp.Ref); ref != "" {
		lines = append(lines, "ref = "+strconv.Quote(ref))
	}
	if tracking := strings.TrimSpace(imp.Tracking); tracking != "" {
		lines = append(lines, "tracking = "+strconv.Quote(tracking))
	}
	if write := strings.TrimSpace(imp.Write); write != "" {
		lines = append(lines, "write = "+strconv.Quote(write))
	}
	if push := config.NormalizeSkillRepository(imp.PushRepository); push != "" {
		lines = append(lines, "push_repository = "+strconv.Quote(push))
	}
	if branch := strings.TrimSpace(imp.PushBranch); branch != "" {
		lines = append(lines, "push_branch = "+strconv.Quote(branch))
	}
	return lines
}

// AppendImportBlock appends a rendered block to the end of a config document,
// leaving every existing line untouched.
func AppendImportBlock(content string, imp config.SkillImport) string {
	lines := splitLines(content)
	trailingNewline := strings.HasSuffix(content, "\n")
	lines = trimTrailingBlankLines(lines)
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	lines = append(lines, RenderImportBlock(imp)...)
	return joinDocument(lines, trailingNewline)
}

// RemoveImportBlock deletes one `[[skills.imports]]` block, identified by its
// position among import blocks, and the blank line that separated it.
func RemoveImportBlock(content string, blockIndex int) (string, error) {
	lines := splitLines(content)
	spans := skillImportBlockSpans(lines)
	if blockIndex < 0 || blockIndex >= len(spans) {
		return "", fmt.Errorf("skills.imports block %d is not present in the configuration", blockIndex)
	}
	span := spans[blockIndex]
	start := span.start
	// Absorb the blank separator lines directly above the block so removing a
	// block does not leave a growing gap behind.
	for start > 0 && strings.TrimSpace(lines[start-1]) == "" {
		start--
	}
	kept := append([]string(nil), lines[:start]...)
	kept = append(kept, lines[span.end+1:]...)
	return joinDocument(kept, strings.HasSuffix(content, "\n")), nil
}

// AddSelectorToBlock adds one selector to an existing block. A multi-line
// selectors array gains one line and keeps every other line, including interior
// comments, exactly as written.
func AddSelectorToBlock(content string, blockIndex int, selector string) (string, error) {
	return editSelectors(content, blockIndex, func(existing []string) ([]string, error) {
		for _, current := range existing {
			if current == selector {
				return nil, fmt.Errorf("selector %q is already configured in this block", selector)
			}
		}
		return append(append([]string(nil), existing...), selector), nil
	})
}

// RemoveSelectorFromBlock removes exactly one selector from a block.
func RemoveSelectorFromBlock(content string, blockIndex int, selector string) (string, error) {
	return editSelectors(content, blockIndex, func(existing []string) ([]string, error) {
		out := make([]string, 0, len(existing))
		removed := false
		for _, current := range existing {
			if !removed && current == selector {
				removed = true
				continue
			}
			out = append(out, current)
		}
		if !removed {
			return nil, fmt.Errorf("selector %q is not configured in this block", selector)
		}
		return out, nil
	})
}

// editSelectors applies a transformation to one block's selectors array.
func editSelectors(content string, blockIndex int, transform func([]string) ([]string, error)) (string, error) {
	lines := splitLines(content)
	spans := skillImportBlockSpans(lines)
	if blockIndex < 0 || blockIndex >= len(spans) {
		return "", fmt.Errorf("skills.imports block %d is not present in the configuration", blockIndex)
	}
	span := spans[blockIndex]
	blockLines := lines[span.start : span.end+1]

	keyOffset := -1
	tomlpatch.WalkLinesOutsideMultiline(blockLines, func(i int, line string, state tomlpatch.StringState) tomlpatch.LineWalkResult {
		if _, ok := tomlpatch.ParseKeyLineWithState(line, selectorsKey, state); ok {
			keyOffset = i
			return tomlpatch.LineWalkResult{Stop: true}
		}
		return tomlpatch.LineWalkResult{}
	})
	if keyOffset < 0 {
		return "", fmt.Errorf("skills.imports block %d has no %s key", blockIndex, selectorsKey)
	}
	endOffset := tomlpatch.MultilineValueEndIndex(blockLines, keyOffset)
	valueLines := blockLines[keyOffset : endOffset+1]

	existing, err := parseSelectorsArray(valueLines)
	if err != nil {
		return "", fmt.Errorf("skills.imports block %d: %w", blockIndex, err)
	}
	updated, err := transform(existing)
	if err != nil {
		return "", err
	}

	indent := leadingWhitespace(valueLines[0])
	inlineComment := ""
	if len(valueLines) == 1 {
		inlineComment = tomlpatch.ExtractInlineCommentWithState(strings.TrimLeft(valueLines[0], " \t"), tomlpatch.StateNone)
	}
	replacement := renderSelectorsArray(indent, updated, inlineComment)

	out := append([]string(nil), lines[:span.start+keyOffset]...)
	out = append(out, replacement...)
	out = append(out, lines[span.start+endOffset+1:]...)
	return joinDocument(out, strings.HasSuffix(content, "\n")), nil
}

// parseSelectorsArray extracts the string elements of a selectors array from its
// source lines. Only string elements are accepted; anything else is a
// configuration error rather than something to rewrite blindly.
func parseSelectorsArray(valueLines []string) ([]string, error) {
	joined := strings.Join(valueLines, "\n")
	open := strings.Index(joined, "[")
	if open < 0 {
		return nil, fmt.Errorf("%s must be an array", selectorsKey)
	}
	body := joined[open:]
	var parsed struct {
		Selectors []string `toml:"selectors"`
	}
	// Decoding the isolated assignment keeps array parsing (nested quotes,
	// escapes, trailing commas, interior comments) in the TOML library rather
	// than in a hand-rolled scanner.
	if err := toml.Unmarshal([]byte(selectorsKey+" = "+body), &parsed); err != nil {
		return nil, fmt.Errorf("%s must be an array of strings: %w", selectorsKey, err)
	}
	return parsed.Selectors, nil
}

// renderSelectorsArray renders a selectors array, choosing a single line when
// the result is short and one element per line otherwise.
func renderSelectorsArray(indent string, selectors []string, inlineComment string) []string {
	quoted := make([]string, 0, len(selectors))
	for _, selector := range selectors {
		quoted = append(quoted, strconv.Quote(selector))
	}
	single := indent + selectorsKey + " = [" + strings.Join(quoted, ", ") + "]"
	if inlineComment != "" {
		single += " " + inlineComment
	}
	if len(single) <= singleLineSelectorsBudget {
		return []string{single}
	}
	lines := []string{indent + selectorsKey + " = ["}
	for _, value := range quoted {
		lines = append(lines, indent+"  "+value+",")
	}
	closing := indent + "]"
	if inlineComment != "" {
		closing += " " + inlineComment
	}
	return append(lines, closing)
}

func leadingWhitespace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

func splitLines(content string) []string {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func joinDocument(lines []string, trailingNewline bool) string {
	joined := strings.Join(lines, "\n")
	if trailingNewline && joined != "" {
		joined += "\n"
	}
	return joined
}

func trimTrailingBlankLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}
