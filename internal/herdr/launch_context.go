package herdr

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	launchContextPrefix    = "herdr-launch-"
	launchContextSuffix    = ".json"
	dispatchBoundaryPrefix = "herdr-dispatch-boundary-"
	maxAncestorDepth       = 8
)

// launchContext is deliberately a small, per-exec handoff record. It is not
// a saved environment or a "last launch" pointer: the hook may use it only
// when one of its current process ancestors has the recorded PID and start
// identity.
type launchContext struct {
	Version       int    `json:"version"`
	PID           int    `json:"pid"`
	ProcessStart  string `json:"process_start"`
	ProjectRoot   string `json:"project_root"`
	Provider      string `json:"provider"`
	SocketPath    string `json:"socket_path"`
	LaunchCWD     string `json:"launch_cwd,omitempty"`
	PaneID        string `json:"pane_id"`
	DevExecutable string `json:"dev_executable,omitempty"`
	DevBypass     bool   `json:"dev_bypass,omitempty"`
}

// CaptureLaunch binds session hooks to an AL terminal launch in this project.
// It stores identity only; recovery reapplies canonical project configuration.
func CaptureLaunch(root, runDir string, env []string, provider string) error {
	values := environment(env)
	if values[EnvEnabled] != "1" {
		return nil
	}
	if values[EnvSocketPath] == "" || values[EnvPaneID] == "" {
		return errors.New("HerdR launch context is incomplete (need HERDR_ENV=1, HERDR_SOCKET_PATH, and HERDR_PANE_ID)")
	}
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		return fmt.Errorf("resolve HerdR project root: %w", err)
	}
	canonicalRunDir, err := filepath.EvalSymlinks(runDir)
	if err != nil {
		return fmt.Errorf("resolve HerdR run directory: %w", err)
	}
	canonicalRunDir, err = filepath.Abs(canonicalRunDir)
	if err != nil {
		return fmt.Errorf("resolve HerdR run directory: %w", err)
	}
	if !pathWithin(canonicalRoot, canonicalRunDir) {
		return fmt.Errorf("HerdR run directory is outside the project root: %s", runDir)
	}
	info, err := os.Lstat(canonicalRunDir)
	if err != nil {
		return fmt.Errorf("stat HerdR run directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("HerdR run directory must be a real directory: %s", runDir)
	}
	pid := os.Getpid()
	_, start, err := processLineage(pid)
	if err != nil || start == "" {
		return fmt.Errorf("identify HerdR launch process %d: %w", pid, err)
	}
	context := launchContext{
		Version:      1,
		PID:          pid,
		ProcessStart: start,
		ProjectRoot:  canonicalRoot,
		Provider:     provider,
		SocketPath:   values[EnvSocketPath],
		PaneID:       values[EnvPaneID],
	}
	if !filepath.IsAbs(context.SocketPath) {
		cwd, err := canonicalDirectory(".")
		if err != nil {
			return fmt.Errorf("resolve HerdR launch working directory: %w", err)
		}
		context.LaunchCWD = cwd
	}
	if values[EnvDevBypass] != "" && filepath.IsAbs(values[EnvDevExecutable]) {
		devExecutable, err := filepath.EvalSymlinks(values[EnvDevExecutable])
		if err != nil {
			return fmt.Errorf("resolve HerdR development executable: %w", err)
		}
		devExecutable, err = filepath.Abs(devExecutable)
		if err != nil {
			return fmt.Errorf("resolve HerdR development executable: %w", err)
		}
		context.DevBypass = true
		context.DevExecutable = filepath.Clean(devExecutable)
	}
	if err := validateLaunchContext(context, canonicalRoot, provider); err != nil {
		return fmt.Errorf("invalid HerdR launch context: %w", err)
	}
	path := filepath.Join(canonicalRunDir, launchContextName(pid, start))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- runDir is validated under the project-owned temporary directory.
	if err != nil {
		return fmt.Errorf("create HerdR launch context: %w", err)
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(context); err != nil {
		_ = file.Close()
		return fmt.Errorf("write HerdR launch context: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close HerdR launch context: %w", err)
	}
	return nil
}

// WriteBoundary writes one private identity record before a native child is
// started. A sanitised hook stops at this ancestor instead of borrowing an
// older HerdR launch context. Dispatch invokes this explicitly because its
// normal launch environment intentionally strips AL_DISPATCH_ACTIVE.
func WriteBoundary(root, runDir string) error {
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		return fmt.Errorf("resolve HerdR project root: %w", err)
	}
	canonicalRunDir, err := filepath.EvalSymlinks(runDir)
	if err != nil {
		return fmt.Errorf("resolve HerdR run directory: %w", err)
	}
	canonicalRunDir, err = filepath.Abs(canonicalRunDir)
	if err != nil {
		return fmt.Errorf("resolve HerdR run directory: %w", err)
	}
	if !pathWithin(canonicalRoot, canonicalRunDir) {
		return fmt.Errorf("HerdR run directory is outside the project root: %s", runDir)
	}
	info, err := os.Lstat(canonicalRunDir)
	if err != nil {
		return fmt.Errorf("stat HerdR run directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("HerdR run directory must be a real directory: %s", runDir)
	}
	pid := os.Getpid()
	_, start, err := processLineage(pid)
	if err != nil || start == "" {
		return fmt.Errorf("identify HerdR dispatch boundary %d: %w", pid, err)
	}
	path := filepath.Join(canonicalRunDir, dispatchBoundaryName(pid, start))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- runDir is validated under the project-owned temporary directory.
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create HerdR dispatch boundary: %w", err)
	}
	if _, err := fmt.Fprintf(file, "%d %s\n", pid, start); err != nil {
		_ = file.Close()
		return fmt.Errorf("write HerdR dispatch boundary: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close HerdR dispatch boundary: %w", err)
	}
	return nil
}

