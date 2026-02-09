package install

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// resetManifestCacheForTest clears the package-level embedded manifest cache.
// This keeps tests isolated if a future test needs to force re-walking the embedded
// manifest directory (for example, to inject faults).
func resetManifestCacheForTest() {
	allTemplateManifestOnce = sync.Once{}
	allTemplateManifestByV = nil
	allTemplateManifestErr = nil
}

func TestResetManifestCacheForTest_AllowsReloadAfterError(t *testing.T) {
	resetManifestCacheForTest()

	manifests, err := loadAllTemplateManifests()
	if err != nil {
		t.Fatalf("load manifests: %v", err)
	}
	if len(manifests) == 0 {
		t.Fatal("expected non-empty manifests")
	}

	// Simulate a prior cached failure.
	allTemplateManifestErr = errors.New("boom")
	if _, err := loadAllTemplateManifests(); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected cached error, got %v", err)
	}

	resetManifestCacheForTest()
	if _, err := loadAllTemplateManifests(); err != nil {
		t.Fatalf("expected reload to succeed after reset, got %v", err)
	}
}
