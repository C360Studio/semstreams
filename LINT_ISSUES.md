# Revive Lint Issues - Project Plan

**Total Warnings:** 210 across 24 packages
**Generated:** 2025-11-21

## Priority Classification

### CRITICAL (Code Smells - Must Fix First)

#### processor/graph
- **2 context-as-argument warnings** - Context not being passed properly (bad practice)
- **5 cyclomatic complexity warnings** - Functions too complex (maintainability issue)
- **Status:** Production code with architectural issues

#### pkg/retry
- **1 cyclomatic complexity warning** - Function `Do` has complexity 21 (threshold: 20)
- **Status:** Core utility code

---

## HIGH PRIORITY (Production Code - Many Warnings)

### 1. processor/graph (60 warnings total)
- 53 unused-parameter
- 5 cyclomatic complexity (CRITICAL)
- 2 context-as-argument (CRITICAL)

**Impact:** Core message processing pipeline
**Action:** go-developer fixes → go-reviewer reviews

### 2. gateway/graphql (41 warnings total)
- 40 unused-parameter
- 1 exported (naming/documentation)

**Impact:** GraphQL API gateway
**Action:** go-developer fixes → go-reviewer reviews

### 3. pkg/graphclustering (39 warnings total)
- 37 unused-parameter
- 1 cyclomatic complexity
- 1 package-comments

**Impact:** Graph clustering algorithms
**Action:** go-developer fixes → go-reviewer reviews

### 4. processor/rule (28 warnings total)
- 18 unused-parameter
- 9 exported (naming stutters, missing comments)
- 1 indent-error-flow (FIXED)

**Impact:** Rule processing engine
**Action:** go-developer fixes → go-reviewer reviews

---

## MEDIUM PRIORITY (Production Code - Some Warnings)

### 5. pkg/embedding (7 warnings)
- 7 exported (all naming/documentation issues)

**Action:** go-developer fixes → go-reviewer reviews

### 6. cmd/semstreams-gqlgen (7 warnings)
- Mixed types

**Action:** go-developer fixes

### 7. pkg/tlsutil (5 warnings)
- Mixed types

**Action:** go-developer fixes

### 8. pkg/acme (3 warnings)
- 2 unused-parameter
- 1 if-return

**Action:** go-developer fixes

### 9. gateway/http (3 warnings)
- 2 unused-parameter
- 1 exported

**Action:** go-developer fixes

---

## LOW PRIORITY (Tests & Single Warnings)

- cmd/schema-exporter (2 warnings)
- vocabulary/registry (2 empty-block - FIXED)
- service/flow_runtime_* (3 warnings scattered)
- processor/json_* (2 warnings)
- component/* (3 warnings)
- test/e2e (1 warning)
- natsclient (1 warning)
- input/websocket (1 warning)

**Action:** Quick fixes, no review needed

---

## Warning Type Summary

| Warning Type | Count | Severity | Fix Complexity |
|--------------|-------|----------|----------------|
| unused-parameter | 163 | Low | Easy (rename to `_`) |
| exported | 19 | Medium | Medium (rename types, add docs) |
| cyclomatic | 18 | **HIGH** | Hard (refactor functions) |
| package-comments | 3 | Low | Easy (add comment) |
| context-as-argument | 2 | **CRITICAL** | Medium (fix signatures) |
| if-return | 2 | Low | Easy (remove redundant) |
| empty-block | 2 | Low | **FIXED** |
| indent-error-flow | 1 | Low | **FIXED** |

---

## Execution Plan

### Phase 1: Critical Issues (Immediate)
1. **go-developer**: Fix processor/graph context-as-argument warnings
2. **go-reviewer**: Review processor/graph context fixes
3. **go-developer**: Refactor processor/graph high-complexity functions
4. **go-reviewer**: Review processor/graph complexity refactors
5. **go-developer**: Fix pkg/retry complexity
6. **go-reviewer**: Review pkg/retry

### Phase 2: High Priority Packages (Sequential)
For each package (processor/graph, gateway/graphql, pkg/graphclustering, processor/rule):
1. **go-developer**: Fix all warnings in package
2. **go-reviewer**: Review package fixes
3. Run tests: `go test ./[package]/...`
4. Run revive: `revive ./[package]/...`
5. Verify 0 warnings before moving to next package

### Phase 3: Medium Priority Packages (Batch)
1. **go-developer**: Fix remaining packages in batch
2. **go-reviewer**: Review all medium priority fixes

### Phase 4: Validation
1. Run full revive: `revive -config .revive.toml -formatter friendly ./...`
2. Verify 0 warnings
3. Run all CI checks:
   - `go fmt ./...`
   - `go vet ./...`
   - `revive -config .revive.toml -formatter friendly ./...`
   - `go test -v -race ./...`
   - `INTEGRATION_TESTS=1 go test -v -race ./...`

---

## Notes

- **No gaming**: Each package must be properly fixed, not just warning-suppressed
- **Test after each fix**: Ensure no regressions
- **Review is mandatory**: All HIGH priority packages require go-reviewer approval
- **Context warnings are architectural**: May require signature changes across callers

---

## Completed Fixes

- ✅ vocabulary/registry.go - 2 empty-block warnings (removed empty if blocks)
- ✅ processor/rule/expression/evaluator.go - 1 indent-error-flow (removed unnecessary else)
