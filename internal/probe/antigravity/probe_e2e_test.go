//go:build agy_e2e

package antigravity

import (
	"context"
	"os/exec"
	"testing"
)

func TestProbeE2E(t *testing.T) {
	if _, err := exec.LookPath("agy"); err != nil {
		t.Skip("agy is not on PATH")
	}
	result, err := Probe(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if !result.Capabilities.PermissionsLoaded {
		t.Fatalf("expected permissions_loaded capability, got %#v", result.Capabilities)
	}
}
