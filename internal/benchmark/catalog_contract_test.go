package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func writeCatalogManifest(t *testing.T, checkout string, count int, ids ...string) {
	t.Helper()
	records := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		records = append(records, map[string]string{"task_id": id})
	}
	data, err := json.Marshal(map[string]any{"task_count": count, "tasks": records})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "tasks", taskCatalogManifestFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPinnedTaskCatalogMustAgreeWithItsManifest(t *testing.T) {
	for _, test := range []struct {
		name   string
		build  func(t *testing.T, checkout string)
		wanted string
	}{
		{
			"manifest counts more tasks than exist",
			func(t *testing.T, checkout string) {
				writeAuditCatalogFixture(t, checkout, "first-task")
				writeCatalogManifest(t, checkout, 2, "first-task", "absent-task")
			},
			"declares 2 tasks",
		},
		{
			"directory is absent from the manifest",
			func(t *testing.T, checkout string) {
				writeAuditCatalogFixture(t, checkout, "first-task", "second-task")
				writeCatalogManifest(t, checkout, 2, "first-task", "third-task")
			},
			"is absent from",
		},
		{
			"manifest names an unusable task",
			func(t *testing.T, checkout string) {
				writeAuditCatalogFixture(t, checkout, "first-task")
				writeCatalogManifest(t, checkout, 1, "../escape")
			},
			"invalid task name",
		},
		{
			"catalog directory is not a task",
			func(t *testing.T, checkout string) {
				writeAuditCatalogFixture(t, checkout, "first-task")
				if err := os.MkdirAll(filepath.Join(checkout, "tasks", "not a task"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			"invalid task name",
		},
		{
			"catalog has no tasks",
			func(t *testing.T, checkout string) {
				if err := os.MkdirAll(filepath.Join(checkout, "tasks"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			"task catalog is empty",
		},
		{
			"manifest is unreadable",
			func(t *testing.T, checkout string) {
				writeAuditCatalogFixture(t, checkout, "first-task")
				if err := os.WriteFile(filepath.Join(checkout, "tasks", taskCatalogManifestFile), []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			"parse pinned DeepSWE task manifest",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			checkout := t.TempDir()
			test.build(t, checkout)
			// The audit reports "every task in the catalog is ready". That claim
			// is only true if the directory listing and the pinned manifest
			// describe the same set, so a disagreement must stop the audit
			// instead of silently narrowing what was checked.
			if _, err := listPinnedBenchmarkTasks(checkout); err == nil ||
				!strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("%s error = %v", test.name, err)
			}
		})
	}
}

func TestTaskReadinessContractMustPinTheTaskItCertifies(t *testing.T) {
	const task = "first-task"
	base := map[string]any{
		"schema": readinessContractSchema, "task": task,
		"image": auditTaskImage(task), "image_digest": testReadinessDigest,
		"check": "check.sh",
	}
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
		wanted string
	}{
		{"foreign schema", func(c map[string]any) { c["schema"] = "deepswe-task-readiness-v0" }, "invalid environment readiness contract"},
		{"other task", func(c map[string]any) { c["task"] = "second-task" }, "invalid environment readiness contract"},
		{"unpinned image", func(c map[string]any) { c["image_digest"] = "sha256:short" }, "invalid environment readiness contract"},
		{"traversing check path", func(c map[string]any) { c["check"] = "../check.sh" }, "invalid environment readiness contract"},
		{"no check program", func(c map[string]any) { delete(c, "check") }, "invalid environment readiness contract"},
		{
			"undecodable digest",
			func(c map[string]any) { c["image_digest"] = "sha256:" + strings.Repeat("z", 64) },
			"invalid image digest",
		},
		{
			"image the task does not use",
			func(c map[string]any) { c["image"] = "registry.example/other:v1" },
			"does not match task.toml image",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			contract := map[string]any{}
			for key, value := range base {
				contract[key] = value
			}
			test.mutate(contract)
			data, err := json.Marshal(contract)
			if err != nil {
				t.Fatal(err)
			}
			root := "readiness/" + DeepSWECommit + "/" + task + "/"
			checkout := t.TempDir()
			writeAuditCatalogFixture(t, checkout, task)
			installReadinessTestBoundaries(t, fstest.MapFS{
				root + "contract.json": {Data: data},
				root + "check.sh":      {Data: []byte("#!/bin/bash\ncheck-tools\n")},
			}, func(context.Context, ...string) ([]byte, error) { return nil, nil })

			// The contract is what pins the exact image digest a paid run
			// executes against. A contract that describes a different task,
			// image, or an unpinned digest would certify something other than
			// what the campaign actually runs.
			if _, err := loadTaskReadiness(checkout, task); err == nil ||
				!strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("%s contract error = %v", test.name, err)
			}
		})
	}

	t.Run("no contract at all", func(t *testing.T) {
		checkout := t.TempDir()
		writeAuditCatalogFixture(t, checkout, task)
		installReadinessTestBoundaries(t, fstest.MapFS{},
			func(context.Context, ...string) ([]byte, error) { return nil, nil })
		// Certification is mandatory, so an uncontracted task must fail rather
		// than fall back to running an unverified environment.
		if _, err := loadTaskReadiness(checkout, task); err == nil ||
			!strings.Contains(err.Error(), "no mandatory environment readiness contract") {
			t.Fatalf("uncontracted task error = %v", err)
		}
	})
}

func TestCertifiedTaskEnvironmentIsNotReCertifiedForTheSameContract(t *testing.T) {
	const task = "first-task"
	repository, checkout := t.TempDir(), t.TempDir()
	writeAuditCatalogFixture(t, checkout, task)
	runs := 0
	installReadinessTestBoundaries(t, auditContractsFixture(task),
		func(context.Context, ...string) ([]byte, error) {
			runs++
			return nil, nil
		})
	checksum := strings.Repeat("1", 64)

	first, err := certifyTaskEnvironment(context.Background(), repository, checkout, task, checksum)
	if err != nil {
		t.Fatal(err)
	}
	second, err := certifyTaskEnvironment(context.Background(), repository, checkout, task, checksum)
	if err != nil {
		t.Fatal(err)
	}
	// Certification starts a container per task. Repeating it for an unchanged
	// contract would add minutes to every report and would fail on a machine
	// that is currently offline.
	if runs != 1 || first != second || len(first) != 64 {
		t.Fatalf("certification ran %d times producing %q and %q", runs, first, second)
	}

	// A different task tree is a different environment claim, so the identity
	// must change and the check must run again.
	other, err := certifyTaskEnvironment(context.Background(), repository, checkout, task, strings.Repeat("2", 64))
	if err != nil {
		t.Fatal(err)
	}
	if runs != 2 || other == first {
		t.Fatalf("changed task tree reused certification %q after %d runs", other, runs)
	}
}

func TestUncertifiableTaskEnvironmentFailsBeforeProviderExecution(t *testing.T) {
	const task = "first-task"
	repository, checkout := t.TempDir(), t.TempDir()
	writeAuditCatalogFixture(t, checkout, task)
	installReadinessTestBoundaries(t, auditContractsFixture(task),
		func(context.Context, ...string) ([]byte, error) {
			return []byte("bash: node: command not found"), errors.New("exit status 127")
		})

	_, err := certifyTaskEnvironment(context.Background(), repository, checkout, task, strings.Repeat("1", 64))
	if err == nil || !strings.Contains(err.Error(), "readiness failed before provider execution") {
		t.Fatalf("unready environment error = %v", err)
	}
	// Nothing may be recorded, or the next run would treat the broken
	// environment as certified.
	receipts := filepath.Join(repository, ".agent-layer", "state", "benchmarks", "deepswe", "environment-certifications")
	if entries, statErr := os.ReadDir(receipts); statErr == nil && len(entries) > 0 {
		t.Fatalf("failed certification wrote %d receipts", len(entries))
	}

	if _, err := certifyTaskEnvironment(context.Background(), repository, checkout, task, "short"); err == nil ||
		!strings.Contains(err.Error(), "no valid task checksum") {
		t.Fatalf("unchecksummed certification error = %v", err)
	}
}
