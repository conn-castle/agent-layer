package herdr

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const (
	requestNameKey  = "name"
	processNodeName = "node"
)

var (
	codexTitleID  = regexp.MustCompile(`(?i)(?:^|[^0-9a-f-])([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{5}(?:[0-9a-f]{7}|\.\.\.))`)
	codexThreadID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// codexTitleMatchesHook uses the first AL-projected identity token. Native
// title projection puts it before thread names, branch text and later aliases.
// The full resume ID comes from the event, never from reconstructing this token.
func codexTitleMatchesHook(title, hookID string) error {
	for _, match := range codexTitleID.FindAllStringSubmatchIndex(title, -1) {
		end := match[3]
		if end-match[2] == 36 && end < len(title) && strings.ContainsRune("0123456789abcdefABCDEF-", rune(title[end])) {
			continue
		}
		token := title[match[2]:end]
		if strings.EqualFold(token, hookID) || (strings.HasSuffix(token, "...") && strings.HasPrefix(strings.ToLower(hookID), strings.ToLower(strings.TrimSuffix(token, "...")))) {
			return nil
		}
		return errCodexPaneNoMatch
	}
	return errCodexPaneTitlePending
}

// codexRunRecords snapshots canonical launch records for live-pane recovery.
func codexRunRecords(root string) (map[string][]string, error) {
	return runRecordPaths(filepath.Join(root, ".agent-layer", "tmp", "runs"), func(name string) bool {
		return strings.HasSuffix(name, launchContextSuffix) && strings.HasPrefix(name, launchContextPrefix)
	})
}

func liveCodexLaunchRecords(root string, paths map[string][]string) ([]launchContext, error) {
	var records []launchContext
	for _, candidates := range paths {
		for _, path := range candidates {
			launch, live, err := readLiveCodexLaunchContext(path, root)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("read Codex HerdR launch record: %w", err)
			}
			if live {
				records = append(records, launch)
			}
		}
	}
	return records, nil
}

