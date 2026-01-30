package sync

import (
	"errors"
	"strings"
	"testing"
)

func TestRenderVSCodeSettingsContentPreservesBOMAndNewlines(t *testing.T) {
	t.Parallel()
	settings := &vscodeSettings{
		ChatToolsTerminalAutoApprove: OrderedMap[bool]{"/^git(\\b.*)?$/": true},
	}
	existing := "\ufeff\r\n"

	updated, err := renderVSCodeSettingsContent(RealSystem{}, existing, settings)
	if err != nil {
		t.Fatalf("renderVSCodeSettingsContent error: %v", err)
	}
	if !strings.HasPrefix(updated, "\ufeff") {
		t.Fatalf("expected BOM to be preserved")
	}
	for i := 0; i < len(updated); i++ {
		if updated[i] == '\n' && (i == 0 || updated[i-1] != '\r') {
			t.Fatalf("expected CRLF newlines only")
		}
	}
}

func TestRenderVSCodeSettingsContentEmpty(t *testing.T) {
	t.Parallel()
	settings := &vscodeSettings{}
	updated, err := renderVSCodeSettingsContent(RealSystem{}, " \n", settings)
	if err != nil {
		t.Fatalf("renderVSCodeSettingsContent error: %v", err)
	}
	if !strings.HasPrefix(updated, "{") {
		t.Fatalf("expected output to start with root object")
	}
	if !strings.Contains(updated, vscodeSettingsManagedStart) {
		t.Fatalf("expected managed block markers")
	}
	if strings.Contains(updated, "chat.tools.terminal.autoApprove") {
		t.Fatalf("unexpected settings content in empty settings")
	}
}

func TestRenderVSCodeSettingsContentReplaceManagedBlockFallbackIndent(t *testing.T) {
	t.Parallel()
	existing := "{\n// >>> agent-layer\n// Managed by Agent Layer. To customize, edit .agent-layer/config.toml\n// and .agent-layer/commands.allow, then re-run `al sync`.\n//\n\"chat.tools.terminal.autoApprove\": {\n  \"/^old(\\\\b.*)?$/\": true\n}\n// <<< agent-layer\n}\n"
	settings := &vscodeSettings{
		ChatToolsTerminalAutoApprove: OrderedMap[bool]{"/^git(\\b.*)?$/": true},
	}

	updated, err := renderVSCodeSettingsContent(RealSystem{}, existing, settings)
	if err != nil {
		t.Fatalf("renderVSCodeSettingsContent error: %v", err)
	}
	if !strings.Contains(updated, "\n  // >>> agent-layer") {
		t.Fatalf("expected fallback indent for managed block")
	}
	if strings.Contains(updated, "},\n  // <<< agent-layer") {
		t.Fatalf("expected no trailing comma when block is last")
	}
}

func TestRenderVSCodeSettingsContentInsertBlockComplexJSONC(t *testing.T) {
	t.Parallel()
	existing := "{\n  // line comment\n  \"path\": \"C:\\tmp\\\"{\\\"}\",\n  /* block comment { } */\n  \"nested\": {\"inner\": \"value\"}\n}\n"
	settings := &vscodeSettings{
		ChatToolsTerminalAutoApprove: OrderedMap[bool]{"/^git(\\b.*)?$/": true},
	}

	updated, err := renderVSCodeSettingsContent(RealSystem{}, existing, settings)
	if err != nil {
		t.Fatalf("renderVSCodeSettingsContent error: %v", err)
	}
	idxBlock := strings.Index(updated, vscodeSettingsManagedStart)
	idxPath := strings.Index(updated, "\"path\":")
	if idxBlock == -1 || idxPath == -1 || idxBlock > idxPath {
		t.Fatalf("expected managed block to be inserted before existing fields")
	}
	if !strings.Contains(updated, "\"nested\"") {
		t.Fatalf("expected existing fields to be preserved")
	}
}

