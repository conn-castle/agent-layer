package sync

import "testing"

// TestMergeAgentSpecificMap_NestedDeepMerge guards the key-preserving deep-merge
// recursion (agent_specific_merge.go: the branch where both managed and custom
// hold a map at the same key). Existing settings-merge tests only exercise
// one-level merges, so a regression that replaced the recursive merge with a
// wholesale overwrite would drop the managed nested key and still pass those
// tests. Here the managed nested map contributes "a" and the custom nested map
// contributes "b": both must survive, and an overridden leaf must take the
// custom value.
func TestMergeAgentSpecificMap_NestedDeepMerge(t *testing.T) {
	t.Parallel()
	managed := map[string]any{
		"permissions": map[string]any{
			"a":      1,
			"shared": "managed",
		},
	}
	custom := map[string]any{
		"permissions": map[string]any{
			"b":      2,
			"shared": "custom",
		},
	}

	merged := mergeAgentSpecificMap(managed, custom)

	perms, ok := merged["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions to be a map, got %#v", merged["permissions"])
	}
	if perms["a"] != 1 {
		t.Fatalf("expected managed nested key a=1 to be preserved, got %#v", perms["a"])
	}
	if perms["b"] != 2 {
		t.Fatalf("expected custom nested key b=2 to be present, got %#v", perms["b"])
	}
	if perms["shared"] != "custom" {
		t.Fatalf("expected overridden leaf shared=custom to win, got %#v", perms["shared"])
	}
}

// TestMergeAgentSpecificMap_DeepMergeClonesNestedValues confirms the merge does
// not alias the managed map's nested values into the result: mutating the
// managed source after merging must not change the merged output. A shallow
// copy here would let a later mutation of managed config leak into already-
// projected settings.
func TestMergeAgentSpecificMap_DeepMergeClonesNestedValues(t *testing.T) {
	t.Parallel()
	managedNested := map[string]any{"keep": "original"}
	managed := map[string]any{"section": managedNested}
	custom := map[string]any{"other": 1}

	merged := mergeAgentSpecificMap(managed, custom)

	// Mutate the original managed nested map after the merge.
	managedNested["keep"] = "mutated"

	section, ok := merged["section"].(map[string]any)
	if !ok {
		t.Fatalf("expected section map in merged output, got %#v", merged["section"])
	}
	if section["keep"] != "original" {
		t.Fatalf("expected merged value to be cloned (original), got %#v", section["keep"])
	}
}
