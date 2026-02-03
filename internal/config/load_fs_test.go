package config

import (
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestFSPathFromRoot_AbsoluteUnderRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "config.toml")

	got, err := fsPathFromRoot(root, path)
	if err != nil {
		t.Fatalf("fsPathFromRoot error: %v", err)
	}

	expected := pathpkg.Join(".agent-layer", "config.toml")
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
	if strings.Contains(got, "\\") {
		t.Fatalf("expected slash-separated fs path, got %q", got)
	}
	if !fs.ValidPath(got) {
		t.Fatalf("expected valid fs path, got %q", got)
	}
}

func TestFSPathFromRoot_RelativeNormalizesSeparators(t *testing.T) {
	input := filepath.Join(".agent-layer", "config.toml")
	got, err := fsPathFromRoot("ignored", input)
	if err != nil {
		t.Fatalf("fsPathFromRoot error: %v", err)
	}

	expected := pathpkg.Join(".agent-layer", "config.toml")
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
	if strings.Contains(got, "\\") {
		t.Fatalf("expected slash-separated fs path, got %q", got)
	}
}

func TestFSPathFromRoot_OutsideRoot(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()

	if _, err := fsPathFromRoot(root, other); err == nil {
		t.Fatalf("expected error for path outside root")
	}
}

func TestLoadProjectConfigFS_NilFilesystem(t *testing.T) {
	_, err := LoadProjectConfigFS(nil, "/valid/root")
	if err == nil {
		t.Fatalf("expected error for nil filesystem")
	}
	if err.Error() != messages.ConfigFSRequired {
		t.Fatalf("expected %q, got %q", messages.ConfigFSRequired, err.Error())
	}
}

func TestLoadProjectConfigFS_EmptyRoot(t *testing.T) {
	root := t.TempDir()
	fsys := os.DirFS(root)

	_, err := LoadProjectConfigFS(fsys, "")
	if err == nil {
		t.Fatalf("expected error for empty root")
	}
	if err.Error() != messages.ConfigRootRequired {
		t.Fatalf("expected %q, got %q", messages.ConfigRootRequired, err.Error())
	}
}

func TestReadFileFS_PathError(t *testing.T) {
	root := t.TempDir()
	fsys := os.DirFS(root)

	// Absolute path outside root should error
	_, err := readFileFS(fsys, root, "/other/path")
	if err == nil {
		t.Fatalf("expected error for path outside root")
	}
}

func TestReadDirFS_PathError(t *testing.T) {
	root := t.TempDir()
	fsys := os.DirFS(root)

	// Absolute path outside root should error
	_, err := readDirFS(fsys, root, "/other/path")
	if err == nil {
		t.Fatalf("expected error for path outside root")
	}
}

func TestFSPathFromRoot_AbsoluteEmptyRoot(t *testing.T) {
	// Absolute path with empty root should error
	_, err := fsPathFromRoot("", "/some/absolute/path")
	if err == nil {
		t.Fatalf("expected error for empty root with absolute path")
	}
}

func TestFSPathFromRoot_RelError(t *testing.T) {
	// On Windows, paths on different drives can't be made relative
	// On Unix, this is hard to trigger but we can test the code path
	// by using paths that would create ".." prefixes
	root := t.TempDir()
	other := t.TempDir()

	_, err := fsPathFromRoot(root, other)
	if err == nil {
		t.Fatalf("expected error for path outside root")
	}
}

type errorFS struct {
	fs.FS
	errPath string
	err     error
}

func (e errorFS) Open(name string) (fs.File, error) {
	if name == e.errPath {
		return nil, e.err
	}
	return e.FS.Open(name)
}

func TestLoadEnvFS_Invalid(t *testing.T) {
	fsys := fstest.MapFS{
		".agent-layer/.env": {Data: []byte("INVALID LINE")},
	}

	_, err := LoadEnvFS(fsys, "root", ".agent-layer/.env")
	if err == nil {
		t.Fatalf("expected error for invalid env file")
	}
}

func TestLoadInstructionsFS_NoMarkdown(t *testing.T) {
	fsys := fstest.MapFS{
		".agent-layer/instructions":            {Mode: fs.ModeDir},
		".agent-layer/instructions/readme":     {Data: []byte("no markdown")},
		".agent-layer/instructions/readme.txt": {Data: []byte("no markdown")},
	}

	_, err := LoadInstructionsFS(fsys, "root", ".agent-layer/instructions")
	if err == nil {
		t.Fatalf("expected error when no markdown files exist")
	}
}

func TestLoadInstructionsFS_ReadError(t *testing.T) {
	base := fstest.MapFS{
		".agent-layer/instructions":       {Mode: fs.ModeDir},
		".agent-layer/instructions/00.md": {Data: []byte("content")},
	}
	fsys := errorFS{
		FS:      base,
		errPath: ".agent-layer/instructions/00.md",
		err:     fs.ErrPermission,
	}

	_, err := LoadInstructionsFS(fsys, "root", ".agent-layer/instructions")
	if err == nil {
		t.Fatalf("expected error when instruction file cannot be read")
	}
}

func TestLoadSlashCommandsFS_InvalidCommand(t *testing.T) {
	fsys := fstest.MapFS{
		".agent-layer/slash-commands":        {Mode: fs.ModeDir},
		".agent-layer/slash-commands/bad.md": {Data: []byte("no front matter")},
	}

	_, err := LoadSlashCommandsFS(fsys, "root", ".agent-layer/slash-commands")
	if err == nil {
		t.Fatalf("expected error for invalid slash command")
	}
}

func TestLoadSlashCommandsFS_ReadError(t *testing.T) {
	base := fstest.MapFS{
		".agent-layer/slash-commands":        {Mode: fs.ModeDir},
		".agent-layer/slash-commands/cmd.md": {Data: []byte("---\ndescription: test\n---\n")},
	}
	fsys := errorFS{
		FS:      base,
		errPath: ".agent-layer/slash-commands/cmd.md",
		err:     fs.ErrPermission,
	}

	_, err := LoadSlashCommandsFS(fsys, "root", ".agent-layer/slash-commands")
	if err == nil {
		t.Fatalf("expected error when slash command file cannot be read")
	}
}

func TestLoadCommandsAllowFS_ScannerError(t *testing.T) {
	longLine := strings.Repeat("a", 70000)
	fsys := fstest.MapFS{
		".agent-layer/commands.allow": {Data: []byte(longLine)},
	}

	_, err := LoadCommandsAllowFS(fsys, "root", ".agent-layer/commands.allow")
	if err == nil {
		t.Fatalf("expected error for scanner overflow")
	}
}
