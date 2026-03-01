package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestValidateTagFormat(t *testing.T) {
	cases := []struct {
		name    string
		tag     string
		wantErr bool
	}{
		{"valid", "v1.2.3", false},
		{"invalid prerelease", "v1.2.3-rc.1", true},
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

	if _, err := parseVersion("1.2.3a"); err == nil {
		t.Fatal("expected error for version with invalid patch")
	}

	if _, err := parseVersion("1.2.3-rc/1"); err == nil {
		t.Fatal("expected error for prerelease with invalid character")
	}

	if _, err := parseVersion("1.2.3-rc..1"); err == nil {
		t.Fatal("expected error for prerelease with empty identifier")
	}
}

func TestComparePrerelease(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{"numeric identifier ordering", "rc.10", "rc.2", 1},
		{"shorter is lower precedence", "alpha", "alpha.1", -1},
		{"numeric < non-numeric", "alpha.1", "alpha.beta", -1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := comparePrerelease(tc.a, tc.b)
			switch tc.want {
			case -1:
				if got >= 0 {
					t.Fatalf("expected %q < %q, got %d", tc.a, tc.b, got)
				}
			case 0:
				if got != 0 {
					t.Fatalf("expected %q == %q, got %d", tc.a, tc.b, got)
				}
			case 1:
				if got <= 0 {
					t.Fatalf("expected %q > %q, got %d", tc.a, tc.b, got)
				}
			default:
				t.Fatalf("invalid want: %d", tc.want)
			}
		})
	}
}