// codexContextsWithPaintWait waits only for the initial title paint. Report
// retries use the same matching function without waiting.
func codexContextsWithPaintWait(root string, records []launchContext, hookID string, deadline time.Time, noWait bool) ([]launchContext, bool, error) {
	if len(records) == 0 {
		return nil, false, nil
	}
	titleDisabled, err := codexProjectTitleDisabled(root)
	if err != nil {
		return nil, false, err
	}
	if titleDisabled {
		return nil, false, nil
	}

	// A first main prompt can arrive before its title is painted. A title that
	// already names another thread is definitive, and child/background events
	// must return immediately rather than delaying their hook by this paint wait.
	retryUntil := codexTitlePaintDeadline(deadline)
	for {
		matched := make([]launchContext, 0, len(records))
		pendingTitle := false
		for _, context := range records {
			err := codexContextMatchesPane(context, hookID, deadline)
			switch {
			case err == nil:
				matched = append(matched, context)
			case errors.Is(err, errCodexPaneTitlePending):
				pendingTitle = true
			case errors.Is(err, errCodexPaneNoMatch), errors.Is(err, errCodexPaneGone):
				// This owned launch is showing another conversation (or has closed).
			case err != nil:
				return nil, false, err
			}
		}
		if len(matched) > 0 {
			return matched, true, nil
		}
		if noWait || !pendingTitle || !time.Now().Add(100*time.Millisecond).Before(retryUntil) {
			return nil, false, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

var errCodexForeignAuthority = errors.New("HerdR pane is owned by another Codex recovery authority")
var errCodexPaneNoMatch = errors.New("recorded Codex HerdR pane does not own this hook thread")
var errCodexPaneGone = errors.New("recorded Codex HerdR pane is no longer present")
var errCodexPaneTitlePending = errors.New("recorded Codex HerdR pane title is not painted yet")

func codexPaneForLaunch(launch launchContext, deadline time.Time) (map[string]any, string, error) {
	socketPath, err := resolveContextSocket(launch)
	if err != nil {
		return nil, "", err
	}
	response, err := socketRequest(socketPath, map[string]any{
		"id": "agent-layer:codex-pane-list", requestMethodKey: "pane.list", requestParamsKey: map[string]any{},
	}, deadline)
	if err != nil {
		return nil, "", err
	}
	if code := responseError(response); code != "" {
		return nil, "", fmt.Errorf("HerdR rejected Codex pane list: %s", code)
	}
	result, _ := response["result"].(map[string]any)
	panes, _ := result["panes"].([]any)
	var pane map[string]any
	for _, value := range panes {
		candidate, _ := value.(map[string]any)
		if stringValue(candidate[requestPaneIDKey]) == launch.PaneID {
			pane = candidate
			break
		}
	}
	if pane == nil {
		return nil, "", errCodexPaneGone
	}
	return pane, socketPath, nil
}

func codexContextMatchesPane(launch launchContext, hookID string, deadline time.Time) error {
	pane, socketPath, err := codexPaneForLaunch(launch, deadline)
	if err != nil {
		return err
	}
	if err := codexTitleMatchesHook(stringValue(pane["terminal_title"]), hookID); err != nil {
		return err
	}
	return codexPaneHasForegroundLaunch(launch, pane, socketPath, deadline)
}

func codexTitlePaintDeadline(deadline time.Time) time.Time {
	retryUntil := time.Now().Add(3 * time.Second)
	if deadline.Before(retryUntil) {
		return deadline
	}
	return retryUntil
}

// codexProjectTitleDisabled reads the same project-local generated/native
// configuration that sync uses for its opt-out warning. It never consults the
// process CODEX_HOME, whose global state is unrelated to this owned launch.
func codexProjectTitleDisabled(root string) (bool, error) {
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		return false, fmt.Errorf("resolve HerdR project root: %w", err)
	}
	path := filepath.Join(canonicalRoot, ".codex", "config.toml")
	data, err := os.ReadFile(path) // #nosec G304 -- canonical project root and fixed Codex path.
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read Codex terminal title configuration: %w", err)
	}
	return CodexTitleDisabled(data)
}

// CodexTitleDisabled reads Codex's supported empty-list title preference.
func CodexTitleDisabled(configTOML []byte) (bool, error) {
	var document struct {
		TUI struct {
			TerminalTitle *[]string `toml:"terminal_title"`
		} `toml:"tui"`
	}
	if err := toml.Unmarshal(configTOML, &document); err != nil {
		return false, fmt.Errorf("decode Codex terminal title configuration: %w", err)
	}
	return document.TUI.TerminalTitle != nil && len(*document.TUI.TerminalTitle) == 0, nil
}

func codexPaneHasForegroundLaunch(launch launchContext, pane map[string]any, socketPath string, deadline time.Time) error {
	// A competing authority matters only for the pane that actually owns this
	// hook thread. Another pane's integration must not block its recovery.
	if agent, _ := pane["agent_session"].(map[string]any); agent != nil && strings.HasPrefix(stringValue(agent["source"]), "herdr:codex") {
		return errCodexForeignAuthority
	}
	process, err := socketRequest(socketPath, map[string]any{
		"id": "agent-layer:codex-process", requestMethodKey: "pane.process_info", requestParamsKey: map[string]any{requestPaneIDKey: launch.PaneID},
	}, deadline)
	if err != nil {
		return err
	}
	if code := responseError(process); code != "" {
		return fmt.Errorf("HerdR rejected Codex process lookup: %s", code)
	}
	info, _ := process["result"].(map[string]any)
	processInfo, _ := info["process_info"].(map[string]any)
	foreground, _ := processInfo["foreground_processes"].([]any)
	_, start, err := processLineage(launch.PID)
	if err != nil || start != launch.ProcessStart {
		return errCodexPaneGone
	}
	foregroundDescendant := false
	for _, value := range foreground {
		entry, _ := value.(map[string]any)
		pid, ok := entry["pid"].(float64)
		if !ok || !codexProcessDescendsFrom(int(pid), launch.PID) {
			continue
		}
		foregroundDescendant = true
		if codexForegroundProcess(entry) {
			return nil
		}
	}
	if foregroundDescendant {
		return errors.New("recorded Codex HerdR pane has an unrecognized foreground descendant")
	}
	return errCodexPaneNoMatch
}

func codexLiveSessionMatches(pane map[string]any, hookID string) bool {
	agent, _ := pane["agent_session"].(map[string]any)
	return agent != nil && stringValue(agent["source"]) == sourceCodexAL && stringValue(agent["agent"]) == providerCodex && stringValue(agent["kind"]) == "id" && stringValue(agent["value"]) == hookID
}

func codexForegroundProcess(entry map[string]any) bool {
	name := strings.ToLower(stringValue(entry[requestNameKey]))
	argv, _ := entry["argv"].([]any)
	values := anyStrings(argv)
	if name == providerCodex {
		return true
	}
	if name != processNodeName || len(values) < 2 {
		return false
	}
	for _, value := range values[1:] {
		base := strings.ToLower(filepath.Base(value))
		if base == providerCodex || base == "codex.js" {
			return true
		}
	}
	return false
}

func codexProcessDescendsFrom(pid, ancestor int) bool {
	for depth := 0; depth < maxAncestorDepth && pid > 1; depth++ {
		if pid == ancestor {
			return true
		}
		parent, _, err := processLineage(pid)
		if err != nil {
			return false
		}
		pid = parent
	}
	return false
}

func anyStrings(values []any) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
