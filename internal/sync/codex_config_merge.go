package sync

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/tomlpatch"
)

var codexManagedRootScalarKeys = []string{
	config.CodexModelKey,
	config.CodexReasoningEffortKey,
	config.CodexApprovalPolicyKey,
	config.CodexSandboxModeKey,
	config.CodexWebSearchKey,
}

const (
	codexStopKey            = "Stop"
	codexHooksStopPath      = "hooks.Stop"
	codexHooksStopHooksPath = "hooks.Stop.hooks"
	codexTUIKey             = "tui"
)

type codexManagedConfig struct {
	Content       string
	TrustedRoot   string
	AgentSpecific map[string]any
	ChimeEnabled  bool
}

type codexTomlEditor struct {
	lines []string
}

type codexPathValue struct {
	path  []string
	value any
}

func readExistingCodexConfig(sys System, path string) (string, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf(messages.SyncReadFailedFmt, path, err)
	}
	return string(data), nil
}

func mergeCodexConfig(path string, existing string, managed codexManagedConfig) (string, error) {
	var existingMap map[string]any
	if strings.TrimSpace(existing) != "" {
		if err := toml.Unmarshal([]byte(existing), &existingMap); err != nil {
			return "", fmt.Errorf(messages.SyncCodexExistingConfigInvalidFmt, path, err)
		}
	}
	var managedMap map[string]any
	if err := toml.Unmarshal([]byte(managed.Content), &managedMap); err != nil {
		return "", fmt.Errorf("generated Codex config is invalid TOML: %w", err)
	}

	if strings.TrimSpace(existing) == "" {
		return managed.Content, nil
	}
	if err := validateCodexExistingShapes(path, existingMap, managedMap, managed.TrustedRoot); err != nil {
		return "", err
	}

	editor := newCodexTomlEditor(existing)
	editor.replaceAgentLayerHeader()

	for _, key := range codexManagedRootScalarKeys {
		pathParts := []string{key}
		if value, ok := valueAtPath(managedMap, pathParts); ok {
			literal, err := tomlLiteral(value)
			if err != nil {
				return "", err
			}
			editor.setPath(pathParts, literal)
			continue
		}
		editor.removePath(pathParts)
	}

	for _, key := range config.CodexKnownManagedFeatureKeys() {
		pathParts := []string{codexFeaturesKey, key}
		if value, ok := valueAtPath(managedMap, pathParts); ok {
			literal, err := tomlLiteral(value)
			if err != nil {
				return "", err
			}
			editor.setPath(pathParts, literal)
			continue
		}
		editor.removePath(pathParts)
	}

	statuslinePath := []string{codexTUIKey, codexStatusLineKey}
	if value, ok := valueAtPath(managedMap, statuslinePath); ok {
		literal, err := tomlLiteral(value)
		if err != nil {
			return "", err
		}
		editor.setPath(statuslinePath, literal)
	} else {
		editor.removePath(statuslinePath)
	}

	for _, item := range agentSpecificLeafValues(managed.AgentSpecific) {
		if codexPathHandledElsewhere(item.path) {
			continue
		}
		literal, err := tomlLiteral(item.value)
		if err != nil {
			return "", err
		}
		editor.setPath(item.path, literal)
	}

	// Seed missing projects before re-appending the managed mcp_servers block so
	// the emitted block order (projects, then mcp_servers) is stable across
	// repeated syncs regardless of whether the trusted-root project was present.
	editor.removeNamespace([]string{config.CodexMCPServersKey})
	if err := editor.appendMissingProjects(path, existingMap, managedMap); err != nil {
		return "", err
	}
	if _, err := editor.applyCodexChimeHook(path, managed.ChimeEnabled); err != nil {
		return "", err
	}
	if fragment := extractNamespaceLines(managed.Content, []string{config.CodexMCPServersKey}); len(fragment) > 0 {
		editor.appendBlock(fragment)
	}

	out := editor.render()
	var renderCheck map[string]any
	if err := toml.Unmarshal([]byte(out), &renderCheck); err != nil {
		return "", fmt.Errorf("merged Codex config is invalid TOML: %w", err)
	}
	return out, nil
}

