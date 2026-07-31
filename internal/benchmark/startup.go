package benchmark

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	verifierServiceStartupHeading = "# --- Service startup"
	verifierSectionHeading        = "# --- "
	pierEnvironmentImportPath     = "--environment-import-path"
	pierEnvironmentKwarg          = "--environment-kwarg"
	taskEnvironmentClass          = "pier_task_environment:CertifiedDockerEnvironment"
)

//go:embed assets/pier_task_environment.py
var pierTaskEnvironment []byte

// prepareTaskStartup materializes the mandatory task-owned readiness program
// and any startup program shared with the selected task's verifier. It also
// pins Pier to the readiness contract's immutable image identity.
func prepareTaskStartup(checkout, task, stage string) ([]string, error) {
	readiness, err := loadTaskReadiness(checkout, task)
	if err != nil {
		return nil, err
	}
	verifierSource := filepath.Join(checkout, "tasks", task, "tests")
	verifierContext := filepath.Join(stage, "verifier-context")
	if err := prepareVerifierBuildContext(verifierSource, verifierContext, readiness.contract.Image, readiness.pinnedImage); err != nil {
		return nil, err
	}
	body, found, err := readTaskStartup(checkout, task)
	if err != nil {
		return nil, err
	}

	adapterPath := filepath.Join(stage, "pier_task_environment.py")
	if err := os.WriteFile(adapterPath, pierTaskEnvironment, 0o600); err != nil {
		return nil, fmt.Errorf("write Pier task environment adapter: %w", err)
	}
	readinessPath := filepath.Join(stage, "task-readiness.sh")
	if err := os.WriteFile(readinessPath, readiness.check, 0o600); err != nil {
		return nil, fmt.Errorf("write task readiness program: %w", err)
	}
	arguments := []string{
		pierEnvironmentImportPath, taskEnvironmentClass,
		pierEnvironmentKwarg, "readiness_script=" + readinessPath,
		pierEnvironmentKwarg, "pinned_image=" + readiness.pinnedImage,
		pierEnvironmentKwarg, "verifier_source_root=" + verifierSource,
		pierEnvironmentKwarg, "verifier_context=" + verifierContext,
	}
	if found {
		startupPath := filepath.Join(stage, "task-startup.sh")
		// Reproduce the shared verifier frame's shell mode and helper so the
		// extracted section has the same execution context in both environments.
		script := "#!/bin/bash\nset -uo pipefail\nlog() { echo \"[verifier] $*\"; }\n\n" + body
		if err := os.WriteFile(startupPath, []byte(script), 0o600); err != nil {
			return nil, fmt.Errorf("write derived task startup program: %w", err)
		}
		arguments = append(arguments, pierEnvironmentKwarg, "startup_script="+startupPath)
	}
	return arguments, nil
}

func prepareVerifierBuildContext(source, target, image, pinnedImage string) error {
	if err := os.CopyFS(target, os.DirFS(source)); err != nil {
		return fmt.Errorf("copy verifier build context: %w", err)
	}
	found := false
	err := filepath.WalkDir(target, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "Dockerfile" {
			return nil
		}
		data, err := os.ReadFile(path) // #nosec G122,G304 -- path is inside the private copied verifier context.
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		for index, line := range lines {
			trimmed := strings.TrimSpace(line)
			prefix := "FROM " + image
			if trimmed == prefix || strings.HasPrefix(trimmed, prefix+" AS ") {
				lines[index] = strings.Replace(line, image, pinnedImage, 1)
				found = true
			}
		}
		return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600) // #nosec G122,G703 -- path is inside the private copied verifier context.
	})
	if err != nil {
		return fmt.Errorf("pin verifier build context: %w", err)
	}
	if !found {
		return fmt.Errorf("verifier Dockerfile does not derive from certified task image %q", image)
	}
	return nil
}

// validatePlanTaskStartups parses every selected task's mandatory readiness
// contract and optional startup definition without executing either program.
func validatePlanTaskStartups(checkout string, tasks []benchmarkPlanTask) error {
	for _, task := range tasks {
		if _, err := loadTaskReadiness(checkout, task.ID); err != nil {
			return err
		}
		if _, _, err := readTaskStartup(checkout, task.ID); err != nil {
			return err
		}
	}
	return nil
}

func readTaskStartup(checkout, task string) (string, bool, error) {
	verifierPath := filepath.Join(checkout, "tasks", task, "tests", "test.sh")
	data, err := os.ReadFile(verifierPath) // #nosec G304 -- task is validated and checkout is pinned.
	if err != nil {
		return "", false, fmt.Errorf("read benchmark task %s startup source: %w", task, err)
	}
	body, found, err := extractVerifierStartup(string(data))
	if err != nil {
		return "", false, fmt.Errorf("derive benchmark task %s startup from %s: %w", task, verifierPath, err)
	}
	return body, found, nil
}

// extractVerifierStartup returns the exact body of the verifier's delimited
// service-startup section. Commands are not interpreted, so a task may start
// any number or kind of dependencies. Ambiguous or unterminated definitions
// fail instead of silently launching a partial environment.
func extractVerifierStartup(verifier string) (string, bool, error) {
	lines := strings.Split(verifier, "\n")
	start := -1
	end := -1
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, verifierServiceStartupHeading) {
			if start >= 0 {
				return "", false, fmt.Errorf("multiple %q sections", verifierServiceStartupHeading)
			}
			start = index + 1
			continue
		}
		if start >= 0 && end < 0 && strings.HasPrefix(trimmed, verifierSectionHeading) {
			end = index
		}
	}
	if start < 0 {
		return "", false, nil
	}
	if end < 0 {
		return "", false, fmt.Errorf("%q section has no following section heading", verifierServiceStartupHeading)
	}
	body := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
	if body == "" {
		return "", false, fmt.Errorf("%q section is empty", verifierServiceStartupHeading)
	}
	return body + "\n", true, nil
}
