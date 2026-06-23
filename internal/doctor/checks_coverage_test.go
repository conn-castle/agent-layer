package doctor

import (
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// TestHasFailResultForCheck guards the suppression contract: the standalone
// Secrets/Skills checks are skipped only when CheckConfig already emitted a
// StatusFail for the same check name. A regression that also matched WARN/OK,
// or that ignored CheckName, would silence a still-meaningful diagnostic
// (hiding a real "missing secret" failure) or wrongly suppress an unrelated
// check.
func TestHasFailResultForCheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		results   []Result
		checkName string
		want      bool
	}{
		{
			name:      "fail for same check suppresses",
			results:   []Result{{Status: StatusFail, CheckName: messages.DoctorCheckNameSecrets}},
			checkName: messages.DoctorCheckNameSecrets,
			want:      true,
		},
		{
			name:      "warn for same check does not suppress",
			results:   []Result{{Status: StatusWarn, CheckName: messages.DoctorCheckNameSecrets}},
			checkName: messages.DoctorCheckNameSecrets,
			want:      false,
		},
		{
			name:      "ok for same check does not suppress",
			results:   []Result{{Status: StatusOK, CheckName: messages.DoctorCheckNameSecrets}},
			checkName: messages.DoctorCheckNameSecrets,
			want:      false,
		},
		{
			name:      "fail for a different check does not suppress",
			results:   []Result{{Status: StatusFail, CheckName: messages.DoctorCheckNameSkills}},
			checkName: messages.DoctorCheckNameSecrets,
			want:      false,
		},
		{
			name:      "no results does not suppress",
			results:   nil,
			checkName: messages.DoctorCheckNameSecrets,
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := HasFailResultForCheck(tt.results, tt.checkName); got != tt.want {
				t.Fatalf("HasFailResultForCheck(%q) = %v, want %v", tt.checkName, got, tt.want)
			}
		})
	}
}

// TestCompareDoctorSemver exercises the version ordering that decides whether an
// installed `agy` is reported too old. A wrong-sign bug in compareDoctorInt or a
// mis-split in parseDoctorSemver would silently misreport an outdated Antigravity
// as up-to-date (or vice versa).
func TestCompareDoctorSemver(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.1.0", "1.0.0", 1},
		{"1.0.0", "1.1.0", -1},
		{"1.0.5", "1.0.0", 1},
		{"1.0.0", "1.0.5", -1},
		{"2.0.0", "1.9.9", 1},
	}
	for _, tt := range tests {
		got, err := compareDoctorSemver(tt.a, tt.b)
		if err != nil {
			t.Fatalf("compareDoctorSemver(%q,%q) unexpected error: %v", tt.a, tt.b, err)
		}
		if got != tt.want {
			t.Fatalf("compareDoctorSemver(%q,%q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
	for _, bad := range []string{"1.0", "1.0.x", "1.0.0.0", ""} {
		if _, err := compareDoctorSemver(bad, "1.0.0"); err == nil {
			t.Fatalf("compareDoctorSemver(%q,...) expected error", bad)
		}
	}
}

// TestParseAgyVersion confirms the anchored regexes extract only a confident
// X.Y.Z and reject build/version noise; a loosened regex would feed a bogus
// triple into the version comparison.
func TestParseAgyVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{name: "bare", output: "1.0.2", want: "1.0.2"},
		{name: "bare with v prefix", output: "v1.0.2", want: "1.0.2"},
		{name: "bare with whitespace", output: "  1.2.3\n", want: "1.2.3"},
		{name: "unparseable noise", output: "go1.24.2 build abc", want: ""},
		{name: "empty", output: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parseAgyVersion(tt.output); got != tt.want {
				t.Fatalf("parseAgyVersion(%q) = %q, want %q", tt.output, got, tt.want)
			}
		})
	}
}

func TestRelPathForDoctor_EmptyRoot(t *testing.T) {
	got := relPathForDoctor("", "/some/path")
	if got != "/some/path" {
		t.Fatalf("expected /some/path, got %s", got)
	}
}

func TestRelPathForDoctor_WhitespaceRoot(t *testing.T) {
	got := relPathForDoctor("   ", "/some/path")
	if got != "/some/path" {
		t.Fatalf("expected /some/path, got %s", got)
	}
}

func TestRelPathForDoctor_SuccessfulRelPath(t *testing.T) {
	got := relPathForDoctor("/root/project", filepath.Join("/root/project", "sub", "file.md"))
	if got != "sub/file.md" {
		t.Fatalf("expected sub/file.md, got %s", got)
	}
}

func TestRelPathForDoctor_UnrelatedPathFallback(t *testing.T) {
	// On Unix filepath.Rel can still compute a relative path even for
	// seemingly unrelated paths (../../../...), so there is no error case
	// on Unix. Verify that the function at least returns without error.
	got := relPathForDoctor("/root/a", "/other/b")
	if got == "" {
		t.Fatal("expected non-empty result")
	}
}
