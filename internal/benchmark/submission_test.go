package benchmark

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGitSubmissionCollectsActualWorktreeChanges(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python 3 is required to exercise the container's Git collector")
	}
	source, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
	require.NoError(t, err)
	adapter := filepath.Join(t.TempDir(), "adapter.py")
	require.NoError(t, os.WriteFile(adapter, source, 0o600))
	const script = `
import ast
import hashlib
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

source = ast.parse(Path(sys.argv[1]).read_text())
collector = next(node for node in source.body if isinstance(node, ast.ClassDef) and node.name == "WorkspaceSubmission")
exec(compile(ast.Module(body=[collector], type_ignores=[]), sys.argv[1], "exec"))

def exercise(mode):
    with tempfile.TemporaryDirectory() as path:
        root = Path(path) / "app"
        root.mkdir()
        submit = WorkspaceSubmission(root, [".agent-layer"])
        submit.snapshot_path = Path(path) / "baseline.json"
        git = submit.git
        git("init", "-q", "-b", "base")
        (root / "file.txt").write_text("older\n")
        (root / ".agent-layer").mkdir()
        (root / ".agent-layer" / "config").write_text("original\n")
        git("add", ".")
        git("-c", "user.email=fixture@example.invalid", "-c", "user.name=Fixture", "commit", "-qm", "Baseline")
        older = git("rev-parse", "HEAD").decode().strip()
        (root / "file.txt").write_text("base\n")
        git("add", "file.txt")
        git("-c", "user.email=fixture@example.invalid", "-c", "user.name=Fixture", "commit", "-qm", "Task base")
        base = git("rev-parse", "HEAD").decode().strip()
        (root / "fixture.txt").write_text("image fixture\n")
        submit.snapshot()
        assert git("log", "-1", "--format=%H", "main").decode().strip() == base
        (root / ".agent-layer" / "config").write_text("projection\n")
        (root / ".agent-layer" / "secret").write_text("excluded secret\n")
        if mode in ("older-only", "root-with-older"):
            git("worktree", "add", "--detach", Path(path) / "older", older)
        if mode not in ("empty", "older-only"):
            if mode in ("root", "root-with-older"):
                solution = root
            elif mode == "detached":
                solution = Path(path) / "linked"
                git("worktree", "add", "--detach", solution)
            else:
                solution = root / "nested-worktree"
                git("worktree", "add", "-b", "solution", solution)
            (solution / "file.txt").write_text("candidate\n")
            (solution / "addition.txt").write_text("addition\n")
            if mode == "committed" or mode.startswith("removed"):
                if mode in ("removed-squashed", "removed-cherry-picked"):
                    git("add", "file.txt", directory=solution)
                    git("commit", "-qm", "Candidate implementation", directory=solution)
                git("add", ".", directory=solution)
                git("commit", "-qm", "Candidate", directory=solution)
            if mode.startswith("removed"):
                git("worktree", "remove", solution)
            if mode in ("removed-merged", "removed-squashed", "removed-cherry-picked"):
                if mode == "removed-merged":
                    git("merge", "--ff-only", "solution")
                elif mode == "removed-squashed":
                    git("merge", "--squash", "solution")
                    git("commit", "-qm", "Integrate solution")
                else:
                    git("cherry-pick", "-x", f"{base}..solution")
                (root / "file.txt").write_text("candidate with a follow-up\n")
                git("commit", "-qam", "Follow-up")
            if mode in ("ambiguous", "removed-ambiguous"):
                (root / "file.txt").write_text("conflicting root change\n")
                conflicts = Path(path) / "conflicts"
                try:
                    submit.collect(conflicts)
                except RuntimeError as error:
                    assert "multiple different benchmark submissions" in str(error)
                    candidates = json.loads((conflicts / "sources.json").read_text())
                    assert len(candidates) == 2
                    patches = [(conflicts / f"{tree}.patch").read_bytes() for tree in candidates]
                    assert any(b"+conflicting root change" in patch for patch in patches)
                    assert any(b"+candidate" in patch for patch in patches)
                    return
                raise AssertionError("ambiguous changes were accepted")
        submit.collect()
        exported = git("diff", "--binary", base, "HEAD")
        marker = Path(path) / "digest"
        empty = mode in ("empty", "older-only")
        submit.digest(marker, not empty)
        assert marker.read_text().strip() == hashlib.sha256(exported).hexdigest()
        assert git("diff", "--name-only", base, "HEAD").splitlines() == (
            [] if empty else [b"addition.txt", b"file.txt"]
        ), (mode, exported)

for mode in ("empty", "older-only", "root", "root-with-older", "committed", "detached", "removed",
             "ambiguous", "removed-ambiguous", "removed-merged", "removed-squashed", "removed-cherry-picked"):
    exercise(mode)
`
	command := exec.CommandContext(t.Context(), python, "-c", script, adapter) // #nosec G204 -- exercises the embedded collector in disposable Git repositories.
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestPierSubmissionRejectsMissingOrChangedPatch(t *testing.T) {
	stage := t.TempDir()
	patch := filepath.Join("jobs", "trial", benchmarkArtifactsDir, benchmarkModelPatchFile)
	digest := filepath.Join("jobs", "trial", benchmarkAgentDir, submissionDigestFile)
	writeVerifierStageFile(t, stage, digest, digestBytes([]byte("candidate"))+"\n")
	require.ErrorContains(t, validatePierSubmission(stage), "0 exported model patches")
	writeVerifierStageFile(t, stage, patch, "")
	require.ErrorContains(t, validatePierSubmission(stage), "differs from the collected Git submission")
	writeVerifierStageFile(t, stage, patch, "candidate")
	require.NoError(t, validatePierSubmission(stage))
	writeVerifierStageFile(t, stage, patch, "")
	writeVerifierStageFile(t, stage, digest, digestBytes(nil)+"\n")
	require.NoError(t, validatePierSubmission(stage))
}

func TestBareSubmissionCanEditRepositoryInstructions(t *testing.T) {
	stage := t.TempDir()
	patch := filepath.Join("jobs", "trial", benchmarkArtifactsDir, benchmarkModelPatchFile)
	diff := "diff --git a/AGENTS.md b/AGENTS.md\n"
	writeVerifierStageFile(t, stage, patch, diff)
	size, err := submittedPatchBytes(stage, false)
	require.NoError(t, err)
	require.EqualValues(t, len(diff), size)
	_, err = submittedPatchBytes(stage, true)
	require.ErrorContains(t, err, "injected treatment file")
}
