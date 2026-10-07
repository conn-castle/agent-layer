package herdr

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pelletier/go-toml/v2"
)

const (
	codexControlSocket  = "app-server-control/app-server-control.sock"
	requestNameKey      = "name"
	processNodeName     = "node"
	codexTUIConfigKey   = "tui"
	codexTitleConfigKey = "terminal_title"
)

var (
	fullCodexTitleID      = regexp.MustCompile(`(?i)(?:^|[^0-9a-f-])([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(?:$|[^0-9a-f-])`)
	truncatedCodexTitleID = regexp.MustCompile(`(?i)(?:^|[^0-9a-f-])([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{5})\.\.\.`)
	codexThreadID         = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// codexLoadedThreadIDsFromPeer reads the managed daemon's loaded threads and
// returns the kernel-authenticated PID at the other end of the Unix socket.
// The socket name and owner establish the native rendezvous; the peer PID is
// what binds an individual hook invocation to that rendezvous.
func codexLoadedThreadIDsFromPeer(home string, deadline time.Time) ([]string, int, error) {
	socket := filepath.Join(home, codexControlSocket)
	resolved, err := filepath.EvalSymlinks(socket)
	if err != nil {
		return nil, 0, fmt.Errorf("resolve Codex managed control socket: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, 0, fmt.Errorf("stat Codex managed control socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return nil, 0, errors.New("codex managed control socket is not available")
	}
	if !validCodexManagedSocket(resolved, socket) {
		return nil, 0, fmt.Errorf("codex managed control socket resolves outside its native rendezvous: %s", resolved)
	}
	var peerPID int
	dialer := websocket.Dialer{NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		connection, err := (&net.Dialer{}).DialContext(ctx, "unix", resolved)
		if err != nil {
			return nil, err
		}
		peerPID, err = unixPeerPID(connection)
		if err != nil {
			_ = connection.Close()
			return nil, fmt.Errorf("identify Codex managed socket peer: %w", err)
		}
		return connection, nil
	}}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	conn, response, err := dialer.DialContext(ctx, "ws://localhost/", nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, 0, fmt.Errorf("connect Codex managed control socket: %w", err)
	}
	defer func() { _ = conn.Close() }()
	conn.SetReadLimit(1 << 20)
	if err := codexRPCWrite(conn, 1, "initialize", map[string]any{
		"clientInfo":   map[string]any{requestNameKey: "agent-layer-herdr", "version": "1"},
		"capabilities": map[string]any{"experimentalApi": true},
	}); err != nil {
		return nil, 0, err
	}
	if _, err := codexRPCRead(conn, 1, deadline); err != nil {
		return nil, 0, err
	}
	// initialize may take longer than the preceding request's write budget.
	// Each outbound message gets a fresh deadline, including notifications.
	if err := conn.SetWriteDeadline(time.Now().Add(750 * time.Millisecond)); err != nil {
		return nil, 0, err
	}
	if err := conn.WriteJSON(map[string]any{requestMethodKey: "initialized"}); err != nil {
		return nil, 0, fmt.Errorf("initialize Codex managed control socket: %w", err)
	}

	var ids []string
	var cursor string
	seenCursors := map[string]bool{}
	for page := 0; page < 100; page++ {
		params := map[string]any{"limit": 100}
		if cursor != "" {
			params["cursor"] = cursor
		}
		requestID := page + 2
		if err := codexRPCWrite(conn, requestID, "thread/loaded/list", params); err != nil {
			return nil, 0, err
		}
		result, err := codexRPCRead(conn, requestID, deadline)
		if err != nil {
			return nil, 0, err
		}
		data, ok := result["data"].([]any)
		if !ok {
			return nil, 0, errors.New("codex loaded-thread response has no data list")
		}
		seen := make(map[string]bool, len(ids)+len(data))
		for _, id := range ids {
			seen[id] = true
		}
		for _, value := range data {
			id, ok := value.(string)
			if !ok || !codexThreadID.MatchString(id) {
				return nil, 0, errors.New("codex loaded-thread response has an invalid ID")
			}
			if seen[id] {
				return nil, 0, errors.New("codex loaded-thread response repeats an ID")
			}
			seen[id] = true
			ids = append(ids, id)
		}
		next, present := result["nextCursor"]
		if !present || next == nil || next == "" {
			return ids, peerPID, nil
		}
		nextCursor, ok := next.(string)
		if !ok {
			return nil, 0, errors.New("codex loaded-thread response has an invalid cursor")
		}
		if seenCursors[nextCursor] {
			return nil, 0, errors.New("codex loaded-thread response repeats a cursor")
		}
		seenCursors[nextCursor] = true
		cursor = nextCursor
	}
	return nil, 0, errors.New("codex loaded-thread pagination exceeded 100 pages")
}

func codexRPCWrite(conn *websocket.Conn, id int, method string, params map[string]any) error {
	if err := conn.SetWriteDeadline(time.Now().Add(750 * time.Millisecond)); err != nil {
		return err
	}
	if err := conn.WriteJSON(map[string]any{"id": id, requestMethodKey: method, requestParamsKey: params}); err != nil {
		return fmt.Errorf("write Codex %s request: %w", method, err)
	}
	return nil
}

func codexRPCRead(conn *websocket.Conn, id int, deadline time.Time) (map[string]any, error) {
	if err := conn.SetReadDeadline(deadline); err != nil {
		return nil, err
	}
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("read Codex managed control response: %w", err)
		}
		var response map[string]any
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, fmt.Errorf("decode Codex managed control response: %w", err)
		}
		responseID, isResponse := response["id"].(float64)
		if !isResponse || int(responseID) != id {
			continue // native notifications are expected while another TUI is active.
		}
		if failure, ok := response["error"]; ok {
			return nil, fmt.Errorf("codex rejected managed control request: %v", failure)
		}
		result, ok := response["result"].(map[string]any)
		if !ok {
			return nil, errors.New("codex managed control response has no result")
		}
		return result, nil
	}
}