func TestRenderVSCodeSettingsContentManagedBlockMalformed(t *testing.T) {
	t.Parallel()
	existing := "{\n  // >>> agent-layer\n  \"editor.tabSize\": 2\n}\n"
	settings := &vscodeSettings{}
	if _, err := renderVSCodeSettingsContent(RealSystem{}, existing, settings); err == nil {
		t.Fatalf("expected error for malformed managed block")
	}
}

func TestDetectNormalizeNewlines(t *testing.T) {
	t.Parallel()
	content := "a\r\nb\r\n"
	if detectNewline(content) != "\r\n" {
		t.Fatalf("expected CRLF newline detection")
	}
	if detectNewline("a\rb") != "\r" {
		t.Fatalf("expected CR newline detection")
	}
	if detectNewline("a\nb") != "\n" {
		t.Fatalf("expected LF newline detection")
	}
	normalized := normalizeNewlines(content)
	if normalized != "a\nb\n" {
		t.Fatalf("unexpected normalized content: %q", normalized)
	}
	applied := applyNewlineStyle(normalized, "\r")
	if applied != "a\rb\r" {
		t.Fatalf("unexpected newline application: %q", applied)
	}
	if applyNewlineStyle(normalized, "\n") != normalized {
		t.Fatalf("expected newline style to remain unchanged for LF")
	}
}

func TestStripUTF8BOM(t *testing.T) {
	t.Parallel()
	bom, stripped := stripUTF8BOM("\ufeff{}")
	if bom != "\ufeff" || stripped != "{}" {
		t.Fatalf("unexpected bom handling: %q %q", bom, stripped)
	}
	bom, stripped = stripUTF8BOM("{}")
	if bom != "" || stripped != "{}" {
		t.Fatalf("unexpected bom handling: %q %q", bom, stripped)
	}
}

func TestFindJSONCRootBoundsErrors(t *testing.T) {
	t.Parallel()
	if _, _, err := findJSONCRootBounds("// comment\n"); err == nil {
		t.Fatalf("expected error for missing root object")
	}
	if _, _, err := findJSONCRootBounds("{\n  \"a\": 1\n"); err == nil {
		t.Fatalf("expected error for unterminated root object")
	}
}

