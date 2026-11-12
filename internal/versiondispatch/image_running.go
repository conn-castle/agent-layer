package versiondispatch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/conn-castle/agent-layer/internal/imagelease"
	"github.com/conn-castle/agent-layer/internal/messages"
)

var runningImageLeaseOnce sync.Once

// LeaseRunningImage best-effort leases an existing image when startup did not
// inherit a lease. It never recreates a vanished entry and diagnoses at most once.
// Call before admitting work or launching children; workers' stderr is their log.
func LeaseRunningImage() {
	runningImageLeaseOnce.Do(func() {
		if imagelease.Current() != nil {
			return
		}
		if err := leaseRunningImage(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, messages.DispatchImageLeaseUnavailableFmt, err)
		}
	})
}

func leaseRunningImage() error {
	path, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if filepath.Base(path) != imageName || !imagelease.ValidSHA(filepath.Base(dir)) {
		return nil
	}
	cacheRoot, err := cacheRootDir(RealSystem{})
	if err != nil {
		return err
	}
	// Cache-root aliases are supported, but images/ and the entry themselves
	// must be real, owned directories (checked by the creation-lock protocol).
	cacheRoot, err = filepath.EvalSymlinks(cacheRoot)
	if err != nil {
		return err
	}
	if filepath.Dir(dir) != filepath.Join(cacheRoot, imagesDirName) {
		return nil
	}
	lock, err := lockImageMode(context.Background(), RealSystem{}, dir, false)
	if err != nil {
		return err
	}
	defer func() { _ = lock.release() }()
	lease, err := leaseImage(path)
	if err != nil {
		return err
	}
	imagelease.Retain(lease)
	return nil
}