// cleanCodexChimeHook removes only Agent Layer-owned Codex chime hooks from
// .codex/config.toml. It is used when Codex is disabled, so the normal Codex
// config merge path will not run.
func cleanCodexChimeHook(sys System, root string) error {
	path, mode, exists, err := existingChimeCleanupTarget(sys, root, ".codex", "config.toml")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	existing, err := readExistingCodexConfig(sys, path)
	if err != nil {
		return err
	}
	if strings.TrimSpace(existing) == "" {
		return nil
	}
	editor := newCodexTomlEditor(existing)
	changed, err := editor.applyCodexChimeHook(path, false)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	out := editor.render()
	var renderCheck map[string]any
	if err := toml.Unmarshal([]byte(out), &renderCheck); err != nil {
		return fmt.Errorf("merged Codex config is invalid TOML: %w", err)
	}
	if err := sys.WriteFileAtomic(path, []byte(out), mode); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}
	return nil
}

func validateCodexExistingShapes(path string, existing map[string]any, managed map[string]any, trustedRoot string) error {
	for _, managedPath := range [][]string{{codexFeaturesKey}, {codexTUIKey}, {codexProjectsKey}} {
		if value, ok := valueAtPath(existing, managedPath); ok {
			if _, table := value.(map[string]any); !table {
				return fmt.Errorf(messages.SyncCodexExistingConfigShapeConflictFmt, path, strings.Join(managedPath, "."))
			}
		}
	}
	if value, ok := valueAtPath(existing, []string{codexProjectsKey, trustedRoot}); ok {
		if _, table := value.(map[string]any); !table {
			return fmt.Errorf(messages.SyncCodexExistingConfigShapeConflictFmt, path, "projects."+tomlpatch.FormatKey(trustedRoot))
		}
	}
	// A managed root scalar that we will write (e.g. model) cannot coexist with an
	// existing table of the same name (a [model] header); emit the actionable shape
	// message instead of the opaque go-toml render-check error.
	for _, key := range codexManagedRootScalarKeys {
		if _, willSet := valueAtPath(managed, []string{key}); !willSet {
			continue
		}
		if value, ok := valueAtPath(existing, []string{key}); ok {
			if _, table := value.(map[string]any); table {
				return fmt.Errorf(messages.SyncCodexExistingConfigShapeConflictFmt, path, key)
			}
		}
	}
	return nil
}

func newCodexTomlEditor(content string) *codexTomlEditor {
	return &codexTomlEditor{lines: strings.Split(content, "\n")}
}

func (e *codexTomlEditor) render() string {
	lines := collapseConsecutiveBlankLines(trimTrailingBlankLines(e.lines))
	return strings.Join(lines, "\n") + "\n"
}

func (e *codexTomlEditor) replaceAgentLayerHeader() {
	newHeaderLines := headerLines(codexPartialHeader)
	preambleEnd := e.leadingPreambleEnd()
	for _, known := range []string{codexHeader, codexHeaderWithStatusline, codexPartialHeader} {
		knownLines := headerLines(known)
		if start, ok := findLineSequence(e.lines[:preambleEnd], knownLines); ok {
			e.lines = replaceLineRange(e.lines, start, start+len(knownLines), newHeaderLines)
			return
		}
	}
	e.lines = append(append([]string{}, newHeaderLines...), e.lines...)
}

func (e *codexTomlEditor) leadingPreambleEnd() int {
	for i, line := range e.lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return i
	}
	return len(e.lines)
}

func (e *codexTomlEditor) setPath(path []string, literal string) {
	if len(path) > 1 && e.mutateRootInlineTable(path[0], func(table map[string]any) {
		setNestedValue(table, path[1:], literalValue{literal: literal})
	}) {
		return
	}
	// Update an existing key line in place so repeated syncs stay byte-stable and a
	// user's inline comment and the surrounding blank lines are preserved. Only
	// fall through to a fresh insert when the key is absent.
	if ranges := e.rangesForExactPath(path); len(ranges) > 0 {
		e.replaceAssignmentValue(ranges, literal)
		return
	}
	if len(path) == 1 {
		e.insertRootLine(tomlpatch.FormatDottedKeyPath(path) + " = " + literal)
		return
	}
	tablePath := path[:len(path)-1]
	key := path[len(path)-1]
	tableStart := e.ensureTable(tablePath)
	insertAt := tableStart + 1
	e.lines = append(e.lines[:insertAt], append([]string{tomlpatch.FormatKey(key) + " = " + literal}, e.lines[insertAt:]...)...)
}

