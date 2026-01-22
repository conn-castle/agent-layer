package warnings

import (
	"math"
	"unicode/utf8"
)

// EstimateTokens estimates the token count for a given UTF-8 string using a heuristic.
// Heuristic: T = max(ceil(B/3), ceil(R/4)) * 1.10
// where B is the byte length and R is the rune count.
func EstimateTokens(s string) int {
	b := len(s)
	r := utf8.RuneCountInString(s)

	tBytes := math.Ceil(float64(b) / 3.0)
	tRunes := math.Ceil(float64(r) / 4.0)

	tRaw := math.Max(tBytes, tRunes)
	tFinal := math.Ceil(tRaw * 1.10)

	return int(tFinal)
}
