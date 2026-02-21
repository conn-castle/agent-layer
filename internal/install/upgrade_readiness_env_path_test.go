package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestDetectFloatingDependencies_EnvAndURL(t *testing.T) {
	cfg := config.Config{
		MCP: config.MCPConfig{
			Servers: []config.MCPServer{
				{
					ID:      "sample",
					Enabled: testutil.BoolPtr(true),
					URL:     "https://example.com/tool@next",
					Env: map[string]string{
						"PACKAGE_REF": "tool@canary",
					},
				},
			},
		},
	}
	check := detectFloatingDependencies(&cfg)
	if check == nil {
		t.Fatal("expected floating dependencies readiness check")
	}
	joined := strings.Join(check.Details, "\n")
	if !strings.Contains(joined, "url=") || !strings.Contains(joined, "env.PACKAGE_REF") {
		t.Fatalf("expected url/env floating details, got %q", joined)
	}
}

func TestReadAgentLayerEnvForReadiness_Branches(t *testing.T) {
	t.Run("missing env file returns empty map", func(t *testing.T) {
		inst := &installer{root: t.TempDir(), sys: RealSystem{}}
		values, err := readAgentLayerEnvForReadiness(inst)
		if err != nil {
			t.Fatalf("readAgentLayerEnvForReadiness: %v", err)
		}
		if len(values) != 0 {
			t.Fatalf("expected empty env values, got %#v", values)
		}
	})

	t.Run("read error is surfaced", func(t *testing.T) {
		root := t.TempDir()
		envPath := filepath.Join(root, ".agent-layer", ".env")
		if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
			t.Fatalf("mkdir env dir: %v", err)
		}
		if err := os.WriteFile(envPath, []byte("AL_SAMPLE=value\n"), 0o644); err != nil {
			t.Fatalf("write env file: %v", err)
		}

		sys := newFaultSystem(RealSystem{})
		sys.readErrs[normalizePath(envPath)] = errors.New("read boom")
		inst := &installer{root: root, sys: sys}
		_, err := readAgentLayerEnvForReadiness(inst)
		if err == nil || !strings.Contains(err.Error(), "read boom") {
			t.Fatalf("expected read error, got %v", err)
		}
	})

	t.Run("parse error is surfaced", func(t *testing.T) {
		root := t.TempDir()
		envPath := filepath.Join(root, ".agent-layer", ".env")
		if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
			t.Fatalf("mkdir env dir: %v", err)
		}
		if err := os.WriteFile(envPath, []byte("BROKEN_LINE\n"), 0o644); err != nil {
			t.Fatalf("write invalid env file: %v", err)
		}

		inst := &installer{root: root, sys: RealSystem{}}
		_, err := readAgentLayerEnvForReadiness(inst)
		if err == nil || !strings.Contains(err.Error(), "parse") {
			t.Fatalf("expected parse error, got %v", err)
		}
	})

	t.Run("only AL_ keys are retained", func(t *testing.T) {
		root := t.TempDir()
		envPath := filepath.Join(root, ".agent-layer", ".env")
		if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
			t.Fatalf("mkdir env dir: %v", err)
		}
		if err := os.WriteFile(envPath, []byte("AL_KEEP=1\nOTHER=2\nAL_EMPTY=\n"), 0o644); err != nil {
			t.Fatalf("write env file: %v", err)
		}

		inst := &installer{root: root, sys: RealSystem{}}
		values, err := readAgentLayerEnvForReadiness(inst)
		if err != nil {
			t.Fatalf("readAgentLayerEnvForReadiness: %v", err)
		}
		if _, ok := values["AL_KEEP"]; !ok {
			t.Fatalf("expected AL_KEEP in filtered env, got %#v", values)
		}
		if _, ok := values["OTHER"]; ok {
			t.Fatalf("did not expect OTHER in filtered env, got %#v", values)
		}
	})
}

func TestReadinessEnvHelpers_Branches(t *testing.T) {
	filtered := filterAgentLayerEnvForReadiness(map[string]string{
		"AL_ONE": "1",
		"OTHER":  "2",
	})
	if _, ok := filtered["AL_ONE"]; !ok {
		t.Fatalf("expected AL_ONE in filtered env, got %#v", filtered)
	}
	if _, ok := filtered["OTHER"]; ok {
		t.Fatalf("did not expect OTHER in filtered env, got %#v", filtered)
	}
	if len(filterAgentLayerEnvForReadiness(nil)) != 0 {
		t.Fatal("expected empty filtered map for nil input")
	}

	if _, ok := readinessEnvValue("AL_REPO_ROOT", map[string]string{}, "", RealSystem{}); ok {
		t.Fatal("expected built-in AL_REPO_ROOT lookup to fail when repo root is empty")
	}
	if value, ok := readinessEnvValue("AL_REPO_ROOT", map[string]string{}, "/repo", RealSystem{}); !ok || value != "/repo" {
		t.Fatalf("expected built-in AL_REPO_ROOT value, got value=%q ok=%v", value, ok)
	}

	processValue := "from-process"
	sys := newFaultSystem(RealSystem{})
	sys.lookupEnvs["AL_TOKEN"] = &processValue
	if value, ok := readinessEnvValue("AL_TOKEN", map[string]string{"AL_TOKEN": "from-file"}, "/repo", sys); !ok || value != "from-process" {
		t.Fatalf("expected process env precedence, got value=%q ok=%v", value, ok)
	}

	emptyProcess := ""
	sys.lookupEnvs["AL_FALLBACK"] = &emptyProcess
	if value, ok := readinessEnvValue("AL_FALLBACK", map[string]string{"AL_FALLBACK": "from-file"}, "/repo", sys); !ok || value != "from-file" {
		t.Fatalf("expected fallback to file env, got value=%q ok=%v", value, ok)
	}
	if _, ok := readinessEnvValue("AL_MISSING", map[string]string{"AL_MISSING": ""}, "/repo", sys); ok {
		t.Fatal("expected missing readiness env value when both sources are empty")
	}

	if _, ok := processEnvValue(nil, "AL_TOKEN"); ok {
		t.Fatal("expected processEnvValue to return missing for nil system")
	}
	if _, ok := processEnvValue(sys, "AL_UNKNOWN"); ok {
		t.Fatal("expected processEnvValue to return missing for unset key")
	}

	inst := &installer{root: "/repo", sys: sys}
	subst := readinessSubstitutionEnv("${AL_REPO_ROOT}/${AL_TOKEN}/${AL_MISSING}", map[string]string{"AL_MISSING": ""}, inst.root, inst.sys)
	if subst["AL_REPO_ROOT"] != "/repo" || subst["AL_TOKEN"] != "from-process" {
		t.Fatalf("unexpected substitution env values: %#v", subst)
	}
	if _, ok := subst["AL_MISSING"]; ok {
		t.Fatalf("did not expect unresolved env var in substitution map: %#v", subst)
	}
}

