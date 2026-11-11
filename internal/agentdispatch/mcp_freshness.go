package agentdispatch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/executableimage"
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
		err = s.development.fresh(selection)
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

func selectDevelopment(root string) developmentSelection {
	d := developmentSelection{path: versiondispatch.DevelopmentExecutable(root)}
	if d.path == "" || !versiondispatch.RunningFromImageCache() {
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
	d.info, d.err = os.Stat(d.path) // #nosec G703 -- selected development path, identity comparison only.
	if d.err != nil {
		return d
	}
	sum, err := imageSHA256(d.path)
	switch {
	case err != nil:
		d.err = err
	case sum != build.SHA256:
		d.err = errors.New("running snapshot differs from the development executable selected at startup")
	default:
		d.err = executableimage.SameFile(d.info, d.path)
	}
	return d
}

func (d developmentSelection) fresh(path string) error {
	if d.err != nil {
		return d.err
	}
	if path != d.path || d.info == nil {
		return errors.New("development selection changed or cannot be verified")
	}
	// A missing/unreadable development file is not evidence of a rebuild.
	info, err := os.Stat(path) // #nosec G703 -- identity and executable-mode check only.
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return errors.New("selected development executable is not an executable file")
	}
	return executableimage.Unchanged()
}

func (s *dispatchToolServer) serverCode() *ServerCode {
	build := currentBuildIdentity()
	code := &ServerCode{Version: s.version, SHA256: build.SHA256, Snapshot: versiondispatch.RunningFromImageCache()}
	if s.development.path != "" {
		replaced := s.development.info != nil && executableimage.SameFile(s.development.info, s.development.path) != nil
		code.Development = &DevelopmentCode{Executable: s.development.path, Replaced: replaced}
		if replaced {
			code.Development.Notice = "Development executable replaced; reconnect the agent-layer MCP server to run the new build."
		}
	}
	return code
}
