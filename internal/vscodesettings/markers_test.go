package vscodesettings

import (
	"reflect"
	"testing"
)

func TestManagedMarkerLines(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		lines        []string
		starts, ends []int
	}{
		{
			name: "quoted braces and block comment",
			lines: []string{
				"{", `  "text": "literal \" } // >>> agent-layer",`,
				"  /*", "  // >>> agent-layer", "  */",
				"  // >>> agent-layer", "  // <<< agent-layer", "}",
			},
			starts: []int{5}, ends: []int{6},
		},
		{
			name: "nested arrays and objects",
			lines: []string{
				"{", `  "nested": [`, "    {", "      // >>> agent-layer",
				"      // <<< agent-layer", "    }", "  ],",
				"  // >>> agent-layer", "  // <<< agent-layer", "}",
			},
			starts: []int{7}, ends: []int{8},
		},
		{
			name: "header examples",
			lines: []string{
				"// >>> agent-layer", "// <<< agent-layer", "{",
				"  // >>> agent-layer", "  // <<< agent-layer", "}",
			},
			starts: []int{3}, ends: []int{4},
		},
		{
			name: "real duplicates remain visible",
			lines: []string{
				"{", "  // >>> agent-layer", "  // >>> agent-layer",
				"  // <<< agent-layer", "  // <<< agent-layer", "}",
			},
			starts: []int{1, 2}, ends: []int{3, 4},
		},
		{name: "no root object", lines: []string{"// >>> agent-layer", "// <<< agent-layer"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			starts, ends := ManagedMarkerLines(test.lines)
			if !reflect.DeepEqual(starts, test.starts) || !reflect.DeepEqual(ends, test.ends) {
				t.Fatalf("markers = %v/%v, want %v/%v", starts, ends, test.starts, test.ends)
			}
		})
	}
}
