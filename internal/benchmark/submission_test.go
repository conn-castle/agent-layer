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
import shutil
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
        (root / ".gitignore").write_text("*.ignored\n")
        (root / "file.txt").write_text("older\n")
        (root / ".agent-layer").mkdir()
        (root / ".agent-layer" / "config").write_text("original\n")
        git("add", ".")
        git("-c", "user.email=fixture@example.invalid", "-c", "user.name=Fixture", "commit", "-qm", "Baseline")
        older = git("rev-parse", "HEAD").decode().strip()
        base_file = "one\ntwo\nthree\nfour\nfive\nsix\n" if mode == "removed-context" else "base\n"
        (root / "file.txt").write_text(base_file)
        git("add", "file.txt")
        git("-c", "user.email=fixture@example.invalid", "-c", "user.name=Fixture", "commit", "-qm", "Task base")
        base = git("rev-parse", "HEAD").decode().strip()
        (root / "fixture.txt").write_text("image fixture\n")
        (root / "image.ignored").write_text("ignored image fixture\n")
        submit.snapshot()
        assert git("log", "-1", "--format=%H", "main").decode().strip() == base
        (root / ".agent-layer" / "config").write_text("projection\n")
        (root / ".agent-layer" / "secret").write_text("excluded secret\n")
        if mode in ("older-only", "root-with-older"):
            git("worktree", "add", "--detach", Path(path) / "older", older)
        if mode not in ("empty", "older-only"):
            if mode in ("root", "root-with-older", "root-amended", "force-staged"):
                solution = root
            elif mode == "detached":
                solution = Path(path) / "linked"
                git("worktree", "add", "--detach", solution)
            else:
                solution = root / "nested-worktree"
                git("worktree", "add", "-b", "solution", solution)
            candidate_file = base_file.replace("five", "linked change") if mode == "removed-context" else "candidate\n"
            (solution / "file.txt").write_text(candidate_file)
            (solution / "addition.txt").write_text("addition\n")
            if mode == "force-staged":
                (solution / "forced.ignored").write_text("explicitly staged fixture\n")
                git("add", "-f", "forced.ignored", "image.ignored", directory=solution)
            if mode == "root-amended":
                git("add", "file.txt", "addition.txt")
                git("commit", "--amend", "--no-edit")
            if mode == "committed" or mode.startswith(("removed", "integrated")):
                if mode in ("removed-squashed", "removed-cherry-picked", "integrated-squashed"):
                    git("add", "file.txt", directory=solution)
                    git("commit", "-qm", "Candidate implementation", directory=solution)
                git("add", ".", directory=solution)
                git("commit", "-qm", "Candidate", directory=solution)
            if mode == "removed-context":
                (root / "file.txt").write_text(base_file.replace("three", "main checkout change"))
                git("add", "file.txt")
                git("commit", "-qm", "Nearby independent change")
                git("cherry-pick", "-x", f"{base}..solution")
            if mode == "removed-merge-extra":
                source_commit = git("rev-parse", "solution").decode().strip()
                side = Path(path) / "side"
                git("worktree", "add", "--detach", side, base)
                git("commit", "--allow-empty", "-qm", "Side branch", directory=side)
                side_commit = git("rev-parse", "HEAD", directory=side).decode().strip()
                git("worktree", "remove", side)
                git("merge", "--no-ff", "--no-commit", side_commit, directory=solution)
                (solution / "merge-only.txt").write_text("change introduced in the merge\n")
                git("add", "merge-only.txt", directory=solution)
                git("commit", "-qm", "Merge with additional change", directory=solution)
                git("cherry-pick", "-x", source_commit)
            if mode.startswith("removed"):
                git("worktree", "remove", solution)
            if mode.startswith("integrated") or mode in ("removed-merged", "removed-squashed", "removed-cherry-picked"):
                if mode in ("removed-merged", "integrated-clean", "integrated-dirty"):
                    git("merge", "--ff-only", "solution")
                elif mode in ("removed-squashed", "integrated-squashed"):
                    git("merge", "--squash", "solution")
                    git("commit", "-qm", "Integrate solution")
                else:
                    git("cherry-pick", "-x", f"{base}..solution")
                (root / "file.txt").write_text("candidate with a follow-up\n")
                git("commit", "-qam", "Follow-up")
            if mode == "integrated-dirty":
                (solution / "independent.txt").write_text("independent uncommitted change\n")
            if mode in ("ambiguous", "removed-ambiguous", "integrated-dirty", "removed-merge-extra"):
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
                    if mode == "integrated-dirty":
                        assert any(b"+independent uncommitted change" in patch for patch in patches)
                    if mode == "removed-merge-extra":
                        assert any(b"+change introduced in the merge" in patch for patch in patches)
                    return
                raise AssertionError("ambiguous changes were accepted")
        submit.collect()
        exported = git("diff", "--binary", base, "HEAD")
        marker = Path(path) / "digest"
        empty = mode in ("empty", "older-only")
        submit.digest(marker, not empty)
        assert marker.read_text().strip() == hashlib.sha256(exported).hexdigest()
        expected = [] if empty else [b"addition.txt", b"file.txt"]
        if mode == "force-staged":
            expected.append(b"forced.ignored")
        assert git("diff", "--name-only", base, "HEAD").splitlines() == expected, (mode, exported)

for mode in ("empty", "older-only", "root", "root-with-older", "root-amended", "force-staged", "committed", "detached", "removed",
             "ambiguous", "removed-ambiguous", "removed-merged", "removed-squashed", "removed-cherry-picked",
             "integrated-clean", "integrated-dirty", "integrated-squashed", "removed-context", "removed-merge-extra"):
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

func TestPreservedSubmissionDigestSurvivesEvidenceSanitization(t *testing.T) {
	stage := t.TempDir()
	patch := filepath.Join("jobs", "trial", benchmarkArtifactsDir, benchmarkModelPatchFile)
	marker := filepath.Join("jobs", "trial", benchmarkAgentDir, submissionDigestFile)
	content := "exact submission"
	writeVerifierStageFile(t, stage, patch, content)
	writeVerifierStageFile(t, stage, marker, digestBytes([]byte(content))+"\n")
	require.NoError(t, preserveReplayPatch(stage))
	writeVerifierStageFile(t, stage, marker, "rewritten evidence\n")
	require.NoError(t, verifySubmissionDigest(
		filepath.Join(stage, replayInputDir, benchmarkModelPatchFile),
		filepath.Join(stage, replayInputDir, submissionDigestFile),
	))
}