func launchContextName(pid int, start string) string {
	return launchContextPrefix + launchGeneration(pid, start) + launchContextSuffix
}

// launchGeneration is bounded, non-secret metadata derived from the same
// process identity that names a private launch record. It makes one Muse
// resume command distinct from a prior native exec without creating another
// state authority.
func launchGeneration(pid int, start string) string {
	digest := sha256.Sum256([]byte(start))
	return strconv.Itoa(pid) + "-" + hex.EncodeToString(digest[:8])
}

func dispatchBoundaryName(pid int, start string) string {
	return dispatchBoundaryPrefix + launchGeneration(pid, start) + launchContextSuffix
}

func resolveLaunchContext(root, provider string) (launchContext, bool, error) {
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		return launchContext{}, false, fmt.Errorf("resolve HerdR project root: %w", err)
	}
	runsDir := filepath.Join(canonicalRoot, ".agent-layer", "tmp", "runs")
	type ancestor struct {
		pid   int
		start string
	}
	ancestors := make([]ancestor, 0, maxAncestorDepth)
	names := map[string]bool{}
	pid := os.Getpid()
	for depth := 0; depth < maxAncestorDepth && pid > 1; depth++ {
		parent, start, err := processLineage(pid)
		if err != nil {
			return launchContext{}, false, fmt.Errorf("inspect HerdR hook ancestry: %w", err)
		}
		ancestors = append(ancestors, ancestor{pid, start})
		names[dispatchBoundaryName(pid, start)] = true
		names[launchContextName(pid, start)] = true
		pid = parent
	}
	// Scan each existing run once, rather than once for every ancestor and
	// record kind. Records remain in their canonical owning run directory.
	records, err := runRecordPaths(runsDir, names)
	if err != nil {
		return launchContext{}, false, fmt.Errorf("find HerdR launch records: %w", err)
	}
	for _, ancestor := range ancestors {
		if matches := records[dispatchBoundaryName(ancestor.pid, ancestor.start)]; len(matches) > 0 {
			if err := readDispatchBoundary(matches[0], ancestor.pid, ancestor.start); err != nil {
				return launchContext{}, false, err
			}
			return launchContext{}, false, nil
		}
		for _, path := range records[launchContextName(ancestor.pid, ancestor.start)] {
			context, err := readLaunchContext(path, canonicalRoot, provider)
			if err != nil {
				return launchContext{}, false, err
			}
			if context.Provider != provider || context.ProjectRoot != canonicalRoot {
				return launchContext{}, false, nil
			}
			if context.PID == ancestor.pid && context.ProcessStart == ancestor.start {
				return context, true, nil
			}
		}
	}
	return launchContext{}, false, nil
}

func runRecordPaths(runsDir string, names map[string]bool) (map[string][]string, error) {
	paths := map[string][]string{}
	entries, err := os.ReadDir(runsDir)
	if os.IsNotExist(err) {
		return paths, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(runsDir, entry.Name())
		records, err := os.ReadDir(directory)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			if names[record.Name()] {
				paths[record.Name()] = append(paths[record.Name()], filepath.Join(directory, record.Name()))
			}
		}
	}
	return paths, nil
}

