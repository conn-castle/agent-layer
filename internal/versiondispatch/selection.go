package versiondispatch

import "strings"

// SourceCurrent labels a selection that neither a pin nor AL_VERSION made.
const SourceCurrent = sourceCurrent

// ReleaseSelection resolves the CLI's pin/AL_VERSION policy without downloading
// or executing anything. source is SourceCurrent, "pin", or AL_VERSION; matches
// compares normalized versions, including dev.
func ReleaseSelection(root, currentVersion string) (requested string, source string, matches bool, err error) {
	current, err := normalizeCurrentVersion(currentVersion)
	if err != nil {
		return "", "", false, err
	}
	sys := RealSystem{}
	if strings.TrimSpace(sys.Getenv(EnvDevelopmentBypassVersionDispatch)) != "" &&
		(sys.Getenv(EnvDevelopmentSourceRoot) == "" || sameDevelopmentRoot(root, sys.Getenv(EnvDevelopmentSourceRoot))) {
		return current, sourceCurrent, true, nil
	}
	requested, source, _, _, _, err = resolveRequestedVersion(sys, root, true, current) //nolint:dogsled // CLI diagnostics are not needed for a freshness check.
	return requested, source, requested == current, err
}
