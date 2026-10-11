package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstructionSchemaRequiresOneFileAndUniqueScalarOrder(t *testing.T) {
	raw := `[[instructions.imports]]
repository = "local-repo"
selectors = ["instructions/rules.md"]
order = 0
[[instructions.imports]]
repository = "local-repo"
selectors = ["instructions/memory.md"]
order = 10
[[instructions.local]]
selectors = ["conventions.md"]
order = 40
`
	cfg, err := ParseConfigLenient([]byte(raw), "test")
	require.NoError(t, err)
	require.NoError(t, validateInstructions("test", cfg.Instructions))
	for _, bad := range []string{
		strings.Replace(raw, "order = 0\n", "", 1), strings.Replace(raw, "order = 10", "order = 0", 1),
		strings.Replace(raw, "order = 40", "order = -1", 1), strings.Replace(raw, "order = 40", "order = [40]", 1),
		strings.Replace(raw, "order = 40", "order = 1.5", 1), strings.Replace(raw, `["conventions.md"]`, `["a.md", "b.md"]`, 1),
		strings.Replace(raw, `"conventions.md"`, `"*.md"`, 1), strings.Replace(raw, `"conventions.md"`, `"rules.md"`, 1),
		strings.Replace(raw, `"conventions.md"`, `"ｒｕｌｅｓ.md"`, 1),
	} {
		cfg, err := ParseConfigLenient([]byte(bad), "test")
		if err == nil {
			err = validateInstructions("test", cfg.Instructions)
		}
		require.Error(t, err, bad)
	}
}

func TestLegacyOrderPreviewPreservesBytesAndRefusesLinks(t *testing.T) {
	root := t.TempDir()
	paths := DefaultPaths(root)
	require.NoError(t, os.MkdirAll(paths.InstructionsDir, 0o750))
	for name, body := range map[string]string{"00_rules.md": "custom rules\r\n", "20_custom.md": "custom project bytes\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(paths.InstructionsDir, name), []byte(body), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(paths.InstructionsDir, "notes.txt"), []byte("ignored"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(paths.InstructionsDir, "archive.md"), 0o750))
	raw := "# untouched comment\n"
	_, err := MigrateInstructionOrder(raw, []string{"rules.md", "Rules.md"})
	require.ErrorContains(t, err, "duplicate instruction filename")
	next, err := MigrateInstructionOrderFS(os.DirFS(root), root, raw)
	require.NoError(t, err)
	require.Contains(t, next, "order = 0")
	require.Contains(t, next, "order = 10")
	files, err := ParseConfigLenient([]byte(next), "test")
	require.NoError(t, err)
	loaded, err := LoadOrderedInstructionsFS(os.DirFS(root), root, files.Instructions)
	require.NoError(t, err)
	require.Equal(t, "custom rules\r\n", loaded[0].Content)
	require.Equal(t, "20_custom.md", loaded[1].Name)
	require.Len(t, loaded, 2)
	require.NoError(t, os.WriteFile(filepath.Join(paths.InstructionsDir, "team.md"), []byte("declare me"), 0o600))
	_, err = LoadOrderedInstructionsFS(os.DirFS(root), root, files.Instructions)
	require.ErrorContains(t, err, "[[instructions.local]]")
	require.NoError(t, os.Remove(filepath.Join(paths.InstructionsDir, "team.md")))
	_, err = os.Stat(paths.ConfigPath)
	require.True(t, os.IsNotExist(err), "preview must not write")
	require.NoError(t, os.Symlink(filepath.Join(root, "external"), filepath.Join(paths.InstructionsDir, "linked.md")))
	_, err = MigrateInstructionOrderFS(os.DirFS(root), root, raw)
	require.ErrorContains(t, err, "unlinked")
}

func TestInstructionEditorAdoptsOrderAndRejectsHiddenDeclarations(t *testing.T) {
	order := 40
	block := SkillImport{Repository: "local-repo", Selectors: []string{"instructions/rules.md"}, Ref: "release", Tracking: SkillTrackingPinned, WritePolicy: SkillWritePolicyBranch, PushRepository: "fork-repo", PushBranch: "updates"}
	prefix := "# keep this comment\n[dispatch]\nmax_depth = 3\n"
	local := "[[instructions.local]]\nselectors = [\"00_rules.md\"]\norder = 0\n"
	next, err := AddInstructionImport(prefix+"\n"+local, block, &order, "00_rules.md")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(next, prefix))
	cfg, err := ParseConfigLenient([]byte(next), "test")
	require.NoError(t, err)
	require.Empty(t, cfg.Instructions.Local)
	require.Equal(t, 0, *cfg.Instructions.Imports[0].Order)
	require.Equal(t, block, cfg.Instructions.Imports[0].SkillImport)
	removed, err := SetInstructionImport(next, block, nil, true)
	require.NoError(t, err)
	require.Equal(t, prefix, removed)
	noNewline := strings.TrimSuffix(prefix, "\n")
	added, err := SetInstructionImport(noNewline, block, &order, false)
	require.NoError(t, err)
	require.False(t, strings.HasSuffix(added, "\n"))
	removed, err = SetInstructionImport(added, block, nil, true)
	require.NoError(t, err)
	require.Equal(t, noNewline, removed)
	_, err = AddInstructionImport(prefix, block, &order, "00_rules.md")
	require.ErrorContains(t, err, "needs explicit ordering")
	_, err = AddInstructionImport(local, block, new(int), "")
	require.ErrorContains(t, err, "duplicate instruction order")
	_, err = AddInstructionImport(local, SkillImport{Repository: "local-repo"}, &order, "")
	require.ErrorContains(t, err, "exactly one selector")

	for _, hidden := range []string{
		strings.Replace(local, "instructions.local", `instructions."local"`, 1),
		"[instructions]\nlocal = [{ selectors = [\"00_rules.md\"], order = 0 }]\n",
	} {
		_, _, err := RemoveLocalInstruction(hidden, "00_rules.md")
		require.ErrorContains(t, err, "[[instructions.local]] header")
	}
	for _, hidden := range []string{
		strings.Replace(next, "instructions.imports", `instructions."imports"`, 1),
		"[instructions]\nimports = [{ repository = \"local-repo\", selectors = [\"instructions/rules.md\"], ref = \"release\", tracking = \"pinned\", write_policy = \"branch\", push_repository = \"fork-repo\", push_branch = \"updates\", order = 0 }]\n",
	} {
		for _, remove := range []bool{false, true} {
			_, err := SetInstructionImport(hidden, block, &order, remove)
			require.ErrorContains(t, err, "[[instructions.imports]] header")
		}
	}
}