// replaceAssignmentValue rewrites the first assignment range's value with literal
// (keeping its left-hand side, and any inline comment when it is a single line)
// and deletes any duplicate ranges for the same key.
func (e *codexTomlEditor) replaceAssignmentValue(ranges []lineRange, literal string) {
	for i := len(ranges) - 1; i >= 1; i-- {
		r := ranges[i]
		e.lines = append(e.lines[:r.start], e.lines[r.end+1:]...)
	}
	first := ranges[0]
	startLine := e.lines[first.start]
	// rangesForExactPath only matches assignment lines, so the key/value `=` is
	// always present; Cut returns the whole (comment-stripped) line as lhs otherwise.
	lhs, _, _ := strings.Cut(stripLineComment(startLine), "=")
	newLine := strings.TrimRight(lhs, " \t") + " = " + literal
	if first.start == first.end {
		if comment := inlineCommentOf(startLine); comment != "" {
			newLine += " " + comment
		}
	}
	e.lines = replaceLineRange(e.lines, first.start, first.end+1, []string{newLine})
}

func stripLineComment(line string) string {
	commentPos, _ := tomlpatch.ScanLineForComment(line, tomlpatch.StateNone)
	if commentPos < 0 {
		return line
	}
	return line[:commentPos]
}

func inlineCommentOf(line string) string {
	commentPos, _ := tomlpatch.ScanLineForComment(line, tomlpatch.StateNone)
	if commentPos < 0 {
		return ""
	}
	return strings.TrimSpace(line[commentPos:])
}

func (e *codexTomlEditor) removePath(path []string) {
	if len(path) > 1 && e.mutateRootInlineTable(path[0], func(table map[string]any) {
		deleteNestedValue(table, path[1:])
	}) {
		return
	}
	ranges := e.rangesForExactPath(path)
	e.removeRanges(ranges)
}

func (e *codexTomlEditor) removeNamespace(path []string) {
	ranges := e.rangesForNamespace(path)
	e.removeRanges(ranges)
}

func (e *codexTomlEditor) appendBlock(block []string) {
	block = tomlpatch.TrimEmptyLines(block)
	if len(block) == 0 {
		return
	}
	e.lines = trimTrailingBlankLines(e.lines)
	if len(e.lines) > 0 && strings.TrimSpace(e.lines[len(e.lines)-1]) != "" {
		e.lines = append(e.lines, "")
	}
	e.lines = append(e.lines, block...)
}

func (e *codexTomlEditor) applyCodexChimeHook(path string, enabled bool) (bool, error) {
	changed, err := e.removeCodexChimeHook(path)
	if err != nil {
		return false, err
	}
	if !enabled {
		return changed, nil
	}
	if err := e.expandCodexStopAssignment(path); err != nil {
		return false, err
	}
	if e.rootInlineTableExists(hooksKey) {
		return false, fmt.Errorf(messages.SyncCodexExistingConfigShapeConflictFmt, path, hooksKey)
	}
	var builder strings.Builder
	appendCodexChimeBlock(&builder)
	e.appendBlock(strings.Split(strings.TrimSuffix(builder.String(), "\n"), "\n"))
	return true, nil
}

// expandCodexStopAssignment converts an existing hooks.Stop assignment into
// array-table blocks so the managed chime block can be appended without
// creating a duplicate TOML path.
func (e *codexTomlEditor) expandCodexStopAssignment(path string) error {
	var assignment assignmentInfo
	var found bool
	e.walkAssignments(func(info assignmentInfo) {
		if slices.Equal(info.fullPath, []string{hooksKey, codexStopKey}) {
			assignment = info
			found = true
		}
	})
	if !found {
		return nil
	}
	fragment := strings.Join(e.lines[assignment.start:assignment.end+1], "\n")
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(fragment), &parsed); err != nil {
		return fmt.Errorf(messages.SyncCodexExistingConfigInvalidFmt, path, err)
	}
	value, _ := valueAtPath(parsed, assignment.keyPath)
	entries, ok := value.([]any)
	if !ok {
		return fmt.Errorf(messages.SyncCodexExistingConfigShapeConflictFmt, path, codexHooksStopPath)
	}
	var lines []string
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			return fmt.Errorf(messages.SyncCodexExistingConfigShapeConflictFmt, path, codexHooksStopPath)
		}
		lines = append(lines, "[["+codexHooksStopPath+"]]")
		entryKeys := make([]string, 0, len(entryMap))
		for key := range entryMap {
			if key != hooksKey {
				entryKeys = append(entryKeys, key)
			}
		}
		sort.Strings(entryKeys)
		for _, key := range entryKeys {
			lines = append(lines, tomlpatch.FormatKey(key)+" = "+formatInlineValue(entryMap[key]))
		}
		if hooksValue, ok := entryMap[hooksKey]; ok {
			hooks, ok := hooksValue.([]any)
			if !ok {
				return fmt.Errorf(messages.SyncCodexExistingConfigShapeConflictFmt, path, codexHooksStopHooksPath)
			}
			if len(hooks) == 0 {
				lines = append(lines, hooksKey+" = []")
				continue
			}
			for _, hook := range hooks {
				hookMap, ok := hook.(map[string]any)
				if !ok {
					return fmt.Errorf(messages.SyncCodexExistingConfigShapeConflictFmt, path, codexHooksStopHooksPath)
				}
				lines = append(lines, "[["+codexHooksStopHooksPath+"]]")
				hookKeys := make([]string, 0, len(hookMap))
				for key := range hookMap {
					hookKeys = append(hookKeys, key)
				}
				sort.Strings(hookKeys)
				for _, key := range hookKeys {
					lines = append(lines, tomlpatch.FormatKey(key)+" = "+formatInlineValue(hookMap[key]))
				}
			}
		}
	}
	e.removeRanges([]lineRange{{start: assignment.start, end: assignment.end}})
	e.appendBlock(lines)
	return nil
}

