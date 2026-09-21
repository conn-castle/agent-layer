"""Test native fixture isolation and cancellation without launching Muse."""
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest

sys.dont_write_bytecode = True
SOURCE = Path(__file__).with_name("test-muse-native.py").resolve()
spec = importlib.util.spec_from_file_location("fixture", SOURCE)
f = importlib.util.module_from_spec(spec)
spec.loader.exec_module(f)


def wait_for(predicate):
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(.05)
    raise AssertionError("Fixture condition did not occur within 10 seconds")


class FixtureSafetyTest(unittest.TestCase):
    def setUp(self):
        output = SOURCE.parents[1] / ".agent-layer/tmp/muse-fixture-tests"
        output.mkdir(parents=True, exist_ok=True)
        self.case = Path(tempfile.mkdtemp(dir=output))

    def test_sanitized_children_cannot_select_keychain(self):
        fake = self.case / "fake native"
        fake.write_text("#!" + sys.executable + "\nimport json, os, sys\n"
                        "print(json.dumps({'backend': os.environ['TBH_CREDENTIAL_BACKEND'], "
                        "'key': os.environ['META_API_KEY'], 'args': sys.argv[1:]}))\n")
        fake.chmod(0o700)
        wrapper = f.fixture_binary(fake, self.case)
        for env in ({}, {"TBH_CREDENTIAL_BACKEND": "keychain", "META_API_KEY": "unwanted"}):
            result = subprocess.run([str(wrapper), "argument with spaces"], env=env,
                                    check=True, capture_output=True, text=True)
            self.assertEqual(json.loads(result.stdout), {
                "backend": "file", "key": "local-fixture-not-a-real-key",
                "args": ["argument with spaces"]})

    def test_cancellation_and_parent_exit_stop_children_and_cancel_dispatch(self):
        for parent_exit in (False, True):
            with self.subTest(parent_exit=parent_exit):
                case = self.case / str(parent_exit)
                case.mkdir()
                records = case / ".agent-layer/tmp/runs/fixture-id"
                records.mkdir(parents=True)
                (records / "dispatch.json").write_text('{"termination_confirmed": false}')
                al = case / "al"
                al.write_text("#!" + sys.executable + "\nfrom pathlib import Path\nimport sys\n"
                              "assert sys.argv[1:] == ['dispatch', 'cancel', 'fixture-id']\n"
                              "Path('cancelled').write_text('yes')\n"
                              "print('{\"termination_confirmed\": true}')\n")
                al.chmod(0o700)
                driver = case / "driver.py"
                driver.write_text(
                    "import importlib.util, os, sys\nfrom pathlib import Path\n"
                    "sys.dont_write_bytecode=True\n"
                    "s=importlib.util.spec_from_file_location('f', " + repr(str(SOURCE)) + ")\n"
                    "f=importlib.util.module_from_spec(s);s.loader.exec_module(f)\n"
                    "p=Path(" + repr(str(case)) + ")\n"
                    "with f.fixture_guard():\n"
                    " f.register_workspace(p/'al', p, os.environ)\n"
                    " f.run([sys.executable, '-c', \"import os,time;from pathlib import Path;"
                    "Path('child.pid').write_text(str(os.getpid()));time.sleep(60)\"], p, os.environ, p/'child')\n")
                launcher = None
                process = None
                log = (case / "driver.log").open("w")
                try:
                    if parent_exit:
                        launcher = subprocess.Popen([sys.executable, "-c",
                            "import subprocess,sys; p=subprocess.Popen(sys.argv[1:]); "
                            "print(p.pid,flush=True);sys.stdin.read()", sys.executable, str(driver)],
                            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=log, text=True)
                        driver_pid = int(launcher.stdout.readline())
                    else:
                        process = subprocess.Popen([sys.executable, str(driver)], stdout=log, stderr=log)
                        driver_pid = process.pid
                    wait_for(lambda: (case / "child.pid").exists())
                    child_pid = int((case / "child.pid").read_text())
                    if launcher:
                        launcher.stdin.close()
                        launcher.wait(timeout=5)
                    else:
                        os.kill(driver_pid, signal.SIGTERM)
                        self.assertEqual(process.wait(timeout=10), 128 + signal.SIGTERM)
                    wait_for(lambda: (case / "cancelled").exists())
                    def child_gone():
                        try:
                            os.kill(child_pid, 0)
                            return False
                        except ProcessLookupError:
                            return True
                    wait_for(child_gone)
                finally:
                    for candidate in (process, launcher):
                        if candidate and candidate.poll() is None:
                            candidate.kill()
                            candidate.wait()
                    if launcher:
                        launcher.stdout.close()
                    log.close()


if __name__ == "__main__":
    unittest.main()
