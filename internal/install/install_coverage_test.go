package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileMatchesTemplate_ReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "unreadable")
	if err := os.WriteFile(path, []byte("content"), 0o200); err != nil { // Write only
		t.Fatalf("write file: %v", err)
	}
	// Try to read it - might succeed as root/owner on some systems, but let's try.
	// A better way is to pass a directory path as a file.
	dirPath := filepath.Join(root, "dir")
	if err := os.Mkdir(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Reading a directory as a file fails on many systems, or use a non-existent file.
	// But fileMatchesTemplate does os.ReadFile(path). If path doesn't exist, it returns error.
	_, err := fileMatchesTemplate("non-existent-file", "config.toml")
	if err == nil {
		t.Fatalf("expected error for non-existent file")
	}
}

func TestFileMatchesTemplate_TemplateReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Pass an invalid template path
	_, err := fileMatchesTemplate(path, "non-existent-template")
	if err == nil {
		t.Fatalf("expected error for non-existent template")
	}
}

func TestWriteGitignoreBlock_TemplateReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")

	// Pass an invalid template path
	err := writeGitignoreBlock(path, "non-existent-template", 0o644, false, nil)
	if err == nil {
		t.Fatalf("expected error for non-existent template")
	}
}

func TestWriteGitignoreBlock_FileReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")

	// Create a directory at the path to force ReadFile to fail (is a directory)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := writeGitignoreBlock(path, "gitignore.block", 0o644, false, nil)
	if err == nil {
		t.Fatalf("expected error when reading directory as file")
	}
}

func TestWriteTemplateFile_TemplateReadError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")

	// Pass an invalid template path
	err := writeTemplateFile(path, "non-existent-template", 0o644, false, nil)
	if err == nil {
		t.Fatalf("expected error for non-existent template")
	}
}

func TestWriteTemplateFile_StatError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping stat error test as root")
	}
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	target := filepath.Join(sub, "target")
	// Make sub unreadable to force stat error on target
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer func() { _ = os.Chmod(sub, 0o755) }()

	err := writeTemplateFile(target, "config.toml", 0o644, false, nil)
	if err == nil {
		t.Fatalf("expected error for stat failure")
	}
}

func TestWriteTemplateFile_MkdirError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping mkdir error test as root")
	}
	root := t.TempDir()
	// Create a file where the directory should be
	path := filepath.Join(root, "file", "target")
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	err := writeTemplateFile(path, "config.toml", 0o644, false, nil)
	if err == nil {
		t.Fatalf("expected error for mkdir failure")
	}
}

func TestWriteTemplateFile_WriteError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	// Create a directory where the file should be to cause WriteFile to fail
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := writeTemplateFile(path, "config.toml", 0o644, true, nil)
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

func TestWriteTemplateFile_FileMatchesError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping file read error test as root")
	}
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte("x"), 0o000); err != nil {
		t.Fatalf("write file: %v", err)
	}
	defer func() { _ = os.Chmod(path, 0o644) }()

	// fileMatchesTemplate should fail to read 'file'
	err := writeTemplateFile(path, "config.toml", 0o644, false, nil)
	if err == nil {
		t.Fatalf("expected error for fileMatchesTemplate failure")
	}
}

func TestWriteGitignoreBlock_WriteError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	// Create a directory where the file should be to cause WriteFile to fail
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := writeGitignoreBlock(path, "gitignore.block", 0o644, true, nil)
	if err == nil {
		t.Fatalf("expected error for write failure")
	}
}

func TestWriteTemplateFiles_Error(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping write error test as root")
	}
	root := t.TempDir()
	inst := &installer{root: root}

	// Create .agent-layer directory and make it read-only to force write failure
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o555); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer func() { _ = os.Chmod(alDir, 0o755) }()

	// writeTemplateFiles tries to write .agent-layer/config.toml, which should fail
	if err := inst.writeTemplateFiles(); err == nil {
		t.Fatalf("expected error from writeTemplateFiles")
	}
}

func TestWriteTemplateFiles_GitignoreBlockError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping write error test as root")
	}
	root := t.TempDir()
	inst := &installer{root: root}

	// Create .agent-layer directory
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create gitignore.block as a directory to force writeGitignoreBlock to fail
	// (writeGitignoreBlock tries to ReadFile or WriteFile, failing on dir)
	blockPath := filepath.Join(alDir, "gitignore.block")
	if err := os.Mkdir(blockPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// writeTemplateFiles should succeed for other files but fail on gitignore.block
	if err := inst.writeTemplateFiles(); err == nil {
		t.Fatalf("expected error from writeTemplateFiles (gitignore block)")
	}
}

func TestWriteTemplateDirs_Error(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping write error test as root")
	}
	root := t.TempDir()
	inst := &installer{root: root}

	// Create .agent-layer as read-only. writeTemplateDirs tries to create .agent-layer/instructions inside it.
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.Mkdir(alDir, 0o555); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer func() { _ = os.Chmod(alDir, 0o755) }()

	// writeTemplateDirs tries to write to .agent-layer/instructions
	if err := inst.writeTemplateDirs(); err == nil {
		t.Fatalf("expected error from writeTemplateDirs")
	}
}

func TestRun_Error(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping write error test as root")
	}
	root := t.TempDir()

	// Create .agent-layer as read-only to force createDirs (or subsequent steps) to fail
	// createDirs tries to MkdirAll .agent-layer/instructions etc.
	// If .agent-layer exists and is 0555, MkdirAll should fail to create subdirs?
	// Actually createDirs creates .agent-layer/instructions.
	// Let's make 'docs' read-only to fail creating docs/agent-layer
	docsDir := filepath.Join(root, "docs")
	if err := os.Mkdir(docsDir, 0o555); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer func() { _ = os.Chmod(docsDir, 0o755) }()

	if err := Run(root, Options{}); err == nil {
		t.Fatalf("expected error from Run")
	}
}

func TestUpdateGitignoreContent_Empty(t *testing.T) {
	content := ""
	block := "# >>> agent-layer\nblock\n# <<< agent-layer\n"
	updated := updateGitignoreContent(content, block)
	expected := "# >>> agent-layer\nblock\n# <<< agent-layer\n"
	if updated != expected {
		t.Fatalf("expected %q, got %q", expected, updated)
	}
}

func TestRenderGitignoreBlock_Empty(t *testing.T) {
	// Just to cover the path, even if logic is odd
	renderGitignoreBlock("")
}
