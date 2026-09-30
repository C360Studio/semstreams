#!/usr/bin/env python3
"""Offline Task exit/cleanup fixtures for the required E2E reporting wrappers."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
PYTHON = str(Path(shutil.which("python3")).resolve())

REPORTER = r'''#!PYTHON
import json, os, pathlib, sys
args = sys.argv[1:]
with open(os.environ["TRACE"], "a") as trace:
    trace.write(json.dumps(["report", *args]) + "\n")
if "--report-init" in args:
    selection = args[args.index("--report-selection") + 1]
    name = "child-core.json" if selection == "core" else "initial.json"
    path = pathlib.Path(os.environ["FIXTURE_ROOT"]) / "test/e2e/results" / name
    path.write_text('{"evidence_status":"unattested"}')
    run_id = "fixture-child-core" if selection == "core" else "fixture-run"
    print(json.dumps({"run_id": run_id, "path": str(path)}))
elif "--report-finalize" in args:
    path = args[args.index("--report-run-path") + 1]
    if path.endswith("/child-core.json") and os.environ.get("CHILD_FINALIZE_REMOVE") == "1":
        pathlib.Path(path).unlink()
    if not (path.endswith("/child-core.json") and os.environ.get("CHILD_FINALIZE_NO_PATH") == "1"):
        print(path)
    code = os.environ.get("CHILD_FINALIZE_EXIT", "0") if path.endswith("/child-core.json") else os.environ.get("FINALIZE_EXIT", "0")
    sys.exit(int(code))
elif "--report-child" in args:
    sys.exit(int(os.environ.get("CHILD_REPORT_EXIT", "1")))
elif "--report-record" in args:
    print("fixture-record")
else:
    raise SystemExit("unexpected reporter action")
'''.replace("PYTHON", PYTHON, 1)

DOCKER = r'''#!PYTHON
import json, os, sys
args = sys.argv[1:]
with open(os.environ["TRACE"], "a") as trace:
    trace.write(json.dumps(["docker", *args]) + "\n")
if "up" in args:
    if os.environ.get("UP_STDOUT"):
        print(os.environ["UP_STDOUT"])
    sys.exit(int(os.environ.get("UP_EXIT", "0")))
if "down" in args:
    sys.exit(int(os.environ.get("DOWN_EXIT", "0")))
raise SystemExit("unexpected docker command")
'''.replace("PYTHON", PYTHON, 1)

EARLY_DOCKER = r'''#!PYTHON
import json, os, pathlib, sys
args = sys.argv[1:]
with open(os.environ["TRACE"], "a") as trace:
    trace.write(json.dumps(["docker", *args]) + "\n")
state = pathlib.Path(os.environ["FIXTURE_ROOT"]) / "killed"
if args[:2] == ["network", "create"] or args[0] == "run":
    print("fixture-container")
elif args[0] == "logs":
    print('{"msg":"Connecting to NATS"}')
    print('{"msg":"context canceled"}')
elif args[0] == "inspect":
    print(json.dumps([{"State":{"Running":not state.exists(),"ExitCode":1}}]))
elif args[0] == "exec":
    print("ESTABLISHED")
elif args[0] == "kill":
    state.touch()
elif args[0] == "rm":
    sys.exit(int(os.environ.get("RM_EXIT", "0")))
elif args[:2] == ["network", "rm"] or "down" in args:
    sys.exit(0)
else:
    raise SystemExit("unexpected Docker call: " + repr(args))
'''.replace("PYTHON", PYTHON, 1)


class TaskReportFixture(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="e2e-report-task-")
        self.addCleanup(temporary.cleanup)
        self.home = Path(temporary.name)
        (self.home / "cmd/e2e").mkdir(parents=True)
        (self.home / "scripts").mkdir()
        (self.home / "bin").mkdir()
        shutil.copy2(ROOT / "scripts/e2e-required-report.sh",
                     self.home / "scripts/e2e-required-report.sh")
        reporter = self.home / "cmd/e2e/e2e"
        reporter.write_text(REPORTER)
        reporter.chmod(0o755)
        docker = self.home / "bin/docker"
        docker.write_text(DOCKER)
        docker.chmod(0o755)
        for name in ("task", "jq", "sed", "tail", "cat", "mkdir", "dirname", "basename",
                     "grep", "sleep", "touch"):
            binary = shutil.which(name)
            self.assertIsNotNone(binary)
            (self.home / "bin" / name).symlink_to(binary)
        self.trace = self.home / "trace.jsonl"
        self.env = {
            **os.environ, "PATH": str(self.home / "bin"), "TRACE": str(self.trace),
            "FIXTURE_ROOT": str(self.home), "UP_EXIT": "42", "DOWN_EXIT": "0",
        }

    def install_structural(self, include_config=True):
        source = (ROOT / "taskfiles/e2e/structural.yml").read_text()
        start = source.index("    deps:\n")
        end = source.index("    cmds:\n", start)
        (self.home / "Taskfile.yml").write_text(source[:start] + source[end:])
        (self.home / "docker/compose").mkdir(parents=True)
        (self.home / "docker/compose/tiered.yml").write_text("services: {}")
        (self.home / "configs").mkdir()
        if include_config:
            (self.home / "configs/e2e-structural.json").write_text("{}")
        (self.home / "testdata/semantic").mkdir(parents=True)
        (self.home / "testdata/semantic/controlled.jsonl").write_text("{}\n")

    def install_structural_with_deps(self):
        (self.home / "taskfiles/e2e").mkdir(parents=True, exist_ok=True)
        shutil.copy2(ROOT / "taskfiles/e2e/structural.yml",
                     self.home / "taskfiles/e2e/structural.yml")
        (self.home / "Taskfile.yml").write_text('''version: "3"
includes:
  e2e:structural:
    taskfile: ./taskfiles/e2e/structural.yml
tasks:
  build:e2e:
    cmds:
      - 'touch "$BOOT_TRACE.build"; exit "$BUILD_EXIT"'
  e2e:clean:
    cmds:
      - 'touch "$BOOT_TRACE.clean"; exit "$CLEAN_EXIT"'
  e2e:check-ports:
    deps: [e2e:clean]
    cmds:
      - 'test -f "$BOOT_TRACE.clean" || exit 90; touch "$BOOT_TRACE.ports"; exit "$PORT_EXIT"'
''')
        self.env["BOOT_TRACE"] = str(self.home / "bootstrap.log")
        self.env.update(BUILD_EXIT="0", CLEAN_EXIT="0", PORT_EXIT="0")

    def install_composite(self, successful_children=False, failed_report_child=False):
        source = (ROOT / "Taskfile.yml").read_text()
        composite = source[source.index("  e2e:core-inference-agentic:\n"):]
        composite = composite.replace('    deps: ["build:e2e"]\n', "")
        children = ""
        for member in ("core", "structural", "statistical", "semantic", "agentic"):
            if member == "core" and failed_report_child:
                children += '''  e2e:core:
    cmds:
      - |
        . scripts/e2e-required-report.sh
        e2e_report_begin core fixtures e2e:core || exit 1
        e2e_report_finalize 17 0
'''
                continue
            command = ('printf "E2E_RESULT_PATH=/fixture/grandchild.json\\n'
                       'E2E_TASK_RESULT_PATH=/fixture/task-' + member + '.json\\n"'
                       if successful_children else "exit 33")
            children += f'  e2e:{member}:\n    cmds:\n      - \'{command}\'\n'
        (self.home / "Taskfile.yml").write_text('version: "3"\ntasks:\n' + composite + children)

    def install_core_early_cleanup(self):
        source = (ROOT / "taskfiles/e2e/core.yml").read_text()
        start = source.index("          (\n            current_check=core.early-cancel.exit")
        end = source.index("          ) || return $?", start) + len("          ) || return $?")
        actual_block = source[start:end]
        taskfile = '''version: "3"
tasks:
  default:
    cmds:
      - |
        . scripts/e2e-required-report.sh
        e2e_report_begin core fixtures e2e:core || exit 1
        command_exit=0
        finish() {
          trap - EXIT
          cleanup_exit=0
          docker compose -f docker/compose/e2e.yml down -v --timeout 15 >> "$E2E_REPORT_LOG" 2>&1 || cleanup_exit=$?
          report_exit=0
          e2e_report_finalize "$command_exit" "$cleanup_exit" || report_exit=$?
          cat "$E2E_REPORT_LOG"
          if [ "$command_exit" -ne 0 ]; then exit "$command_exit"; fi
          if [ "$cleanup_exit" -ne 0 ]; then exit "$cleanup_exit"; fi
          if [ "$report_exit" -ne 0 ]; then exit "$report_exit"; fi
        }
        trap finish EXIT
        run_body() {
''' + actual_block + '''
        }
        run_body >> "$E2E_REPORT_LOG" 2>&1 || command_exit=$?
        finish
'''
        (self.home / "Taskfile.yml").write_text(taskfile)
        docker = self.home / "bin/docker"
        docker.write_text(EARLY_DOCKER)
        docker.chmod(0o755)

    def run_task(self, target):
        process = subprocess.run(
            ["task", target], cwd=self.home, env=self.env,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=15,
            check=False,
        )
        entries = ([json.loads(line) for line in self.trace.read_text().splitlines()]
                   if self.trace.exists() else [])
        return process, entries

    def test_body_failure_still_cleans_then_finalizes(self):
        self.install_structural()
        self.env["DOWN_EXIT"] = "23"
        self.env["FINALIZE_EXIT"] = "1"
        self.env["UP_STDOUT"] = "fixture compose startup detail"
        process, entries = self.run_task("default")
        self.assertNotEqual(process.returncode, 0, process.stdout)
        self.assertIn("fixture compose startup detail", process.stdout)
        actions = [entry[0] for entry in entries]
        self.assertEqual(actions, ["report", "docker", "docker", "report"], entries)
        self.assertIn("up", entries[1])
        self.assertIn("down", entries[2])
        self.assertIn("--report-finalize", entries[3])
        self.assertEqual(entries[3][entries[3].index("--report-command-exit") + 1], "42")
        self.assertEqual(entries[3][entries[3].index("--report-cleanup-exit") + 1], "23")

    def test_post_init_metadata_failure_never_starts_body(self):
        self.install_structural(include_config=False)
        process, entries = self.run_task("default")
        self.assertNotEqual(process.returncode, 0, process.stdout)
        self.assertFalse(any(entry[0] == "docker" and "up" in entry for entry in entries), entries)
        self.assertTrue(any(entry[0] == "docker" and "down" in entry for entry in entries), entries)
        final = entries[-1]
        self.assertIn("--report-finalize", final)
        self.assertEqual(final[final.index("--report-command-exit") + 1], "1")

    def test_failed_prerequisites_never_initialize_report(self):
        for failed, expected_absent in (
            ("BUILD_EXIT", None),
            ("CLEAN_EXIT", "ports"),
            ("PORT_EXIT", None),
        ):
            with self.subTest(failed=failed):
                self.install_structural_with_deps()
                self.trace.unlink(missing_ok=True)
                for marker in ("build", "clean", "ports"):
                    (self.home / f"bootstrap.log.{marker}").unlink(missing_ok=True)
                self.env[failed] = "19"
                process, _ = self.run_task("e2e:structural")
                self.assertNotEqual(process.returncode, 0, process.stdout)
                self.assertFalse(self.trace.exists(), "reporter or Docker ran before prerequisites passed")
                events = [marker for marker in ("clean", "ports")
                          if (self.home / f"bootstrap.log.{marker}").exists()]
                if failed in ("CLEAN_EXIT", "PORT_EXIT"):
                    self.assertIn("clean", events)
                if failed == "PORT_EXIT":
                    self.assertIn("ports", events)
                if expected_absent:
                    self.assertNotIn(expected_absent, events)
                self.env[failed] = "0"

    def test_composite_child_prerequisite_failure_retains_parent_slot(self):
        self.install_composite()
        process, entries = self.run_task("e2e:core-inference-agentic")
        self.assertNotEqual(process.returncode, 0, process.stdout)
        self.assertIn("Required members: core, structural, statistical, semantic, agentic", process.stdout)
        child = next(entry for entry in entries if "--report-child" in entry)
        self.assertEqual(child[child.index("--report-member") + 1], "core")
        self.assertNotEqual(child[child.index("--report-child-exit") + 1], "0")
        self.assertEqual(json.loads(child[child.index("--report-argv-json") + 1]),
                         ["task", "e2e:core"])
        final = entries[-1]
        self.assertEqual(final[final.index("--report-command-exit") + 1], "1")

    def test_composite_links_failed_child_aggregate_and_stays_red(self):
        self.install_composite(failed_report_child=True)
        self.env["CHILD_FINALIZE_EXIT"] = "1"
        process, entries = self.run_task("e2e:core-inference-agentic")
        self.assertNotEqual(process.returncode, 0, process.stdout)
        child = next(entry for entry in entries if "--report-child" in entry)
        path = child[child.index("--report-child-path") + 1]
        self.assertEqual(path, str(self.home / "test/e2e/results/child-core.json"))
        self.assertTrue(Path(path).is_file(), "parent referenced an absent failed child artifact")
        self.assertNotEqual(child[child.index("--report-child-exit") + 1], "0")
        self.assertIn(f"E2E_TASK_RESULT_PATH={path}", process.stdout)
        final = entries[-1]
        self.assertIn("--report-finalize", final)
        self.assertNotEqual(final[final.index("--report-command-exit") + 1], "0")

    def test_composite_links_existing_initial_envelope_after_final_write_failure(self):
        self.install_composite(failed_report_child=True)
        self.env["CHILD_FINALIZE_EXIT"] = "1"
        self.env["CHILD_FINALIZE_NO_PATH"] = "1"
        process, entries = self.run_task("e2e:core-inference-agentic")
        self.assertNotEqual(process.returncode, 0, process.stdout)
        child = next(entry for entry in entries if "--report-child" in entry)
        path = child[child.index("--report-child-path") + 1]
        self.assertEqual(path, str(self.home / "test/e2e/results/child-core.json"))
        self.assertTrue(Path(path).is_file())
        self.assertNotEqual(child[child.index("--report-child-exit") + 1], "0")

    def test_composite_does_not_name_missing_failed_child_artifact(self):
        self.install_composite(failed_report_child=True)
        self.env["CHILD_FINALIZE_EXIT"] = "1"
        self.env["CHILD_FINALIZE_NO_PATH"] = "1"
        self.env["CHILD_FINALIZE_REMOVE"] = "1"
        process, entries = self.run_task("e2e:core-inference-agentic")
        self.assertNotEqual(process.returncode, 0, process.stdout)
        child = next(entry for entry in entries if "--report-child" in entry)
        self.assertEqual(child[child.index("--report-child-path") + 1], "")
        self.assertFalse((self.home / "test/e2e/results/child-core.json").exists())

    def test_composite_selects_each_task_aggregate_after_grandchild_marker(self):
        self.install_composite(successful_children=True)
        self.env["CHILD_REPORT_EXIT"] = "0"
        process, entries = self.run_task("e2e:core-inference-agentic")
        self.assertEqual(process.returncode, 0, process.stdout)
        children = [entry for entry in entries if "--report-child" in entry]
        self.assertEqual(len(children), 5, entries)
        for member, entry in zip(("core", "structural", "statistical", "semantic", "agentic"), children):
            self.assertEqual(entry[entry.index("--report-member") + 1], member)
            self.assertEqual(entry[entry.index("--report-child-path") + 1], f"/fixture/task-{member}.json")
            self.assertEqual(json.loads(entry[entry.index("--report-argv-json") + 1]), ["task", f"e2e:{member}"])

    def test_core_early_cleanup_failure_fails_after_two_passed_observations(self):
        self.install_core_early_cleanup()
        self.env["RM_EXIT"] = "17"
        process, entries = self.run_task("default")
        self.assertNotEqual(process.returncode, 0, process.stdout)
        observations = sorted((self.home / "test/e2e/results").glob("e2e-observation-*.json"))
        self.assertEqual(len(observations), 2, process.stdout)
        self.assertEqual({json.loads(path.read_text())["status"] for path in observations}, {"passed"})
        final = entries[-1]
        self.assertIn("--report-finalize", final)
        self.assertEqual(final[final.index("--report-command-exit") + 1], "1")
        self.assertEqual(final[final.index("--report-cleanup-exit") + 1], "0")
        self.assertTrue(any(entry[:2] == ["docker", "rm"] for entry in entries))
        self.assertTrue(any(entry[:3] == ["docker", "network", "rm"] for entry in entries))

    def test_graph_child_uses_fixture_phase_without_erasing_parent_phases(self):
        result_dir = self.home / "test/e2e/results"
        result_dir.mkdir(parents=True)
        production = result_dir / "production.bin"
        fixtures = result_dir / "fixtures.bin"
        production.write_bytes(b"production bytes")
        fixtures.write_bytes(b"fixture bytes")
        apps = [
            {"name": "production", "image_id": "sha256:" + "a" * 64,
             "image_digest": "unavailable: local image", "binary_path": str(production), "build": "production"},
            {"name": "fixtures", "image_id": "sha256:" + "b" * 64,
             "image_digest": "unavailable: local image", "binary_path": str(fixtures), "build": "fixtures"},
        ]
        env = {**self.env, "E2E_REPORT_DIR": str(result_dir), "E2E_REPORT_RUN_ID": "fixture-run",
               "E2E_REPORT_APPS": json.dumps(apps), "E2E_REPORT_FILES": "[]",
               "E2E_REPORT_PROFILES": "fixtures"}
        script = ('. scripts/e2e-required-report.sh; '
                  'e2e_report_child_input core.graph-roundtrip.identity core-graph-roundtrip fixtures')
        process = subprocess.run(["/bin/bash", "-c", script], cwd=self.home, env=env,
                                 capture_output=True, text=True, check=False)
        self.assertEqual(process.returncode, 0, process.stderr)
        child = json.loads(Path(process.stdout.strip()).read_text())
        self.assertEqual([app["name"] for app in child["app_phases"]], ["fixtures"])
        self.assertEqual(child["app_phases"][0]["binary_path"], str(fixtures))
        self.assertNotEqual(production.read_bytes(), fixtures.read_bytes())
        parent_input = result_dir / "parent-input.json"
        process = subprocess.run(
            ["/bin/bash", "-c", '. scripts/e2e-required-report.sh; '
             'e2e_report_write_input "$E2E_REPORT_DIR/parent-input.json" core "" ""'],
            cwd=self.home, env=env, capture_output=True, text=True, check=False,
        )
        self.assertEqual(process.returncode, 0, process.stderr)
        parent = json.loads(parent_input.read_text())
        self.assertEqual([app["name"] for app in parent["app_phases"]], ["production", "fixtures"])


if __name__ == "__main__":
    unittest.main()
