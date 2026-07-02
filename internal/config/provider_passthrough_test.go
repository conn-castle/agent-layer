package config

import "testing"

func TestCodexFeatureKeyLists_ReturnDefensiveCopies(t *testing.T) {
	t.Parallel()

	browserKeys := CodexBrowserFeatureKeys()
	browserKeys[0] = "mutated"
	if got := CodexBrowserFeatureKeys()[0]; got != "browser_use" {
		t.Fatalf("browser feature keys aliased internal storage, got %q", got)
	}

	knownKeys := CodexKnownManagedFeatureKeys()
	knownKeys[0] = "mutated"
	got := CodexKnownManagedFeatureKeys()
	want := []string{"apps", "plugins", "browser_use", "in_app_browser", "computer_use"}
	if len(got) != len(want) {
		t.Fatalf("known feature keys length = %d (%#v), want %d (%#v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("known feature key %d = %q, want %q; full=%#v", i, got[i], want[i], got)
		}
	}
}

func TestCodexManagedTopLevelKeys_IncludesSharedConfigOwnershipSet(t *testing.T) {
	t.Parallel()

	keys := CodexManagedTopLevelKeys()
	keys[0] = "mutated"
	got := CodexManagedTopLevelKeys()
	for _, want := range []string{
		CodexApprovalPolicyKey,
		CodexMCPServersKey,
		CodexModelKey,
		CodexReasoningEffortKey,
		CodexProjectsKey,
		CodexSandboxModeKey,
		CodexWebSearchKey,
	} {
		if !containsString(got, want) {
			t.Fatalf("expected managed key %q in %#v", want, got)
		}
	}
	if containsString(got, "mutated") {
		t.Fatalf("managed keys aliased caller mutation: %#v", got)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
