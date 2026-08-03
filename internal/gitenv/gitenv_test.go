package gitenv

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestWithoutDiscoveryDropsOnlyRedirectingVariables(t *testing.T) {
	// The point of the filter is that git resolves the repository from the
	// directory it is given. Dropping too little re-introduces the redirect;
	// dropping too much silently discards credentials and config the caller set.
	kept := []string{
		"GIT_AUTHOR_NAME=keep me",
		"GIT_SSH_COMMAND=ssh -i key",
		"GIT_CONFIG_GLOBAL=/tmp/gitconfig",
		"PATH=/usr/bin",
		"MALFORMED_ENTRY_WITHOUT_EQUALS",
	}
	// Build the input from the canonical list so a variable added to it later is
	// covered here without anyone remembering to extend this test.
	environ := make([]string, 0, len(discoveryVariables)+len(kept))
	for _, name := range discoveryVariables {
		environ = append(environ, name+"=/elsewhere")
	}
	environ = append(environ, kept...)
	got := withoutDiscovery(environ)

	for _, name := range discoveryVariables {
		if slices.ContainsFunc(got, func(entry string) bool { return strings.HasPrefix(entry, name+"=") }) {
			t.Errorf("%s survived; git would still resolve a different repository", name)
		}
	}
	for _, entry := range kept {
		if !slices.Contains(got, entry) {
			t.Errorf("%q was dropped; only discovery variables should be removed", entry)
		}
	}
}

func TestWithoutDiscoveryReadsTheProcessEnvironment(t *testing.T) {
	t.Setenv("GIT_DIR", "/elsewhere/.git")
	t.Setenv("GITENV_SENTINEL", "present")

	got := WithoutDiscovery()
	if slices.ContainsFunc(got, func(entry string) bool { return strings.HasPrefix(entry, "GIT_DIR=") }) {
		t.Error("GIT_DIR survived WithoutDiscovery")
	}
	if !slices.Contains(got, "GITENV_SENTINEL=present") {
		t.Error("WithoutDiscovery must return the rest of the process environment")
	}
	if os.Getenv("GIT_DIR") != "/elsewhere/.git" {
		t.Error("WithoutDiscovery must not mutate the process environment")
	}
}
