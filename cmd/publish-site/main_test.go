package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateTagFormat(t *testing.T) {
	cases := []struct {
		name    string
		tag     string
		wantErr bool
	}{
		{"valid", "v1.2.3", false},
		{"valid prerelease", "v1.2.3-rc.1", false},
		{"missing v", "1.2.3", true},
		{"missing patch", "v1.2", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTagFormat(tc.tag)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.tag)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.tag, err)
			}
		})
	}
}

func TestStripV(t *testing.T) {
	if got := stripV("v1.2.3"); got != "1.2.3" {
		t.Fatalf("stripV returned %q", got)
	}
	if got := stripV("1.2.3"); got != "1.2.3" {
		t.Fatalf("stripV returned %q", got)
	}
}

func TestParseAvailableCommands(t *testing.T) {
	help := strings.Join([]string{
		"Agent Layer CLI",
		"",
		"Available Commands:",
		"  init        Initialize Agent Layer",
		"  sync        Regenerate configs",
		"",
		"Flags:",
		"  -h, --help   help",
	}, "\n")
	cmds := parseAvailableCommands(help)
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}
	if cmds[0] != "init" || cmds[1] != "sync" {
		t.Fatalf("unexpected commands: %v", cmds)
	}
}

func TestParseVersion(t *testing.T) {
	v, err := parseVersion("1.2.3-rc.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.major != 1 || v.minor != 2 || v.patch != 3 || v.prerelease != "rc.1" {
		t.Fatalf("unexpected version: %+v", v)
	}

	if _, err := parseVersion("1.2"); err == nil {
		t.Fatal("expected error for invalid version")
	}

	if _, err := parseVersion("a.b.c"); err == nil {
		t.Fatal("expected error for non-numeric version")
	}
}

func TestNormalizeVersionsJSON(t *testing.T) {
	repo := t.TempDir()
	versionsPath := filepath.Join(repo, "versions.json")
	data := []string{"0.2.0", "0.1.0", "0.2.0", "0.2.0-rc.1", "0.10.0"}
	payload, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(versionsPath, payload, 0o644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}

	if err := normalizeVersionsJSON(repo); err != nil {
		t.Fatalf("normalize: %v", err)
	}

	out, err := os.ReadFile(versionsPath)
	if err != nil {
		t.Fatalf("read versions.json: %v", err)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"0.10.0", "0.2.0", "0.2.0-rc.1", "0.1.0"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected order: %v", got)
	}
}