func TestNormalizeVersionsJSON(t *testing.T) {
	repo := t.TempDir()
	versionsPath := filepath.Join(repo, "versions.json")
	data := []string{
		"0.8.3",
		"0.8.7",
		"0.8.6",
		"0.8.5",
		"0.8.4",
		"0.8.2",
		"0.8.1",
		"0.8.0",
		"0.7.0",
		"0.6.1",
		"0.6.0",
		"0.8.7",
	}
	payload, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(versionsPath, payload, 0o644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}

	for _, v := range []string{
		"0.8.7", "0.8.6", "0.8.5", "0.8.4", "0.8.3", "0.8.2", "0.8.1", "0.8.0", "0.7.0", "0.6.1", "0.6.0",
	} {
		docsDir := filepath.Join(repo, "versioned_docs", "version-"+v)
		if err := os.MkdirAll(docsDir, 0o755); err != nil {
			t.Fatalf("mkdir docs dir for %s: %v", v, err)
		}
		if err := os.WriteFile(filepath.Join(docsDir, "doc.mdx"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write docs file for %s: %v", v, err)
		}

		sidebarsDir := filepath.Join(repo, "versioned_sidebars")
		if err := os.MkdirAll(sidebarsDir, 0o755); err != nil {
			t.Fatalf("mkdir sidebars dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sidebarsDir, "version-"+v+"-sidebars.json"), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write sidebar for %s: %v", v, err)
		}
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
	want := []string{"0.8.7", "0.8.6", "0.8.5", "0.8.4", "0.7.0", "0.6.1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected order: %v", got)
	}

	for _, v := range []string{"0.8.3", "0.8.2", "0.8.1", "0.8.0", "0.6.0"} {
		if _, err := os.Stat(filepath.Join(repo, "versioned_docs", "version-"+v)); !os.IsNotExist(err) {
			t.Fatalf("expected dropped docs artifact removed for %s", v)
		}
		if _, err := os.Stat(filepath.Join(repo, "versioned_sidebars", "version-"+v+"-sidebars.json")); !os.IsNotExist(err) {
			t.Fatalf("expected dropped sidebar artifact removed for %s", v)
		}
	}

	for _, v := range []string{"0.8.7", "0.8.6", "0.8.5", "0.8.4", "0.7.0", "0.6.1"} {
		if _, err := os.Stat(filepath.Join(repo, "versioned_docs", "version-"+v)); err != nil {
			t.Fatalf("expected retained docs artifact for %s: %v", v, err)
		}
		if _, err := os.Stat(filepath.Join(repo, "versioned_sidebars", "version-"+v+"-sidebars.json")); err != nil {
			t.Fatalf("expected retained sidebar artifact for %s: %v", v, err)
		}
	}
}

func TestSelectRetainedVersions_SparseHistory(t *testing.T) {
	sorted := []string{"1.3.2", "1.3.1", "1.2.0", "1.1.5"}

	retained, dropped, err := selectRetainedVersions(sorted)
	if err != nil {
		t.Fatalf("selectRetainedVersions: %v", err)
	}

	if strings.Join(retained, ",") != strings.Join(sorted, ",") {
		t.Fatalf("unexpected retained versions: %v", retained)
	}
	if len(dropped) != 0 {
		t.Fatalf("expected no dropped versions, got %v", dropped)
	}
}

func TestSelectRetainedVersions_DropsPrereleases(t *testing.T) {
	sorted := []string{
		"1.5.2",
		"1.5.2-rc.2",
		"1.5.2-rc.1",
		"1.5.1",
		"1.5.0",
		"1.4.9",
		"1.3.8",
		"1.2.7",
	}

	retained, dropped, err := selectRetainedVersions(sorted)
	if err != nil {
		t.Fatalf("selectRetainedVersions: %v", err)
	}

	wantRetained := []string{"1.5.2", "1.5.1", "1.5.0", "1.4.9", "1.3.8", "1.2.7"}
	if strings.Join(retained, ",") != strings.Join(wantRetained, ",") {
		t.Fatalf("unexpected retained versions: %v", retained)
	}

	wantDropped := []string{"1.5.2-rc.2", "1.5.2-rc.1"}
	if strings.Join(dropped, ",") != strings.Join(wantDropped, ",") {
		t.Fatalf("unexpected dropped versions: %v", dropped)
	}
}

func TestSelectRetainedVersions_PrereleaseOnly(t *testing.T) {
	if _, _, err := selectRetainedVersions([]string{"1.2.3-rc.1"}); err == nil || !strings.Contains(err.Error(), "no stable releases") {
		t.Fatalf("expected no stable releases error, got %v", err)
	}
}

func TestNormalizeVersionsJSON_Idempotent(t *testing.T) {
	repo := t.TempDir()
	versions := []string{
		"0.8.7",
		"0.8.6",
		"0.8.5",
		"0.8.4",
		"0.8.3",
		"0.7.0",
		"0.6.1",
	}
	data, err := json.Marshal(versions)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	versionsPath := filepath.Join(repo, "versions.json")
	if err := os.WriteFile(versionsPath, data, 0o644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}

	for _, v := range []string{"0.8.7", "0.8.3"} {
		docsDir := filepath.Join(repo, "versioned_docs", "version-"+v)
		if err := os.MkdirAll(docsDir, 0o755); err != nil {
			t.Fatalf("mkdir docs dir for %s: %v", v, err)
		}
		if err := os.WriteFile(filepath.Join(docsDir, "doc.mdx"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write docs file for %s: %v", v, err)
		}

		sidebarsDir := filepath.Join(repo, "versioned_sidebars")
		if err := os.MkdirAll(sidebarsDir, 0o755); err != nil {
			t.Fatalf("mkdir sidebars dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sidebarsDir, "version-"+v+"-sidebars.json"), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write sidebar for %s: %v", v, err)
		}
	}

	if err := normalizeVersionsJSON(repo); err != nil {
		t.Fatalf("first normalize: %v", err)
	}
	firstOutput, err := os.ReadFile(versionsPath)
	if err != nil {
		t.Fatalf("read first versions.json: %v", err)
	}

	if err := normalizeVersionsJSON(repo); err != nil {
		t.Fatalf("second normalize: %v", err)
	}
	secondOutput, err := os.ReadFile(versionsPath)
	if err != nil {
		t.Fatalf("read second versions.json: %v", err)
	}

	if string(firstOutput) != string(secondOutput) {
		t.Fatalf("expected idempotent output; first=%q second=%q", firstOutput, secondOutput)
	}
	if _, err := os.Stat(filepath.Join(repo, "versioned_docs", "version-0.8.3")); !os.IsNotExist(err) {
		t.Fatalf("expected dropped docs artifact removed after rerun")
	}
	if _, err := os.Stat(filepath.Join(repo, "versioned_sidebars", "version-0.8.3-sidebars.json")); !os.IsNotExist(err) {
		t.Fatalf("expected dropped sidebar artifact removed after rerun")
	}
}

func TestPruneDroppedVersionArtifacts_RemoveSidebarError(t *testing.T) {
	repo := t.TempDir()
	sidebarPath := filepath.Join(repo, "versioned_sidebars", "version-1.2.3-sidebars.json")
	if err := os.MkdirAll(sidebarPath, 0o755); err != nil {
		t.Fatalf("mkdir sidebar path as directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sidebarPath, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	if err := pruneDroppedVersionArtifacts(repo, []string{"1.2.3"}); err == nil {
		t.Fatal("expected remove sidebar error when sidebar path is a directory")
	}
}

func TestNormalizeVersionsJSON_WritesBeforePrune(t *testing.T) {
	repo := t.TempDir()
	versions := []string{"1.0.4", "1.0.3", "1.0.2", "1.0.1", "1.0.0"}
	data, err := json.Marshal(versions)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	versionsPath := filepath.Join(repo, "versions.json")
	if err := os.WriteFile(versionsPath, data, 0o644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}

	droppedDocs := filepath.Join(repo, "versioned_docs", "version-1.0.0")
	if err := os.MkdirAll(droppedDocs, 0o755); err != nil {
		t.Fatalf("mkdir dropped docs: %v", err)
	}

	sidebarPath := filepath.Join(repo, "versioned_sidebars", "version-1.0.0-sidebars.json")
	if err := os.MkdirAll(sidebarPath, 0o755); err != nil {
		t.Fatalf("mkdir sidebar path as directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sidebarPath, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write nested sidebar file: %v", err)
	}

	if err := normalizeVersionsJSON(repo); err == nil {
		t.Fatal("expected normalize error when prune fails")
	}

	out, err := os.ReadFile(versionsPath)
	if err != nil {
		t.Fatalf("read versions.json: %v", err)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"1.0.4", "1.0.3", "1.0.2", "1.0.1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expected versions.json to be written before prune, got %v", got)
	}
}

func TestNormalizeVersionsJSON_WriteErrorSkipsPrune(t *testing.T) {
	repo := t.TempDir()
	versionsPath := filepath.Join(repo, "versions.json")
	if err := os.WriteFile(versionsPath, []byte("[\"1.0.4\",\"1.0.3\",\"1.0.2\",\"1.0.1\",\"1.0.0\"]"), 0o644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}

	droppedDocs := filepath.Join(repo, "versioned_docs", "version-1.0.0")
	if err := os.MkdirAll(droppedDocs, 0o755); err != nil {
		t.Fatalf("mkdir dropped docs: %v", err)
	}

	sidebarPath := filepath.Join(repo, "versioned_sidebars", "version-1.0.0-sidebars.json")
	if err := os.MkdirAll(filepath.Dir(sidebarPath), 0o755); err != nil {
		t.Fatalf("mkdir sidebars dir: %v", err)
	}
	if err := os.WriteFile(sidebarPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write sidebar: %v", err)
	}

	withWriteFileError(t, versionsPath, os.ErrPermission)
	if err := normalizeVersionsJSON(repo); err == nil || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected write error, got %v", err)
	}

	if _, err := os.Stat(droppedDocs); err != nil {
		t.Fatalf("expected dropped docs to remain when write fails, stat err=%v", err)
	}
	if _, err := os.Stat(sidebarPath); err != nil {
		t.Fatalf("expected dropped sidebar to remain when write fails, stat err=%v", err)
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

func TestValidateRepoBRoot_StatError(t *testing.T) {
	repo := t.TempDir()
	withStatError(t, repo, os.ErrPermission)

	err := validateRepoBRoot(repo)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected permission error, got %v", err)
	}
}

func TestValidateRepoBRoot_GitAndRequiredPathStatErrors(t *testing.T) {
	t.Run("git stat error", func(t *testing.T) {
		repo := t.TempDir()
		withStatError(t, filepath.Join(repo, ".git"), os.ErrPermission)
		err := validateRepoBRoot(repo)
		if err == nil || !errors.Is(err, os.ErrPermission) {
			t.Fatalf("expected .git stat error, got %v", err)
		}
	})

	t.Run("required path stat error", func(t *testing.T) {
		repo := setupRepoB(t)
		withStatError(t, filepath.Join(repo, "package.json"), os.ErrPermission)
		err := validateRepoBRoot(repo)
		if err == nil || !errors.Is(err, os.ErrPermission) {
			t.Fatalf("expected required-path stat error, got %v", err)
		}
	})

	t.Run("src/pages stat error", func(t *testing.T) {
		repo := setupRepoB(t)
		withStatError(t, filepath.Join(repo, "src", "pages"), os.ErrPermission)
		err := validateRepoBRoot(repo)
		if err == nil || !errors.Is(err, os.ErrPermission) {
			t.Fatalf("expected src/pages stat error, got %v", err)
		}
	})
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

func TestRepoRoot_GoModStatError(t *testing.T) {
	repo := t.TempDir()
	goModPath := filepath.Join(repo, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module example.com/test"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	nested := filepath.Join(repo, "nested", "dir")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	testutil.WithWorkingDir(t, nested, func() {
		withStatError(t, goModPath, os.ErrPermission)

		_, err := repoRoot()
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, os.ErrPermission) {
			t.Fatalf("expected permission error, got %v", err)
		}
	})
}

func TestNormalizeVersionsJSON_Missing(t *testing.T) {
	repo := t.TempDir()
	if err := normalizeVersionsJSON(repo); err == nil {
		t.Fatal("expected error for missing versions.json")
	}
}

func TestRun_MissingRepoBDir(t *testing.T) {
	setArgs(t, "--tag", "v0.1.0")
	if err := run(); err == nil || !strings.Contains(err.Error(), "--repo-b-dir is required") {
		t.Fatalf("expected repo-b-dir error, got %v", err)
	}
}

func TestRun_InvalidTimeout(t *testing.T) {
	setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", "repo-b", "--docusaurus-timeout", "0s")
	if err := run(); err == nil || !strings.Contains(err.Error(), "positive duration") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestRun_InvalidTag(t *testing.T) {
	setArgs(t, "--tag", "bad", "--repo-b-dir", "repo-b")
	if err := run(); err == nil || !strings.Contains(err.Error(), "invalid tag format") {
		t.Fatalf("expected tag format error, got %v", err)
	}
}

func TestRun_RepoRootMissing(t *testing.T) {
	root := t.TempDir()
	testutil.WithWorkingDir(t, root, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", "repo-b")
		if err := run(); err == nil || !strings.Contains(err.Error(), "failed to find repo root") {
			t.Fatalf("expected repo root error, got %v", err)
		}
	})
}

func TestRun_RepoBDirAbsError(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: true, withDocs: true, withChangelog: true})

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", "bad\x00path")
		if err := run(); err == nil || !strings.Contains(err.Error(), "stat --repo-b-dir") {
			t.Fatalf("expected repo-b-dir stat error, got %v", err)
		}
	})
}

