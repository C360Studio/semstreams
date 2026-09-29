#!/usr/bin/env bash
set -euo pipefail

# One uncached invocation owns full-suite cleanup admission. Loading integration
# and live_llm source checks types only; it never executes those tests.
cleanup_repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$cleanup_repo_root"
exec go test -count=1 ./test/testinfra -run '^TestCleanupRootGuard$'
