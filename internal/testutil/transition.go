package testutil

import (
	"embed"
	"testing"

	"github.com/stretchr/testify/require"
)

// Original v0.24.3 release bytes, checked against published manifests by tests.
//
//go:embed testdata/transition/*.md
var transitionFixtures embed.FS

// TransitionFixture returns pristine published transition content.
func TransitionFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := transitionFixtures.ReadFile("testdata/transition/" + name)
	require.NoError(t, err)
	return raw
}
