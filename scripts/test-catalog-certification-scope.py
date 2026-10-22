#!/usr/bin/env python3
"""Exercise catalog proof selection against real Git histories and fake GitHub API data."""

import copy
import datetime
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
NOW = 1800000000
REPOSITORY = "example/catalog"
WORKFLOW = ".github/workflows/release-catalog-certification.yml"


def date(seconds):
    return datetime.datetime.fromtimestamp(seconds, datetime.timezone.utc).isoformat()


class CatalogScopeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "repo"
        self.repo.mkdir()
        self.env = {key: value for key, value in os.environ.items()
                    if not key.startswith(("GIT_", "GITHUB_", "CATALOG_"))}
        self.env.update(GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1",
                        GITHUB_REPOSITORY=REPOSITORY, GITHUB_EVENT_NAME="push",
                        CATALOG_FORCE="false", FIXTURES=str(self.root / "api.json"),
                        API_LOG=str(self.root / "api.log"),
                        GITHUB_STEP_SUMMARY=str(self.root / "summary"))
        self.git("init", "-q")
        self.git("config", "user.name", "Catalog Test")
        self.git("config", "user.email", "catalog@example.invalid")
        self.git("config", "commit.gpgsign", "false")
        self.git("config", "core.filemode", "true")
        for name in ("catalog-certification-scope.sh", "catalog-certification-scope.py"):
            target = self.repo / "scripts" / name
            target.parent.mkdir(exist_ok=True)
            shutil.copy2(ROOT / "scripts" / name, target)
        for name in ("internal/benchmark/readiness.go", "internal/gitenv/env.go",
                     "internal/fsutil/file.go", "cmd/al/benchmark.go", "go.mod", "go.sum",
                     WORKFLOW, ".agent-layer/repo-profile.toml", "docs/note.md", "internal/benchmark/readiness_test.go",
                     "internal/benchmark/testdata/task.json"):
            self.write(name)
        self.commit()
        self.source = self.git("rev-parse", "HEAD").strip()
        binary = self.root / "bin"
        binary.mkdir()
        gh = binary / "gh"
        gh.write_text(f"#!{sys.executable}\n" + '''import json, os, sys
from pathlib import Path
with open(os.environ["API_LOG"], "a") as log:
    log.write(sys.argv[-1] + "\\n")
data = json.loads(Path(os.environ["FIXTURES"]).read_text())
if data.get("error"):
    sys.exit(1)
if data.get("malformed"):
    print("not json")
elif "/jobs?" in sys.argv[-1]:
    run_id = sys.argv[-1].split("/runs/")[1].split("/")[0]
    print(json.dumps(data["jobs"][run_id]))
else:
    print(json.dumps(data["runs"]))
''')
        gh.chmod(0o755)
        self.env["PATH"] = str(binary) + os.pathsep + self.env["PATH"]
        self.run = self.proof(1)
        self.data = {"runs": {"workflow_runs": [self.run]}, "jobs": {"1": self.jobs()}}

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.repo, env=self.env,
                                       stderr=subprocess.PIPE, text=True)

    def write(self, name, content="baseline\n"):
        path = self.repo / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)

    def commit(self):
        self.git("add", "-A")
        self.git("commit", "-q", "--allow-empty", "-m", "fixture")

    def proof(self, run_id, **changes):
        proof = dict(id=run_id, run_attempt=1, head_sha=self.source,
                     repository={"full_name": REPOSITORY}, head_repository={"full_name": REPOSITORY},
                     head_branch="main", path=WORKFLOW, event="push", status="completed",
                     conclusion="success", created_at=date(NOW - 3600), updated_at=date(NOW - 1800))
        proof.update(changes)
        return proof

    @staticmethod
    def jobs(attempt=1):
        jobs = [dict(id=shard + 100 * attempt, run_attempt=attempt,
                     name=f"catalog-readiness ({shard}/16)", status="completed", conclusion="success")
                for shard in range(1, 17)]
        jobs.append(dict(id=100 * attempt, run_attempt=attempt, name="classify",
                         status="completed", conclusion="success"))
        return {"total_count": len(jobs), "jobs": jobs}

    def check(self, required, **env):
        Path(self.env["FIXTURES"]).write_text(json.dumps(self.data))
        result = subprocess.run([str(self.repo / "scripts/catalog-certification-scope.sh"),
                                 "--now", str(NOW)], cwd=self.repo, env=self.env | env,
                                capture_output=True, text=True, check=True)
        self.assertEqual(str(required).lower() + "\n", result.stdout, result.stderr)
        self.assertIn(result.stderr.strip(), Path(self.env["GITHUB_STEP_SUMMARY"]).read_text())
        return result.stderr

    def test_full_proof_reuses_and_reports_source(self):
        self.assertIn(f"{REPOSITORY}/actions/runs/1", self.check(False))
        self.assertIn("filter=all", Path(self.env["API_LOG"]).read_text())

    def test_unrelated_changes_reuse(self):
        for path in ("docs/note.md", "internal/benchmark/readiness_test.go",
                     "internal/benchmark/testdata/task.json", "cmd/al/update.go"):
            with self.subTest(path=path):
                self.write(path, "changed\n")
                self.commit()
                self.check(False)

    def test_critical_changes_require_full(self):
        for path in ("internal/benchmark/readiness.go", "internal/gitenv/env.go",
                     "internal/fsutil/file.go", "cmd/al/benchmark.go", "go.mod", "go.sum", WORKFLOW,
                     ".agent-layer/repo-profile.toml",
                     "scripts/catalog-certification-scope.sh", "scripts/catalog-certification-scope.py"):
            with self.subTest(path=path):
                # Reset each scenario; preserve the runnable helper in helper-change cases.
                self.git("reset", "--hard", self.source)
                target = self.repo / path
                with target.open("a") as output:
                    output.write("\n# changed\n" if path.endswith((".py", ".sh")) else "\nchanged\n")
                self.commit()
                self.check(True)

    def test_renames_deletions_modes_and_unusual_names(self):
        for operation in ("rename", "delete", "mode", "newline"):
            with self.subTest(operation=operation):
                self.git("reset", "--hard", self.source)
                path = self.repo / "internal/benchmark/readiness.go"
                if operation == "rename":
                    path.rename(self.repo / "docs/moved.go")
                elif operation == "delete":
                    path.unlink()
                elif operation == "mode":
                    path.chmod(0o755)
                else:
                    self.write("internal/benchmark/a\nstrange.go")
                self.commit()
                self.check(True)

    def test_untrusted_and_missing_runs(self):
        baseline = copy.deepcopy(self.run)
        changes = [dict(repository={"full_name": "fork/catalog"}),
                   dict(head_repository={"full_name": "fork/catalog"}), dict(head_branch="topic"),
                   dict(path=".github/workflows/other.yml"), dict(event="pull_request"),
                   dict(status="in_progress"), dict(conclusion="failure"), dict(conclusion="cancelled"),
                   dict(created_at=date(NOW - 8 * 86400)), dict(created_at=date(NOW + 1)),
                   dict(updated_at=date(NOW + 1)), dict(created_at="invalid"), dict(run_attempt=0),
                   dict(head_sha="bogus")]
        for change in changes:
            with self.subTest(change=change):
                self.data["runs"]["workflow_runs"] = [baseline | change]
                self.check(True)
        self.data["runs"]["workflow_runs"] = []
        self.check(True)

    def test_incomplete_skipped_and_malformed_jobs(self):
        baseline = self.jobs()
        for change in ("missing", "skipped", "extra", "truncated", "future", "malformed"):
            with self.subTest(change=change):
                jobs = copy.deepcopy(baseline)
                if change == "missing":
                    jobs["jobs"].pop(0)
                    jobs["total_count"] -= 1
                elif change == "skipped":
                    jobs["jobs"][0]["conclusion"] = "skipped"
                elif change == "extra":
                    jobs["jobs"][0]["name"] = "catalog-readiness (17/16)"
                elif change == "truncated":
                    jobs["total_count"] += 1
                elif change == "future":
                    jobs["jobs"][0]["run_attempt"] = 2
                else:
                    del jobs["jobs"][0]["status"]
                self.data["jobs"]["1"] = jobs
                self.check(True)

    def test_reuse_runs_cannot_renew_original_age(self):
        reused = self.proof(2, created_at=date(NOW - 60), updated_at=date(NOW - 30))
        self.data["runs"]["workflow_runs"].insert(0, reused)
        self.data["jobs"]["2"] = {"jobs": [dict(id=250, run_attempt=1, status="completed",
            conclusion="skipped", name="catalog-readiness (${{ matrix.shard }}/16)")], "total_count": 1}
        self.assertIn("/runs/1", self.check(False))
        self.run["created_at"] = date(NOW - 8 * 86400)
        self.check(True)

    def test_newer_failure_invalidates_and_newer_success_repairs(self):
        failed = self.proof(2, conclusion="failure", updated_at=date(NOW - 100))
        jobs = self.jobs()
        jobs["jobs"][0]["conclusion"] = "failure"
        self.data["runs"]["workflow_runs"].append(failed)
        self.data["jobs"]["2"] = jobs
        self.assertIn("failed shard", self.check(True))
        self.data["runs"]["workflow_runs"].append(self.proof(3, updated_at=date(NOW - 50)))
        self.data["jobs"]["3"] = self.jobs()
        self.assertIn("/runs/3", self.check(False))

    def test_failed_job_reruns_carry_successes_without_renewing_age(self):
        self.run["run_attempt"] = 2
        jobs = self.data["jobs"]["1"]
        jobs["jobs"][0]["conclusion"] = "failure"
        jobs["jobs"].append(self.jobs(attempt=2)["jobs"][0])
        jobs["total_count"] += 1
        self.check(False)
        self.run["created_at"] = date(NOW - 8 * 86400)
        self.check(True)

    def test_reruns_with_copied_successful_jobs(self):
        self.run["run_attempt"] = 2
        jobs = self.data["jobs"]["1"]
        jobs["jobs"][0]["conclusion"] = "failure"
        jobs["jobs"].extend(self.jobs(attempt=2)["jobs"])
        jobs["total_count"] = len(jobs["jobs"])
        self.check(False)

    def test_timeouts_and_future_failures_revoke_older_proof(self):
        later = self.proof(2, conclusion="timed_out", updated_at=date(NOW - 100))
        self.data["runs"]["workflow_runs"].append(later)
        self.data["jobs"]["2"] = self.jobs()
        for conclusion in ("failure", "timed_out", "startup_failure", "action_required"):
            with self.subTest(conclusion=conclusion):
                self.data["jobs"]["2"]["jobs"][0]["conclusion"] = conclusion
                self.check(True)
        later["updated_at"] = date(NOW + 1)
        self.assertIn("unavailable", self.check(True))

    def test_api_errors_fail_closed(self):
        baseline = copy.deepcopy(self.data)
        for change in (dict(error=True), dict(malformed=True), dict(runs={}),
                       dict(runs={"workflow_runs": {}}), dict(jobs={"1": {}})):
            with self.subTest(change=change):
                self.data = baseline | change
                self.assertIn("unavailable", self.check(True))

    def test_nonancestor_and_missing_source_require_full(self):
        self.git("checkout", "--orphan", "unrelated")
        self.git("commit", "-q", "--allow-empty", "-m", "unrelated")
        self.check(True)
        self.run["head_sha"] = "a" * 40
        self.check(True)

    def test_force_and_schedule_do_not_query_github(self):
        self.check(True, CATALOG_FORCE="true")
        self.check(True, GITHUB_EVENT_NAME="schedule")
        self.check(True, GITHUB_RUN_ATTEMPT="2")
        self.assertFalse(Path(self.env["API_LOG"]).exists())
        self.check(True, CATALOG_FORCE="unexpected")


if __name__ == "__main__":
    unittest.main()
