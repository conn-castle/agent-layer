//go:build benchmark_contract

package benchmark

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBareCustomProviderLoadsThroughPinnedPierFactory(t *testing.T) {
	stage := t.TempDir()
	path, err := stagePierAgentLayerAdapter(ExecutionRequest{
		Arm: ArmBaseline, Model: Model{Adapter: adapterGrok},
	}, stage)
	if err != nil {
		t.Fatal(err)
	}
	script := `
from pathlib import Path
from pier.agents.factory import AgentFactory

agent = AgentFactory.create_agent_from_import_path(
    "pier_agent_layer:AgentLayerGrok",
    logs_dir=Path("/tmp/agent-layer-pier-factory-test"),
    model_name="grok-4.5",
    version="1.0.5",
    treatment_agent="grok",
    treatment_model="grok-4.5",
    treatment_reasoning_effort="low",
    treatment_mode="bare",
)
assert agent.name() == "grok"
`
	command := exec.CommandContext(t.Context(), "uvx", "--from", "datacurve-pier=="+PierVersion, "python", "-c", script) // #nosec G204 -- pinned Pier constructs the embedded benchmark adapter.
	command.Env = replaceEnvValue(os.Environ(), "PYTHONPATH", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pinned Pier failed to construct staged bare adapter: %v\n%s", err, output)
	}
}

func TestGrokDryRunAndPaidExecutionUseDisposableContainerSandbox(t *testing.T) {
	adapter, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pier_agent_layer.py")
	if err := os.WriteFile(path, adapter, 0o600); err != nil {
		t.Fatal(err)
	}
	script := `
import asyncio
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("pier_agent_layer", sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

class Environment:
    async def upload_file(self, source, destination):
        pass

async def rendered_commands(preflight):
    with tempfile.NamedTemporaryFile() as credential, tempfile.TemporaryDirectory() as logs:
        agent = module.AgentLayerGrok(
            logs_dir=Path(logs), model_name="grok-4.5",
            version=module.AgentLayerGrok.PINNED_VERSION,
            treatment_agent="grok", treatment_model="grok-4.5",
            treatment_reasoning_effort="low", treatment_mode="bare",
            grok_credentials_path=credential.name, preflight_only=preflight,
        )
        commands = []
        async def capture_exec(environment, command, **kwargs):
            commands.append(command)
        async def capture_run(environment, command, env):
            commands.append(command)
        async def prepare(instruction, environment):
            return instruction
        validations = []
        async def capture_validation(environment, remote_path, provider, session_id=""):
            validations.append((remote_path, provider, session_id))
        async def no_op(*args, **kwargs):
            pass
        agent.exec_as_agent = capture_exec
        agent._run_command = capture_run
        agent._prepare = prepare
        agent._validate_retained_stream = capture_validation
        agent._collect_evidence = no_op
        await agent.run("task", Environment(), None)
        checkpoint_path = Path(logs) / "provider-checkpoint.json"
        checkpoint = json.loads(checkpoint_path.read_text()) if checkpoint_path.exists() else None
        if checkpoint is not None:
            class Context:
                def model_dump(self, mode):
                    assert mode == "json"
                    return {"cost_usd": 0.25}
            agent._record_provider_checkpoint(Context())
            enriched = json.loads(checkpoint_path.read_text())
            assert enriched["completed_at"] == checkpoint["completed_at"]
            assert enriched["agent_result"] == {"cost_usd": 0.25}
        return commands, validations, checkpoint

dry_run, dry_validations, dry_checkpoint = asyncio.run(rendered_commands(True))
paid, paid_validations, paid_checkpoint = asyncio.run(rendered_commands(False))
assert any("grok --no-auto-update --sandbox devbox models" in command for command in dry_run), dry_run
assert any("--trust --sandbox devbox --permission-mode bypassPermissions" in command for command in paid), paid
assert not any("--sandbox workspace" in command for command in dry_run + paid), dry_run + paid
assert len(dry_validations) == 1 and dry_validations[0][1] == "grok", dry_validations
assert len(paid_validations) == 1 and paid_validations[0][1] == "grok", paid_validations
assert dry_checkpoint is None
assert paid_checkpoint["schema"] == "agent-layer-provider-checkpoint-v1"
assert paid_checkpoint["agent_result"] == {}
`
	command := exec.CommandContext(t.Context(), "uvx", "--from", "datacurve-pier=="+PierVersion, "python", "-c", script, path) // #nosec G204 -- embedded test loads the checked-in adapter against its pinned runtime.
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("render Grok sandbox commands: %v\n%s", err, output)
	}
}

