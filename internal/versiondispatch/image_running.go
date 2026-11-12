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

var runningImageLeaseMu sync.Mutex
var runningImageLeaseWarning sync.Once

// LeaseRunningImage best-effort leases an existing image when startup did not
// inherit a lease. It never recreates a vanished entry and diagnoses at most once.
// Call before admitting work or launching children; workers' stderr is their log.
func LeaseRunningImage() {
	runningImageLeaseMu.Lock()
	defer runningImageLeaseMu.Unlock()
	if imagelease.Current() != nil {
		return
	}
	path, err := os.Executable()
	if err == nil {
		err = leaseRunningImage(path)
	}
	if err != nil {
		runningImageLeaseWarning.Do(func() {
			_, _ = fmt.Fprintf(os.Stderr, messages.DispatchImageLeaseUnavailableFmt, err)
		})
	}
}

func leaseRunningImage(path string) error {
	dir := filepath.Dir(path)
	images := filepath.Dir(dir)
	if filepath.Base(path) != imageName || !imagelease.ValidSHA(filepath.Base(dir)) || filepath.Base(images) != imagesDirName {
		return nil
	}
	cacheRoot, err := cacheRootDir(RealSystem{})
	if err != nil {
		return err
	}
	// Compare roots by filesystem identity: Darwin preserves executable spelling
	// (including /var aliases), and AL_CACHE_DIR can be relative. An unrelated
	// shaped executable should stay quiet even when the configured root is absent.
	rootInfo, rootErr := os.Stat(cacheRoot)
	imageRootInfo, imageRootErr := os.Stat(filepath.Dir(images)) // #nosec G703 -- read-only identity check of the executing image’s cache root.
	if rootErr != nil || imageRootErr != nil || !os.SameFile(rootInfo, imageRootInfo) {
		return nil
	}
	// Resolve only the root after rejecting symlinked images/ and entries. Resolving
	// the entire executable first would hide those unsafe layout components.
	if !ownedDir(images) || !ownedDir(dir) {
		return errImageMoved
	}
	physicalRoot, err := filepath.EvalSymlinks(filepath.Dir(images))
	if err != nil {
		return err
	}
	dir = filepath.Join(physicalRoot, imagesDirName, filepath.Base(dir))
	path = filepath.Join(dir, imageName)
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