func (e *codexTomlEditor) removeCodexChimeHook(path string) (bool, error) {
	ranges, err := e.managedCodexChimeRegions(path)
	if err != nil {
		return false, err
	}
	changed := false
	if len(ranges) > 0 {
		e.removeRanges(ranges)
		changed = true
	}
	unmarked := e.unmarkedCodexChimeStopGroupRanges()
	if len(unmarked) > 0 {
		e.removeRanges(unmarked)
		changed = true
	}
	if err := e.rejectAmbiguousCodexChimeHook(path); err != nil {
		return false, err
	}
	return changed, nil
}

// rejectAmbiguousCodexChimeHook reports remaining exact chime commands that
// cannot be proven to belong to an Agent Layer-managed hook region.
func (e *codexTomlEditor) rejectAmbiguousCodexChimeHook(path string) error {
	if !e.containsActiveCodexChimeCommand() {
		return nil
	}
	content := e.render()
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(content), &parsed); err != nil {
		return fmt.Errorf(messages.SyncCodexExistingConfigInvalidFmt, path, err)
	}
	hooks, ok := parsed[hooksKey]
	if !ok {
		return nil
	}
	if containsExactChimeCommand(hooks, managedChimeCommandVariants(agentLayerCodexChimeCommand)) {
		return fmt.Errorf(messages.SyncCodexChimeOwnershipConflictFmt, path)
	}
	return nil
}

// containsActiveCodexChimeCommand finds an uncommented command assignment
// whose complete string value is an Agent Layer-managed Codex chime command.
func (e *codexTomlEditor) containsActiveCodexChimeCommand() bool {
	commands := managedChimeCommandVariants(agentLayerCodexChimeCommand)
	found := false
	tomlpatch.WalkLinesOutsideMultiline(e.lines, func(_ int, line string, state tomlpatch.StringState) tomlpatch.LineWalkResult {
		_, literal, ok := tomlpatch.ParseKeyValueWithState(line, chimeHandlerCommandKey, state)
		if !ok {
			return tomlpatch.LineWalkResult{}
		}
		command, err := strconv.Unquote(literal)
		if err != nil {
			return tomlpatch.LineWalkResult{}
		}
		if _, ok := commands[command]; ok {
			found = true
			return tomlpatch.LineWalkResult{Stop: true}
		}
		return tomlpatch.LineWalkResult{}
	})
	return found
}

func (e *codexTomlEditor) managedCodexChimeRegions(path string) ([]lineRange, error) {
	open := -1
	var invalid bool
	markerCount := 0
	var ranges []lineRange
	tomlpatch.WalkLinesOutsideMultiline(e.lines, func(i int, line string, _ tomlpatch.StringState) tomlpatch.LineWalkResult {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case codexChimeBeginMarker:
			if open >= 0 {
				invalid = true
				return tomlpatch.LineWalkResult{Stop: true}
			}
			open = i
		case codexChimeEndMarker:
			if open < 0 {
				invalid = true
				return tomlpatch.LineWalkResult{Stop: true}
			}
			if e.codexChimeRegionHasTrailingContent(i + 1) {
				invalid = true
				return tomlpatch.LineWalkResult{Stop: true}
			}
			removalRanges, ok := e.codexChimeRegionRemovalRanges(lineRange{start: open, end: i})
			if !ok {
				invalid = true
				return tomlpatch.LineWalkResult{Stop: true}
			}
			markerCount++
			ranges = append(ranges, removalRanges...)
			open = -1
		}
		return tomlpatch.LineWalkResult{}
	})
	if invalid || open >= 0 || markerCount > 1 {
		return nil, fmt.Errorf(messages.SyncCodexChimeMarkerConflictFmt, path)
	}
	return ranges, nil
}

