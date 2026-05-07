package config

import (
	"testing"
)

func TestIsAgentEnabled(t *testing.T) {
	trueVal := true
	falseVal := false
	tests := []struct {
		name string
		ptr  *bool
		want bool
	}{
		{"nil pointer", nil, false},
		{"false pointer", &falseVal, false},
		{"true pointer", &trueVal, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAgentEnabled(tt.ptr); got != tt.want {
				t.Fatalf("IsAgentEnabled(%v) = %v, want %v", tt.ptr, got, tt.want)
			}
		})
	}
}
