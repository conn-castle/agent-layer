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
import re
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
        provider_paths = (".claude",) if mode == "provider-paths" else ()
        submit = WorkspaceSubmission(root, [".agent-layer"], provider_paths)
        submit.snapshot_path = Path(path) / "baseline.json"
        git = submit.git
        git("init", "-q", "-b", "base")
        (root / ".gitignore").write_text("*.ignored\n")
        (root / "file.txt").write_text("older\n")
        (root / ".agent-layer").mkdir()
        (root / ".agent-layer" / "config").write_text("original\n")
        if provider_paths:
            (root / ".claude").mkdir()
            (root / ".claude" / "tracked.json").write_text("{}\n")
        git("add", ".")
        git("-c", "user.email=fixture@example.invalid", "-c", "user.name=Fixture", "commit", "-qm", "Baseline")
        older = git("rev-parse", "HEAD").decode().strip()
        base_file = "one\ntwo\nthree\nfour\nfive\nsix\n" if mode in ("removed-context", "reverted-hunk") else "base\n"
        (root / "file.txt").write_text(base_file)
        git("add", "file.txt")
        git("-c", "user.email=fixture@example.invalid", "-c", "user.name=Fixture", "commit", "-qm", "Task base")
        base = git("rev-parse", "HEAD").decode().strip()
        if mode == "unrelated-root":
            git("switch", "-c", "unrelated", older)
            git("-c", "user.email=fixture@example.invalid", "-c", "user.name=Fixture",
                "commit", "--allow-empty", "-qm", "Unrelated image branch")
            git("switch", "base")
        (root / "fixture.txt").write_text("image fixture\n")
        (root / "image.ignored").write_text("ignored image fixture\n")
        submit.snapshot()
        assert git("log", "-1", "--format=%H", "main").decode().strip() == base
        (root / ".agent-layer" / "config").write_text("projection\n")
        (root / ".agent-layer" / "secret").write_text("excluded secret\n")
        if mode == "new-unrelated-root":
            git("switch", "-c", "unrelated", older)
            git("commit", "--allow-empty", "-qm", "Unrelated agent branch")
        if mode in ("unrelated-root", "new-unrelated-root"):
            git("switch", "unrelated")
            try:
                submit.collect(Path(path) / "conflicts")
            except RuntimeError as error:
                assert "outside the pinned task history" in str(error), error
                assert git("branch", "--show-current").strip() == b"unrelated"
                return
            raise AssertionError("unrelated image branch was submitted")
        if mode in ("older-only", "root-with-older"):
            git("worktree", "add", "--detach", Path(path) / "older", older)
        if mode not in ("empty", "older-only"):
            if mode.startswith("root") or mode in ("force-staged", "assume-unchanged", "skip-worktree", "provider-paths"):
                solution = root
            elif mode == "detached":
                solution = Path(path) / "linked"
                git("worktree", "add", "--detach", solution)
            else:
                solution = root / "nested-worktree"
                git("worktree", "add", "-b", "solution", solution)
            if mode in ("assume-unchanged", "skip-worktree"):
                git("update-index", f"--{mode}", "file.txt", directory=solution)
            candidate_file = base_file.replace("five", "linked change") if mode == "removed-context" else "candidate\n"
            if mode == "reverted-hunk":
                candidate_file = base_file.replace("one", "candidate first hunk").replace("five", "candidate second hunk")
            (solution / "file.txt").write_text(candidate_file)
            (solution / "addition.txt").write_text("addition\n")
            if mode == "force-staged":
                (solution / "forced.ignored").write_text("explicitly staged fixture\n")
                git("add", "-f", "forced.ignored", "image.ignored", directory=solution)
            if provider_paths:
                (solution / ".claude" / "tracked.json").write_text('{"updated": true}\n')
                (solution / ".claude" / "generated.json").write_text("{}\n")
                (solution / ".gemini").mkdir()
                (solution / ".gemini" / "settings.json").write_text("{}\n")
            if mode.startswith("root-amended") or mode in ("linked-amended", "removed-amended"):
                git("add", "file.txt", "addition.txt", directory=solution)
                git("commit", "--amend", "--no-edit", directory=solution)
                if mode in ("root-amended-twice", "root-amended-follow-up"):
                    (root / "addition.txt").write_text("later improvement\n")
                    git("add", "addition.txt")
                    if mode == "root-amended-twice":
                        git("commit", "--amend", "--no-edit")
                    else:
                        git("commit", "-qm", "Follow-up to amended commit")
            if mode in ("committed", "reverted-hunk", "cherry-reverted") or (
                    mode.startswith(("removed", "integrated")) and mode != "removed-amended"):
                if mode in ("removed-squashed", "removed-cherry-picked", "integrated-squashed"):
                    git("add", "file.txt", directory=solution)
                    git("commit", "-qm", "Candidate implementation", directory=solution)
                git("add", ".", directory=solution)
                git("commit", "-qm", "Candidate", directory=solution)
            if mode == "integrated-duplicate":
                duplicate = Path(path) / "duplicate"
                git("worktree", "add", "-b", "duplicate", duplicate, base)
                git("cherry-pick", "-x", "solution", directory=duplicate)
                assert git("rev-parse", "solution") != git("rev-parse", "duplicate")
                assert git("rev-parse", "solution^{tree}") == git("rev-parse", "duplicate^{tree}")
                git("worktree", "remove", duplicate)
            if mode == "cherry-reverted":
                (root / "main-only.txt").write_text("unrelated change\n")
                git("add", "main-only.txt")
                git("commit", "-qm", "Independent main change")
                git("cherry-pick", "-x", "solution")
                git("revert", "--no-edit", "HEAD", "HEAD^")
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
            if mode.startswith("integrated") or mode in ("removed-merged", "removed-reverted", "removed-squashed", "removed-cherry-picked"):
                if mode in ("removed-merged", "removed-reverted", "integrated-clean", "integrated-duplicate", "integrated-dirty",
                            "integrated-reverted", "integrated-partially-reverted"):
                    git("merge", "--ff-only", "solution")
                elif mode in ("removed-squashed", "integrated-squashed"):
                    git("merge", "--squash", "solution")
                    git("commit", "-qm", "Integrate solution")
                else:
                    git("cherry-pick", "-x", f"{base}..solution")
                if mode in ("removed-reverted", "integrated-reverted"):
                    git("revert", "--no-edit", "solution")
                else:
                    (root / "follow-up.txt").write_text("independent follow-up\n")
                    git("add", "follow-up.txt")
                    if mode in ("removed-merged", "integrated-clean", "integrated-duplicate"):
                        (root / "file.txt").write_text("refine the integrated implementation\n")
                        git("add", "file.txt")
                    git("commit", "-qm", "Follow-up")
            if mode == "integrated-partially-reverted":
                (root / "file.txt").write_text(base_file)
                git("add", "file.txt")
                git("commit", "-qm", "Revert just the implementation")
            if mode == "reverted-hunk":
                git("merge", "--ff-only", "solution")
                (root / "file.txt").write_text(base_file.replace("five", "candidate second hunk"))
                git("add", "file.txt")
                git("commit", "-qm", "Revert the first hunk")
            if mode == "integrated-dirty":
                (solution / "independent.txt").write_text("independent uncommitted change\n")
            if mode in ("ambiguous", "removed-ambiguous", "integrated-dirty", "removed-merge-extra",
                        "integrated-partially-reverted", "integrated-reverted", "removed-reverted",
                        "reverted-hunk", "cherry-reverted"):
                fully_reverted = mode in ("integrated-reverted", "removed-reverted", "cherry-reverted")
                if not fully_reverted and mode not in ("integrated-partially-reverted", "reverted-hunk"):
                    (root / "file.txt").write_text("conflicting root change\n")
                conflicts = Path(path) / "conflicts"
                try:
                    submit.collect(conflicts)
                except RuntimeError as error:
                    assert "multiple different benchmark submissions" in str(error)
                    candidates = json.loads((conflicts / "sources.json").read_text())
                    assert len(candidates) == 2
                    patches = [(conflicts / f"{tree}.patch").read_bytes() for tree in candidates]
                    if fully_reverted:
                        assert b"" in patches
                    elif mode == "reverted-hunk":
                        assert sum(b"+candidate first hunk" in patch for patch in patches) == 1
                        assert all(b"+candidate second hunk" in patch for patch in patches)
                    else:
                        root_change = b"+independent follow-up" if mode == "integrated-partially-reverted" else b"+conflicting root change"
                        assert any(root_change in patch for patch in patches)
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
        if mode in ("removed-merged", "removed-squashed", "removed-cherry-picked",
                    "integrated-clean", "integrated-duplicate", "integrated-squashed"):
            expected.append(b"follow-up.txt")
        if provider_paths:
            expected.extend([b".claude/tracked.json", b".gemini/settings.json"])
        assert git("diff", "--name-only", base, "HEAD").splitlines() == sorted(expected), (mode, exported)
        if mode in ("removed-merged", "integrated-clean", "integrated-duplicate"):
            assert b"+refine the integrated implementation" in exported
        if mode in ("root-amended-twice", "root-amended-follow-up"):
            assert b"+later improvement" in exported

