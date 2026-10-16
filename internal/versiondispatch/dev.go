package versiondispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigureDevelopmentEnvironment preserves explicit source provenance and
// makes nested al invocations select the same source path. Bypass alone never
// creates provenance from os.Executable (in particular, from go run).
func ConfigureDevelopmentEnvironment() error {
	if os.Getenv(EnvDevelopmentBypassVersionDispatch) == "" || os.Getenv(EnvDevelopmentSourceRoot) == "" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, _, err := (RealSystem{}).FindAgentLayerRoot(cwd)
	if err != nil {
		return err
	}
	path := DevelopmentExecutable(root)
	if path == "" {
		return nil
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("development executable must be absolute")
	}
	dir := filepath.Dir(path)
	inherited := os.Getenv("PATH")
	if inherited == dir || strings.HasPrefix(inherited, dir+string(os.PathListSeparator)) {
		return nil
	}
	return os.Setenv("PATH", dir+string(os.PathListSeparator)+inherited)
}

// DevelopmentExecutable returns source selection only for its explicitly named
// configuration root. A Make session inherited by another project cannot opt it in.
// Resolve symlinks so alternate spellings of the same root retain provenance.
func DevelopmentExecutable(root string) string {
	if strings.TrimSpace(os.Getenv(EnvDevelopmentBypassVersionDispatch)) == "" {
		return ""
	}
	selectedRoot := os.Getenv(EnvDevelopmentSourceRoot)
	if !filepath.IsAbs(root) || !filepath.IsAbs(selectedRoot) {
		return ""
	}
	if !sameDevelopmentRoot(root, selectedRoot) {
		return ""
	}
	return os.Getenv(EnvDevelopmentExecutable)
}

func sameDevelopmentRoot(a, b string) bool {
	if !filepath.IsAbs(a) || !filepath.IsAbs(b) {
		return false
	}
	canonical := func(path string) string {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved
		}
		return filepath.Clean(path)
	}
	return canonical(a) == canonical(b)
}