func TestPinnedStreamAdaptersImplementPierInstallAndEgressContracts(t *testing.T) {
	adapter, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pier_agent_layer.py")
	if err := os.WriteFile(path, adapter, 0o600); err != nil {
		t.Fatal(err)
	}
	script := `
import asyncio
import importlib.util
import inspect
import subprocess
import sys
import tempfile
import types
from pathlib import Path
from pier.trial.trial import Trial

retrying_verify = Trial._verify_with_retry
single_verify = getattr(retrying_verify, "__wrapped__", None)
assert single_verify is not None
spec = importlib.util.spec_from_file_location("pier_agent_layer", sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
assert Trial._verify_with_retry is single_verify
with tempfile.TemporaryDirectory() as directory:
    bundle = Path(directory)
    (bundle / "workflow-prompt.md").write_text("Work from this exact task:\n\n{{task}}\n")
    fake = types.SimpleNamespace(
        _treatment_bundle=bundle,
        _required_dispatch_roles=["code-reviewer", "implementer", "plan-reviewer"],
        _dispatch_config={
            "plan_reviewers": [{"agent": "grok", "model": "grok-4.6", "reasoning_effort": "low"}],
            "implementer": {"agent": "grok", "model": "grok-4.6", "reasoning_effort": "low"},
            "code_reviewer": {"agent": "grok", "model": "grok-4.6", "reasoning_effort": "low"},
        },
    )
    rendered = module._AgentLayerTreatment._workflow_instruction(fake, "TASK BODY")
    assert rendered.count("TASK BODY") == 1
    assert "Agent Layer benchmark workflow contract (mandatory)" in rendered
    assert "plan_reviewers: [agent=grok, model=grok-4.6, reasoning_effort=low, role=plan-reviewer]" in rendered
    assert "implementer: agent=grok, model=grok-4.6, reasoning_effort=low, role=implementer" in rendered
    assert "code_reviewer: agent=grok, model=grok-4.6, reasoning_effort=low, role=code-reviewer" in rendered
    assert "Pass the exact role value above in every dispatch_start call" in rendered
    assert "A direct single-agent implementation is noncompliant" in rendered
from pier.verifier.verifier import Verifier
verifier_source = inspect.getsource(Verifier.verify)
assert "await self._environment.exec" in verifier_source
assert "stdout_path=test_stdout_path" in verifier_source
for cls, model, domains in [
    (module.AgentLayerAntigravity, "gemini-3.5-flash-low", {"storage.googleapis.com", ".googleapis.com"}),
    (module.AgentLayerGrok, "grok-4.5", {"storage.googleapis.com", "api.x.ai", "auth.x.ai"}),
]:
    agent = cls(
        logs_dir=Path("/tmp/pier-adapter-contract"), model_name=model, version=cls.PINNED_VERSION,
        treatment_agent=agent_name if (agent_name := cls.name()) else "", treatment_model=model,
        treatment_reasoning_effort="low", treatment_mode="bare",
    )
    install = agent.install_spec()
    assert install.agent_name == cls.name() and install.version == cls.PINNED_VERSION
    assert install.verification_command == f"{cls.BINARY} --version"
    assert install.metadata["network_allowlist"] == ["storage.googleapis.com"]
    assert set(agent.network_allowlist().domains) == domains
    command = install.steps[0].run
    assert "urllib.request.urlretrieve" in command and "sha" in command and "latest" not in command
    with tempfile.TemporaryDirectory() as directory:
        out, err = Path(directory) / "stdout", Path(directory) / "stderr"
        capture = agent._bounded_provider_capture("printf stream; printf diagnostic >&2", str(out), str(err))
        completed = subprocess.run(capture, shell=True, text=True, capture_output=True)
        assert completed.returncode == 0, completed.stderr
        assert out.read_text() == "stream" and err.read_text() == "diagnostic"

        async def execute_validator(environment, command, **kwargs):
            validated = subprocess.run(command, shell=True, text=True, capture_output=True)
            assert validated.returncode == 0, validated.stderr
        agent.exec_as_agent = execute_validator
        for provider in ("grok", "antigravity"):
            asyncio.run(agent._preflight_retained_stream_validator(object(), provider))
            assert not Path(f"/tmp/agent-layer-{provider}-stream-validator-preflight.jsonl").exists()

        failed_path = Path("/tmp/agent-layer-grok-stream-validator-preflight.jsonl")
        failed_commands = []
        async def fail_validator_write(environment, command, **kwargs):
            failed_commands.append(command)
            if command.startswith("printf"):
                failed_path.write_text("partial")
                raise RuntimeError("simulated validator write failure")
            completed = subprocess.run(command, shell=True, text=True, capture_output=True)
            assert completed.returncode == 0, completed.stderr
        agent.exec_as_agent = fail_validator_write
        try:
            asyncio.run(agent._preflight_retained_stream_validator(object(), "grok"))
            raise AssertionError("validator write failure was not propagated")
        except RuntimeError as error:
            assert str(error) == "simulated validator write failure"
        assert len(failed_commands) == 2 and failed_commands[-1] == f"rm -f {failed_path}"
        assert not failed_path.exists()

commands = []
async def capture_command(environment, command, **kwargs):
    commands.append(command)
agent.exec_as_agent = capture_command
asyncio.run(agent._snapshot_workspace(object()))
assert len(commands) == 1
assert "git ls-files --others -z" in commands[0]
assert "git config user.email" in commands[0]
assert "agent-layer-original" in commands[0]
commands.clear()
asyncio.run(agent._collect_evidence(object()))
assert any("for path in" in command and "agent-layer-original" in command for command in commands)
assert any("git reset -q --pathspec-from-file" in command and "git commit -m" in command for command in commands)
`
	command := exec.CommandContext(t.Context(), "uvx", "--from", "datacurve-pier=="+PierVersion, "python", "-c", script, path) // #nosec G204 -- embedded test loads the checked-in adapter against its pinned runtime.
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pinned Pier adapter contract failed: %v\n%s", err, output)
	}
}