func readDispatchBoundary(path string, pid int, start string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat HerdR dispatch boundary: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("invalid HerdR dispatch boundary: %s", path)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path was selected from the validated project run directory.
	if err != nil {
		return fmt.Errorf("read HerdR dispatch boundary: %w", err)
	}
	if string(data) != fmt.Sprintf("%d %s\n", pid, start) {
		return fmt.Errorf("invalid HerdR dispatch boundary identity: %s", path)
	}
	return nil
}

func readLaunchContext(path, root, provider string) (launchContext, error) {
	runsDir := filepath.Join(root, ".agent-layer", "tmp", "runs")
	relative, err := filepath.Rel(runsDir, path)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || strings.Contains(relative, string(os.PathSeparator)+".."+string(os.PathSeparator)) {
		return launchContext{}, fmt.Errorf("HerdR launch context is outside the project run directory: %s", path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return launchContext{}, fmt.Errorf("stat HerdR launch context: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return launchContext{}, fmt.Errorf("HerdR launch context must be a regular file: %s", path)
	}
	runInfo, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return launchContext{}, fmt.Errorf("stat HerdR launch context directory: %w", err)
	}
	if !runInfo.IsDir() || runInfo.Mode()&os.ModeSymlink != 0 {
		return launchContext{}, fmt.Errorf("HerdR launch context directory must be real: %s", filepath.Dir(path))
	}
	if info.Mode().Perm()&0o077 != 0 {
		return launchContext{}, fmt.Errorf("HerdR launch context must be private: %s", path)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path was selected from the validated project run directory.
	if err != nil {
		return launchContext{}, fmt.Errorf("read HerdR launch context: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var context launchContext
	if err := decoder.Decode(&context); err != nil {
		return launchContext{}, fmt.Errorf("decode HerdR launch context: %w", err)
	}
	if context.Provider != provider || context.ProjectRoot != root {
		return context, nil
	}
	if err := validateLaunchContext(context, root, provider); err != nil {
		return launchContext{}, fmt.Errorf("invalid HerdR launch context: %w", err)
	}
	return context, nil
}

func validateLaunchContext(context launchContext, root, provider string) error {
	if context.Version != 1 || context.PID <= 1 || context.ProcessStart == "" || context.Provider != provider || context.ProjectRoot != root {
		return errors.New("unexpected identity")
	}
	if !validSocketPath(context.SocketPath, context.LaunchCWD, root) || !safePaneID(context.PaneID) {
		return errors.New("invalid HerdR socket or pane")
	}
	if context.DevBypass != (context.DevExecutable != "") {
		return errors.New("incomplete development executable")
	}
	if context.DevExecutable != "" {
		if !filepath.IsAbs(context.DevExecutable) {
			return errors.New("development executable is not absolute")
		}
		canonicalExecutable, err := filepath.EvalSymlinks(context.DevExecutable)
		if err != nil {
			return fmt.Errorf("resolve development executable: %w", err)
		}
		canonicalExecutable, err = filepath.Abs(canonicalExecutable)
		if err != nil || filepath.Clean(canonicalExecutable) != context.DevExecutable {
			return errors.New("development executable is not canonical")
		}
		info, err := os.Stat(canonicalExecutable)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("development executable is not a regular file")
		}
	}
	return nil
}

func validSocketPath(socketPath, launchCWD, root string) bool {
	if socketPath == "" || strings.ContainsAny(socketPath, "\x00\r\n") {
		return false
	}
	if filepath.IsAbs(socketPath) {
		return launchCWD == ""
	}
	if launchCWD == "" || !filepath.IsAbs(launchCWD) || !pathWithin(root, launchCWD) {
		return false
	}
	resolved := filepath.Clean(filepath.Join(launchCWD, socketPath))
	return pathWithin(root, resolved)
}

func resolveContextSocket(context launchContext) (string, error) {
	if filepath.IsAbs(context.SocketPath) {
		return context.SocketPath, nil
	}
	target := filepath.Join(context.LaunchCWD, context.SocketPath)
	if !filepath.IsAbs(target) {
		return "", errors.New("relative HerdR socket did not resolve to an absolute path")
	}
	hookCWD, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("read HerdR hook working directory: %w", err)
	}
	canonicalHookCWD, err := canonicalDirectory(hookCWD)
	if err != nil {
		return "", fmt.Errorf("resolve HerdR hook working directory: %w", err)
	}
	path, err := filepath.Rel(canonicalHookCWD, target)
	if err != nil || path == "." {
		return "", errors.New("relative HerdR socket could not be resolved from hook working directory")
	}
	return path, nil
}

func canonicalDirectory(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("not a directory")
	}
	return filepath.Clean(canonical), nil
}

func safePaneID(value string) bool {
	return value != "" && len(value) <= 200 && !strings.ContainsAny(value, "\x00\r\n")
}
