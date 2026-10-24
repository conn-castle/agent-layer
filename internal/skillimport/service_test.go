package skillimport

import (
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/gitrepo"
	"github.com/conn-castle/agent-layer/internal/skilllock"
)

func TestRetargetRecognizesChangedResolvedRef(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, ref, lockedResolved, resolved string }{
		{"default branch rename", "", "old-default", "new-default"},
		{"ASCII-padded omitted ref", " \t", "old-default", "new-default"},
		// Older locks recorded the Unicode-trimmed sibling of an explicit ref.
		{"literal Unicode ref", " v1", "v1", " v1"},
	} {
		block := config.SkillImport{Ref: tt.ref}
		locked := skilllock.Entry{ConfiguredRef: tt.ref, ResolvedRef: tt.lockedResolved}
		if !isRetarget(block, locked, gitrepo.Resolution{Ref: tt.resolved, Kind: skilllock.RefKindBranch}) {
			t.Fatalf("%s was not a retarget", tt.name)
		}
	}
	block := config.SkillImport{Ref: "v1"}
	locked := skilllock.Entry{ConfiguredRef: "v1", ResolvedRef: "v1"}
	if isRetarget(block, locked, gitrepo.Resolution{Ref: "v1", Kind: skilllock.RefKindTag}) {
		t.Fatal("an unchanged explicit ref was a retarget")
	}
}