func codexThreadIDFromTitle(title string, loaded []string) (string, bool, error) {
	// Stock native emits either a full UUID or exactly the first 29 UUID
	// characters plus an ellipsis. Do not treat arbitrary short hex text in a
	// project/thread name as ownership evidence.
	matches := fullCodexTitleID.FindAllStringSubmatch(title, -1)
	truncated := truncatedCodexTitleID.FindAllStringSubmatch(title, -1)
	var match string
	for _, candidate := range matches {
		for _, id := range loaded {
			if strings.EqualFold(id, candidate[1]) {
				if match != "" && match != id {
					return "", false, errors.New("codex terminal title matches multiple loaded threads")
				}
				match = id
			}
		}
	}
	for _, candidate := range truncated {
		prefix := candidate[1]
		for _, id := range loaded {
			if codexThreadPrefixMatch(id, prefix) {
				if match != "" && match != id {
					return "", false, errors.New("codex terminal title matches multiple loaded threads")
				}
				match = id
			}
		}
	}
	return match, match != "", nil
}

// codexTitleCouldBeThread is intentionally cheaper than the managed list
// lookup. It avoids connecting to a recorded server when this pane visibly
// selected another conversation; only a possible match earns the stricter
// peer-credential and loaded-ID checks.
func codexTitleCouldBeThread(title, hookID string) bool {
	for _, candidate := range fullCodexTitleID.FindAllStringSubmatch(title, -1) {
		if strings.EqualFold(candidate[1], hookID) {
			return true
		}
	}
	for _, candidate := range truncatedCodexTitleID.FindAllStringSubmatch(title, -1) {
		if codexThreadPrefixMatch(hookID, candidate[1]) {
			return true
		}
	}
	return false
}

func codexThreadPrefixMatch(id, prefix string) bool {
	return strings.HasPrefix(strings.ToLower(id), strings.ToLower(prefix))
}

func codexTitleHasThreadToken(title string) bool {
	return fullCodexTitleID.MatchString(title) || truncatedCodexTitleID.MatchString(title)
}

// codexHookUsesManagedServer distinguishes an embedded hook beneath a launch
// from a hook beneath the native shared server that launch started. It never
// consults hook environment markers: only the recorded home and a kernel peer
// can establish this relationship.
func codexHookUsesManagedServer(launch launchContext, deadline time.Time) (bool, error) {
	advertised := filepath.Join(launch.CodexHome, codexControlSocket)
	if _, err := os.Lstat(advertised); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("inspect Codex managed control socket: %w", err)
	}
	probeDeadline := time.Now().Add(3 * time.Second)
	if deadline.Before(probeDeadline) {
		probeDeadline = deadline
	}
	_, peerPID, err := codexLoadedThreadIDsFromPeer(launch.CodexHome, probeDeadline)
	if err != nil {
		// A daemon that crashed before cleaning its rendezvous can leave a
		// dangling alias, and an advertised socket with no listener cannot be
		// this hook's live parent. Keep all other rendezvous/API/peer failures
		// loud rather than mistaking them for embedded Codex.
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return false, nil
		}
		return false, err
	}
	return codexProcessDescendsFrom(os.Getpid(), peerPID), nil
}