func TestRun_ValidateRepoBRootError(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: true, withDocs: true, withChangelog: true})
	repoB := t.TempDir() // missing .git and required files

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", repoB)
		if err := run(); err == nil {
			t.Fatal("expected validate repo-b-dir error")
		}
	})
}

func TestRun_MissingSitePages(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: false, withDocs: true, withChangelog: true})
	repoB := setupRepoB(t)

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", repoB)
		if err := run(); err == nil || !strings.Contains(err.Error(), "missing Repo A site pages dir") {
			t.Fatalf("expected missing pages error, got %v", err)
		}
	})
}

func TestRun_MissingSiteDocs(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: true, withDocs: false, withChangelog: true})
	repoB := setupRepoB(t)

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", repoB)
		if err := run(); err == nil || !strings.Contains(err.Error(), "missing Repo A site docs dir") {
			t.Fatalf("expected missing docs error, got %v", err)
		}
	})
}

func TestRun_MissingChangelog(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: true, withDocs: true, withChangelog: false})
	repoB := setupRepoB(t)

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", repoB)
		if err := run(); err == nil || !strings.Contains(err.Error(), "missing Repo A changelog") {
			t.Fatalf("expected missing changelog error, got %v", err)
		}
	})
}

func TestRun_ChangelogStatError(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: true, withDocs: true, withChangelog: true})
	repoB := setupRepoB(t)

	changelogPath := filepath.Join(repoA, "CHANGELOG.md")
	withStatError(t, changelogPath, os.ErrPermission)

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", repoB)
		if err := run(); err == nil || !strings.Contains(err.Error(), "failed to stat Repo A changelog") {
			t.Fatalf("expected stat changelog error, got %v", err)
		}
	})
}

