package clients

import (
	"slices"
	"testing"
)

func TestMergeArgs(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		defaults, passed, want []string
	}{
		{"separate", []string{"--model", "default", "--effort", "high"}, []string{"--model", "chosen"}, []string{"--effort", "high", "--model", "chosen"}},
		{"equals", []string{"--model=default"}, []string{"--model=chosen"}, []string{"--model=chosen"}},
		{"short", []string{"--model", "default"}, []string{"-m", "chosen"}, []string{"-m", "chosen"}},
		{"short attached", []string{"--model", "default"}, []string{"-mchosen"}, []string{"-mchosen"}},
		{"short equals", []string{"--model", "default"}, []string{"-m=chosen"}, []string{"-m=chosen"}},
		{"long alias", []string{"--reasoning-effort", "high"}, []string{"--effort=low"}, []string{"--effort=low"}},
		{"boolean", []string{"--yolo"}, []string{"--yolo"}, []string{"--yolo"}},
		{"explicit false", []string{"--yolo"}, []string{"--yolo=false"}, []string{"--yolo=false"}},
		{"terminator", []string{"--model", "default"}, []string{"--", "--model", "literal"}, []string{"--model", "default", "--", "--model", "literal"}},
		{"exact name", []string{"--model", "default"}, []string{"--model-other=chosen"}, []string{"--model", "default", "--model-other=chosen"}},
		{"repeatable", []string{"--disable-mcp-server", "a", "--disable-mcp-server", "b"}, []string{"--disable-mcp-server", "c", "--disable-mcp-server", "d"}, []string{"--disable-mcp-server", "a", "--disable-mcp-server", "b", "--disable-mcp-server", "c", "--disable-mcp-server", "d"}},
		{"same list entry", []string{"--disable-mcp-server", "a", "--disable-mcp-server", "b"}, []string{"--disable-mcp-server=a"}, []string{"--disable-mcp-server", "b", "--disable-mcp-server=a"}},
		{"joined list default", []string{"--disable-mcp-server=a"}, []string{"--disable-mcp-server", "a"}, []string{"--disable-mcp-server", "a"}},
		{"caller repeats preserved", []string{"--disable-mcp-server", "a"}, []string{"--disable-mcp-server", "a", "--disable-mcp-server=a"}, []string{"--disable-mcp-server", "a", "--disable-mcp-server=a"}},
		{"list after terminator", []string{"--disable-mcp-server", "a"}, []string{"--", "--disable-mcp-server=a"}, []string{"--disable-mcp-server", "a", "--", "--disable-mcp-server=a"}},
		{"joined default separate caller", []string{"--model=default"}, []string{"--model", "chosen"}, []string{"--model", "chosen"}},
		{"separate default joined caller", []string{"--model", "default"}, []string{"--model=chosen"}, []string{"--model=chosen"}},
		{"flag-shaped generated value overridden", []string{"--model", "-preview", "--effort", "high"}, []string{"--model", "chosen"}, []string{"--effort", "high", "--model", "chosen"}},
		{"flag-shaped generated value retained", []string{"--model", "-preview", "--reasoning-effort", "high"}, []string{"--effort", "low"}, []string{"--model", "-preview", "--effort", "low"}},
		{"option-shaped generated value overridden", []string{"--model", "--preview", "--effort", "high"}, []string{"--model=chosen"}, []string{"--effort", "high", "--model=chosen"}},
		{"flag-shaped additive value deduplicated", []string{"--disable-mcp-server", "-local"}, []string{"--disable-mcp-server=-local"}, []string{"--disable-mcp-server=-local"}},
		{"empty scalar", []string{"--model", "default"}, []string{"--model="}, []string{"--model="}},
		{"short cluster", []string{"--model", "default"}, []string{"-cm", "chosen"}, []string{"-cm", "chosen"}},
		{"short cluster attached", []string{"--model", "default"}, []string{"-cmchosen"}, []string{"-cmchosen"}},
		{"short value is not a cluster", []string{"--model", "default"}, []string{"-cpmodel"}, []string{"--model", "default", "-cpmodel"}},
		{"joined literal flag", []string{"--yolo"}, []string{"--prompt=--yolo"}, []string{"--yolo", "--prompt=--yolo"}},
		{"prompt", []string{"--model", "default"}, []string{"explain --model"}, []string{"--model", "default", "explain --model"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defaults, passed := slices.Clone(tc.defaults), slices.Clone(tc.passed)
			got := MergeArgs(tc.defaults, tc.passed, map[string]string{"-m": "--model", "--effort": "--reasoning-effort", "-c": ""},
				[]string{"--model", "--effort", "--reasoning-effort", "--disable-mcp-server"}, "--disable-mcp-server")
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if !slices.Equal(defaults, tc.defaults) || !slices.Equal(passed, tc.passed) {
				t.Fatal("input arguments mutated")
			}
		})
	}
}
