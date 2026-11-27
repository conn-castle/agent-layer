package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/agentdispatch"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/quota"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

func TestDispatchQuotaCLIProfilesAndOutcomes(t *testing.T) {
	for _, tc := range []struct {
		status string
		exit   int
	}{
		{quota.ResponseComplete, 0},
		{quota.ResponsePartial, agentdispatch.ExitQuotaPartial},
		{quota.ResponseNoReadings, agentdispatch.ExitQuotaNone},
	} {
		t.Run(tc.status, func(t *testing.T) {
			t.Setenv("AL_NO_NETWORK", "1")
			cmd := dispatchQuotaCommandWithReader(func(ctx context.Context, req quota.Request) quota.Response {
				deadline, ok := ctx.Deadline()
				assert.True(t, ok)
				assert.Greater(t, time.Until(deadline), time.Duration(0))
				assert.LessOrEqual(t, time.Until(deadline), 5*time.Second)
				assert.Nil(t, req.Project)
				assert.True(t, req.Offline)
				assert.Equal(t, 1, req.MaxRetries)
				assert.Equal(t, []quota.Source{
					{Selector: "work", Agent: "codex", File: "a.json", Expected: "workspace-a", Models: []string{"custom-model"}},
					{Selector: "personal", Agent: "codex", File: "b.json"},
				}, req.Sources)
				return quota.Response{SchemaVersion: quota.SchemaVersion, Status: tc.status, Results: []quota.Result{{Selector: "work"}}}
			})
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"--profile", "work=codex:a.json", "--profile", "personal=codex:b.json",
				"--expect-account", "work=workspace-a", "--model", "work=custom-model", "--json", "--timeout=5s", "--retries=1"})
			err := cmd.ExecuteContext(t.Context())
			assertQuotaExit(t, err, tc.exit)
			var out quota.Response
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &out))
			assert.Equal(t, quota.SchemaVersion, out.SchemaVersion)
			assert.Equal(t, tc.status, out.Status)
			assert.Empty(t, stderr.String())
		})
	}
}

func TestDispatchQuotaCLIRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{"positional"},
		{"--unknown=fixture-sensitive-argument"},
		{"--agent", "fixture-sensitive-argument"},
		{"--profile", "broken=codex"},
		{"--profile", "again=codex:a.json", "--profile", "again=muse:b.json"},
		{"--profile", "only=codex:a.json", "--expect-account", "unselected=value"},
		{"--profile", "Work=codex:a.json", "--expect-account", "work=value"},
		{"--profile", "only=codex:a.json", "--model", "nope=custom"},
		{"--timeout", "0s"},
		{"--timeout", "61s"},
		{"--retries=-1"},
		{"--retries=6"},
		{"--retries=fixture-sensitive-argument"},
		{"--format=fixture-sensitive-argument"},
		{"--timeout", "fixture-sensitive-argument"},
		{"--json=false"},
	} {
		t.Run(args[0], func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			cmd := newDispatchQuotaCmd()
			cmd.SilenceErrors = true
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(args)
			assertQuotaExit(t, cmd.ExecuteContext(t.Context()), agentdispatch.ExitUsage)
			assert.Empty(t, stdout.String())
			assert.NotContains(t, stderr.String(), "fixture-sensitive-argument")
		})
	}
}

func TestDispatchQuotaCLIProjectAndMixedSelection(t *testing.T) {
	root := canonicalPath(t.TempDir())
	writeTestRepo(t, root)
	t.Chdir(root)
	t.Setenv("AL_NO_NETWORK", "1")
	for _, tc := range []struct {
		args      []string
		selectors []string
	}{
		{nil, []string{"codex", "claude", "antigravity", "grok", "muse"}},
		{[]string{"--agent", "codex", "--profile", "other=codex:relative.json"}, []string{"codex", "other"}},
		{[]string{"--agent", "Claude", "--expect-account", "Claude=claude:id", "--model", " CLAUDE =opus"}, []string{"claude"}},
	} {
		var out, stderr bytes.Buffer
		cmd := dispatchQuotaCommandWithReader(func(_ context.Context, req quota.Request) quota.Response {
			require.NotNil(t, req.Project)
			assert.Equal(t, 3, req.MaxRetries)
			assert.Equal(t, root, req.Project.Root)
			assert.Equal(t, root, req.WorkDir)
			assert.True(t, req.Offline)
			var selectors []string
			for _, source := range req.Sources {
				selectors = append(selectors, source.Selector)
			}
			assert.Equal(t, tc.selectors, selectors)
			return quota.Response{SchemaVersion: quota.SchemaVersion, Status: quota.ResponseNoReadings, Missing: []quota.MissingReading{}, Results: []quota.Result{}}
		})
		cmd.SetOut(&out)
		cmd.SetErr(&stderr)
		cmd.SetArgs(tc.args)
		assertQuotaExit(t, cmd.ExecuteContext(t.Context()), agentdispatch.ExitQuotaNone)
		assert.JSONEq(t, `{"schema_version":1,"status":"no_readings","missing_readings":[],"results":[]}`, out.String())
	}
}

