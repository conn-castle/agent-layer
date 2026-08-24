package organizescratch

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const publicCertPEM = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"

// The phrase is split so repository secret scanners do not mistake the fixture
// for a committed private key.
const privateKeyPEM = "-----BEGIN OPENSSH " + "PRIVATE KEY-----\nnot-a-real-key\n" +
	"-----END OPENSSH " + "PRIVATE KEY-----\n"

func encodedPrivateKeyFixture() string {
	return base64.StdEncoding.EncodeToString([]byte(privateKeyPEM))
}

func writeFileAt(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func mkdirAt(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
}

func topLevel(root, name string) entry {
	info, err := os.Lstat(filepath.Join(root, name))
	if err != nil {
		panic(err)
	}
	return newEntry(root, canonicalPath(root), name, info.IsDir(), info.Mode()&os.ModeSymlink != 0)
}

func fileEntry(t *testing.T, name, content string) entry {
	t.Helper()
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, name), content)
	return topLevel(root, name)
}

func dirEntry(t *testing.T, root, name string) entry {
	t.Helper()
	mkdirAt(t, filepath.Join(root, name))
	return topLevel(root, name)
}

func emptyContext() classifyContext {
	return classifyContext{
		skillPrefixes: map[string]struct{}{},
		worktrees:     map[string]struct{}{},
		tracked:       map[string]struct{}{},
	}
}

func TestNameParsingDropsArtifactAndTimestampSegments(t *testing.T) {
	cases := []struct{ name, ext, prefix, stem string }{
		{"ship-pr.pr-body.md", "md", "ship-pr", "ship-pr.pr-body"},
		{"audit.report.20250104T1200-final.md", "md", "audit", "audit"},
		{"NOTES", "", "NOTES", "NOTES"},
		{".gitignore", "gitignore", "", ""},
	}
	for _, tc := range cases {
		if got := extOf(tc.name); got != tc.ext {
			t.Errorf("extOf(%q) = %q, want %q", tc.name, got, tc.ext)
		}
		if got := prefixOf(tc.name); got != tc.prefix {
			t.Errorf("prefixOf(%q) = %q, want %q", tc.name, got, tc.prefix)
		}
		if got := stemOf(tc.name); got != tc.stem {
			t.Errorf("stemOf(%q) = %q, want %q", tc.name, got, tc.stem)
		}
	}
}

