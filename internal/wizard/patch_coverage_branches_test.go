package wizard

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplyLegacyAliasToOrder_ReplaceLegacy(t *testing.T) {
	order := []string{"a", "legacy", "b"}
	got := applyLegacyAliasToOrder(order, "legacy", "canonical")
	assert.Equal(t, []string{"a", "canonical", "b"}, got)
}

func TestApplyLegacyAliasToOrder_CanonicalAlreadyPresent(t *testing.T) {
	order := []string{"a", "canonical", "b"}
	got := applyLegacyAliasToOrder(order, "legacy", "canonical")
	assert.Equal(t, []string{"a", "canonical", "b"}, got)
}

func TestApplyLegacyAliasToOrder_BothPresent_DedupLegacy(t *testing.T) {
	order := []string{"legacy", "canonical", "other"}
	got := applyLegacyAliasToOrder(order, "legacy", "canonical")
	assert.Equal(t, []string{"canonical", "other"}, got)
}

func TestApplyLegacyAliasToOrder_BothPresent_DedupCanonicalSecond(t *testing.T) {
	order := []string{"canonical", "legacy", "other"}
	got := applyLegacyAliasToOrder(order, "legacy", "canonical")
	assert.Equal(t, []string{"canonical", "other"}, got)
}

func TestApplyLegacyAliasToOrder_NeitherPresent(t *testing.T) {
	order := []string{"a", "b", "c"}
	got := applyLegacyAliasToOrder(order, "legacy", "canonical")
	assert.Equal(t, []string{"a", "b", "c"}, got)
}

func TestApplyLegacyAliasToOrder_Empty(t *testing.T) {
	got := applyLegacyAliasToOrder(nil, "legacy", "canonical")
	assert.Empty(t, got)
}

func TestApplyLegacyAliasToOrder_MultipleLegacy(t *testing.T) {
	order := []string{"legacy", "legacy", "other"}
	got := applyLegacyAliasToOrder(order, "legacy", "canonical")
	assert.Equal(t, []string{"canonical", "other"}, got)
}
