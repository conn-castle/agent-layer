package benchmark

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const submissionDigestFile = "submission.sha256"

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

func validatePierSubmission(stage string) error {
	patches, err := stageJobFiles(stage, false, func(path string, entry fs.DirEntry) bool {
		return entry.Type().IsRegular() && isSubmittedModelPatch(path, entry)
	})
	if err != nil {
		return fmt.Errorf("find benchmark submission: %w", err)
	}
	if len(patches) != 1 {
		return fmt.Errorf("benchmark submission has %d exported model patches, expected one", len(patches))
	}
	patch := patches[0]
	agentDir := filepath.Join(filepath.Dir(filepath.Dir(patch)), benchmarkAgentDir)
	return verifySubmissionDigest(patch, filepath.Join(agentDir, submissionDigestFile))
}

func verifySubmissionDigest(patch, digest string) error {
	agentEvidence, err := os.ReadFile(digest) // #nosec G304 -- path is under the private trial stage.
	if err != nil {
		return fmt.Errorf("read benchmark submission digest: %w", err)
	}
	exported, err := os.ReadFile(patch) // #nosec G304 -- path is under the private trial stage.
	if err != nil {
		return fmt.Errorf("read benchmark submission patch: %w", err)
	}
	if strings.TrimSpace(string(agentEvidence)) != digestBytes(exported) {
		return fmt.Errorf("benchmark model.patch differs from the collected Git submission")
	}
	return nil
}

func isDispatchRecord(path string, entry fs.DirEntry) bool {
	return parentDirIs(path, dispatchEvidenceDir) && filepath.Ext(path) == ".json" &&
		!isDispatchPreflightEvidence(entry.Name())
}
