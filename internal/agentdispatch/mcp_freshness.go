package agentdispatch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/executableimage"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

// releaseSelection is a release-mode version selection and its provenance.
type releaseSelection struct {
	version string
	source  string
	matches bool
	err     error
	// installed and installedInfo identify AL_INSTALLED_EXECUTABLE for an
	// unpinned server that version dispatch started from a cached path, so a
	// replaced global al is still detected.
	installed     string
	installedInfo os.FileInfo
}

// selectRelease resolves the selection the way a fresh server launch would.
func selectRelease(root, version string) releaseSelection {
	selection := releaseSelection{}
	selection.version, selection.source, selection.matches, selection.err = versiondispatch.ReleaseSelection(root, version)
	if selection.err == nil && selection.source == versiondispatch.SourceCurrent && os.Getenv(versiondispatch.EnvShimActive) != "" {
		// Version dispatch started this unpinned server from a cached path that
		// never changes, so only the installed entry reveals a global update.
		// Without it freshness cannot be verified.
		installed := os.Getenv(versiondispatch.EnvInstalledExecutable)
		if installed == "" {
			selection.err = fmt.Errorf("an unpinned server started by version dispatch cannot verify the installed al: %s is unset", versiondispatch.EnvInstalledExecutable)
			return selection
		}
		selection.installed = installed
		selection.installedInfo, selection.err = os.Stat(installed) // #nosec G703 -- the installed entry is only stat-ed to compare identity.
	}
	return selection
}

// checkFreshness runs before discovery, configuration or provider access. Work
// already admitted keeps this image; durable control operations remain available.
func (s *dispatchToolServer) checkFreshness() error {
	var err error
	selection := versiondispatch.DevelopmentExecutable(s.root)
	switch {
	case s.development.snapshot:
		err = s.development.fresh()
	case selection != "":
		err = executableimage.SameAs(selection)
	default:
		current := selectRelease(s.root, s.version)
		selection = current.version
		err = s.start.fresh(current)
	}
	if err == nil {
		return nil
	}
	return fmt.Errorf("agent-layer MCP server is stale or freshness cannot be verified (running %s, selected %q): %w. No provider work was started. Reconnect the agent-layer MCP server (Claude Code: /mcp reconnect; Codex app: restart the app or reload MCP configuration), then retry. Wait, inspect, output and cancel remain available", s.version, selection, err)
}

// checkProviderWork guards operations that regenerate project launchers and hooks.
// Snapshot options remain readable when the selected dev source is unavailable,
// but start and continue must not project that unusable path into client config.
func (s *dispatchToolServer) checkProviderWork() error {
	if err := s.checkFreshness(); err != nil {
		return err
	}
	if err := versiondispatch.ValidateDevelopmentExecutable(s.root); err != nil {
		return fmt.Errorf("%w. No provider work was started and project projections were not changed. Restore that executable, or restore project projections and reconnect the agent-layer MCP server, then retry. Options, wait, inspect, output and cancel remain available", err)
	}
	return nil
}

// fresh reports whether a server started with this selection still runs what a
// fresh launch would select. A pin or AL_VERSION selection runs from an
// immutable cached image, so replacing the installed al keeps it fresh; any
// change of selection or provenance, and any unpinned image change, is stale.
func (start releaseSelection) fresh(current releaseSelection) error {
	switch {
	case start.err != nil:
		return fmt.Errorf("selection at server start: %w", start.err)
	case current.err != nil:
		return current.err
	case !current.matches:
		return fmt.Errorf("project selects %s", current.version)
	case current.version != start.version || current.source != start.source:
		return fmt.Errorf("selection changed from %s (%s) to %s (%s) since the server started", start.version, start.source, current.version, current.source)
	}
	if err := executableimage.Unchanged(); err != nil {
		return err
	}
	if current.source != versiondispatch.SourceCurrent {
		return nil
	}
	if versiondispatch.RunningFromImageCache() {
		return errors.New("an unpinned server runs from the image cache")
	}
	if start.installedInfo != nil {
		if err := executableimage.SameFile(start.installedInfo, start.installed); err != nil {
			return fmt.Errorf("installed %s: %w", start.installed, err)
		}
	}
	return nil
}

// developmentSelection binds a verified snapshot to the dev file selected at
// startup. No handoff marker leaks into providers or their nested al commands.
type developmentSelection struct {
	path     string
	info     os.FileInfo
	snapshot bool
	err      error
}

// selectDevelopment verifies that the running cache image matches the selected dev build at startup.
func selectDevelopment(root string, snapshot bool) developmentSelection {
	d := developmentSelection{path: versiondispatch.DevelopmentExecutable(root)}
	if d.path == "" || !snapshot {
		return d
	}
	// Treat an unverifiable image-cache launch as an error, never as a
	// successful snapshot just because its path lies somewhere in images/.
	d.snapshot = true
	build := currentBuildIdentity()
	if build.SHA256 == "" || filepath.Base(build.Executable) != "al" || filepath.Base(filepath.Dir(build.Executable)) != build.SHA256 {
		d.err = errors.New("running snapshot identity cannot be verified")
		return d
	}
	d.info, d.err = verifyDevelopmentExecutable(d.path, build.SHA256)
	return d
}

// verifyDevelopmentExecutable binds a regular executable's identity to the startup snapshot.
func verifyDevelopmentExecutable(path, sha string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := fsutil.RequireExecutable(path, info); err != nil {
		return nil, err
	}
	sum, err := imageSHA256(path)
	switch {
	case err != nil:
		return nil, err
	case sum != sha:
		return nil, errors.New("development executable changed during server startup")
	}
	if err := executableimage.SameFile(info, path); err != nil {
		return nil, fmt.Errorf("development executable changed during server startup: %w", err)
	}
	return info, nil
}

// fresh accepts a verified startup selection while the server's own snapshot stays intact.
// Options can report selected-path changes; projection-writing operations check source usability separately.
func (d developmentSelection) fresh() error {
	if d.err != nil {
		return d.err
	}
	if d.info == nil {
		return errors.New("development selection cannot be verified")
	}
	return executableimage.Unchanged()
}

// changed compares the selected dev file's current identity and mode with startup evidence.
// Missing or unusable paths count as changed without opening or hashing them.
func (d developmentSelection) changed() bool {
	if d.info == nil {
		return false
	}
	if executableimage.SameFile(d.info, d.path) != nil {
		return true
	}
	info, err := os.Stat(d.path)
	return err != nil || d.info.Mode() != info.Mode()
}

// serverCode reports the running code and any change to its selected development source.
func (s *dispatchToolServer) serverCode() *ServerCode {
	build := currentBuildIdentity()
	code := &ServerCode{Version: s.version, SHA256: build.SHA256, Snapshot: s.snapshot}
	if s.development.path != "" {
		unusable := versiondispatch.ValidateDevelopmentExecutable(s.root) != nil
		replaced := unusable || s.development.changed()
		code.Development = &DevelopmentCode{Executable: s.development.path, Replaced: replaced}
		if unusable {
			code.Development.Notice = "Selected development executable is missing or unusable; start and continue are refused until it is restored, or project projections are restored and the agent-layer MCP server is reconnected."
		} else if replaced {
			code.Development.Notice = "Development executable changed since the server started; reconnect the agent-layer MCP server to run the current build."
		}
	}
	return code
}
