package version

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "0.5.0", want: "0.5.0"},
		{input: "v0.5.0", want: "0.5.0"},
		{input: "  v1.2.3  ", want: "1.2.3"},
		{input: "", wantErr: true},
		{input: "v1.2", wantErr: true},
		{input: "1.2.3.4", wantErr: true},
		{input: "dev", wantErr: true},
	}
	for _, tt := range tests {
		got, err := Normalize(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("Normalize(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("Normalize(%q) error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsDev(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{input: "dev", want: true},
		{input: "  dev  ", want: true}, // surrounding whitespace is trimmed
		{input: "\tdev\n", want: true}, // tabs/newlines too
		{input: "v0.5.0", want: false},
		{input: "DEV", want: false}, // comparison is case-sensitive
		{input: "develop", want: false},
		{input: "", want: false},
	}
	for _, tt := range tests {
		if got := IsDev(tt.input); got != tt.want {
			t.Fatalf("IsDev(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "equal", a: "1.2.3", b: "1.2.3", want: 0},
		{name: "a less major", a: "0.9.9", b: "1.0.0", want: -1},
		{name: "a greater major", a: "2.0.0", b: "1.9.9", want: 1},
		{name: "a less minor", a: "1.0.9", b: "1.1.0", want: -1},
		{name: "a greater minor", a: "1.2.0", b: "1.1.9", want: 1},
		{name: "a less patch", a: "1.2.3", b: "1.2.4", want: -1},
		{name: "a greater patch", a: "1.2.5", b: "1.2.4", want: 1},
		{name: "with v prefix", a: "v1.2.3", b: "v1.2.3", want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Compare(tc.a, tc.b)
			if err != nil {
				t.Fatalf("Compare(%q, %q) err: %v", tc.a, tc.b, err)
			}
			if got != tc.want {
				t.Fatalf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}

	t.Run("invalid a", func(t *testing.T) {
		if _, err := Compare("1.2", "1.0.0"); err == nil {
			t.Fatal("expected error for invalid a")
		}
	})
	t.Run("invalid b", func(t *testing.T) {
		if _, err := Compare("1.0.0", "1.2"); err == nil {
			t.Fatal("expected error for invalid b")
		}
	})
}

func TestParse(t *testing.T) {
	t.Run("valid version", func(t *testing.T) {
		parts, err := Parse("1.2.3")
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if parts != [3]int{1, 2, 3} {
			t.Fatalf("Parse = %v, want [1 2 3]", parts)
		}
	})
	t.Run("v-prefix version", func(t *testing.T) {
		parts, err := Parse("v0.7.0")
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if parts != [3]int{0, 7, 0} {
			t.Fatalf("Parse = %v, want [0 7 0]", parts)
		}
	})
	t.Run("invalid version", func(t *testing.T) {
		if _, err := Parse("abc"); err == nil {
			t.Fatal("expected error for invalid version")
		}
	})
	t.Run("empty version", func(t *testing.T) {
		if _, err := Parse(""); err == nil {
			t.Fatal("expected error for empty version")
		}
	})
	t.Run("segment integer overflow", func(t *testing.T) {
		if _, err := Parse("9999999999999999999999999.0.0"); err == nil {
			t.Fatal("expected error for overflowed version segment")
		}
	})
}
