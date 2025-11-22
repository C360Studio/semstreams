# E2E Testing Quick Start

**For AI Agents: This is the canonical reference for running E2E tests. Read this first!**

## ⚡ TL;DR - Most Common Commands

```bash
# Run all semantic tests (RECOMMENDED for verification)
task e2e:semantic

# Run fast tests (good for development)
task e2e:semantic-indexes

# Run core protocol tests
task e2e:health
task e2e:dataflow

# Clean up if tests fail
task e2e:clean
```

## 🎯 Current E2E Test Status

✅ **All semantic E2E tests passing** (as of last run)
- `semantic-basic`: Entity processing pipeline
- `semantic-indexes`: Core indexing (fast, no external deps)
- `semantic-kitchen-sink`: Full stack with SemEmbed

## 📋 Available Tests

### Semantic Tests (Primary Test Suite)

| Command | Duration | Purpose | Dependencies |
|---------|----------|---------|--------------|
| `task e2e:semantic` | ~20s | **ALL semantic tests** | NATS + SemEmbed |
| `task e2e:semantic-indexes` | ~5s | **Fast indexing tests** | NATS only |
| `task e2e:semantic-basic` | ~5s | Basic entity processing | NATS only |
| `task e2e:semantic-kitchen-sink` | ~15s | Full stack + metrics | NATS + SemEmbed |

### Protocol Tests (Core Layer)

| Command | Duration | Purpose |
|---------|----------|---------|
| `task e2e:health` | ~3s | Component health checks |
| `task e2e:dataflow` | ~5s | Data pipeline validation |
| `task e2e` | ~8s | All core tests |

## 🚨 Common Mistakes (for Agents)

### ❌ WRONG: Trying to run outdated commands
```bash
task e2e:smoke  # ← Does NOT exist
```

### ✅ CORRECT: Use current test commands
```bash
task e2e:semantic-indexes  # Fast test for development
task e2e:semantic          # Complete semantic test suite
```

### ❌ WRONG: Assuming tests are broken without checking
If you see "StreamKit" in errors, the tests are fine - just old binaries/containers.

### ✅ CORRECT: Clean and rebuild
```bash
task clean:all    # Clean everything
task e2e:semantic # Rebuild and run tests
```

## 🔧 Troubleshooting

### "Port 8080 already in use"
```bash
task e2e:clean  # Stop all E2E containers
# or
lsof -ti:8080 | xargs kill -9
```

### "Container semstreams-e2e-app not found"
This is normal! Containers are created during test run. Just run the test.

### "Tests failing after code changes"
```bash
task clean:all    # Clean binaries AND docker cache
task e2e:semantic # Rebuild everything fresh
```

### "Waiting for SemStreams... timeout"
```bash
# Check logs
docker logs semstreams-e2e-app

# Debug mode (leaves containers running)
task e2e:debug
docker logs -f semstreams-e2e-app
```

## 📚 Full Documentation

- **Detailed E2E docs**: `test/e2e/README.md`
- **Task reference**: `task --list`
- **CLAUDE.md**: Project development standards

## 🎓 For New Agents

**IMPORTANT**: Do not assume test structure from other projects. SemStreams uses:
- **Task runner** (not make, not npm scripts)
- **Docker Compose** for E2E environment
- **Observer Pattern** (tests observe running containers)
- **Real services** (NATS, SemEmbed - not mocks)

**Before asking "how do I run tests?"**, run:
```bash
cat E2E_QUICK_START.md  # You're reading it now!
task --list             # See all available tasks
```

## ✅ Test Status Verification

To verify ALL tests are passing:
```bash
task e2e:semantic  # Should show: "passed=3 failed=0"
```

Expected output:
```
✅ Semantic scenario PASSED name=semantic-basic
✅ Semantic scenario PASSED name=semantic-indexes
✅ Semantic scenario PASSED name=semantic-kitchen-sink
✅ Semantic test suite complete passed=3 failed=0 total=3
```