func TestRun_CopyPagesError(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: true, withDocs: true, withChangelog: true})
	repoB := setupRepoB(t)

	badFile := filepath.Join(repoA, "site", "pages", "index.mdx")
	withReadFileError(t, badFile, os.ErrPermission)

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", repoB)
		if err := run(); err == nil || !strings.Contains(err.Error(), "failed to copy pages") {
			t.Fatalf("expected copy pages error, got %v", err)
		}
	})
}

func TestRun_CopyDocsError(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: true, withDocs: true, withChangelog: true})
	repoB := setupRepoB(t)

	badFile := filepath.Join(repoA, "site", "docs", "reference.mdx")
	withReadFileError(t, badFile, os.ErrPermission)

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", repoB)
		if err := run(); err == nil || !strings.Contains(err.Error(), "failed to copy docs") {
			t.Fatalf("expected copy docs error, got %v", err)
		}
	})
}

func TestRun_CopyPagesCreateSrcDirError(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: true, withDocs: true, withChangelog: true})
	repoB := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoB, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	for _, name := range []string{"package.json", "docusaurus.config.js", "sidebars.js"} {
		if err := os.WriteFile(filepath.Join(repoB, name), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	srcPath := filepath.Join(repoB, "src")
	if err := os.WriteFile(srcPath, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatalf("write src file: %v", err)
	}

	originalStat := osStatFunc
	osStatFunc = func(name string) (os.FileInfo, error) {
		if filepath.Clean(name) == filepath.Clean(filepath.Join(repoB, "src", "pages")) {
			return originalStat(srcPath)
		}
		return originalStat(name)
	}
	t.Cleanup(func() { osStatFunc = originalStat })

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", repoB)
		if err := run(); err == nil || !strings.Contains(err.Error(), "failed to create src dir") {
			t.Fatalf("expected src mkdir error, got %v", err)
		}
	})
}

