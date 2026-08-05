package wizard

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnvPreviewNeverShowsASecretValue covers the wizard's `.env` diff preview.
// The preview is printed to the terminal and is the one place a stored API key
// could be echoed back, so every parsed value must be replaced with a redaction
// marker. An assignment line is rebuilt rather than copied — the key is trimmed
// and the value is requoted — so what is asserted here is that the parts a user
// needs to read the diff survive that rewrite: the key, any `export` prefix, any
// trailing comment, comment-only lines, and the line count.
func TestEnvPreviewNeverShowsASecretValue(t *testing.T) {
	const kept = "kept-secret-value"
	const rotated = "rotated-secret-value"
	const added = "added-secret-value"

	current := strings.Join([]string{
		"# Agent Layer secrets",
		"",
		"KEPT=" + kept,
		`ROTATED="` + rotated + `x" # rotate me`,
		"export EXPORTED='" + kept + "' # exported form",
		"EMPTY=",
		"",
	}, "\n")
	next := strings.Join([]string{
		"# Agent Layer secrets",
		"",
		"KEPT=" + kept,
		`ROTATED="` + rotated + `" # rotate me`,
		"export EXPORTED='" + kept + "' # exported form",
		"ADDED=" + added,
		"EMPTY=",
		"",
	}, "\n")

	currentPreview, nextPreview, err := redactEnvPreviewContent(current, next)
	require.NoError(t, err)

	for _, secret := range []string{kept, rotated, added} {
		assert.NotContains(t, currentPreview, secret, "current preview leaked a secret")
		assert.NotContains(t, nextPreview, secret, "proposed preview leaked a secret")
	}

	// An unchanged value is redacted without implying it changed.
	assert.Contains(t, currentPreview, `KEPT="<redacted>"`)
	assert.Contains(t, nextPreview, `KEPT="<redacted>"`)
	// A value that differs between the two sides is labelled per side, so the
	// diff still shows the user that the secret is being replaced.
	assert.Contains(t, currentPreview, `ROTATED="<redacted current>"`)
	assert.Contains(t, nextPreview, `ROTATED="<redacted proposed>"`)
	assert.Contains(t, nextPreview, `ADDED="<redacted proposed>"`)
	// An unset key stays visibly unset rather than looking like a hidden value.
	assert.Contains(t, nextPreview, `EMPTY=""`)

	// Context a reader needs to interpret the diff survives untouched.
	assert.Contains(t, nextPreview, `export EXPORTED="<redacted>" # exported form`)
	assert.Contains(t, nextPreview, `ROTATED="<redacted proposed>" # rotate me`)
	assert.Contains(t, nextPreview, "# Agent Layer secrets")
	assert.Equal(t, strings.Count(next, "\n"), strings.Count(nextPreview, "\n"))
}

func TestEnvPreviewFailsOnUnparseableEnvContent(t *testing.T) {
	_, _, err := redactEnvPreviewContent("KEY=value\n", "not an assignment\n")
	assert.Error(t, err, "the wizard previewed a .env file it could not parse")
}

func TestEmptyEnvPreviewIsLeftAlone(t *testing.T) {
	currentPreview, nextPreview, err := redactEnvPreviewContent("", "KEY=secret\n")
	require.NoError(t, err)
	assert.Equal(t, "", currentPreview)
	assert.Contains(t, nextPreview, `KEY="<redacted proposed>"`)
}