func (e *codexTomlEditor) codexChimeRegionRemovalRanges(marker lineRange) ([]lineRange, bool) {
	ranges := []lineRange{
		{start: marker.start, end: marker.start},
		{start: marker.end, end: marker.end},
	}
	removedChimeGroup := false
	headers := e.headerLines()
	for i, header := range headers {
		if header.index <= marker.start || header.index >= marker.end {
			continue
		}
		if !header.parsed {
			return nil, false
		}
		if !header.isArray || !slices.Equal(header.path, []string{hooksKey, codexStopKey}) {
			continue
		}
		if removedChimeGroup {
			return nil, false
		}
		groupEnd := marker.end - 1
		for j := i + 1; j < len(headers); j++ {
			next := headers[j]
			if next.index >= marker.end {
				break
			}
			if next.index <= header.index {
				continue
			}
			if !next.parsed {
				return nil, false
			}
			if slices.Equal(next.path, []string{hooksKey, codexStopKey}) || !pathHasPrefix(next.path, []string{hooksKey, codexStopKey}) {
				groupEnd = next.index - 1
				break
			}
		}
		group := lineRange{start: header.index, end: groupEnd}
		if !e.codexStopGroupIsExactChimeOnly(group) {
			return nil, false
		}
		ranges = append(ranges, group)
		removedChimeGroup = true
	}
	if !removedChimeGroup {
		return nil, false
	}
	return ranges, true
}

func (e *codexTomlEditor) codexChimeRegionHasTrailingContent(start int) bool {
	for i := start; i < len(e.lines); i++ {
		line := e.lines[i]
		if _, _, ok := tomlpatch.ParseHeader(line); ok {
			return false
		}
		trimmed := strings.TrimSpace(stripLineComment(line))
		if trimmed == "" {
			continue
		}
		return true
	}
	return false
}

func (e *codexTomlEditor) unmarkedCodexChimeStopGroupRanges() []lineRange {
	headers := e.headerLines()
	var ranges []lineRange
	for i, header := range headers {
		if !header.parsed || !header.isArray || !slices.Equal(header.path, []string{hooksKey, codexStopKey}) {
			continue
		}
		end := len(e.lines)
		for j := i + 1; j < len(headers); j++ {
			next := headers[j]
			if !next.parsed {
				end = next.index
				break
			}
			if slices.Equal(next.path, []string{hooksKey, codexStopKey}) || !pathHasPrefix(next.path, []string{hooksKey, codexStopKey}) {
				end = next.index
				break
			}
		}
		r := lineRange{start: header.index, end: end - 1}
		if e.codexStopGroupIsExactChimeOnly(r) {
			ranges = append(ranges, r)
		}
	}
	return ranges
}

func (e *codexTomlEditor) codexStopGroupIsExactChimeOnly(r lineRange) bool {
	fragment := strings.Join(e.lines[r.start:r.end+1], "\n")
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(fragment), &parsed); err != nil {
		return false
	}
	hooks, ok := parsed[hooksKey].(map[string]any)
	if !ok || len(hooks) != 1 {
		return false
	}
	stop, ok := hooks[codexStopKey].([]any)
	if !ok || len(stop) != 1 {
		return false
	}
	stopEntry, ok := stop[0].(map[string]any)
	if !ok || len(stopEntry) != 1 {
		return false
	}
	stopHooks, ok := stopEntry[hooksKey].([]any)
	if !ok || len(stopHooks) != 1 {
		return false
	}
	return chimeHandlerMatchesAny(stopHooks[0], managedChimeCommandVariants(agentLayerCodexChimeCommand))
}

func (e *codexTomlEditor) insertRootLine(line string) {
	insertAt := e.firstTableIndex()
	if insertAt < len(e.lines) {
		for insertAt > 0 && strings.HasPrefix(strings.TrimSpace(e.lines[insertAt-1]), "#") {
			insertAt--
		}
	}
	if insertAt > 0 && strings.TrimSpace(e.lines[insertAt-1]) != "" {
		e.lines = append(e.lines[:insertAt], append([]string{"", line}, e.lines[insertAt:]...)...)
		return
	}
	e.lines = append(e.lines[:insertAt], append([]string{line}, e.lines[insertAt:]...)...)
}