func TestRepoRoot_GetwdError(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove temp dir: %v", err)
	}
	defer func() {
		_ = os.Chdir(orig)
	}()

	if _, err := repoRoot(); err == nil {
		t.Fatal("expected error from repoRoot when getwd fails")
	}
}

func TestCopyTree_RemoveAllError(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := copyTree(src, "bad\x00"); err == nil {
		t.Fatal("expected error for invalid destination path")
	}
}

func TestCopyTree_WalkError(t *testing.T) {
	src := t.TempDir()
	blocked := filepath.Join(src, "blocked")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatalf("mkdir blocked: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocked file: %v", err)
	}

	withWalkError(t, blocked, os.ErrPermission)

	dst := t.TempDir()
	if err := copyTree(src, filepath.Join(dst, "out")); err == nil {
		t.Fatal("expected walk error")
	}
}

func TestCopyTree_CallbackErrParameterPropagates(t *testing.T) {
	originalWalk := filepathWalkFunc
	filepathWalkFunc = func(root string, fn filepath.WalkFunc) error {
		return fn(root, nil, errors.New("walk callback boom"))
	}
	t.Cleanup(func() { filepathWalkFunc = originalWalk })

	if err := copyTree(t.TempDir(), filepath.Join(t.TempDir(), "out")); err == nil || !strings.Contains(err.Error(), "walk callback boom") {
		t.Fatalf("expected callback err propagation, got %v", err)
	}
}

func TestCopyTree_RelPathErrorPropagates(t *testing.T) {
	originalWalk := filepathWalkFunc
	filepathWalkFunc = func(root string, fn filepath.WalkFunc) error {
		return fn("bad\x00path", nil, nil)
	}
	t.Cleanup(func() { filepathWalkFunc = originalWalk })

	if err := copyTree(t.TempDir(), filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("expected filepath.Rel error")
	}
}

func TestCopyTree_WriteError(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write src file: %v", err)
	}

	target := filepath.Join(dst, "out", "file.txt")
	withWriteFileError(t, target, os.ErrPermission)

	if err := copyTree(src, filepath.Join(dst, "out")); err == nil || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected write error, got %v", err)
	}
}

func TestRun_ReadChangelogError(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: true, withDocs: true, withChangelogDir: true})
	repoB := setupRepoB(t)

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", repoB)
		if err := run(); err == nil || !strings.Contains(err.Error(), "failed to read Repo A changelog") {
			t.Fatalf("expected read changelog error, got %v", err)
		}
	})
}

func TestRun_WriteChangelogError(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: true, withDocs: true, withChangelog: true})
	repoB := setupRepoB(t)
	requireMkdir(t, filepath.Join(repoB, "CHANGELOG.md"))

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", repoB)
		if err := run(); err == nil || !strings.Contains(err.Error(), "failed to write Repo B changelog") {
			t.Fatalf("expected write changelog error, got %v", err)
		}
	})
}

func TestEnsureIdempotentVersion_ReadError(t *testing.T) {
	repo := t.TempDir()
	versionsPath := filepath.Join(repo, "versions.json")
	if err := os.MkdirAll(versionsPath, 0o755); err != nil {
		t.Fatalf("mkdir versions.json: %v", err)
	}

	if err := ensureIdempotentVersion(repo, "1.2.3"); err == nil {
		t.Fatal("expected read error for versions.json directory")
	}
}

func TestEnsureIdempotentVersion_WriteError(t *testing.T) {
	repo := t.TempDir()
	versionsPath := filepath.Join(repo, "versions.json")
	if err := os.WriteFile(versionsPath, []byte("[\"1.2.3\"]"), 0o644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}

	withWriteFileError(t, versionsPath, os.ErrPermission)

	if err := ensureIdempotentVersion(repo, "1.2.3"); err == nil {
		t.Fatal("expected write error")
	}
}

func TestEnsureIdempotentVersion_AdditionalErrorBranches(t *testing.T) {
	t.Run("remove versioned docs error", func(t *testing.T) {
		repo := t.TempDir()
		if err := ensureIdempotentVersion(repo, "bad\x00version"); err == nil {
			t.Fatal("expected remove-all error for invalid docsVersion path")
		}
	})

	t.Run("remove sidebar error", func(t *testing.T) {
		repo := t.TempDir()
		sidebarPath := filepath.Join(repo, "versioned_sidebars", "version-1.2.3-sidebars.json")
		if err := os.MkdirAll(sidebarPath, 0o755); err != nil {
			t.Fatalf("mkdir sidebar path as directory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sidebarPath, "keep.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write nested file: %v", err)
		}
		if err := ensureIdempotentVersion(repo, "1.2.3"); err == nil {
			t.Fatal("expected remove sidebar error when sidebar path is a directory")
		}
	})
}

func TestRun_EnsureIdempotentVersionError(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: true, withDocs: true, withChangelog: true})
	repoB := setupRepoB(t)
	versionsPath := filepath.Join(repoB, "versions.json")
	if err := os.WriteFile(versionsPath, []byte("invalid-json"), 0o644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", repoB)
		if err := run(); err == nil || !strings.Contains(err.Error(), "failed to ensure idempotent version") {
			t.Fatalf("expected idempotent error, got %v", err)
		}
	})
}

