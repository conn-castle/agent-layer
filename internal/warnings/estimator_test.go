package warnings

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEstimateTokens(t *testing.T) {
	// Each expected value is the exact heuristic output:
	// T = ceil(max(ceil(B/3), ceil(R/4)) * 1.10), B = bytes, R = runes.
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			// 44 bytes, 44 runes: max(ceil(44/3)=15, ceil(44/4)=11)=15; ceil(15*1.10)=17.
			name:     "ascii prose",
			input:    "The quick brown fox jumps over the lazy dog.",
			expected: 17,
		},
		{
			// 36 bytes, 36 runes: max(ceil(36/3)=12, ceil(36/4)=9)=12; ceil(12*1.10)=14.
			name:     "code snippet",
			input:    `func main() { fmt.Println("Hello") }`,
			expected: 14,
		},
		{
			// 12 bytes, 8 runes: max(ceil(12/3)=4, ceil(8/4)=2)=4; ceil(4*1.10)=5.
			// Exercises the byte-vs-rune divergence for multibyte input.
			name:     "unicode text",
			input:    "Hello 世界",
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, EstimateTokens(tt.input))
		})
	}
}

func TestEstimateTokens_LargeInput(t *testing.T) {
	// 3000 'a's. 3000 bytes, 3000 runes.
	// B/3 = 1000. R/4 = 750. Max = 1000.
	// 1000 * 1.1 = 1100.
	input := strings.Repeat("a", 3000)
	got := EstimateTokens(input)
	assert.Equal(t, 1100, got)
}