// codexHeaderLine is a real TOML table header line (outside any multiline
// string) with its parsed dotted path.
type codexHeaderLine struct {
	index   int
	path    []string
	parsed  bool
	isArray bool
}

// headerLines returns every real table-header line in order. Header-looking
// lines inside multiline string bodies are skipped so shared user config that
// embeds such text (e.g. a "[mcp_servers.x]" line in a triple-quoted value) is
// not misparsed as a table and corrupted on sync.
func (e *codexTomlEditor) headerLines() []codexHeaderLine {
	var out []codexHeaderLine
	tomlpatch.WalkLinesOutsideMultiline(e.lines, func(i int, line string, _ tomlpatch.StringState) tomlpatch.LineWalkResult {
		name, isArray, ok := tomlpatch.ParseHeader(line)
		if !ok {
			return tomlpatch.LineWalkResult{}
		}
		path, parsed := tomlpatch.ParseKeyPath(name)
		out = append(out, codexHeaderLine{index: i, path: path, parsed: parsed, isArray: isArray})
		return tomlpatch.LineWalkResult{}
	})
	return out
}

func (e *codexTomlEditor) firstTableIndex() int {
	if headers := e.headerLines(); len(headers) > 0 {
		return headers[0].index
	}
	return len(e.lines)
}

func (e *codexTomlEditor) ensureTable(path []string) int {
	for _, header := range e.headerLines() {
		if header.isArray || !header.parsed {
			continue
		}
		if slices.Equal(header.path, path) {
			return header.index
		}
	}
	header := "[" + tomlpatch.FormatDottedKeyPath(path) + "]"
	e.appendBlock([]string{header})
	return len(e.lines) - 1
}

func (e *codexTomlEditor) rangesForExactPath(path []string) []lineRange {
	var ranges []lineRange
	e.walkAssignments(func(info assignmentInfo) {
		if slices.Equal(info.fullPath, path) {
			ranges = append(ranges, lineRange{start: info.start, end: info.end})
		}
	})
	return ranges
}

func (e *codexTomlEditor) rangesForNamespace(path []string) []lineRange {
	var ranges []lineRange
	headers := e.headerLines()
	for k, header := range headers {
		if !header.parsed || !pathHasPrefix(header.path, path) {
			continue
		}
		// The block runs from this header to the line before the next real header
		// (or end of file). Using the multiline-aware header list means a
		// header-looking line inside a string body cannot truncate the block early.
		end := len(e.lines)
		if k+1 < len(headers) {
			end = headers[k+1].index
		}
		ranges = append(ranges, lineRange{start: header.index, end: end - 1})
	}
	e.walkAssignments(func(info assignmentInfo) {
		if pathHasPrefix(info.fullPath, path) {
			ranges = append(ranges, lineRange{start: info.start, end: info.end})
		}
	})
	return ranges
}

func (e *codexTomlEditor) removeRanges(ranges []lineRange) {
	if len(ranges) == 0 {
		return
	}
	ranges = mergeLineRanges(ranges)
	for i := len(ranges) - 1; i >= 0; i-- {
		r := ranges[i]
		e.lines = append(e.lines[:r.start], e.lines[r.end+1:]...)
	}
}

func (e *codexTomlEditor) walkAssignments(fn func(assignmentInfo)) {
	var tablePath []string
	state := tomlpatch.StateNone
	for i := 0; i < len(e.lines); i++ {
		line := e.lines[i]
		if tomlpatch.StateInMultiline(state) {
			_, state = tomlpatch.ScanLineForComment(line, state)
			continue
		}
		if name, _, ok := tomlpatch.ParseHeader(line); ok {
			if parsed, parsedOK := tomlpatch.ParseKeyPath(name); parsedOK {
				tablePath = parsed
			} else {
				tablePath = nil
			}
			_, state = tomlpatch.ScanLineForComment(line, state)
			continue
		}
		keyPath, ok := assignmentKeyPath(line, state)
		if !ok {
			_, state = tomlpatch.ScanLineForComment(line, state)
			continue
		}
		end := tomlpatch.MultilineValueEndIndex(e.lines, i)
		fullPath := append(append([]string(nil), tablePath...), keyPath...)
		fn(assignmentInfo{start: i, end: end, fullPath: fullPath, keyPath: keyPath, tablePath: tablePath})
		for j := i; j <= end && j < len(e.lines); j++ {
			_, state = tomlpatch.ScanLineForComment(e.lines[j], state)
		}
		i = end
	}
}