func validCodexManagedSocket(path, advertised string) bool {
	clean := filepath.Clean(path)
	directory := filepath.Dir(clean)
	if filepath.Base(clean) == "." || len(filepath.Base(clean)) != 64 {
		return false
	}
	for _, r := range filepath.Base(clean) {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	uid, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(directory), "codex-daemon-"))
	if err != nil || uid != os.Geteuid() || filepath.Base(directory) != "codex-daemon-"+strconv.Itoa(uid) {
		return false
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != uid {
		return false
	}
	// Codex canonicalizes the advertised socket parent before hashing the
	// basename. Hashing filepath.Clean alone would accept the wrong native
	// rendezvous for a symlinked CODEX_HOME.
	parent, err := filepath.EvalSymlinks(filepath.Dir(advertised))
	if err != nil {
		return false
	}
	advertised = filepath.Join(parent, filepath.Base(advertised))
	digest := sha256.Sum256([]byte(advertised))
	return fmt.Sprintf("%x", digest) == filepath.Base(clean)
}

// liveCodexLaunchRecords reads private launch records and retains only live
// Codex launches in this exact project, validating every retained owner.
func liveCodexLaunchRecords(root string) ([]launchContext, error) {
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		return nil, fmt.Errorf("resolve HerdR project root: %w", err)
	}
	liveDir := filepath.Join(canonicalRoot, ".agent-layer", "tmp", codexLiveLaunchIndexDir)
	entries, err := readDirFunc(liveDir)
	if os.IsNotExist(err) {
		return liveCodexLaunchRecordsFromRuns(canonicalRoot)
	}
	if err != nil {
		return nil, err
	}
	var records []launchContext
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, launchContextPrefix) || !strings.HasSuffix(name, launchContextSuffix) {
			continue
		}
		indexPath := filepath.Join(liveDir, name)
		recordPath, err := readCodexLiveLaunchIndex(canonicalRoot, indexPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(recordPath) != name {
			return nil, fmt.Errorf("codex live launch index name mismatch: %s", indexPath)
		}
		context, live, err := readLiveCodexDaemonLaunchContext(recordPath, canonicalRoot)
		if errors.Is(err, os.ErrNotExist) || (err == nil && !live) {
			_ = os.Remove(indexPath)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read Codex HerdR launch record: %w", err)
		}
		records = append(records, context)
	}
	return records, nil
}

func liveCodexLaunchRecordsFromRuns(canonicalRoot string) ([]launchContext, error) {
	runs := filepath.Join(canonicalRoot, ".agent-layer", "tmp", "runs")
	runDirs, err := readDirFunc(runs)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []launchContext
	for _, run := range runDirs {
		if !run.IsDir() {
			continue
		}
		entries, err := readDirFunc(filepath.Join(runs, run.Name()))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), launchContextPrefix) || !strings.HasSuffix(entry.Name(), launchContextSuffix) {
				continue
			}
			context, live, err := readLiveCodexDaemonLaunchContext(filepath.Join(runs, run.Name(), entry.Name()), canonicalRoot)
			if err != nil {
				return nil, fmt.Errorf("read Codex HerdR launch record: %w", err)
			}
			if !live {
				continue // legacy records remain harmless to ordinary recovery.
			}
			records = append(records, context)
		}
	}
	return records, nil
}