func TestRun_DocusaurusCommandError(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: true, withDocs: true, withChangelog: true})
	repoB := setupRepoB(t)
	withHelperCommand(t, "HELPER_FAIL=1")

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", repoB)
		if err := run(); err == nil || !strings.Contains(err.Error(), "docusaurus docs:version failed") {
			t.Fatalf("expected docusaurus error, got %v", err)
		}
	})
}

func TestRun_DocusaurusTimeout(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: true, withDocs: true, withChangelog: true})
	repoB := setupRepoB(t)
	withHelperCommand(t, "HELPER_SLEEP=50ms")

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", repoB, "--docusaurus-timeout", "1ms")
		if err := run(); err == nil || !strings.Contains(err.Error(), "exceeded timeout") {
			t.Fatalf("expected timeout error, got %v", err)
		}
	})
}

func TestRun_NormalizeVersionsJSONError(t *testing.T) {
	repoA := setupRepoA(t, repoAOptions{withPages: true, withDocs: true, withChangelog: true})
	repoB := setupRepoB(t)
	withHelperCommand(t, "HELPER_SKIP_VERSIONS=1")

	testutil.WithWorkingDir(t, repoA, func() {
		setArgs(t, "--tag", "v0.1.0", "--repo-b-dir", repoB)
		if err := run(); err == nil || !strings.Contains(err.Error(), "failed to normalize versions.json") {
			t.Fatalf("expected normalize error, got %v", err)
		}
	})
}

func TestParseVersion_InvalidMinor(t *testing.T) {
	if _, err := parseVersion("1.a.3"); err == nil {
		t.Fatal("expected error for invalid minor version")
	}
}

func TestComparePrerelease_BranchCoverage(t *testing.T) {
	tests := []struct {
		a    string
		b    string
		want int
	}{
		{"beta", "1", 1},        // non-numeric > numeric
		{"alpha", "beta", -1},   // lexical compare
		{"beta", "alpha", 1},    // lexical compare
		{"alpha", "alpha", 0},   // equal identifiers
		{"alpha.1", "alpha", 1}, // longer list is higher precedence
	}
	for _, tt := range tests {
		got := comparePrerelease(tt.a, tt.b)
		switch tt.want {
		case -1:
			if got >= 0 {
				t.Fatalf("comparePrerelease(%q,%q) = %d, want < 0", tt.a, tt.b, got)
			}
		case 0:
			if got != 0 {
				t.Fatalf("comparePrerelease(%q,%q) = %d, want 0", tt.a, tt.b, got)
			}
		case 1:
			if got <= 0 {
				t.Fatalf("comparePrerelease(%q,%q) = %d, want > 0", tt.a, tt.b, got)
			}
		}
	}
}

func TestParseNumericIdentifier_EdgeCases(t *testing.T) {
	if _, ok := parseNumericIdentifier("0"); !ok {
		t.Fatal("expected 0 to be numeric")
	}
	if _, ok := parseNumericIdentifier(""); ok {
		t.Fatal("expected empty string to be invalid")
	}
	if _, ok := parseNumericIdentifier("01"); ok {
		t.Fatal("expected leading zero to be invalid")
	}
	if _, ok := parseNumericIdentifier("1a"); ok {
		t.Fatal("expected non-digit to be invalid")
	}
	if _, ok := parseNumericIdentifier(strings.Repeat("9", 200)); ok {
		t.Fatal("expected overflow to be invalid")
	}
}

func TestNormalizeVersionsJSON_InvalidJSON(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "versions.json"), []byte("not json"), 0o644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}
	if err := normalizeVersionsJSON(repo); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestNormalizeVersionsJSON_ReadError(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, "versions.json"), 0o755); err != nil {
		t.Fatalf("mkdir versions.json: %v", err)
	}
	if err := normalizeVersionsJSON(repo); err == nil {
		t.Fatal("expected read error for versions.json directory")
	}
}

func TestNormalizeVersionsJSON_WriteError(t *testing.T) {
	repo := t.TempDir()
	versionsPath := filepath.Join(repo, "versions.json")
	if err := os.WriteFile(versionsPath, []byte("[\"1.0.0\"]"), 0o644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}
	withWriteFileError(t, versionsPath, os.ErrPermission)
	if err := normalizeVersionsJSON(repo); err == nil || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("expected write error, got %v", err)
	}
}

