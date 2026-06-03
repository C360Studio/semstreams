#!/usr/bin/env bash
# lint-test-ports.sh — substrate-flake guard for gh#220 Subclass 2.
#
# Fails (exit 1) if any *_test.go file binds a fixed TCP port via
# net.Listen. Ephemeral allocation via net.Listen("tcp", ":0") or
# "127.0.0.1:0" is the required pattern. Fixed-port literals collide
# under parallel test execution and were the source of gh#209 and the
# websocket integration flakes.
#
# Sourced from BOTH taskfiles/lint.yml and .github/workflows/ci.yml so
# the regex has a single source of truth (gh#220 review I4).
#
# Known false-negative shapes (acceptable for Step 1 — sweep in
# follow-up if violations surface):
#   - tcp6 protocol variant: net.Listen("tcp6", ":1234")
#   - Multi-line: net.Listen("tcp",\n\t":1234")
#   - String concatenation: net.Listen("tcp", host+":"+portStr)
#   - Variable indirection: addr := ":1234"; net.Listen("tcp", addr)
#   - Non-net.Listen APIs: http.Server{Addr: fmt.Sprintf(":%d", N)} etc.
#     (the github-webhook tests use this — sweep in Subclass 2 follow-up)
#
# Run from repo root:
#   scripts/lint-test-ports.sh

set -euo pipefail

# Pattern 1: literal port like ":18082" or "127.0.0.1:8080" (anything
# ending in :NNNN where NNNN is non-zero).
LITERAL_PATTERN='net\.Listen\("tcp[46]?",\s*"[^"]*:[1-9][0-9]*"'

# Pattern 2: Sprintf form, both bare (":%d") and host-prefixed
# ("127.0.0.1:%d"). The greedy [^"]* between quotes accepts any
# host segment (or none) before the :%d.
SPRINTF_PATTERN='net\.Listen\("tcp[46]?",\s*fmt\.Sprintf\("[^"]*:%d'

# Comment-suppression: lines ending with `// gh#220:allow-fixed-port`
# are exempted (intended for the rare case where the test semantically
# requires rebinding to a specific port — e.g. verifying the OS
# released a previously-allocated ephemeral port). The marker is grep-
# greppable so reviewers can audit all exemptions in one pass.
SUPPRESS_MARKER='// gh#220:allow-fixed-port'

matches=$(grep -rnE "$LITERAL_PATTERN" --include='*_test.go' . 2>/dev/null | grep -vF "$SUPPRESS_MARKER" || true)
sprintf_matches=$(grep -rnE "$SPRINTF_PATTERN" --include='*_test.go' . 2>/dev/null | grep -vF "$SUPPRESS_MARKER" || true)
all=$(printf '%s\n%s\n' "$matches" "$sprintf_matches" | grep -v '^$' || true)

if [ -n "$all" ]; then
  echo 'FAIL: fixed-port net.Listen in test files (substrate-flake guard, gh#220 Subclass 2)'
  echo
  echo "$all"
  echo
  echo 'Fix: use net.Listen("tcp", ":0") + read the resolved port from listener.Addr().(*net.TCPAddr).Port.'
  echo 'See service/service_manager_health_listener_test.go freePort() helper for the canonical pattern.'
  exit 1
fi