func TestFindJSONCRootBoundsWithCommentsAndStrings(t *testing.T) {
	t.Parallel()
	content := "// leading comment\n{\n  \"value\": \"brace { inside } and escaped \\\\\"quote\\\\\"\",\n  /* block { comment } */\n  \"nested\": {\"inner\": \"x\"}\n}\n"
	start, end, err := findJSONCRootBounds(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start < 0 || end <= start {
		t.Fatalf("unexpected bounds: %d-%d", start, end)
	}
}

func TestIndexToLineColAndWhitespaceHelpers(t *testing.T) {
	t.Parallel()
	content := "a\nbc"
	line, col := indexToLineCol(content, 0)
	if line != 0 || col != 0 {
		t.Fatalf("unexpected line/col for idx 0: %d/%d", line, col)
	}
	line, col = indexToLineCol(content, 2)
	if line != 1 || col != 0 {
		t.Fatalf("unexpected line/col for idx 2: %d/%d", line, col)
	}
	if leadingWhitespace(" \tvalue") != " \t" {
		t.Fatalf("expected leading whitespace to be preserved")
	}
	if leadingWhitespace("value") != "" {
		t.Fatalf("expected no leading whitespace")
	}
}

func TestIndexToLineColSingleLine(t *testing.T) {
	t.Parallel()
	line, col := indexToLineCol("abc", 2)
	if line != 0 || col != 2 {
		t.Fatalf("unexpected line/col for single line: %d/%d", line, col)
	}
}

func TestFindVSCodeManagedBlockErrors(t *testing.T) {
	t.Parallel()
	lines := []string{
		"// >>> agent-layer",
		"// >>> agent-layer",
		"// <<< agent-layer",
	}
	if _, _, _, _, err := findVSCodeManagedBlock(lines, 0, len(lines)-1); err == nil {
		t.Fatalf("expected duplicate start error")
	}

	lines = []string{
		"// >>> agent-layer",
		"// <<< agent-layer",
		"// <<< agent-layer",
	}
	if _, _, _, _, err := findVSCodeManagedBlock(lines, 0, len(lines)-1); err == nil {
		t.Fatalf("expected duplicate end error")
	}

	lines = []string{
		"// >>> agent-layer",
	}
	if _, _, _, _, err := findVSCodeManagedBlock(lines, 0, len(lines)-1); err == nil {
		t.Fatalf("expected incomplete markers error")
	}

	lines = []string{
		"// <<< agent-layer",
		"// >>> agent-layer",
	}
	if _, _, _, _, err := findVSCodeManagedBlock(lines, 0, len(lines)-1); err == nil {
		t.Fatalf("expected end-before-start error")
	}

	lines = []string{
		"// >>> agent-layer",
		"// <<< agent-layer",
	}
	if _, _, _, _, err := findVSCodeManagedBlock(lines, 1, 1); err == nil {
		t.Fatalf("expected outside scan range error")
	}
}

func TestFindVSCodeManagedBlockNotFound(t *testing.T) {
	t.Parallel()
	lines := []string{"{", "  \"editor.tabSize\": 2", "}"}
	_, _, _, found, err := findVSCodeManagedBlock(lines, 0, len(lines)-1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatalf("expected managed block to be absent")
	}
}

func TestDetectVSCodeIndentSkipsComments(t *testing.T) {
	t.Parallel()
	lines := []string{
		"{",
		"  // comment",
		"  \"editor.tabSize\": 2",
		"}",
	}
	indent := detectVSCodeIndent(lines, 0, len(lines)-1)
	if indent != "  " {
		t.Fatalf("expected two-space indent, got %q", indent)
	}
}

func TestDetectVSCodeIndentEmpty(t *testing.T) {
	t.Parallel()
	lines := []string{
		"{",
		"  // comment",
		"  * continuation",
		"}",
	}
	indent := detectVSCodeIndent(lines, 0, len(lines)-1)
	if indent != "" {
		t.Fatalf("expected empty indent, got %q", indent)
	}
}

func TestDetectVSCodeIndentBounds(t *testing.T) {
	t.Parallel()
	lines := []string{
		"{",
		"\t\"editor.tabSize\": 2",
		"}",
	}
	indent := detectVSCodeIndent(lines, -5, 99)
	if indent != "\t" {
		t.Fatalf("expected tab indent, got %q", indent)
	}
}

func TestHasJSONCContentBetween(t *testing.T) {
	t.Parallel()
	lines := []string{
		"{",
		"  // comment",
		"}",
	}
	if hasJSONCContentBetween(lines, 0, 1, 2, 0) {
		t.Fatalf("expected no content between braces")
	}

	lines = []string{
		"{",
		"  \"editor.tabSize\": 2",
		"}",
	}
	if !hasJSONCContentBetween(lines, 0, 1, 2, 0) {
		t.Fatalf("expected content between braces")
	}

	lines = []string{
		"{",
		"  \"editor.wordWrap\": \"before } after\"",
		"}",
	}
	if !hasJSONCContentBetween(lines, 0, 1, 2, 0) {
		t.Fatalf("expected string content to be ignored for closing brace detection")
	}
}

func TestHasJSONCContentBetweenBlockComment(t *testing.T) {
	t.Parallel()
	lines := []string{
		"{",
		"  /* comment */",
		"}",
	}
	if hasJSONCContentBetween(lines, 0, 1, 2, 0) {
		t.Fatalf("expected block comments to be ignored")
	}
}

func TestHasJSONCContentBetweenBounds(t *testing.T) {
	t.Parallel()
	lines := []string{"{", "}"}
	if hasJSONCContentBetween(lines, 1, 0, 0, 0) {
		t.Fatalf("expected false when endLine < startLine")
	}
	if hasJSONCContentBetween(lines, 0, 5, 1, 0) {
		t.Fatalf("expected false when startCol is past line end")
	}
}

func TestHasJSONCContentBetweenBraceCloses(t *testing.T) {
	t.Parallel()
	lines := []string{
		"{",
		"  }",
	}
	if hasJSONCContentBetween(lines, 0, 1, 1, 2) {
		t.Fatalf("expected false when closing brace encountered")
	}
}

func TestHasJSONCNonTrivia(t *testing.T) {
	t.Parallel()
	content := "// comment\n/* block */\n"
	if hasJSONCNonTrivia(content, 0, len(content)) {
		t.Fatalf("expected trivia-only content to be false")
	}
	if !hasJSONCNonTrivia("x", 0, 1) {
		t.Fatalf("expected non-trivia content to be true")
	}
	if hasJSONCNonTrivia("x", 2, 1) {
		t.Fatalf("expected false when end <= start")
	}
	if !hasJSONCNonTrivia("/* ok */x", -1, 100) {
		t.Fatalf("expected non-trivia content after comments")
	}
}

func TestBuildVSCodeManagedBlockTrailingComma(t *testing.T) {
	t.Parallel()
	settings := &vscodeSettings{
		ChatToolsTerminalAutoApprove: OrderedMap[bool]{"/^git(\\b.*)?$/": true},
	}
	block, err := buildVSCodeManagedBlock(RealSystem{}, settings, "  ", "  ", true)
	if err != nil {
		t.Fatalf("buildVSCodeManagedBlock error: %v", err)
	}
	last := block[len(block)-2]
	if !strings.HasSuffix(strings.TrimSpace(last), ",") {
		t.Fatalf("expected trailing comma on last managed line")
	}
}

func TestBuildVSCodeManagedBlockEmptySettings(t *testing.T) {
	t.Parallel()
	block, err := buildVSCodeManagedBlock(RealSystem{}, &vscodeSettings{}, "  ", "", false)
	if err != nil {
		t.Fatalf("buildVSCodeManagedBlock error: %v", err)
	}
	if len(block) < 3 {
		t.Fatalf("expected managed block with header lines")
	}
	for _, line := range block {
		if strings.Contains(line, "chat.tools.terminal.autoApprove") {
			t.Fatalf("unexpected settings content in empty block")
		}
	}
}

func TestBuildVSCodeManagedBlockInvalidShape(t *testing.T) {
	t.Parallel()
	sys := &MockSystem{
		MarshalIndentFunc: func(_ any, _ string, _ string) ([]byte, error) {
			return []byte(`"bad"`), nil
		},
	}
	if _, err := buildVSCodeManagedBlock(sys, &vscodeSettings{}, "  ", "  ", false); err == nil {
		t.Fatalf("expected error for invalid JSON shape")
	}
}

func TestRenderVSCodeSettingsContentBuildError(t *testing.T) {
	t.Parallel()
	sys := &MockSystem{
		MarshalIndentFunc: func(_ any, _ string, _ string) ([]byte, error) {
			return nil, errors.New("marshal fail")
		},
	}
	if _, err := renderVSCodeSettingsContent(sys, "", &vscodeSettings{}); err == nil {
		t.Fatalf("expected error when managed block build fails")
	}
}

func TestInsertVSCodeManagedBlockBounds(t *testing.T) {
	t.Parallel()
	lines := []string{"{"}
	block := []string{"  // >>> agent-layer", "  // <<< agent-layer"}

	updated := insertVSCodeManagedBlock(lines, -1, 0, block)
	if len(updated) != len(lines) {
		t.Fatalf("expected unchanged lines when startLine is invalid")
	}

	updated = insertVSCodeManagedBlock(lines, 0, 10, block)
	if len(updated) != len(lines)+len(block) {
		t.Fatalf("expected managed block insertion")
	}
}

func TestInsertVSCodeManagedBlockPreservesAfter(t *testing.T) {
	t.Parallel()
	lines := []string{"{\"editor.tabSize\": 2}"}
	block := []string{"  // >>> agent-layer", "  // <<< agent-layer"}
	updated := insertVSCodeManagedBlock(lines, 0, 0, block)
	if len(updated) < 3 {
		t.Fatalf("expected block insertion with trailing content")
	}
	if updated[len(updated)-1] != "\"editor.tabSize\": 2}" {
		t.Fatalf("expected trailing content preserved, got %q", updated[len(updated)-1])
	}
}

func TestInsertVSCodeManagedBlockEmptyLine(t *testing.T) {
	t.Parallel()
	lines := []string{""}
	block := []string{"  // >>> agent-layer", "  // <<< agent-layer"}
	updated := insertVSCodeManagedBlock(lines, 0, 0, block)
	if len(updated) != len(lines) {
		t.Fatalf("expected unchanged lines for empty start line")
	}
}

func TestRenderVSCodeSettingsContentUnexpectedContentBefore(t *testing.T) {
	t.Parallel()
	existing := "garbage { \"editor.tabSize\": 2 }"
	settings := &vscodeSettings{}
	_, err := renderVSCodeSettingsContent(RealSystem{}, existing, settings)
	if err == nil {
		t.Fatalf("expected error for content before root object")
	}
	if !strings.Contains(err.Error(), "before root object") {
		t.Fatalf("expected 'before root object' error, got: %v", err)
	}
}

func TestRenderVSCodeSettingsContentUnexpectedContentAfter(t *testing.T) {
	t.Parallel()
	existing := "{ \"editor.tabSize\": 2 } garbage"
	settings := &vscodeSettings{}
	_, err := renderVSCodeSettingsContent(RealSystem{}, existing, settings)
	if err == nil {
		t.Fatalf("expected error for content after root object")
	}
	if !strings.Contains(err.Error(), "after root object") {
		t.Fatalf("expected 'after root object' error, got: %v", err)
	}
}

func TestRenderVSCodeSettingsContentBuildBlockError(t *testing.T) {
	t.Parallel()
	existing := "{ \"editor.tabSize\": 2 }"
	settings := &vscodeSettings{}
	sys := &MockSystem{
		MarshalIndentFunc: func(_ any, _ string, _ string) ([]byte, error) {
			return nil, errors.New("marshal error")
		},
	}
	_, err := renderVSCodeSettingsContent(sys, existing, settings)
	if err == nil {
		t.Fatalf("expected error from marshal failure")
	}
}

func TestRenderVSCodeSettingsContentExistingBlockBuildError(t *testing.T) {
	t.Parallel()
	existing := "{\n  // >>> agent-layer\n  // <<< agent-layer\n}"
	settings := &vscodeSettings{}
	sys := &MockSystem{
		MarshalIndentFunc: func(_ any, _ string, _ string) ([]byte, error) {
			return nil, errors.New("marshal error")
		},
	}
	_, err := renderVSCodeSettingsContent(sys, existing, settings)
	if err == nil {
		t.Fatalf("expected error from marshal failure when replacing block")
	}
}

func TestFindJSONCRootBoundsUnexpectedClosingBrace(t *testing.T) {
	t.Parallel()
	content := "}}}"
	_, _, err := findJSONCRootBounds(content)
	// Isolated closing braces without a start are ignored until we find an opening
	// This tests behavior where we get depth < 0
	if err == nil {
		t.Fatalf("expected error for missing root object")
	}
}

func TestHasJSONCContentBetweenEmptyLines(t *testing.T) {
	t.Parallel()
	if hasJSONCContentBetween(nil, 0, 0, 0, 0) {
		t.Fatalf("expected false for empty lines slice")
	}
	if hasJSONCContentBetween([]string{}, 0, 0, 0, 0) {
		t.Fatalf("expected false for empty lines slice")
	}
}

func TestHasJSONCContentBetweenStartColTruncation(t *testing.T) {
	t.Parallel()
	// When startCol is past line end on the only line being scanned, it continues
	// to the next line. If all lines are skipped this way, expect false.
	lines := []string{
		"{",
	}
	// Start at column past line end on the only line
	if hasJSONCContentBetween(lines, 0, 100, 0, 0) {
		t.Fatalf("expected false when startCol is past line end on single line")
	}
}

func TestHasJSONCContentBetweenEscapedString(t *testing.T) {
	t.Parallel()
	lines := []string{
		"{",
		"  \"key\": \"val\\\"ue}\"",
		"}",
	}
	// String with escaped quote and closing brace inside
	if !hasJSONCContentBetween(lines, 0, 1, 2, 0) {
		t.Fatalf("expected content to be detected")
	}
}

func TestHasJSONCContentBetweenLineComment(t *testing.T) {
	t.Parallel()
	lines := []string{
		"{",
		"  // this is content }",
		"}",
	}
	// Line comment should be ignored
	if hasJSONCContentBetween(lines, 0, 1, 2, 0) {
		t.Fatalf("expected line comment to be ignored")
	}
}

func TestHasJSONCContentBetweenCommaAndBrace(t *testing.T) {
	t.Parallel()
	lines := []string{
		"{ ,",
		"  }",
	}
	// Just comma and closing brace
	if hasJSONCContentBetween(lines, 0, 1, 1, 2) {
		t.Fatalf("expected comma and brace to be ignored")
	}
}

func TestDetectVSCodeIndentBlockComment(t *testing.T) {
	t.Parallel()
	lines := []string{
		"{",
		"  /* block comment",
		"   * continuation",
		"   */",
		"  \"key\": \"value\"",
		"}",
	}
	indent := detectVSCodeIndent(lines, 0, len(lines)-1)
	if indent != "  " {
		t.Fatalf("expected two-space indent, got %q", indent)
	}
}

func TestDetectVSCodeIndentAllComments(t *testing.T) {
	t.Parallel()
	lines := []string{
		"{",
		"  //",
		"  /*",
		"  *",
		"}",
	}
	indent := detectVSCodeIndent(lines, 0, len(lines)-1)
	if indent != "" {
		t.Fatalf("expected empty indent when all lines are comments, got %q", indent)
	}
}

func TestBuildVSCodeManagedBlockMarshalError(t *testing.T) {
	t.Parallel()
	sys := &MockSystem{
		MarshalIndentFunc: func(_ any, _ string, _ string) ([]byte, error) {
			return nil, errors.New("marshal failed")
		},
	}
	_, err := buildVSCodeManagedBlock(sys, &vscodeSettings{}, "  ", "  ", false)
	if err == nil {
		t.Fatalf("expected error from marshal failure")
	}
}

func TestBuildVSCodeManagedBlockSingleLineJSON(t *testing.T) {
	t.Parallel()
	sys := &MockSystem{
		MarshalIndentFunc: func(_ any, _ string, _ string) ([]byte, error) {
			// Return single-line JSON
			return []byte(`{"key": "value"}`), nil
		},
	}
	_, err := buildVSCodeManagedBlock(sys, &vscodeSettings{}, "  ", "  ", false)
	if err == nil {
		t.Fatalf("expected error for single-line JSON shape")
	}
}

func TestBuildVSCodeManagedBlockNoOpeningBrace(t *testing.T) {
	t.Parallel()
	sys := &MockSystem{
		MarshalIndentFunc: func(_ any, _ string, _ string) ([]byte, error) {
			// Return multi-line but no proper opening brace
			return []byte("x\n{\n}\n"), nil
		},
	}
	_, err := buildVSCodeManagedBlock(sys, &vscodeSettings{}, "  ", "  ", false)
	if err == nil {
		t.Fatalf("expected error for unexpected JSON shape")
	}
}

func TestRenderVSCodeSettingsTrailingCommaNeeded(t *testing.T) {
	t.Parallel()
	existing := "{\n  \"editor.tabSize\": 2\n}\n"
	settings := &vscodeSettings{
		ChatToolsTerminalAutoApprove: OrderedMap[bool]{"/^git(\\b.*)?$/": true},
	}
	updated, err := renderVSCodeSettingsContent(RealSystem{}, existing, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Managed block should have trailing comma since there's content after it
	if !strings.Contains(updated, "},") {
		t.Fatalf("expected trailing comma in managed block when content follows")
	}
}

func TestHasJSONCContentBetweenMultiLine(t *testing.T) {
	t.Parallel()
	lines := []string{
		"{",
		"  /* block",
		"     comment */",
		"  \"key\": \"value\"",
		"}",
	}
	// Should find content starting from inside the block comment
	if !hasJSONCContentBetween(lines, 1, 5, 4, 0) {
		t.Fatalf("expected content to be found after block comment")
	}
}

func TestHasJSONCContentBetweenEndColPastLine(t *testing.T) {
	t.Parallel()
	lines := []string{
		"{",
		"  \"key\": \"value\"",
		"}",
	}
	// endCol past line end should be handled
	if !hasJSONCContentBetween(lines, 0, 1, 2, 100) {
		t.Fatalf("expected content when endCol is past line end")
	}
}

func TestDetectVSCodeIndentEmptyFile(t *testing.T) {
	t.Parallel()
	lines := []string{""}
	indent := detectVSCodeIndent(lines, 0, 0)
	if indent != "" {
		t.Fatalf("expected empty indent for empty file, got %q", indent)
	}
}

func TestDetectVSCodeIndentEmptyLines(t *testing.T) {
	t.Parallel()
	lines := []string{
		"{",
		"",
		"",
		"}",
	}
	indent := detectVSCodeIndent(lines, 0, len(lines)-1)
	if indent != "" {
		t.Fatalf("expected empty indent for empty lines, got %q", indent)
	}
}

func TestFindJSONCRootBoundsNestedBraces(t *testing.T) {
	t.Parallel()
	content := "{\n  \"nested\": {\n    \"inner\": {}\n  }\n}\n"
	start, end, err := findJSONCRootBounds(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if start != 0 {
		t.Fatalf("expected start at 0, got %d", start)
	}
	if content[end] != '}' {
		t.Fatalf("expected end to be closing brace")
	}
}

func TestRenderVSCodeSettingsNoTrailingNewline(t *testing.T) {
	t.Parallel()
	// Input without trailing newline
	existing := "{}"
	settings := &vscodeSettings{}
	updated, err := renderVSCodeSettingsContent(RealSystem{}, existing, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(updated, "\n") {
		t.Fatalf("expected trailing newline")
	}
}

func TestRenderVSCodeSettingsExistingBlockNoIndent(t *testing.T) {
	t.Parallel()
	existing := "{\n// >>> agent-layer\n// <<< agent-layer\n}\n"
	settings := &vscodeSettings{
		ChatToolsTerminalAutoApprove: OrderedMap[bool]{"/^git(\\b.*)?$/": true},
	}
	updated, err := renderVSCodeSettingsContent(RealSystem{}, existing, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(updated, "// >>> agent-layer") {
		t.Fatalf("expected managed block markers")
	}
}

func TestHasJSONCContentBetweenStringEscapeBackslash(t *testing.T) {
	t.Parallel()
	lines := []string{
		`{"key": "val\\ue"}`,
	}
	if !hasJSONCContentBetween(lines, 0, 0, 0, len(lines[0])-1) {
		t.Fatalf("expected content with escaped backslash in string")
	}
}
