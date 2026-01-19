package run

import (
	"crypto/rand"
	"testing"
)

func TestCreate_RandomError(t *testing.T) {
	original := rand.Reader
	rand.Reader = errReader{} // Defined in run_test.go, assumes same package
	defer func() {
		rand.Reader = original
	}()

	root := t.TempDir()
	_, err := Create(root)
	if err == nil {
		t.Fatalf("expected error from random source failure")
	}
}