func (e *codexTomlEditor) mutateRootInlineTable(top string, mutate func(map[string]any)) bool {
	var target *assignmentInfo
	e.walkAssignments(func(info assignmentInfo) {
		if target != nil || len(info.tablePath) != 0 || len(info.keyPath) != 1 || info.keyPath[0] != top {
			return
		}
		target = &info
	})
	if target == nil {
		return false
	}
	assignment := strings.Join(e.lines[target.start:target.end+1], "\n")
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(assignment), &parsed); err != nil {
		return false
	}
	table, ok := parsed[top].(map[string]any)
	if !ok {
		return false
	}
	mutate(table)
	replacement := []string{}
	if len(table) > 0 {
		startLine := e.lines[target.start]
		indent := startLine[:len(startLine)-len(strings.TrimLeft(startLine, " \t"))]
		newLine := indent + tomlpatch.FormatKey(top) + " = " + formatInlineValue(table)
		// Preserve a single-line inline table's trailing comment; a multiline inline
		// table has no single unambiguous comment line to carry over.
		if target.start == target.end {
			if comment := inlineCommentOf(startLine); comment != "" {
				newLine += " " + comment
			}
		}
		replacement = []string{newLine}
	}
	e.lines = replaceLineRange(e.lines, target.start, target.end+1, replacement)
	return true
}

func (e *codexTomlEditor) appendMissingProjects(path string, existing map[string]any, managed map[string]any) error {
	managedProjects, ok := managed[config.CodexProjectsKey].(map[string]any)
	if !ok || len(managedProjects) == 0 {
		return nil
	}
	existingProjects, _ := existing[config.CodexProjectsKey].(map[string]any)
	projectKeys := make([]string, 0, len(managedProjects))
	for projectPath := range managedProjects {
		projectKeys = append(projectKeys, projectPath)
	}
	sort.Strings(projectKeys)
	for _, projectPath := range projectKeys {
		if _, exists := existingProjects[projectPath]; exists {
			continue
		}
		entry, ok := managedProjects[projectPath].(map[string]any)
		if !ok {
			return fmt.Errorf(messages.SyncCodexAgentSpecificProjectEntryTableFmt, projectPath)
		}
		// A new [projects."<path>"] header cannot extend a root inline-table
		// `projects = { ... }`; fail with the actionable shape message rather than
		// the opaque go-toml render-check error.
		if e.rootInlineTableExists(config.CodexProjectsKey) {
			return fmt.Errorf(messages.SyncCodexExistingConfigShapeConflictFmt, path, config.CodexProjectsKey)
		}
		e.appendBlock(renderProjectBlock(projectPath, entry))
	}
	return nil
}

// rootInlineTableExists reports whether top is defined as a single root-level
// assignment (an inline table such as `projects = { ... }`), which forbids a
// later `[top.<key>]` table header.
func (e *codexTomlEditor) rootInlineTableExists(top string) bool {
	found := false
	e.walkAssignments(func(info assignmentInfo) {
		if len(info.tablePath) == 0 && len(info.keyPath) == 1 && info.keyPath[0] == top {
			found = true
		}
	})
	return found
}

type assignmentInfo struct {
	start     int
	end       int
	fullPath  []string
	keyPath   []string
	tablePath []string
}

type lineRange struct {
	start int
	end   int
}

type literalValue struct {
	literal string
}

func assignmentKeyPath(line string, state tomlpatch.StringState) ([]string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "#") {
		return nil, false
	}
	commentPos, _ := tomlpatch.ScanLineForComment(trimmed, state)
	clean := trimmed
	if commentPos >= 0 {
		clean = strings.TrimSpace(trimmed[:commentPos])
	}
	left, _, ok := strings.Cut(clean, "=")
	if !ok {
		return nil, false
	}
	return tomlpatch.ParseKeyPath(strings.TrimSpace(left))
}

func extractNamespaceLines(content string, namespace []string) []string {
	editor := newCodexTomlEditor(content)
	ranges := editor.rangesForNamespace(namespace)
	ranges = mergeLineRanges(ranges)
	var out []string
	for _, r := range ranges {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, tomlpatch.TrimEmptyLines(editor.lines[r.start:r.end+1])...)
	}
	return out
}