for mode in ("empty", "older-only", "root", "root-with-older", "root-amended", "root-amended-twice",
             "root-amended-follow-up", "linked-amended", "removed-amended", "unrelated-root", "new-unrelated-root", "force-staged",
             "assume-unchanged", "skip-worktree", "provider-paths", "committed", "detached", "removed",
             "ambiguous", "removed-ambiguous", "removed-merged", "removed-squashed", "removed-cherry-picked",
             "removed-reverted", "integrated-clean", "integrated-duplicate", "integrated-dirty", "integrated-squashed",
             "integrated-reverted", "integrated-partially-reverted", "reverted-hunk", "cherry-reverted",
             "removed-context", "removed-merge-extra"):
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

func TestPreserveReplayPatchBackfillsOnlyMatchingDigest(t *testing.T) {
	stage := t.TempDir()
	patch := filepath.Join("jobs", "trial", benchmarkArtifactsDir, benchmarkModelPatchFile)
	marker := filepath.Join("jobs", "trial", benchmarkAgentDir, submissionDigestFile)
	exact := "exact submission"
	writeVerifierStageFile(t, stage, patch, exact)
	require.NoError(t, preserveReplayPatch(stage))

	writeVerifierStageFile(t, stage, patch, "sanitized patch")
	writeVerifierStageFile(t, stage, marker, "sanitized digest")
	require.NoError(t, preserveReplayPatch(stage))
	preserved := filepath.Join(stage, replayInputDir, submissionDigestFile)
	require.NoFileExists(t, preserved)

	writeVerifierStageFile(t, stage, marker, digestBytes([]byte(exact))+"\n")
	require.NoError(t, preserveReplayPatch(stage))
	require.NoError(t, verifySubmissionDigest(filepath.Join(stage, replayInputDir, benchmarkModelPatchFile), preserved))
}

func TestPreserveReplayPatchRejectsMismatchedPatchAfterPartialWrite(t *testing.T) {
	stage := t.TempDir()
	patch := filepath.Join("jobs", "trial", benchmarkArtifactsDir, benchmarkModelPatchFile)
	marker := filepath.Join(replayInputDir, submissionDigestFile)
	exact := "exact patch"
	writeVerifierStageFile(t, stage, marker, digestBytes([]byte(exact))+"\n")
	writeVerifierStageFile(t, stage, patch, "rewritten evidence")
	preserved := filepath.Join(stage, replayInputDir, benchmarkModelPatchFile)
	require.ErrorContains(t, preserveReplayPatch(stage), "recover exact replay patch")
	require.NoFileExists(t, preserved)

	writeVerifierStageFile(t, stage, patch, exact)
	require.NoError(t, preserveReplayPatch(stage))
	require.NoError(t, verifySubmissionDigest(preserved, filepath.Join(stage, marker)))
}