// resolveCodexDaemonContexts is intentionally narrower than ancestry based
// resolution: a daemon hook is not a child of the original al process. A
// candidate must still be a live exact launch record, its recorded HerdR pane,
// and the full thread recovered uniquely from that pane's live title.
func resolveCodexDaemonContexts(root, hookID string, deadline time.Time, selectedChild bool) ([]launchContext, bool, error) {
	records, err := liveCodexLaunchRecords(root)
	if err != nil {
		return nil, false, err
	}
	if len(records) == 0 {
		return nil, false, nil
	}
	titleDisabled, err := codexProjectTitleDisabled(root)
	if err != nil {
		return nil, false, err
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
				pendingTitle = !titleDisabled
			case errors.Is(err, errCodexPaneNoMatch), errors.Is(err, errCodexPaneGone):
				// This owned launch is showing another conversation (or has closed).
			case err != nil:
				return nil, false, err
			}
		}
		if len(matched) > 0 {
			return matched, true, nil
		}
		if selectedChild || !pendingTitle || !time.Now().Add(100*time.Millisecond).Before(retryUntil) {
			return nil, false, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// hasLiveCodexDaemonLaunch is used only when a native lifecycle event omits
// its required thread identity. It prevents a live owned daemon from turning
// that malformed event into a deceptive successful no-op.
func hasLiveCodexDaemonLaunch(root string) (bool, error) {
	records, err := liveCodexLaunchRecords(root)
	if err != nil {
		return false, err
	}
	return len(records) > 0, nil
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
	// The title is the selected-thread authority. A different title is an
	// ordinary non-match, not a reason to inspect another pane, server, or
	// route by argv. In particular, do this before contacting its daemon.
	title := stringValue(pane["terminal_title"])
	if !codexTitleCouldBeThread(title, hookID) {
		if !codexTitleHasThreadToken(title) {
			return errCodexPaneTitlePending
		}
		return errCodexPaneNoMatch
	}
	loaded, peerPID, err := codexLoadedThreadIDsFromPeer(launch.CodexHome, deadline)
	if err != nil {
		return err
	}
	if !codexProcessDescendsFrom(os.Getpid(), peerPID) {
		return errors.New("codex managed control server is not an ancestor of this hook")
	}
	id, found, err := codexThreadIDFromTitle(title, loaded)
	if err != nil {
		return err
	}
	if !found || !strings.EqualFold(id, hookID) {
		return errCodexPaneNoMatch
	}
	return codexPaneHasForegroundLaunch(launch, pane, socketPath, deadline)
}

// codexEmbeddedContextMatchesPane validates the original launch directly.
// Embedded Codex has no managed peer, so a live title, the recorded launch
// identity, and its foreground Codex process are the complete authority.
func codexEmbeddedContextMatchesPane(launch launchContext, hookID string, deadline time.Time) error {
	pane, socketPath, err := codexPaneForLaunch(launch, deadline)
	if err != nil {
		return err
	}
	if !codexTitleCouldBeThread(stringValue(pane["terminal_title"]), hookID) {
		if !codexTitleHasThreadToken(stringValue(pane["terminal_title"])) {
			return errCodexPaneTitlePending
		}
		return errCodexPaneNoMatch
	}
	return codexPaneHasForegroundLaunch(launch, pane, socketPath, deadline)
}

// codexEmbeddedContextMatchesPaneWithPaintWait gives only the first embedded
// report attempt the same bounded title-paint window as daemon recovery. Later
// retries must stop promptly when the foreground selection becomes pending.
func codexEmbeddedContextMatchesPaneWithPaintWait(launch launchContext, hookID string, deadline time.Time, titleDisabled bool) error {
	err := codexEmbeddedContextMatchesPane(launch, hookID, deadline)
	if titleDisabled || !errors.Is(err, errCodexPaneTitlePending) {
		return err
	}
	retryUntil := codexTitlePaintDeadline(deadline)
	for time.Now().Add(100 * time.Millisecond).Before(retryUntil) {
		time.Sleep(100 * time.Millisecond)
		err = codexEmbeddedContextMatchesPane(launch, hookID, deadline)
		if !errors.Is(err, errCodexPaneTitlePending) {
			return err
		}
	}
	return err
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
	var document map[string]any
	if err := toml.Unmarshal(data, &document); err != nil {
		return false, fmt.Errorf("decode Codex terminal title configuration: %w", err)
	}
	tuiValue, exists := document[codexTUIConfigKey]
	if !exists {
		return false, nil
	}
	tui, ok := tuiValue.(map[string]any)
	if !ok {
		return false, errors.New("codex tui configuration must be a table")
	}
	title, exists := tui[codexTitleConfigKey]
	if !exists {
		return false, nil
	}
	items, ok := title.([]any)
	if !ok {
		return false, errors.New("codex tui.terminal_title must be a list of non-empty title items")
	}
	if len(items) == 0 {
		return true, nil
	}
	for _, item := range items {
		name, ok := item.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return false, errors.New("codex tui.terminal_title must contain only non-empty title items")
		}
	}
	return false, nil
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
		return errors.New("recorded Codex launch PID is stale")
	}
	for _, value := range foreground {
		entry, _ := value.(map[string]any)
		pid, ok := entry["pid"].(float64)
		if !ok || !codexProcessDescendsFrom(int(pid), launch.PID) {
			continue
		}
		if codexForegroundProcess(entry) {
			return nil
		}
	}
	return errors.New("recorded Codex HerdR pane has no foreground Codex process")
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
