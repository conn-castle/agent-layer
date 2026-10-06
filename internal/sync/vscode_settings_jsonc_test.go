package sync

import (
	"errors"
	"strings"
	"testing"
)

func TestRenderVSCodeSettingsContentPreservesBOMAndNewlines(t *testing.T) {
	t.Parallel()
	settings := &vscodeSettings{
		ChatToolsTerminalAutoApprove: map[string]bool{"/^git(\\b.*)?$/": true},
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
		ChatToolsTerminalAutoApprove: map[string]bool{"/^git(\\b.*)?$/": true},
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
		ChatToolsTerminalAutoApprove: map[string]bool{"/^git(\\b.*)?$/": true},
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
	content := "// leading comment\n{\n  \"value\": \"brace { inside } and escaped \\\"quote\\\" \\\\\",\n  /* block { comment } */\n  \"nested\": {\"inner\": \"x\"}\n}\n"
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
	//nolint:dogsled // we only need found and err in this test
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

func TestBuildVSCodeManagedBlockTrailingComma(t *testing.T) {
	t.Parallel()
	settings := &vscodeSettings{
		ChatToolsTerminalAutoApprove: map[string]bool{"/^git(\\b.*)?$/": true},
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
		ChatToolsTerminalAutoApprove: map[string]bool{"/^git(\\b.*)?$/": true},
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
		ChatToolsTerminalAutoApprove: map[string]bool{"/^git(\\b.*)?$/": true},
	}
	updated, err := renderVSCodeSettingsContent(RealSystem{}, existing, settings)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(updated, "// >>> agent-layer") {
		t.Fatalf("expected managed block markers")
	}
}

func TestRenderVSCodeSettingsContentMovesUserSettingsOutOfManagedBlock(t *testing.T) {
	t.Parallel()
	settings := &vscodeSettings{
		ChatToolsTerminalAutoApprove: map[string]bool{"/^git(\\b.*)?$/": true},
	}
	header := "  // >>> agent-layer\n" +
		"  // Managed by Agent Layer. To customize, edit .agent-layer/config.toml\n" +
		"  // and .agent-layer/commands.allow, then re-run `al sync`.\n" +
		"  //\n"
	managed := "  \"chat.tools.terminal.autoApprove\": {\n" +
		"    \"/^git(\\\\b.*)?$/\": true\n" +
		"  }"

	tests := []struct {
		name     string
		existing string
		want     string
	}{
		{
			name:     "setting appended by VS Code after the last managed property",
			existing: "{\n" + header + managed + ",\n  \"editor.formatOnSave\": true\n  // <<< agent-layer\n}\n",
			want:     "{\n" + header + managed + ",\n  // <<< agent-layer\n  \"editor.formatOnSave\": true\n}\n",
		},
		{
			name: "settings with comments, nested values, and content after the block",
			existing: "{\n" + header +
				"  // stale note on a managed key\n" +
				"  \"chat.tools.terminal.autoApprove\": {\"/^old(\\\\b.*)?$/\": true},\n" +
				"  \"chat.tools.global.autoApprove\": true,\n" +
				"  // Peacock color\n" +
				"  \"peacock.color\": \"#123\", // tint\n" +
				"  \"files.exclude\": {\n" +
				"    \"**/x\": true, // not // a } comment\n" +
				"    \"a}b\": [\"//\", {\"c\": null}]\n" +
				"  } /* end */\n" +
				"  // dangling note\n" +
				"  // <<< agent-layer\n" +
				"  \"editor.tabSize\": 2\n" +
				"}\n",
			want: "{\n" + header + managed + ",\n" +
				"  // <<< agent-layer\n" +
				"  // Peacock color\n" +
				"  \"peacock.color\": \"#123\", // tint\n" +
				"  \"files.exclude\": {\n" +
				"    \"**/x\": true, // not // a } comment\n" +
				"    \"a}b\": [\"//\", {\"c\": null}]\n" +
				"  }, /* end */\n" +
				"  // dangling note\n" +
				"  \"editor.tabSize\": 2\n" +
				"}\n",
		},
		{
			name:     "properties sharing a line with a trailing comma",
			existing: "{\n" + header + "  \"a\": 1, \"chat.agentSkillsLocations\": {}, \"b\": false,\n  // <<< agent-layer\n}\n",
			want:     "{\n" + header + managed + ",\n  // <<< agent-layer\n  \"a\": 1,\n  \"b\": false\n}\n",
		},
		{
			name:     "comment before the header",
			existing: "{\n  // >>> agent-layer\n  // mine\n" + header[len("  // >>> agent-layer\n"):] + "  \"a\": 1\n  // <<< agent-layer\n}\n",
			want:     "{\n" + header + managed + ",\n  // <<< agent-layer\n  // mine\n  \"a\": 1\n}\n",
		},
		{
			name:     "comma on the next line",
			existing: "{\n" + header + "  \"a\": 1\n  , \"b\": 2\n  // <<< agent-layer\n}\n",
			want:     "{\n" + header + managed + ",\n  // <<< agent-layer\n  \"a\": 1,\n  \"b\": 2\n}\n",
		},
		{
			name:     "separator after the end marker",
			existing: "{\n" + header + "  \"a\": 1\n  // <<< agent-layer\n  , \"b\": 2\n}\n",
			want:     "{\n" + header + managed + ",\n  // <<< agent-layer\n  \"a\": 1\n  , \"b\": 2\n}\n",
		},
		{
			name: "separator after comments following the end marker",
			existing: "{\n" + header +
				"  // first setting\n  \"a\": 1, // first\n  \"c\": 3 // last\n" +
				"  // dangling note\n  // <<< agent-layer\n" +
				"  // outside comment with ,\n  /* another ,\n     comment */ , \"b\": 2\n}\n",
			want: "{\n" + header + managed + ",\n  // <<< agent-layer\n" +
				"  // first setting\n  \"a\": 1, // first\n  \"c\": 3 // last\n" +
				"  // dangling note\n  // outside comment with ,\n" +
				"  /* another ,\n     comment */ , \"b\": 2\n}\n",
		},
		{
			name:     "separator after a block containing only managed properties",
			existing: "{\n" + header + managed + "\n  // <<< agent-layer\n  , \"b\": 2\n}\n",
			want:     "{\n" + header + managed + "\n  // <<< agent-layer\n  , \"b\": 2\n}\n",
		},
		{
			name:     "missing comma between properties and a bare literal",
			existing: "{\n" + header + "  \"a\": nonstandard\n  \"b\": false\n  // <<< agent-layer\n}\n",
			want:     "{\n" + header + managed + ",\n  // <<< agent-layer\n  \"a\": nonstandard,\n  \"b\": false\n}\n",
		},
		{
			name:     "nested missing comma and bare literals remain verbatim",
			existing: "{\n" + header + "  \"chat.tools.terminal.autoApprove\": false,\n  \"a\": {\"b\": nonstandard \"c\": [NaN, undefined]}\n  // <<< agent-layer\n}\n",
			want:     "{\n" + header + managed + ",\n  // <<< agent-layer\n  \"a\": {\"b\": nonstandard \"c\": [NaN, undefined]}\n}\n",
		},
		{
			name: "nested JSONC comments, strings, and trailing commas remain verbatim",
			existing: "{\n" + header + "  \"a\": [\n" +
				"    {}, [], {\"b\" /* key */: /* value */ [true, \"a\\\"}b\",],}, // item\n" +
				"    {\"c\": null /* end */},\n  ]\n  // <<< agent-layer\n}\n",
			want: "{\n" + header + managed + ",\n  // <<< agent-layer\n  \"a\": [\n" +
				"    {}, [], {\"b\" /* key */: /* value */ [true, \"a\\\"}b\",],}, // item\n" +
				"    {\"c\": null /* end */},\n  ]\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			updated, err := renderVSCodeSettingsContent(RealSystem{}, tt.existing, settings)
			if err != nil {
				t.Fatalf("renderVSCodeSettingsContent error: %v", err)
			}
			if updated != tt.want {
				t.Fatalf("unexpected output:\n%s\nwant:\n%s", updated, tt.want)
			}
			again, err := renderVSCodeSettingsContent(RealSystem{}, updated, settings)
			if err != nil {
				t.Fatalf("second renderVSCodeSettingsContent error: %v", err)
			}
			if again != updated {
				t.Fatalf("second sync changed output:\n%s", again)
			}
		})
	}
}

func TestRenderVSCodeSettingsContentMovesUserSettingsWithBOMAndCRLF(t *testing.T) {
	t.Parallel()
	existing := "\ufeff{\r\n  // >>> agent-layer\r\n  \"editor.formatOnSave\": true\r\n  // <<< agent-layer\r\n}\r\n"
	want := "\ufeff{\r\n  // >>> agent-layer\r\n" +
		"  // Managed by Agent Layer. To customize, edit .agent-layer/config.toml\r\n" +
		"  // and .agent-layer/commands.allow, then re-run `al sync`.\r\n" +
		"  //\r\n" +
		"  // <<< agent-layer\r\n  \"editor.formatOnSave\": true\r\n}\r\n"

	updated, err := renderVSCodeSettingsContent(RealSystem{}, existing, &vscodeSettings{})
	if err != nil {
		t.Fatalf("renderVSCodeSettingsContent error: %v", err)
	}
	if updated != want {
		t.Fatalf("unexpected output:\n%q\nwant:\n%q", updated, want)
	}
}

func TestRenderVSCodeSettingsContentMovesUserSettingsFromEmptyManagedBlock(t *testing.T) {
	t.Parallel()
	existing := "{\n  // >>> agent-layer\n  \"a\": 1\n  // note\n  // <<< agent-layer\n  \"b\": 2\n}\n"
	want := "{\n  // >>> agent-layer\n" +
		"  // Managed by Agent Layer. To customize, edit .agent-layer/config.toml\n" +
		"  // and .agent-layer/commands.allow, then re-run `al sync`.\n" +
		"  //\n" +
		"  // <<< agent-layer\n  \"a\": 1,\n  // note\n  \"b\": 2\n}\n"

	updated, err := renderVSCodeSettingsContent(RealSystem{}, existing, &vscodeSettings{})
	if err != nil {
		t.Fatalf("renderVSCodeSettingsContent error: %v", err)
	}
	if updated != want {
		t.Fatalf("unexpected output:\n%s\nwant:\n%s", updated, want)
	}
}

func TestRenderVSCodeSettingsContentRejectsInvalidManagedBlockContent(t *testing.T) {
	t.Parallel()
	for _, inner := range []string{
		"  \"a\": tru e",
		"  \"a\" 1",
		"  \"a\": {\"b\": 1]",
		"  \"a\": \"unterminated",
		"  /* unterminated",
		"  , \"a\": 1",
		"  \"a\": 1,,",
		"  editor: 1",
		"  \"a\":",
		`  "a": {"b": }`,
		`  "a": {"b": /* no value */}`,
		`  "a": [{"b": }]`,
		`  "a": {"b": 1, "c": }`,
		`  "a": {"b": 1,, "c": 2}`,
		`  "a": {"b" 1}`,
		`  "a": {b: 1}`,
		`  "a": {"b": tru e}`,
		`  "a": {"b": [1,,2]}`,
		`  "a": [,1]`,
		`  "a": [1 2]`,
		`  "a": ["b": 1]`,
		`  "a": {"b": "bad\q"}`,
		`  "a": {"bad\q": 1}`,
	} {
		t.Run(inner, func(t *testing.T) {
			t.Parallel()
			existing := "{\n  // >>> agent-layer\n" + inner + "\n  // <<< agent-layer\n}\n"
			updated, err := renderVSCodeSettingsContent(RealSystem{}, existing, &vscodeSettings{})
			if !errors.Is(err, errInvalidVSCodeSettings) {
				t.Fatalf("block %q: expected invalid settings error, got %v", inner, err)
			}
			if updated != "" {
				t.Fatalf("expected no rewritten content on error, got %q", updated)
			}
		})
	}
}

func TestRenderVSCodeSettingsContentSeparatesPropertyBeforeManagedBlock(t *testing.T) {
	t.Parallel()
	skip := true
	settings := &vscodeSettings{ClaudeCodeAllowDangerouslySkipPerms: &skip}
	block := "  // >>> agent-layer\n" +
		"  // Managed by Agent Layer. To customize, edit .agent-layer/config.toml\n" +
		"  // and .agent-layer/commands.allow, then re-run `al sync`.\n" +
		"  //\n"
	managed := "  \"claudeCode.allowDangerouslySkipPermissions\": true\n  // <<< agent-layer\n}\n"

	tests := []struct {
		name     string
		existing string
		want     string
	}{
		{
			name:     "property without a comma",
			existing: "{\n  \"editor.fontSize\": 14\n" + block + "  // <<< agent-layer\n}\n",
			want:     "{\n  \"editor.fontSize\": 14,\n" + block + managed,
		},
		{
			name:     "file broken by an earlier sync",
			existing: "{\n  \"editor.fontSize\": 14\n" + block + managed,
			want:     "{\n  \"editor.fontSize\": 14,\n" + block + managed,
		},
		{
			name:     "comma goes before a trailing comment",
			existing: "{\n  \"a\": \"x // \\\"y\" // note, with comma\n  /* block, comment */\n" + block + "  // <<< agent-layer\n}\n",
			want:     "{\n  \"a\": \"x // \\\"y\", // note, with comma\n  /* block, comment */\n" + block + managed,
		},
		{
			name:     "string ending in an escaped backslash",
			existing: "{\n  \"a\": \"x\\\\\"\n" + block + "  // <<< agent-layer\n}\n",
			want:     "{\n  \"a\": \"x\\\\\",\n" + block + managed,
		},
		{
			name:     "nested value",
			existing: "{\n  \"a\": {\"b\": [1]}\n" + block + "  // <<< agent-layer\n}\n",
			want:     "{\n  \"a\": {\"b\": [1]},\n" + block + managed,
		},
		{
			name:     "existing comma",
			existing: "{\n  \"a\": 1, // note\n" + block + "  // <<< agent-layer\n}\n",
			want:     "{\n  \"a\": 1, // note\n" + block + managed,
		},
		{
			name:     "block after the root brace",
			existing: "{ // settings\n" + block + "  // <<< agent-layer\n}\n",
			want:     "{ // settings\n" + block + managed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			updated, err := renderVSCodeSettingsContent(RealSystem{}, tt.existing, settings)
			if err != nil {
				t.Fatalf("renderVSCodeSettingsContent error: %v", err)
			}
			if updated != tt.want {
				t.Fatalf("unexpected output:\n%s\nwant:\n%s", updated, tt.want)
			}
			again, err := renderVSCodeSettingsContent(RealSystem{}, updated, settings)
			if err != nil {
				t.Fatalf("second renderVSCodeSettingsContent error: %v", err)
			}
			if again != updated {
				t.Fatalf("second sync changed output:\n%s", again)
			}
		})
	}
}

