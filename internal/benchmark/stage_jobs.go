package benchmark

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// walkStageJobFiles calls visit for each non-directory below stage/jobs in lexical
// walk order. Visit errors stop the walk and are returned unchanged; visitors must
// not return fs.SkipDir or fs.SkipAll, which WalkDir treats as control values.
// With ignoreMissing, traversal errors matching os.ErrNotExist (a missing jobs
// root or entries that vanish mid-walk) are skipped; visit errors are never tolerated.
func walkStageJobFiles(stage string, ignoreMissing bool, visit func(path string, entry fs.DirEntry) error) error {
	return filepath.WalkDir(filepath.Join(stage, "jobs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if ignoreMissing && errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		return visit(path, entry)
	})
}

// stageJobFiles returns the non-directory paths accepted by match in lexical
// walk order, with the same traversal error handling as walkStageJobFiles.
func stageJobFiles(stage string, ignoreMissing bool, match func(path string, entry fs.DirEntry) bool) ([]string, error) {
	var paths []string
	err := walkStageJobFiles(stage, ignoreMissing, func(path string, entry fs.DirEntry) error {
		if match(path, entry) {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}

func parentDirIs(path, name string) bool {
	return filepath.Base(filepath.Dir(path)) == name
}

func isSubmittedModelPatch(path string, entry fs.DirEntry) bool {
	return entry.Name() == benchmarkModelPatchFile && parentDirIs(path, benchmarkArtifactsDir)
}

func isDispatchRecord(path string, entry fs.DirEntry) bool {
	return parentDirIs(path, dispatchEvidenceDir) && filepath.Ext(path) == ".json" &&
		!isDispatchPreflightEvidence(entry.Name())
}
