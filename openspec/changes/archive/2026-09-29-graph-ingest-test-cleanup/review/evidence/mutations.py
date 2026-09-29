import hashlib
import json
import subprocess
from pathlib import Path

root = Path("/Users/coby/.codex/worktrees/gh1397-runner-tests/semstreams")
source = root / "processor/graph-ingest/test_owner_support_test.go"
backup = Path("/private/tmp/gh1423-test-owner-support.bak")
subprocess.run(["cp", str(source), str(backup)], check=True)
original = source.read_text()
before = subprocess.check_output(["md5", "-q", str(source)], text=True).strip()
cases = [
    ("provisional_removed", "if !o.transferred {\n\t\to.finish(t, operationCtx)\n\t}", "if !o.transferred {\n\t\t// Mutation: abandon provisional ownership.\n\t}", "^TestGraphIngestOwnerAssertionExitAndCleanupError$/^setup-exit$", "child missing witness"),
    ("premature_start_cancel", "defer o.cancelStart() // Accepted Start authority stays live through concrete Stop.", "o.cancelStart() // Mutation: end Start authority before concrete Stop.", "^TestGraphIngestTestOwnerControlledStopPreservesAuthority$", "accepted Start authority ended before Stop"),
    ("discard_stop_error", "stopErr := o.component.Stop(stopCtx)", "_ = o.component.Stop(stopCtx) // Mutation: discard concrete result.\n\tvar stopErr error", "^TestGraphIngestTestOwnerCanceledOperationKeepsFreshTerminalAuthority$", "want concrete and operation causes"),
    ("implicit_retry", "if o.attempted {\n\t\treturn\n\t}\n\tif err := o.stop(operationCtx)", "o.attempted = false // Mutation: retry after an explicit terminal attempt.\n\tif err := o.stop(operationCtx)", "^TestGraphIngestTestOwnerFailedAttemptIsNotRetried$", "failed-start Drain attempts"),
]
results = []
try:
    for name, old, new, selector, oracle in cases:
        assert source.read_text() == original, f"unexpected source before {name}"
        assert original.count(old) == 1, f"mutation anchor count {name}: {original.count(old)}"
        source.write_text(original.replace(old, new, 1))
        mutant_md5 = subprocess.check_output(["md5", "-q", str(source)], text=True).strip()
        try:
            command = ["go", "test", "-race", "-count=1", "./processor/graph-ingest", "-run", selector]
            run = subprocess.run(command, cwd=root, capture_output=True, text=True, timeout=60)
            output = run.stdout + run.stderr
            exit_code = run.returncode
        except subprocess.TimeoutExpired as exc:
            output = str(exc)
            exit_code = 124
        log = Path("/private/tmp") / f"gh1423-mutation-{name}.log"
        log.write_text(output)
        results.append({"name": name, "command": command, "source_md5_before": before,
                        "mutant_md5": mutant_md5, "exit_code": exit_code,
                        "oracle": oracle, "oracle_reached": oracle in output, "log": str(log)})
        subprocess.run(["cp", str(backup), str(source)], check=True)
        assert subprocess.check_output(["md5", "-q", str(source)], text=True).strip() == before
finally:
    subprocess.run(["cp", str(backup), str(source)], check=True)
    assert subprocess.check_output(["md5", "-q", str(source)], text=True).strip() == before
Path("/private/tmp/gh1423-mutations.json").write_text(json.dumps(results, indent=2) + "\n")
print(json.dumps(results, indent=2))
if any(item["exit_code"] in (0, 124) or not item["oracle_reached"] for item in results):
    raise SystemExit(1)