func TestRenderVSCodeSettingsContentKeepsPropertyBeforeEmptyManagedBlock(t *testing.T) {
	t.Parallel()
	existing := "{\n  \"editor.fontSize\": 14\n  // >>> agent-layer\n" +
		"  // Managed by Agent Layer. To customize, edit .agent-layer/config.toml\n" +
		"  // and .agent-layer/commands.allow, then re-run `al sync`.\n" +
		"  //\n  // <<< agent-layer\n}\n"

	updated, err := renderVSCodeSettingsContent(RealSystem{}, existing, &vscodeSettings{})
	if err != nil {
		t.Fatalf("renderVSCodeSettingsContent error: %v", err)
	}
	if updated != existing {
		t.Fatalf("unexpected output:\n%s", updated)
	}
}

func TestRenderVSCodeSettingsContentInsertsBlockSeparatorOnlyBeforeProperties(t *testing.T) {
	t.Parallel()
	skip := true
	settings := &vscodeSettings{ClaudeCodeAllowDangerouslySkipPerms: &skip}
	block := "  // >>> agent-layer\n" +
		"  // Managed by Agent Layer. To customize, edit .agent-layer/config.toml\n" +
		"  // and .agent-layer/commands.allow, then re-run `al sync`.\n" +
		"  //\n" +
		"  \"claudeCode.allowDangerouslySkipPermissions\": true"

	tests := []struct {
		name     string
		existing string
		want     string
	}{
		{
			name:     "comments only",
			existing: "{\n  // note }\n  /* block } */\n}\n",
			want:     "{\n" + block + "\n  // <<< agent-layer\n  // note }\n  /* block } */\n}\n",
		},
		{
			name:     "property after comments",
			existing: "{\n  /* note */ \"a\": \"x } \\\\\"\n}\n",
			want:     "{\n" + block + ",\n  // <<< agent-layer\n  /* note */ \"a\": \"x } \\\\\"\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			updated, err := renderVSCodeSettingsContent(RealSystem{}, tt.existing, settings)
			if err != nil {
				t.Fatalf("renderVSCodeSettingsContent error: %v", err)
			}
			if updated != tt.want {
				t.Fatalf("unexpected output:\n%s\nwant:\n%s", updated, tt.want)
			}
		})
	}
}

func TestRenderVSCodeSettingsContentRejectsMalformedSettings(t *testing.T) {
	t.Parallel()
	for _, existing := range []string{
		"{\"a\" 1}\n",
		"{\"a\": [1 2]}\n",
		"{\"a\": \"bad\\q\"}\n",
		"{\"a\": \"line\nbreak\"}\n",
		"{}\n/* unterminated\n",
		"{\n  \"a\": \"bad\\q\"\n  // >>> agent-layer\n  // <<< agent-layer\n}\n",
		"{\n  /* unterminated\n  // >>> agent-layer\n  // <<< agent-layer\n}\n",
		"{\n  // >>> agent-layer\n  // <<< agent-layer\n  /* unterminated\n  \"b\": 2\n}\n",
	} {
		t.Run(existing, func(t *testing.T) {
			t.Parallel()
			skip := true
			updated, err := renderVSCodeSettingsContent(RealSystem{}, existing, &vscodeSettings{ClaudeCodeAllowDangerouslySkipPerms: &skip})
			if !errors.Is(err, errInvalidVSCodeSettings) {
				t.Fatalf("expected invalid settings error, got %v", err)
			}
			if updated != "" {
				t.Fatalf("expected no rewritten content on error, got %q", updated)
			}
		})
	}
}
