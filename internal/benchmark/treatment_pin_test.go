package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pinnableTreatmentBundle returns a builder for a minimal but internally
// consistent treatment bundle, plus a counter of how many times it built.
func pinnableTreatmentBundle(t *testing.T, adapterBody string) (func() (*TreatmentBundle, error), *int) {
	t.Helper()
	builds := 0
	return func() (*TreatmentBundle, error) {
		builds++
		root := t.TempDir()
		adapter := filepath.Join(root, "adapter", "pier_agent_layer.py")
		if err := os.MkdirAll(filepath.Dir(adapter), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(adapter, []byte(adapterBody), 0o600); err != nil {
			return nil, err
		}
		manifest, err := treatmentManifest(root, TreatmentInstructionsOnly, nil, TreatmentDispatchConfig{})
		if err != nil {
			return nil, err
		}
		manifestHash, err := hashCanonical(manifest)
		if err != nil {
			return nil, err
		}
		adapterHash, err := fileSHA256(adapter)
		if err != nil {
			return nil, err
		}
		return &TreatmentBundle{
			Root: root, Manifest: manifest, ManifestHash: manifestHash,
			AdapterPath: adapter, AdapterSHA256: adapterHash,
		}, nil
	}, &builds
}

func pinTreatmentFixture(t *testing.T, name string) (string, *TreatmentBundle) {
	t.Helper()
	state := t.TempDir()
	build, _ := pinnableTreatmentBundle(t, "pinned adapter\n")
	model, effort, err := ParseModelSelection("luna:medium")
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := pinMatrixTreatmentBundle(
		state, name, "Agent Layer "+name, model, effort,
		TreatmentInstructionsOnly, TreatmentDispatchConfig{}, build,
	)
	if err != nil {
		t.Fatal(err)
	}
	return state, bundle
}

// reloadTreatmentPin repeats the exact pin lookup a later matrix run performs.
// The builder fails the test, because a valid pin must never be rebuilt.
func reloadTreatmentPin(t *testing.T, state, name string) (*TreatmentBundle, error) {
	t.Helper()
	model, effort, err := ParseModelSelection("luna:medium")
	if err != nil {
		t.Fatal(err)
	}
	return pinMatrixTreatmentBundle(
		state, name, "Agent Layer "+name, model, effort,
		TreatmentInstructionsOnly, TreatmentDispatchConfig{},
		func() (*TreatmentBundle, error) {
			t.Fatal("a pin that should have been reused or rejected was rebuilt instead")
			return nil, nil
		},
	)
}

func TestMatrixTreatmentPinRefusesABundleWhoseContentChanged(t *testing.T) {
	state, bundle := pinTreatmentFixture(t, "final-skills")
	if err := os.WriteFile(filepath.Join(bundle.Root, "smuggled.md"), []byte("extra instruction\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The pin's manifest hash is the treatment's published identity. Reusing a
	// bundle whose bytes no longer hash to it would attribute paid results to a
	// treatment that was never run.
	_, err := reloadTreatmentPin(t, state, "final-skills")
	if err == nil || !strings.Contains(err.Error(), "content does not match its manifest") {
		t.Fatalf("edited bundle error = %v", err)
	}
}

func TestMatrixTreatmentPinRefusesEditedPinMetadata(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*matrixTreatmentPin)
		wanted string
	}{
		{"renamed", func(p *matrixTreatmentPin) { p.Name = "other-pin" }, "invalid identity metadata"},
		{"relabeled", func(p *matrixTreatmentPin) { p.Label = "Something Else" }, "invalid identity metadata"},
		{"remodelled", func(p *matrixTreatmentPin) { p.Model = publishedFable }, "invalid identity metadata"},
		{"unhashed", func(p *matrixTreatmentPin) { p.ManifestHash = "" }, "invalid identity metadata"},
		{
			"forged adapter checksum",
			func(p *matrixTreatmentPin) { p.AdapterSHA256 = strings.Repeat("0", 64) },
			"adapter checksum does not match",
		},
		{
			"claims a binary it does not have",
			func(p *matrixTreatmentPin) { p.LinuxBinarySHA256 = strings.Repeat("0", 64) },
			"binary checksum does not match",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, _ := pinTreatmentFixture(t, "final-skills")
			pinPath := ""
			pins := filepath.Join(state, "treatment-pins")
			entries, err := os.ReadDir(pins)
			if err != nil || len(entries) != 1 {
				t.Fatalf("pin directory = %#v, %v", entries, err)
			}
			pinPath = filepath.Join(pins, entries[0].Name(), "pin.json")

			var pin matrixTreatmentPin
			if err := readCampaignJSON(pinPath, &pin); err != nil {
				t.Fatal(err)
			}
			test.mutate(&pin)
			data, err := json.Marshal(pin)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(pinPath, data, 0o600); err != nil {
				t.Fatal(err)
			}

			// Pin state is the only record of what a paid arm actually ran, so an
			// inconsistent record has to stop the campaign rather than be
			// repaired by rebuilding from the current working tree.
			if _, err := reloadTreatmentPin(t, state, "final-skills"); err == nil ||
				!strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("%s pin error = %v", test.name, err)
			}
		})
	}
}