func TestDispatchQuotaCLIFormats(t *testing.T) {
	response := quota.Response{
		SchemaVersion: quota.SchemaVersion, Status: quota.ResponsePartial,
		Missing: []quota.MissingReading{{Selector: "personal", Agent: "muse", Status: "unavailable", Reason: "no_usage"}},
		Results: []quota.Result{
			{Selector: "work", Agent: "codex", Status: "ok",
				Subscription: &quota.Subscription{ID: "test-subscription"},
				Models:       []quota.Model{{ID: "chosen", Coverage: "complete"}, {ID: "catalog-extra", Coverage: "complete"}}},
			{Selector: "personal", Agent: "muse", Status: "unavailable", Reason: "no_usage"},
		},
	}
	for _, tc := range []struct {
		name  string
		flags []string
	}{
		{"default", nil},
		{quotaFormatFull, []string{"--format", quotaFormatFull}},
		{quotaFormatSummary, []string{"--format", quotaFormatSummary}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			cmd := dispatchQuotaCommandWithReader(func(_ context.Context, req quota.Request) quota.Response {
				calls++
				assert.Equal(t, 3, req.MaxRetries)
				return response
			})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs(append([]string{"--profile", "work=codex:a.json", "--profile", "personal=muse:b.json",
				"--model", "work=chosen"}, tc.flags...))
			assertQuotaExit(t, cmd.ExecuteContext(t.Context()), agentdispatch.ExitQuotaPartial)
			assert.Equal(t, 1, calls)
			if tc.name != quotaFormatSummary {
				expected, err := json.Marshal(response)
				require.NoError(t, err)
				assert.JSONEq(t, string(expected), out.String())
				return
			}
			assert.JSONEq(t, `{
				"schema_version":1, "format":"summary", "status":"partial",
				"missing_readings":[{"selector":"personal","agent":"muse","status":"unavailable","reason":"no_usage"}],
				"results":[
					{"selector":"work","agent":"codex","status":"ok","observed_at":null,"reauth_required":null,
					 "retryable":false,"blocked":null,"lockout_clears_at":null,
					 "models":[{"id":"chosen","coverage":"complete","blocked":null,"lockout_clears_at":null}]},
					{"selector":"personal","agent":"muse","status":"unavailable","reason":"no_usage","observed_at":null,
					 "reauth_required":null,"retryable":false,"blocked":null,"lockout_clears_at":null}
				]
			}`, out.String())
		})
	}
}

func TestDispatchQuotaCLIReportsCanceledOutput(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	cmd := dispatchQuotaCommandWithReader(func(context.Context, quota.Request) quota.Response {
		cancel(errors.New(syscall.SIGTERM.String() + " signal received"))
		return quota.Response{Status: quota.ResponsePartial, Results: []quota.Result{{Selector: "done"}}}
	})
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--profile", "done=codex:" + filepath.Join(t.TempDir(), "profile")})
	assertQuotaExit(t, cmd.ExecuteContext(ctx), agentdispatch.ExitSigterm)
	assert.Contains(t, out.String(), "done")
}

func TestDispatchQuotaBypassesVersionDownload(t *testing.T) {
	assert.True(t, isDispatchQuota([]string{"dispatch", "quota", "--json"}))
	assert.True(t, isDispatchQuota([]string{"--quiet", "dispatch", "-q", "quota", "--profile", "work=codex:auth.json"}))
	assert.False(t, isDispatchQuota([]string{"dispatch", "options"}))
}

func assertQuotaExit(t *testing.T, err error, code int) {
	t.Helper()
	if code == 0 {
		require.NoError(t, err)
		return
	}
	var exit *SilentExitError
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, code, exit.Code)
}

func TestDispatchQuotaUsesEffectiveProjectNoNetwork(t *testing.T) {
	root := canonicalPath(t.TempDir())
	writeTestRepo(t, root)
	t.Chdir(root)
	envFile := config.DefaultPaths(root).EnvPath
	require.NoError(t, os.WriteFile(envFile, []byte(versiondispatch.EnvNoNetwork+"=1\n"), 0o600)) // #nosec G703 -- owned fixture path.
	for _, ambient := range []string{"", " ", "1"} {
		t.Setenv(versiondispatch.EnvNoNetwork, ambient)
		var stdout bytes.Buffer
		cmd := dispatchQuotaCommandWithReader(func(_ context.Context, req quota.Request) quota.Response {
			assert.Equal(t, ambient == "" || strings.TrimSpace(ambient) != "", req.Offline)
			return quota.Response{Status: quota.ResponseComplete}
		})
		cmd.SetOut(&stdout)
		cmd.SetArgs([]string{"--agent", "codex"})
		require.NoError(t, cmd.ExecuteContext(t.Context()))
	}
}