func TestGrokRetainedStreamValidatorIsDependencyCompleteAndExecutesOffline(t *testing.T) {
	adapter, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pier_agent_layer.py")
	if err := os.WriteFile(path, adapter, 0o600); err != nil {
		t.Fatal(err)
	}
	script := `
import asyncio
import base64
import importlib.util
import shlex
import subprocess
import sys
import tempfile

spec = importlib.util.spec_from_file_location("pier_agent_layer", sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

commands = []
class Harness:
    async def exec_as_agent(self, environment, command, **kwargs):
        commands.append(command)

session_id = "11111111-1111-4111-8111-111111111111"
with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8") as stream:
    stream.write('{"type":"usage","usage":{"input_tokens":1,"output_tokens":1}}\n')
    stream.write('{"type":"end","sessionId":"' + session_id + '","stopReason":"end_turn"}\n')
    stream.flush()
    asyncio.run(module._AgentLayerStreamAgent._validate_retained_stream(
        Harness(), object(), stream.name, "grok", session_id,
    ))

    assert len(commands) == 1
    parts = shlex.split(commands[0])
    assert parts[:2] == ["printf", "%s"] and parts[3:] == ["|", "base64", "-d", "|", "python3"], commands[0]
    validator = base64.b64decode(parts[2]).decode("utf-8")
    constant = f"STREAM_BYTE_CAP = {module.STREAM_BYTE_CAP}"
    assert constant in validator
    assert validator.index(constant) < validator.index("def _bounded_json_lines")
    completed = subprocess.run(["python3", "-c", validator], text=True, capture_output=True)
    assert completed.returncode == 0, completed.stderr
`
	command := exec.CommandContext(t.Context(), "uvx", "--from", "datacurve-pier=="+PierVersion, "python", "-c", script, path) // #nosec G204 -- embedded test loads the checked-in adapter against its pinned runtime.
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("offline Grok retained-stream validation failed: %v\n%s", err, output)
	}
}
