#!/usr/bin/env python3
"""Decide whether this commit needs fresh, full catalog certification."""

import argparse
import datetime
import fnmatch
import json
import os
import re
import subprocess
import sys
import time

WORKFLOW = ".github/workflows/release-catalog-certification.yml"
MAX_AGE = 7 * 24 * 60 * 60
SHARDS = {f"catalog-readiness ({shard}/16)" for shard in range(1, 17)}
INPUTS = (
    "internal/benchmark/*", "internal/gitenv/*", "internal/fsutil/*",
    "cmd/al/benchmark.go", "go.mod", "go.sum", WORKFLOW,
    ".agent-layer/repo-profile.toml",
    "scripts/catalog-certification-scope.sh", "scripts/catalog-certification-scope.py",
)


def command(*args):
    return subprocess.check_output(args, stderr=subprocess.PIPE)


def api(path):
    return json.loads(command("gh", "api", path))


def critical(path):
    if path.endswith("_test.go") or "testdata" in path.split("/"):
        return False
    return any(fnmatch.fnmatchcase(path, pattern) for pattern in INPUTS)


def same_inputs(source, head):
    result = subprocess.run(
        ["git", "merge-base", "--is-ancestor", source, head],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False,
    )
    if result.returncode == 1:
        return False
    result.check_returncode()
    paths = command("git", "diff", "--no-renames", "--name-only", "-z", source, head)
    return not any(critical(os.fsdecode(path)) for path in paths.split(b"\0") if path)


def timestamp(value):
    parsed = datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise ValueError("timestamp has no timezone")
    return parsed.timestamp()


def positive_int(value):
    if type(value) is not int or value < 1:
        raise ValueError("invalid run or job number")
    return value


def trusted(run, repository, now):
    if (run["repository"]["full_name"].lower() != repository.lower()
            or run["head_repository"]["full_name"].lower() != repository.lower()
            or run["head_branch"] != "main" or run["path"] != WORKFLOW
            or run["event"] not in ("push", "workflow_dispatch", "schedule")
            or run["status"] != "completed"):
        return False
    if not isinstance(run["conclusion"], str) or not run["conclusion"]:
        raise ValueError("missing completed run conclusion")
    positive_int(run["id"])
    positive_int(run["run_attempt"])
    if not re.fullmatch(r"[0-9a-f]{40}", run["head_sha"]):
        raise ValueError("invalid source commit")
    created, updated = timestamp(run["created_at"]), timestamp(run["updated_at"])
    if created > updated or updated > now:
        raise ValueError("inconsistent or future run timestamp")
    return updated >= now - MAX_AGE


def shard_jobs(repository, run):
    # Inspect every attempt, accepting both copied and retained successful jobs.
    response = api(f"repos/{repository}/actions/runs/{run['id']}/jobs?filter=all&per_page=100")
    jobs = response["jobs"]
    if not isinstance(jobs, list) or response["total_count"] != len(jobs):
        raise ValueError("incomplete jobs response")
    latest = {}
    for job in jobs:
        attempt, job_id = positive_int(job["run_attempt"]), positive_int(job["id"])
        if attempt > run["run_attempt"]:
            raise ValueError("unexpected future job attempt")
        name = job["name"]
        if not isinstance(name, str):
            raise ValueError("invalid job name")
        previous = latest.get(name)
        if previous is None or (attempt, job_id) > (previous["run_attempt"], previous["id"]):
            latest[name] = job
    return {name: job for name, job in latest.items() if name.startswith("catalog-readiness (")}


def decide(head_ref, now):
    force = os.environ.get("CATALOG_FORCE", "false")
    if os.environ.get("GITHUB_EVENT_NAME") == "schedule" or force == "true":
        return True, "Full catalog certification explicitly requested."
    if force != "false":
        raise ValueError("invalid CATALOG_FORCE value")
    attempt = positive_int(int(os.environ.get("GITHUB_RUN_ATTEMPT", "1")))
    if attempt > 1:
        # The current in-progress run is absent from the completed-runs query.
        # Reclassification must not hide its own earlier failed shard attempt.
        return True, "Workflow rerun requires full catalog certification."
    head = command("git", "rev-parse", "--verify", f"{head_ref}^{{commit}}").decode().strip()
    repository = os.environ.get("GITHUB_REPOSITORY") or command(
        "gh", "repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner"
    ).decode().strip()
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise ValueError("invalid repository")
    response = api(f"repos/{repository}/actions/workflows/{WORKFLOW.rsplit('/', 1)[1]}"
                   "/runs?branch=main&status=completed&per_page=100")
    runs = response["workflow_runs"]
    if not isinstance(runs, list):
        raise ValueError("invalid workflow runs response")
    candidates = [run for run in runs if trusted(run, repository, now)]
    candidates.sort(key=lambda run: (timestamp(run["updated_at"]), run["id"]), reverse=True)
    for run in candidates:
        if not same_inputs(run["head_sha"], head):
            continue
        jobs = shard_jobs(repository, run)
        # GitHub may report a shard timeout as cancelled; only a cancelled run
        # (such as a superseded push) leaves cancelled shards non-revoking.
        allowed = ("success", "skipped") + (("cancelled",) if run["conclusion"] == "cancelled" else ())
        if any(job["status"] == "completed" and job["conclusion"] not in allowed
               for job in jobs.values()):
            return True, f"Matching catalog run {run['id']} has a failed shard; fresh certification required."
        age = now - timestamp(run["created_at"])
        if (run["conclusion"] == "success" and age <= MAX_AGE and set(jobs) == SHARDS
                and all(job["status"] == "completed" and job["conclusion"] == "success"
                        for job in jobs.values())):
            server = os.environ.get("GITHUB_SERVER_URL", "https://github.com")
            url = f"{server}/{repository}/actions/runs/{run['id']}"
            return False, (f"Catalog inputs unchanged; reusing all sixteen successful shards from {url} "
                           f"(original run age {age / 3600:.1f} hours; expires after seven days).")
    return True, "No recent successful full run with unchanged catalog inputs; full certification required."


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("head", nargs="?", default="HEAD")
    parser.add_argument("--now", type=float, default=None, help=argparse.SUPPRESS)
    args = parser.parse_args()
    try:
        required, reason = decide(args.head, time.time() if args.now is None else args.now)
    except (OSError, subprocess.CalledProcessError, ValueError, KeyError, TypeError, AttributeError) as error:
        required = True
        # Do not print API response bodies, tokens, or subprocess arguments.
        reason = f"Catalog evidence unavailable ({type(error).__name__}); full certification required."
    print(reason, file=sys.stderr)
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a", encoding="utf-8") as output:
            output.write(reason + "\n")
    print(str(required).lower())


if __name__ == "__main__":
    main()