func TestClassifyFileRoutesOrdinaryExtensionsAndTopLevelLinks(t *testing.T) {
	cases := []struct{ name, dest string }{
		{"run.log", destArtifactsLogs},
		{"shot.PNG", destArtifactsScreenshots},
		{"change.patch", destArtifactsDiffs},
		{"logo.svg", destReviewUniqueAssets},
		{"helper.sh", destArtifactsScripts},
		{"payload.json", destArtifactsData},
	}
	for _, tc := range cases {
		if got := classify(fileEntry(t, tc.name, "x"), emptyContext()); got.dest != tc.dest {
			t.Errorf("classify(%q).dest = %q, want %q", tc.name, got.dest, tc.dest)
		}
	}
	root := t.TempDir()
	if err := os.Symlink("missing", filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	if got := classify(topLevel(root, "dangling"), emptyContext()); got.dest != destReviewSymlinks {
		t.Fatalf("dangling symlink dest = %q, want %q", got.dest, destReviewSymlinks)
	}
}

func TestCredentialContentFindingsAreConfirmedAndActionable(t *testing.T) {
	encodedPrivateKey := encodedPrivateKeyFixture()
	cases := []struct {
		name, content, reason string
	}{
		{"jwt-signing-key", encodedPrivateKey, "decodes to a PEM PRIVATE KEY"},
		{".env.local", "EMPTY=\nTOKEN=real-value\n", "environment assignments"},
		{"production.env", "API_TOKEN=real-value\n", "environment assignments"},
		{"storage-state.json", `{"cookies":[],"origins":[]}`, "cookies array"},
		{"server.key", strings.Repeat("certificate-chain\n", 400) + privateKeyPEM, "PEM PRIVATE KEY"},
	}
	for _, tc := range cases {
		got := classify(fileEntry(t, tc.name, tc.content), emptyContext())
		if got.dest != destReviewSecrets || !strings.Contains(got.reason, tc.reason) {
			t.Errorf("%s: dest=%q reason=%q", tc.name, got.dest, got.reason)
		}
	}
	if got := classify(fileEntry(t, "server.pem", publicCertPEM), emptyContext()); got.dest == destReviewSecrets {
		t.Fatalf("public certificate incorrectly routed to secrets: %q", got.reason)
	}
	encodedCertificate := base64.StdEncoding.EncodeToString([]byte(publicCertPEM))
	if got := classify(fileEntry(t, "jwt-public-certificate.pem", encodedCertificate), emptyContext()); got.dest == destReviewSecrets {
		t.Fatalf("base64 public certificate incorrectly routed to secrets: %q", got.reason)
	}
}

func TestCompleteWalkFindsLateHazardsWithoutDescendingIntoGitMetadata(t *testing.T) {
	root := t.TempDir()
	item := dirEntry(t, root, "evidence")
	for i := 0; i < 4001; i++ {
		writeFileAt(t, filepath.Join(item.abs, fmt.Sprintf("a%04d.json", i)), "{}")
	}
	writeFileAt(t, filepath.Join(item.abs, "z-secret", "jwt-signing-key"), encodedPrivateKeyFixture())
	for i := 0; i < 200; i++ {
		writeFileAt(t, filepath.Join(item.abs, ".git", "objects", fmt.Sprintf("%04d", i)), "metadata")
	}
	scan := scanTree(item.abs)
	if scan.files != 4002 {
		t.Fatalf("files = %d, want 4002 user files with .git metadata excluded", scan.files)
	}
	got := classify(item, emptyContext())
	if got.dest != destReviewSecrets || !strings.Contains(got.reason, "jwt-signing-key") {
		t.Fatalf("dest=%q reason=%q, want late secret", got.dest, got.reason)
	}
}

func TestAuthoredAssetsOutrankStatisticalRegenerability(t *testing.T) {
	root := t.TempDir()
	item := dirEntry(t, root, "generated-output")
	for i := 0; i < vendoredMinFiles+1; i++ {
		writeFileAt(t, filepath.Join(item.abs, fmt.Sprintf("%04d.js", i)), "generated")
	}
	writeFileAt(t, filepath.Join(item.abs, "wordmark.svg"), "<svg/>")
	got := classify(item, emptyContext())
	if got.dest != destReviewUniqueAssets {
		t.Fatalf("dest=%q reason=%q, want authored assets to outrank JS ratio", got.dest, got.reason)
	}

	modules := dirEntry(t, root, "node_modules")
	writeFileAt(t, filepath.Join(modules.abs, "package", "logo.svg"), "<svg/>")
	if got := classify(modules, emptyContext()); got.dest != destReviewRegenerable {
		t.Fatalf("node_modules dest=%q, want regenerable", got.dest)
	}

	mixed := dirEntry(t, root, "mixed")
	writeFileAt(t, filepath.Join(mixed.abs, "node_modules", "package", "logo.svg"), "<svg/>")
	writeFileAt(t, filepath.Join(mixed.abs, "notes.md"), "analysis")
	if got := classify(mixed, emptyContext()); got.dest == destReviewUniqueAssets {
		t.Fatalf("nested dependency asset should not count as authored: %q", got.reason)
	}
}

func TestConfirmedSecretOutranksUnreadableSibling(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read mode-000 fixtures")
	}
	root := t.TempDir()
	item := dirEntry(t, root, "evidence")
	writeFileAt(t, filepath.Join(item.abs, "jwt-signing-key"), encodedPrivateKeyFixture())
	locked := writeFileAt(t, filepath.Join(item.abs, "locked", "notes.txt"), "x")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })
	got := classify(item, emptyContext())
	if got.dest != destReviewSecrets {
		t.Fatalf("dest=%q reason=%q, want confirmed secret precedence", got.dest, got.reason)
	}
}

func TestSizeOverridePreservesUsefulReviewCategoryAndNamesChild(t *testing.T) {
	root := t.TempDir()
	item := dirEntry(t, root, "node_modules")
	for i := 0; i <= maxUnreviewedFiles; i++ {
		writeFileAt(t, filepath.Join(item.abs, "package", fmt.Sprintf("%03d.js", i)), "x")
	}
	got := classify(item, emptyContext())
	if got.dest != destReviewRegenerable || !strings.Contains(got.reason, "package (101 files)") {
		t.Fatalf("dest=%q reason=%q", got.dest, got.reason)
	}
}