func TestCheckPathExpansionValue_Branches(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}

	if detail, err := checkPathExpansionValue(inst, map[string]string{}, 0, "s1", "command", "npx -y mcp-server", true); err != nil || detail != "" {
		t.Fatalf("expected non-path value to produce no readiness detail, detail=%q err=%v", detail, err)
	}

	detail, err := checkPathExpansionValue(inst, map[string]string{}, 0, "s1", "command", "${AL_REPO_ROOT}/${AL_MISSING}/tool", true)
	if err != nil {
		t.Fatalf("checkPathExpansionValue unresolved placeholder: %v", err)
	}
	if !strings.Contains(detail, "unresolved path placeholder") {
		t.Fatalf("expected unresolved-placeholder detail, got %q", detail)
	}

	weirdDetail, weirdErr := checkPathExpansionValue(inst, map[string]string{"AL_WEIRD": "${UNEXPANDED}"}, 0, "s1", "command", "~/${AL_WEIRD}", false)
	if weirdErr != nil {
		t.Fatalf("checkPathExpansionValue weird expansion: %v", weirdErr)
	}
	if !strings.Contains(weirdDetail, "did not expand cleanly") {
		t.Fatalf("expected dirty-expansion detail, got %q", weirdDetail)
	}

	commandDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(commandDir, 0o755); err != nil {
		t.Fatalf("mkdir command dir: %v", err)
	}
	detail, err = checkPathExpansionValue(inst, map[string]string{}, 1, "s2", "command", "${AL_REPO_ROOT}/bin", true)
	if err != nil {
		t.Fatalf("checkPathExpansionValue directory command: %v", err)
	}
	if !strings.Contains(detail, "directory") {
		t.Fatalf("expected directory detail, got %q", detail)
	}

	commandPath := filepath.Join(commandDir, "tool")
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write command path: %v", err)
	}
	if detail, err = checkPathExpansionValue(inst, map[string]string{}, 2, "s3", "command", "${AL_REPO_ROOT}/bin/tool", true); err != nil || detail != "" {
		t.Fatalf("expected clean command path, detail=%q err=%v", detail, err)
	}

	statFault := newFaultSystem(RealSystem{})
	statTarget := filepath.Join(root, "broken", "tool")
	statFault.statErrs[normalizePath(statTarget)] = errors.New("stat boom")
	instWithFault := &installer{root: root, sys: statFault}
	if _, err = checkPathExpansionValue(instWithFault, map[string]string{}, 3, "s4", "command", "${AL_REPO_ROOT}/broken/tool", true); err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected stat error, got %v", err)
	}
}

func TestDetectPathExpansionAnomalies_AdditionalBranches(t *testing.T) {
	t.Run("ignores disabled and non-stdio servers", func(t *testing.T) {
		inst := &installer{root: t.TempDir(), sys: RealSystem{}}
		cfg := config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "disabled-stdio",
						Enabled:   testutil.BoolPtr(false),
						Transport: config.TransportStdio,
						Command:   "${AL_REPO_ROOT}/missing",
					},
					{
						ID:        "enabled-http",
						Enabled:   testutil.BoolPtr(true),
						Transport: config.TransportHTTP,
						URL:       "https://example.com",
						Command:   "${AL_REPO_ROOT}/missing",
					},
				},
			},
		}
		check, err := detectPathExpansionAnomalies(inst, &cfg, map[string]string{})
		if err != nil {
			t.Fatalf("detectPathExpansionAnomalies: %v", err)
		}
		if check != nil {
			t.Fatalf("expected nil check, got %#v", check)
		}
	})

	t.Run("arg stat error propagates", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "broken", "tool")
		faults := newFaultSystem(RealSystem{})
		faults.statErrs[normalizePath(target)] = errors.New("stat boom")
		inst := &installer{root: root, sys: faults}
		cfg := config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "stdio-server",
						Enabled:   testutil.BoolPtr(true),
						Transport: config.TransportStdio,
						Command:   "node",
						Args:      []string{"${AL_REPO_ROOT}/broken/tool"},
					},
				},
			},
		}
		_, err := detectPathExpansionAnomalies(inst, &cfg, map[string]string{})
		if err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected stat error, got %v", err)
		}
	})
}
