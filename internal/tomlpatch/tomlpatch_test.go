package tomlpatch

import (
	"strings"
	"testing"
)

func TestMultilineValueEndIndex_TripleQuotedStringInsideArray(t *testing.T) {
	t.Parallel()
	// A bracketed value whose element is a multiline basic string containing an
	// interior quote followed by `]` inside the string body: the array does not
	// end until the standalone `]` on line 3.
	lines := []string{
		`status_line = [ """`,
		` he said "hi]`,
		` """,`,
		`]`,
		`next = "keep"`,
	}
	if got := MultilineValueEndIndex(lines, 0); got != 3 {
		t.Fatalf("MultilineValueEndIndex basic = %d, want 3", got)
	}
	litLines := []string{
		`status_line = [ '''`,
		` it is ]`,
		` ''',`,
		`]`,
	}
	if got := MultilineValueEndIndex(litLines, 0); got != 3 {
		t.Fatalf("MultilineValueEndIndex literal = %d, want 3", got)
	}
}

func TestMultilineValueEndIndex_BracketDepthIgnoresStringsAndComments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		lines []string
		want  int
	}{
		{
			name:  "escaped quote and bracket inside basic string",
			lines: []string{`x = [`, `  "a\"b]",`, `]`},
			want:  2,
		},
		{
			name:  "comment after opening bracket",
			lines: []string{`x = [ # note`, `  1,`, `]`},
			want:  2,
		},
		{
			name:  "closing bracket inside single-line literal string",
			lines: []string{`x = [ 'a]b', 'c' ]`},
			want:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := MultilineValueEndIndex(tt.lines, 0); got != tt.want {
				t.Fatalf("MultilineValueEndIndex = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRemoveKeyFromBlock_TripleQuotedArrayValueNotCorrupted(t *testing.T) {
	t.Parallel()
	block := &Block{Name: "tui", Lines: []string{
		"[tui]",
		`status_line = [ """`,
		` he said "hi]`,
		` """,`,
		`]`,
		`notifications = true`,
	}}
	RemoveKeyFromBlock(block, "status_line")
	got := strings.Join(block.Lines, "\n")
	for _, leftover := range []string{"status_line", `"""`, "he said"} {
		if strings.Contains(got, leftover) {
			t.Fatalf("expected multiline status_line fully removed, leftover %q in:\n%s", leftover, got)
		}
	}
	if !strings.Contains(got, "notifications = true") {
		t.Fatalf("expected sibling key preserved, got:\n%s", got)
	}
}

func TestParseKeyPath_QuotedSegments(t *testing.T) {
	t.Parallel()
	got, ok := ParseKeyPath(`projects."/tmp/repo\"quote]#\\slash"`)
	if !ok {
		t.Fatal("expected quoted path to parse")
	}
	want := []string{"projects", `/tmp/repo"quote]#\slash`}
	if len(got) != len(want) {
		t.Fatalf("len = %d (%#v), want %d (%#v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("segment %d = %q, want %q; full=%#v", i, got[i], want[i], got)
		}
	}
	if rendered := FormatDottedKeyPath(got); !strings.Contains(rendered, `"`) {
		t.Fatalf("expected rendered path to quote special segment, got %q", rendered)
	}
}

func TestRemoveKeyFromBlock_RemovesMultilineAndQuotedDottedPrefix(t *testing.T) {
	t.Parallel()
	block := &Block{
		Name: "root",
		Lines: []string{
			"[root]",
			`headers = {`,
			`  "Authorization" = "old",`,
			`}`,
			`"headers"."Content-Type" = "application/json"`,
			`# headers.Keep = "commented"`,
			`other = true`,
		},
	}

	RemoveKeyFromBlock(block, "headers")

	got := strings.Join(block.Lines, "\n")
	if strings.Contains(got, "Authorization") || strings.Contains(got, "Content-Type") {
		t.Fatalf("expected active headers entries removed, got:\n%s", got)
	}
	if !strings.Contains(got, "# headers.Keep") {
		t.Fatalf("expected commented header to remain, got:\n%s", got)
	}
	if !strings.Contains(got, "other = true") {
		t.Fatalf("expected unrelated key to remain, got:\n%s", got)
	}
}

func TestParseDocument_PreservesCommentsAndRootBeforeTables(t *testing.T) {
	t.Parallel()
	content := "# preamble\nmodel = \"gpt\"\n\n[hooks.state]\nlast = \"keep\"\n"

	doc := ParseDocument(content)

	if len(doc.Preamble) < 2 || doc.Preamble[0] != "# preamble" || doc.Preamble[1] != `model = "gpt"` {
		t.Fatalf("unexpected preamble: %#v", doc.Preamble)
	}
	block := doc.Sections["hooks.state"]
	if block == nil {
		t.Fatalf("expected hooks.state section in %#v", doc.Sections)
	}
	if strings.Join(block.Lines, "\n") != "[hooks.state]\nlast = \"keep\"\n" {
		t.Fatalf("unexpected hooks.state block: %#v", block.Lines)
	}
}

func TestParseDocument_IgnoresHeadersInsideMultilineStrings(t *testing.T) {
	t.Parallel()
	content := "notes = \"\"\"\n[hooks.state]\nlast = \"x\"\n\"\"\"\n\n[real]\nkeep = true\n"

	doc := ParseDocument(content)

	if _, embedded := doc.Sections["hooks.state"]; embedded {
		t.Fatalf("header-looking line inside a multiline string must not become a section: %#v", doc.Sections)
	}
	if doc.Sections["real"] == nil {
		t.Fatalf("expected real section, got %#v", doc.Sections)
	}
	joinedPreamble := strings.Join(doc.Preamble, "\n")
	if !strings.Contains(joinedPreamble, "[hooks.state]") || !strings.Contains(joinedPreamble, `last = "x"`) {
		t.Fatalf("expected multiline string body kept intact, got:\n%s", joinedPreamble)
	}
}

func TestCommentHelpers_RespectMultilineStringsAndBounds(t *testing.T) {
	t.Parallel()
	lines := []string{
		`title = """start`,
		`# not a comment`,
		`end""" # closing`,
		`# leading one`,
		`# leading two`,
		`model = "gpt" # inline`,
	}

	if got := InlineCommentForLine(lines, 1); got != "" {
		t.Fatalf("expected multiline body hash to be ignored, got %q", got)
	}
	if got := InlineCommentForLine(lines, 2); got != "closing" {
		t.Fatalf("expected closing-line comment, got %q", got)
	}
	if got := CommentForLine(lines, 5); got != "leading one\nleading two\ninline" {
		t.Fatalf("unexpected combined comment %q", got)
	}
	if got := CommentForLine(lines, -1); got != "" {
		t.Fatalf("expected out-of-range comment to be empty, got %q", got)
	}
}

func TestWalkLinesOutsideMultiline_AdvanceAndStop(t *testing.T) {
	t.Parallel()
	lines := []string{
		`first = 1`,
		`skip = [`,
		`  "value"`,
		`]`,
		`text = """`,
		`inside = false`,
		`"""`,
		`last = 2`,
	}
	var visited []int
	WalkLinesOutsideMultiline(lines, func(i int, _ string, _ StringState) LineWalkResult {
		visited = append(visited, i)
		switch i {
		case 1:
			return LineWalkResult{AdvanceTo: 3}
		case 7:
			return LineWalkResult{Stop: true}
		default:
			return LineWalkResult{}
		}
	})

	want := []int{0, 1, 4, 7}
	if len(visited) != len(want) {
		t.Fatalf("visited %#v, want %#v", visited, want)
	}
	for i := range want {
		if visited[i] != want[i] {
			t.Fatalf("visited %#v, want %#v", visited, want)
		}
	}
}

func TestBlockMutationHelpers_PreserveCommentsAndOrdering(t *testing.T) {
	t.Parallel()
	block := &Block{
		Name: "agents.codex",
		Lines: []string{
			"[agents.codex]",
			`enabled = true`,
			`# model = "old"`,
			`model = "old" # keep note`,
			`reasoning_effort = "low"`,
		},
	}
	template := &Block{
		Name: "agents.codex",
		Lines: []string{
			"[agents.codex]",
			`  model = "template" # template note`,
			`statusline = false`,
		},
	}

	if got := ExtractBlockKeyValue(block.Lines, "model"); got != "old" {
		t.Fatalf("expected active model value, got %q", got)
	}
	SetKeyValue(block, template, "model", `"gpt-5"`, "enabled")
	if strings.Count(strings.Join(block.Lines, "\n"), "model") != 1 {
		t.Fatalf("expected duplicate model lines collapsed, got %#v", block.Lines)
	}
	if got := block.Lines[2]; got != `  model = "gpt-5" # template note` {
		t.Fatalf("expected template indent/comment preserved, got %q", got)
	}

	SetCommentedKeyLine(block, template, "statusline", "model")
	if got := block.Lines[3]; got != `# statusline = false` {
		t.Fatalf("expected statusline inserted after model as commented template line, got %q", got)
	}

	ReplaceOrInsertLine(block, "reasoning_effort", `reasoning_effort = "high"`, "model")
	if got := block.Lines[4]; got != `reasoning_effort = "high"` {
		t.Fatalf("expected existing reasoning line replaced in place, got %#v", block.Lines)
	}
	if got := FindInsertIndex(block.Lines, "missing"); got != 1 {
		t.Fatalf("expected missing anchor to insert after header, got %d", got)
	}
}

func TestMultilineValueEndIndex_CoversStringsArraysAndTables(t *testing.T) {
	t.Parallel()
	lines := []string{
		`basic = """open`,
		`value with ] bracket still open`,
		`closed""" # done`,
		`literal = '''open`,
		`closed'''`,
		`arr = [`,
		`  "value with ] bracket",`,
		`  "done",`,
		`]`,
		`table = {`,
		`  nested = { key = "}" },`,
		`}`,
	}
	tests := []struct {
		start int
		want  int
	}{
		{start: 0, want: 2},
		{start: 3, want: 4},
		{start: 5, want: 8},
		{start: 9, want: 11},
		{start: 100, want: 100},
	}
	for _, tt := range tests {
		if got := MultilineValueEndIndex(lines, tt.start); got != tt.want {
			t.Fatalf("start %d: got %d, want %d", tt.start, got, tt.want)
		}
	}
	if !ContainsUnescapedTripleQuote(`a """ close`) {
		t.Fatal("expected unescaped triple quote to be detected")
	}
	if ContainsUnescapedTripleQuote(`a \""" not-close`) {
		t.Fatal("expected escaped triple quote to be ignored")
	}
	if !ContainsUnescapedTripleQuote(`a \\""" close`) {
		t.Fatal("expected even backslash parity to leave the triple quote unescaped")
	}
	if ContainsUnescapedTripleQuote(`a \\\""" not-close`) {
		t.Fatal("expected odd backslash parity to escape the triple quote")
	}
	if !ContainsUnescapedTripleQuote(`a \""" then """ close`) {
		t.Fatal("expected a later unescaped triple quote to be detected after skipping an escaped one")
	}
}

func TestRenderAndCloneHelpers_DoNotAliasInputs(t *testing.T) {
	t.Parallel()
	if got := FormatValue(`a"b`); got != `"a\"b"` {
		t.Fatalf("unexpected string literal %q", got)
	}
	if got := FormatValue(true); got != "true" {
		t.Fatalf("unexpected bool literal %q", got)
	}
	if got := FormatValue(42); got != "42" {
		t.Fatalf("unexpected int literal %q", got)
	}

	if CloneLines(nil) != nil {
		t.Fatal("expected nil line clone to remain nil")
	}

	output := []string{"root = true"}
	AppendBlock(&output, []string{"", "[x]", "a = 1", ""})
	if got := strings.Join(output, "\n"); got != "root = true\n\n[x]\na = 1" {
		t.Fatalf("unexpected appended block:\n%s", got)
	}
	if got := strings.Join(TrimEmptyLines([]string{"", " a ", ""}), "|"); got != " a " {
		t.Fatalf("unexpected trim-empty result %q", got)
	}
	if got := strings.Join(TrimTrailingEmptyLines([]string{"a", "", ""}), "|"); got != "a" {
		t.Fatalf("unexpected trim-trailing result %q", got)
	}
}

func TestParseHelpers_RejectInvalidKeysAndHeaders(t *testing.T) {
	t.Parallel()
	if _, _, ok := ParseHeader(`[[mcp.servers]] # catalog`); !ok {
		t.Fatal("expected array header with comment to parse")
	}
	if _, _, ok := ParseHeader(`# [ignored]`); ok {
		t.Fatal("expected commented header to be ignored")
	}
	if _, ok := ParseKeyPath(`projects."unterminated`); ok {
		t.Fatal("expected unterminated quoted key to fail")
	}
	if _, ok := ParseKeyPath(`a.`); ok {
		t.Fatal("expected trailing dot to fail")
	}
	if _, ok := ParseKeyPath(`a..b`); ok {
		t.Fatal("expected empty dotted-key segment to fail")
	}
	if _, ok := ParseKeyPath(`'literal.key'.leaf`); !ok {
		t.Fatal("expected literal quoted key path to parse")
	}
	if key, value, ok := ParseKeyValueWithState(`"complex.key" = "value" # note`, `"complex.key"`, StateNone); !ok || key != `"complex.key"` || value != `"value"` {
		t.Fatalf("unexpected parsed key/value key=%q value=%q ok=%v", key, value, ok)
	}
	if _, _, ok := ParseKeyValueWithState(`complex = "value"`, `other`, StateNone); ok {
		t.Fatal("expected mismatched key to fail")
	}
	if got := MultilineValueEndIndex([]string{"not an assignment"}, 0); got != 0 {
		t.Fatalf("non-assignment end index = %d, want 0", got)
	}
	if got := MultilineValueEndIndex([]string{`key = """unterminated`}, 0); got != 0 {
		t.Fatalf("unterminated multiline end index = %d, want 0", got)
	}
	if got := FormatKey(""); got != `""` {
		t.Fatalf("empty TOML key rendered as %q, want quoted empty key", got)
	}
}

func TestScanLineForComment_StateTransitions(t *testing.T) {
	t.Parallel()
	if comment, state := ScanLineForComment(`key = 'value' # note`, StateNone); comment < 0 || state != StateNone {
		t.Fatalf("literal string comment parse = (%d, %v), want comment and none", comment, state)
	}
	if comment, state := ScanLineForComment(`escaped \" # still string`, StateBasic); comment != -1 || state != StateBasic {
		t.Fatalf("basic escaped quote parse = (%d, %v), want no comment and basic", comment, state)
	}
	if comment, state := ScanLineForComment(`done" # note`, StateBasic); comment < 0 || state != StateNone {
		t.Fatalf("basic closing parse = (%d, %v), want comment and none", comment, state)
	}
	if comment, state := ScanLineForComment(`done' # note`, StateLiteral); comment < 0 || state != StateNone {
		t.Fatalf("literal closing parse = (%d, %v), want comment and none", comment, state)
	}
	if _, state := ScanLineForComment(`key = '''open`, StateNone); state != StateMultiLiteral {
		t.Fatalf("expected multiline literal state, got %v", state)
	}
	if comment, state := ScanLineForComment(`close''' # note`, StateMultiLiteral); comment < 0 || state != StateNone {
		t.Fatalf("multiline literal closing parse = (%d, %v), want comment and none", comment, state)
	}
}

func TestMutationHelpers_FallbackBranches(t *testing.T) {
	t.Parallel()
	block := &Block{Name: "root", Lines: []string{"[root]", "enabled = true"}}

	SetKeyValue(block, nil, "model", `"gpt"`, "enabled")
	if got := strings.Join(block.Lines, "\n"); !strings.Contains(got, "enabled = true\nmodel = \"gpt\"") {
		t.Fatalf("expected model inserted after enabled, got:\n%s", got)
	}
	SetCommentedKeyLine(block, nil, "model", "")
	if got := block.Lines[2]; got != `# model = "gpt"` {
		t.Fatalf("expected existing model commented, got %q", got)
	}
	if got := BuildKeyLine(KeyLine{Indent: "\t"}, "debug", "true", true); got != "\t# debug = true" {
		t.Fatalf("unexpected commented key line %q", got)
	}
	if got := EnsureCommented("  # already = true"); got != "  # already = true" {
		t.Fatalf("already-commented line changed to %q", got)
	}
	if got := FindInsertIndex(nil, "enabled"); got != 0 {
		t.Fatalf("empty insert index = %d, want 0", got)
	}
	var output []string
	AppendBlock(&output, []string{"", ""})
	if len(output) != 0 {
		t.Fatalf("empty block should not append, got %#v", output)
	}
	if got := FormatValue(1.25); got != "1.25" {
		t.Fatalf("unexpected fallback literal %q", got)
	}
}

func TestParseDocument_DuplicateSectionsAndArrays(t *testing.T) {
	t.Parallel()
	doc := ParseDocument("[a]\nfirst = true\n\n[a]\nsecond = true\n\n[[mcp.servers]]\nid = \"one\"\n\n[[mcp.servers]]\nid = \"two\"\n")

	if got := strings.Join(doc.Sections["a"].Lines, "\n"); strings.Contains(got, "second") {
		t.Fatalf("expected first duplicate section to win, got:\n%s", got)
	}
	if len(doc.Arrays["mcp.servers"]) != 2 {
		t.Fatalf("expected two array blocks, got %#v", doc.Arrays["mcp.servers"])
	}
	if len(doc.Order) != 1 || doc.Order[0] != "a" {
		t.Fatalf("unexpected section order %#v", doc.Order)
	}
	if _, ok := ParseKeyPath(""); ok {
		t.Fatal("expected empty key path to fail")
	}
	if _, ok := ParseKeyPath(`"bad\q"`); ok {
		t.Fatal("expected invalid basic-string escape in key path to fail")
	}
}