func TestMatrixTreatmentPinRefusesAnInterruptedPublish(t *testing.T) {
	state, _ := pinTreatmentFixture(t, "final-skills")
	pins := filepath.Join(state, "treatment-pins")
	entries, err := os.ReadDir(pins)
	if err != nil || len(entries) != 1 {
		t.Fatalf("pin directory = %#v, %v", entries, err)
	}
	if err := os.Remove(filepath.Join(pins, entries[0].Name(), "pin.json")); err != nil {
		t.Fatal(err)
	}

	// A pin directory without its record cannot be identified. Treating it as
	// absent would overwrite bundle bytes that an in-progress arm is using.
	if _, err := reloadTreatmentPin(t, state, "final-skills"); err == nil ||
		!strings.Contains(err.Error(), "is incomplete") {
		t.Fatalf("interrupted pin error = %v", err)
	}
}

func TestDistinctTreatmentPinNamesKeepSeparateBundles(t *testing.T) {
	state := t.TempDir()
	model, effort, err := ParseModelSelection("luna:medium")
	if err != nil {
		t.Fatal(err)
	}
	first, firstBuilds := pinnableTreatmentBundle(t, "first adapter\n")
	second, secondBuilds := pinnableTreatmentBundle(t, "second adapter\n")

	before, err := pinMatrixTreatmentBundle(
		state, "iteration-1", "Iteration 1", model, effort,
		TreatmentInstructionsOnly, TreatmentDispatchConfig{}, first,
	)
	if err != nil {
		t.Fatal(err)
	}
	after, err := pinMatrixTreatmentBundle(
		state, "iteration-2", "Iteration 2", model, effort,
		TreatmentInstructionsOnly, TreatmentDispatchConfig{}, second,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Comparing two Agent Layer iterations in one matrix requires both bundles
	// to survive side by side; a shared location would make the second run
	// silently reuse or overwrite the first.
	if *firstBuilds != 1 || *secondBuilds != 1 {
		t.Fatalf("builds = %d and %d", *firstBuilds, *secondBuilds)
	}
	if before.Root == after.Root || before.ManifestHash == after.ManifestHash {
		t.Fatalf("distinct pins shared identity: %q and %q", before.Root, after.Root)
	}
	for path, wanted := range map[string]string{
		before.AdapterPath: "first adapter\n",
		after.AdapterPath:  "second adapter\n",
	} {
		data, readErr := os.ReadFile(path) // #nosec G304 -- path is inside a test-owned temporary pin.
		if readErr != nil || string(data) != wanted {
			t.Fatalf("pinned adapter %s = %q, %v", path, data, readErr)
		}
	}
}