func TestEnsureIdempotentVersion(t *testing.T) {
	repo := t.TempDir()
	version := "1.2.3"

	versionedDocs := filepath.Join(repo, "versioned_docs", "version-"+version)
	if err := os.MkdirAll(versionedDocs, 0o755); err != nil {
		t.Fatalf("mkdir versioned_docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(versionedDocs, "doc.mdx"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	versionedSidebars := filepath.Join(repo, "versioned_sidebars")
	if err := os.MkdirAll(versionedSidebars, 0o755); err != nil {
		t.Fatalf("mkdir sidebars: %v", err)
	}
	if err := os.WriteFile(filepath.Join(versionedSidebars, "version-"+version+"-sidebars.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("write sidebar: %v", err)
	}

	versionsPath := filepath.Join(repo, "versions.json")
	payload, err := json.Marshal([]string{version, "0.1.0"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(versionsPath, payload, 0o644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}

	if err := ensureIdempotentVersion(repo, version); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	if _, err := os.Stat(versionedDocs); !os.IsNotExist(err) {
		t.Fatalf("expected versioned docs removed")
	}
	if _, err := os.Stat(filepath.Join(versionedSidebars, "version-"+version+"-sidebars.json")); !os.IsNotExist(err) {
		t.Fatalf("expected versioned sidebar removed")
	}

	out, err := os.ReadFile(versionsPath)
	if err != nil {
		t.Fatalf("read versions.json: %v", err)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 || got[0] != "0.1.0" {
		t.Fatalf("unexpected versions: %v", got)
	}
}

func TestEnsureIdempotentVersion_NoVersionsJSON(t *testing.T) {
	repo := t.TempDir()
	version := "1.2.3"

	versionedDocs := filepath.Join(repo, "versioned_docs", "version-"+version)
	if err := os.MkdirAll(versionedDocs, 0o755); err != nil {
		t.Fatalf("mkdir versioned_docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(versionedDocs, "doc.mdx"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	if err := ensureIdempotentVersion(repo, version); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	if _, err := os.Stat(versionedDocs); !os.IsNotExist(err) {
		t.Fatalf("expected versioned docs removed")
	}
}

func TestCopyTree(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.WriteFile(filepath.Join(dst, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected old file to be removed")
	}
	data, err := os.ReadFile(filepath.Join(dst, "nested", "file.txt"))
	if err != nil {
		t.Fatalf("read copied file: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

func TestValidateRepoBRootErrors(t *testing.T) {
	if err := validateRepoBRoot(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected error for missing repo")
	}

	repo := t.TempDir()
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := validateRepoBRoot(repo); err == nil {
		t.Fatal("expected error for missing .git")
	}

	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := validateRepoBRoot(repo); err == nil {
		t.Fatal("expected error for missing required files")
	}

	for _, name := range []string{"package.json", "docusaurus.config.js", "sidebars.js"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := validateRepoBRoot(repo); err == nil {
		t.Fatal("expected error for missing src/pages")
	}
}

func TestRepoRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/test"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	nested := filepath.Join(repo, "nested", "dir")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	got, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	gotEval, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	repoEval, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("eval symlinks repo: %v", err)
	}
	if gotEval != repoEval {
		t.Fatalf("expected %q, got %q", repoEval, gotEval)
	}
}

func TestNormalizeVersionsJSON_Missing(t *testing.T) {
	repo := t.TempDir()
	if err := normalizeVersionsJSON(repo); err == nil {
		t.Fatal("expected error for missing versions.json")
	}
}

func TestRunGoCmd(t *testing.T) {
	origExec := execCommandContext
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=TestHelperProcess", "--"}, append([]string{name}, args...)...)...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		return cmd
	}
	defer func() {
		execCommandContext = origExec
	}()

	out, err := runGoCmd(t.TempDir(), "ldflags", "--help")
	if err != nil {
		t.Fatalf("runGoCmd: %v", err)
	}
	if out != "ok" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestRun(t *testing.T) {
	repoA := t.TempDir()
	repoB := t.TempDir()

	if err := os.WriteFile(filepath.Join(repoA, "go.mod"), []byte("module example.com/test"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	sitePages := filepath.Join(repoA, "site", "pages")
	siteDocs := filepath.Join(repoA, "site", "docs")
	if err := os.MkdirAll(sitePages, 0o755); err != nil {
		t.Fatalf("mkdir site pages: %v", err)
	}
	if err := os.MkdirAll(siteDocs, 0o755); err != nil {
		t.Fatalf("mkdir site docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sitePages, "index.mdx"), []byte("# Home"), 0o644); err != nil {
		t.Fatalf("write page: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(siteDocs, "reference"), 0o755); err != nil {
		t.Fatalf("mkdir docs reference: %v", err)
	}
	if err := os.WriteFile(filepath.Join(siteDocs, "reference", "stub.mdx"), []byte("stub"), 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(repoB, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	for _, name := range []string{"package.json", "docusaurus.config.js", "sidebars.js"} {
		if err := os.WriteFile(filepath.Join(repoB, name), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(repoB, "src", "pages"), 0o755); err != nil {
		t.Fatalf("mkdir src/pages: %v", err)
	}

	origArgs := append([]string{}, os.Args...)
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, origArgs[0], append([]string{"-test.run=TestHelperProcess", "--"}, append([]string{name}, args...)...)...)
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		return cmd
	}
	runGoCmdFunc = func(repoA, ldflags string, args ...string) (string, error) {
		if len(args) == 1 && args[0] == "--help" {
			return strings.Join([]string{
				"Agent Layer CLI",
				"",
				"Available Commands:",
				"  init        Initialize Agent Layer",
				"  doctor      Run health checks",
				"",
				"Flags:",
			}, "\n"), nil
		}
		return "usage", nil
	}
	defer func() {
		execCommandContext = exec.CommandContext
		runGoCmdFunc = runGoCmd
		os.Args = origArgs
	}()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()
	if err := os.Chdir(repoA); err != nil {
		t.Fatalf("chdir repoA: %v", err)
	}

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	os.Args = []string{"cmd", "--tag", "v0.1.0", "--repo-b-dir", repoB}

	if err := run(); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(repoB, "src", "pages", "index.mdx")); err != nil {
		t.Fatalf("expected pages copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoB, "docs", "reference", "cli.mdx")); err != nil {
		t.Fatalf("expected cli reference: %v", err)
	}
}

func TestMainError(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestMainHelper", "--")
	cmd.Env = append(os.Environ(), "GO_WANT_MAIN=1")
	if err := cmd.Run(); err == nil {
		t.Fatal("expected non-zero exit")
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	idx := 0
	for i, arg := range args {
		if arg == "--" {
			idx = i + 1
			break
		}
	}
	if idx == 0 || idx >= len(args) {
		os.Exit(1)
	}

	cmd := args[idx]
	cmdArgs := args[idx+1:]
	if cmd == "go" {
		_, _ = os.Stdout.WriteString("ok")
		os.Exit(0)
	}
	if cmd != "npx" {
		os.Exit(1)
	}
	if len(cmdArgs) < 2 || cmdArgs[0] != "docusaurus" || cmdArgs[1] != "docs:version" {
		os.Exit(1)
	}
	if len(cmdArgs) < 3 {
		os.Exit(1)
	}
	version := cmdArgs[2]

	data, err := json.Marshal([]string{version})
	if err != nil {
		os.Exit(1)
	}
	if err := os.WriteFile("versions.json", data, 0o644); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestMainHelper(t *testing.T) {
	if os.Getenv("GO_WANT_MAIN") != "1" {
		return
	}
	main()
}