func TestNormalizeVersionsJSON_StableVsPrerelease(t *testing.T) {
	repo := t.TempDir()
	versions := []string{"1.0.0-rc.1", "1.0.0"}
	data, err := json.Marshal(versions)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "versions.json"), data, 0o644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}

	droppedDocs := filepath.Join(repo, "versioned_docs", "version-1.0.0-rc.1")
	if err := os.MkdirAll(droppedDocs, 0o755); err != nil {
		t.Fatalf("mkdir prerelease docs dir: %v", err)
	}
	sidebarsDir := filepath.Join(repo, "versioned_sidebars")
	if err := os.MkdirAll(sidebarsDir, 0o755); err != nil {
		t.Fatalf("mkdir sidebars dir: %v", err)
	}
	droppedSidebar := filepath.Join(sidebarsDir, "version-1.0.0-rc.1-sidebars.json")
	if err := os.WriteFile(droppedSidebar, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write prerelease sidebar: %v", err)
	}

	if err := normalizeVersionsJSON(repo); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(repo, "versions.json"))
	if err != nil {
		t.Fatalf("read versions.json: %v", err)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 || got[0] != "1.0.0" {
		t.Fatalf("expected only stable release retained, got %v", got)
	}
	if _, err := os.Stat(droppedDocs); !os.IsNotExist(err) {
		t.Fatalf("expected prerelease docs removed")
	}
	if _, err := os.Stat(droppedSidebar); !os.IsNotExist(err) {
		t.Fatalf("expected prerelease sidebar removed")
	}
}

func TestNormalizeVersionsJSON_PrereleaseOnly(t *testing.T) {
	repo := t.TempDir()
	versions := []string{"1.0.0-rc.1"}
	data, err := json.Marshal(versions)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "versions.json"), data, 0o644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}
	if err := normalizeVersionsJSON(repo); err == nil || !strings.Contains(err.Error(), "no stable releases") {
		t.Fatalf("expected no stable releases error, got %v", err)
	}
}

func TestNormalizeVersionsJSON_InvalidVersion(t *testing.T) {
	repo := t.TempDir()
	versions := []string{"1.0.0", "2.0.0", "1.0.1", "1.0.0-rc.1", "bad"}
	data, err := json.Marshal(versions)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "versions.json"), data, 0o644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}
	if err := normalizeVersionsJSON(repo); err == nil || !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("expected invalid version error, got %v", err)
	}
}