func renderProjectBlock(projectPath string, entry map[string]any) []string {
	keys := make([]string, 0, len(entry))
	for key := range entry {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, 1+len(keys))
	lines = append(lines, "[projects."+tomlpatch.FormatKey(projectPath)+"]")
	for _, key := range keys {
		lines = append(lines, tomlpatch.FormatKey(key)+" = "+formatInlineValue(entry[key]))
	}
	return lines
}

func agentSpecificLeafValues(agentSpecific map[string]any) []codexPathValue {
	var out []codexPathValue
	collectLeafValues(nil, agentSpecific, &out)
	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i].path, "\x00") < strings.Join(out[j].path, "\x00")
	})
	return out
}

func collectLeafValues(prefix []string, value any, out *[]codexPathValue) {
	table, ok := value.(map[string]any)
	if !ok {
		*out = append(*out, codexPathValue{path: append([]string(nil), prefix...), value: value})
		return
	}
	keys := make([]string, 0, len(table))
	for key := range table {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		collectLeafValues(append(prefix, key), table[key], out)
	}
}

func codexPathHandledElsewhere(path []string) bool {
	if len(path) == 1 {
		return slices.Contains(codexManagedRootScalarKeys, path[0])
	}
	switch path[0] {
	case config.CodexProjectsKey, config.CodexMCPServersKey:
		return true
	case codexFeaturesKey:
		return slices.Contains(config.CodexKnownManagedFeatureKeys(), path[1])
	case codexTUIKey:
		return path[1] == codexStatusLineKey
	default:
		return false
	}
}

func valueAtPath(root map[string]any, path []string) (any, bool) {
	var current any = root
	for _, part := range path {
		table, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = table[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func setNestedValue(table map[string]any, path []string, value literalValue) {
	if len(path) == 1 {
		table[path[0]] = value
		return
	}
	next, ok := table[path[0]].(map[string]any)
	if !ok {
		next = map[string]any{}
	}
	table[path[0]] = next
	setNestedValue(next, path[1:], value)
}

func deleteNestedValue(table map[string]any, path []string) {
	if len(path) == 1 {
		delete(table, path[0])
		return
	}
	next, ok := table[path[0]].(map[string]any)
	if !ok {
		return
	}
	deleteNestedValue(next, path[1:])
	if len(next) == 0 {
		delete(table, path[0])
	}
}

func tomlLiteral(value any) (string, error) {
	if _, ok := value.(map[string]any); ok {
		return "", fmt.Errorf("cannot render TOML table as scalar literal")
	}
	return formatInlineValue(value), nil
}

func formatInlineValue(value any) string {
	switch v := value.(type) {
	case literalValue:
		return v.literal
	case string:
		return fmt.Sprintf("%q", v)
	case bool:
		return strconv.FormatBool(v)
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%v", v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, formatInlineValue(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, tomlpatch.FormatKey(key)+" = "+formatInlineValue(v[key]))
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func headerLines(header string) []string {
	return strings.Split(strings.TrimSuffix(header, "\n"), "\n")
}

func findLineSequence(lines []string, seq []string) (int, bool) {
	if len(seq) == 0 || len(seq) > len(lines) {
		return 0, false
	}
	for i := 0; i <= len(lines)-len(seq); i++ {
		if slices.Equal(lines[i:i+len(seq)], seq) {
			return i, true
		}
	}
	return 0, false
}

func replaceLineRange(lines []string, start int, end int, replacement []string) []string {
	out := make([]string, 0, len(lines)-(end-start)+len(replacement))
	out = append(out, lines[:start]...)
	out = append(out, replacement...)
	out = append(out, lines[end:]...)
	return out
}

func mergeLineRanges(ranges []lineRange) []lineRange {
	if len(ranges) == 0 {
		return nil
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].start == ranges[j].start {
			return ranges[i].end < ranges[j].end
		}
		return ranges[i].start < ranges[j].start
	})
	merged := []lineRange{ranges[0]}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if r.start <= last.end+1 {
			if r.end > last.end {
				last.end = r.end
			}
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

func trimTrailingBlankLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}

func collapseConsecutiveBlankLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	previousBlank := false
	for _, line := range lines {
		blank := strings.TrimSpace(line) == ""
		if blank && previousBlank {
			continue
		}
		out = append(out, line)
		previousBlank = blank
	}
	return out
}

func pathHasPrefix(path []string, prefix []string) bool {
	if len(path) < len(prefix) {
		return false
	}
	return slices.Equal(path[:len(prefix)], prefix)
}
