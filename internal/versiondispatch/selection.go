package versiondispatch

import "strings"

// ReleaseSelection resolves the CLI's pin/AL_VERSION policy without downloading
// or executing anything. matches compares normalized versions, including dev.
func ReleaseSelection(root, currentVersion string) (requested string, matches bool, err error) {
	current, err := normalizeCurrentVersion(currentVersion)
	if err != nil {
		return "", false, err
	}
	sys := RealSystem{}
	if strings.TrimSpace(sys.Getenv(EnvDevelopmentBypassVersionDispatch)) != "" &&
		(sys.Getenv(EnvDevelopmentSourceRoot) == "" || sameDevelopmentRoot(root, sys.Getenv(EnvDevelopmentSourceRoot))) {
		return current, true, nil
	}
	requested, _, _, _, _, err = resolveRequestedVersion(sys, root, true, current) //nolint:dogsled // CLI diagnostics are not needed for a freshness check.
	return requested, requested == current, err
}
