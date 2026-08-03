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
	environ := []string{
		"GIT_DIR=/elsewhere/.git",
		"GIT_WORK_TREE=/elsewhere",
		"GIT_INDEX_FILE=/elsewhere/.git/index",
		"GIT_PREFIX=sub/",
		"GIT_AUTHOR_NAME=keep me",
		"GIT_SSH_COMMAND=ssh -i key",
		"GIT_CONFIG_GLOBAL=/tmp/gitconfig",
		"PATH=/usr/bin",
		"MALFORMED_ENTRY_WITHOUT_EQUALS",
	}
	got := withoutDiscovery(environ)

	for _, dropped := range []string{"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_PREFIX="} {
		if slices.ContainsFunc(got, func(entry string) bool { return strings.HasPrefix(entry, dropped) }) {
			t.Errorf("%s survived; git would still resolve a different repository", dropped)
		}
	}
	for _, kept := range []string{
		"GIT_AUTHOR_NAME=keep me",
		"GIT_SSH_COMMAND=ssh -i key",
		"GIT_CONFIG_GLOBAL=/tmp/gitconfig",
		"PATH=/usr/bin",
		"MALFORMED_ENTRY_WITHOUT_EQUALS",
	} {
		if !slices.Contains(got, kept) {
			t.Errorf("%q was dropped; only discovery variables should be removed", kept)
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