func TestNormalizeVersionsJSON_PrereleasePathTraversalRejected(t *testing.T) {
	repo := t.TempDir()
	versions := []string{"1.0.0", "1.0.0-../../../../outside"}
	data, err := json.Marshal(versions)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	versionsPath := filepath.Join(repo, "versions.json")
	if err := os.WriteFile(versionsPath, data, 0o644); err != nil {
		t.Fatalf("write versions.json: %v", err)
	}

	if err := normalizeVersionsJSON(repo); err == nil || !strings.Contains(err.Error(), "invalid prerelease") {
		t.Fatalf("expected invalid prerelease error, got %v", err)
	}

	out, err := os.ReadFile(versionsPath)
	if err != nil {
		t.Fatalf("read versions.json: %v", err)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if strings.Join(got, ",") != strings.Join(versions, ",") {
		t.Fatalf("expected versions.json unchanged on error, got %v", got)
	}
}

func TestRun(t *testing.T) {
	repoA := t.TempDir()
	repoB := t.TempDir()

	if err := os.WriteFile(filepath.Join(repoA, "go.mod"), []byte("module example.com/test"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoA, "CHANGELOG.md"), []byte("# Changelog\n"), 0o644); err != nil {
		t.Fatalf("write changelog: %v", err)
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
	if err := os.WriteFile(filepath.Join(siteDocs, "reference.mdx"), []byte("reference"), 0o644); err != nil {
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
	defer func() {
		execCommandContext = exec.CommandContext
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
	if _, err := os.Stat(filepath.Join(repoB, "CHANGELOG.md")); err != nil {
		t.Fatalf("expected changelog copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoB, "docs", "reference.mdx")); err != nil {
		t.Fatalf("expected reference doc: %v", err)
	}
}

func TestMainError(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestMainHelper", "--") //nolint:gosec // standard test re-exec pattern
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
	if sleep := os.Getenv("HELPER_SLEEP"); sleep != "" {
		if duration, err := time.ParseDuration(sleep); err == nil {
			time.Sleep(duration)
		}
	}
	if os.Getenv("HELPER_FAIL") == "1" {
		os.Exit(1)
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

	if os.Getenv("HELPER_SKIP_VERSIONS") == "1" {
		os.Exit(0)
	}

	data, err := json.Marshal([]string{version})
	if err != nil {
		os.Exit(1)
	}
	if err := os.WriteFile("versions.json", data, 0o644); err != nil { //nolint:gosec // test helper writing to CWD
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

type repoAOptions struct {
	withPages        bool
	withDocs         bool
	withChangelog    bool
	withChangelogDir bool
}

func setupRepoA(t *testing.T, opts repoAOptions) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/test"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if opts.withPages {
		sitePages := filepath.Join(repo, "site", "pages")
		if err := os.MkdirAll(sitePages, 0o755); err != nil {
			t.Fatalf("mkdir site pages: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sitePages, "index.mdx"), []byte("# Home"), 0o644); err != nil {
			t.Fatalf("write page: %v", err)
		}
	}
	if opts.withDocs {
		siteDocs := filepath.Join(repo, "site", "docs")
		if err := os.MkdirAll(siteDocs, 0o755); err != nil {
			t.Fatalf("mkdir site docs: %v", err)
		}
		if err := os.WriteFile(filepath.Join(siteDocs, "reference.mdx"), []byte("reference"), 0o644); err != nil {
			t.Fatalf("write doc: %v", err)
		}
	}
	if opts.withChangelog || opts.withChangelogDir {
		changelog := filepath.Join(repo, "CHANGELOG.md")
		if opts.withChangelogDir {
			if err := os.MkdirAll(changelog, 0o755); err != nil {
				t.Fatalf("mkdir changelog: %v", err)
			}
		} else {
			if err := os.WriteFile(changelog, []byte("# Changelog\n"), 0o644); err != nil {
				t.Fatalf("write changelog: %v", err)
			}
		}
	}
	return repo
}

func setupRepoB(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	for _, name := range []string{"package.json", "docusaurus.config.js", "sidebars.js"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, "src", "pages"), 0o755); err != nil {
		t.Fatalf("mkdir src/pages: %v", err)
	}
	return repo
}

func requireMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func setArgs(t *testing.T, args ...string) {
	t.Helper()
	origArgs := os.Args
	origFlags := flag.CommandLine
	t.Cleanup(func() {
		os.Args = origArgs
		flag.CommandLine = origFlags
	})
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	os.Args = append([]string{origArgs[0]}, args...)
}

// resolveSymlinks returns both the original and symlink-resolved paths.
// On macOS, t.TempDir() returns /var/... while os.Getwd() returns /private/var/...;
// matching both ensures stubs work for direct calls and calls through run().
func resolveSymlinks(path string) (original, resolved string) {
	resolved = path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		resolved = r
	}
	return path, resolved
}

func withStatError(t *testing.T, path string, injectedErr error) {
	t.Helper()
	origPath, resolvedPath := resolveSymlinks(path)
	orig := osStatFunc
	osStatFunc = func(name string) (os.FileInfo, error) {
		if name == origPath || name == resolvedPath {
			return nil, injectedErr
		}
		return orig(name)
	}
	t.Cleanup(func() { osStatFunc = orig })
}

func withReadFileError(t *testing.T, path string, injectedErr error) {
	t.Helper()
	origPath, resolvedPath := resolveSymlinks(path)
	orig := osReadFileFunc
	osReadFileFunc = func(name string) ([]byte, error) {
		if name == origPath || name == resolvedPath {
			return nil, injectedErr
		}
		return orig(name)
	}
	t.Cleanup(func() { osReadFileFunc = orig })
}

func withWriteFileError(t *testing.T, path string, injectedErr error) {
	t.Helper()
	origPath, resolvedPath := resolveSymlinks(path)
	orig := osWriteFileFunc
	osWriteFileFunc = func(name string, data []byte, perm os.FileMode) error {
		if name == origPath || name == resolvedPath {
			return injectedErr
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFileFunc = orig })
}

func withWalkError(t *testing.T, errorDir string, injectedErr error) {
	t.Helper()
	origDir, resolvedDir := resolveSymlinks(errorDir)
	orig := filepathWalkFunc
	filepathWalkFunc = func(root string, fn filepath.WalkFunc) error {
		return orig(root, func(path string, info os.FileInfo, err error) error {
			if (path == origDir || path == resolvedDir) && info != nil && info.IsDir() {
				return injectedErr
			}
			return fn(path, info, err)
		})
	}
	t.Cleanup(func() { filepathWalkFunc = orig })
}

func withHelperCommand(t *testing.T, extraEnv ...string) {
	t.Helper()
	orig := execCommandContext
	t.Cleanup(func() { execCommandContext = orig })
	execCommandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=TestHelperProcess", "--"}, append([]string{name}, args...)...)...) //nolint:gosec // standard test re-exec pattern
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		cmd.Env = append(cmd.Env, extraEnv...)
		return cmd
	}
}
