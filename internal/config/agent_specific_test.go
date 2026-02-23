package config

import "testing"

func TestHasAgentSpecificKey(t *testing.T) {
	tests := []struct {
		name          string
		agentSpecific map[string]any
		key           string
		want          bool
	}{
		{
			name:          "nil map",
			agentSpecific: nil,
			key:           "foo",
			want:          false,
		},
		{
			name:          "empty map",
			agentSpecific: map[string]any{},
			key:           "foo",
			want:          false,
		},
		{
			name:          "missing key",
			agentSpecific: map[string]any{"bar": true},
			key:           "foo",
			want:          false,
		},
		{
			name:          "present key",
			agentSpecific: map[string]any{"foo": "value"},
			key:           "foo",
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasAgentSpecificKey(tt.agentSpecific, tt.key); got != tt.want {
				t.Fatalf("HasAgentSpecificKey(%v, %q) = %v, want %v", tt.agentSpecific, tt.key, got, tt.want)
			}
		})
	}
}
