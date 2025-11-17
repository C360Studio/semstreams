# Folder Structure Analysis & Recommendations

## Current Structure Issues

### Problems Encountered

1. **Path Brittleness in Frontend Tests**
   ```typescript
   // Current: Fragile 4-level relative path
   const OPENAPI_SPEC_PATH = join(__dirname, '../../../../semstreams/specs/openapi.v3.yaml');
   ```
   - Breaks if repos are reorganized
   - Hard to understand at a glance
   - Assumes specific directory nesting

2. **Cross-Repo Dependencies**
   ```go
   // semmem tests require semstreams as sibling
   semstreamsRoot := filepath.Join(filepath.Dir(semmemRoot), "semstreams")
   ```
   - Tests silently skip if repos aren't in expected locations
   - Not obvious from error messages what's wrong

3. **No Shared Contract Location**
   - Schemas live in `semstreams/schemas/`
   - Frontend needs to reach across repo boundaries
   - Each consuming repo has different path calculation

### Current Directory Structure

```
/Users/coby/Code/c360/semstreams/
├── semstreams/              # Backend (Go)
│   ├── cmd/schema-exporter/ # Schema generation tool
│   ├── schemas/            # ✓ Generated JSON schemas
│   ├── specs/              # ✓ OpenAPI spec + meta-schema
│   ├── docs/               # ✓ Documentation
│   └── test/contract/      # ✓ Backend contract tests
│
├── semstreams-ui/          # Frontend (Svelte)
│   ├── src/lib/contract/  # ✓ Frontend contract tests
│   └── src/lib/types/      # ✓ Generated TypeScript types
│
└── semmem/                 # Separate repo
    └── test/contract/      # ✓ Cross-repo validation
```

## Proposed Solutions

### Option 1: Improved Environment Variables (Quick Win)

**Pros**: Minimal changes, CI-friendly
**Cons**: Still relies on file system layout

```typescript
// semstreams-ui/src/lib/contract/openapi.contract.test.ts
const SEMSTREAMS_ROOT = process.env.SEMSTREAMS_ROOT ||
  join(__dirname, '../../../../semstreams');

const OPENAPI_SPEC_PATH = process.env.OPENAPI_SPEC_PATH ||
  join(SEMSTREAMS_ROOT, 'specs/openapi.v3.yaml');

const SCHEMAS_DIR = process.env.SCHEMAS_DIR ||
  join(SEMSTREAMS_ROOT, 'schemas');
```

**Setup** (`.envrc` or CI):
```bash
export SEMSTREAMS_ROOT=/Users/coby/Code/c360/semstreams/semstreams
export OPENAPI_SPEC_PATH=/Users/coby/Code/c360/semstreams/semstreams/specs/openapi.v3.yaml
export SCHEMAS_DIR=/Users/coby/Code/c360/semstreams/semstreams/schemas
```

### Option 2: Shared Artifacts Directory (Monorepo-Style)

**Pros**: Single source of truth, clear ownership
**Cons**: Requires directory restructuring

```
/Users/coby/Code/c360/semstreams/
├── contracts/              # NEW: Shared contract artifacts
│   ├── schemas/           # Generated JSON schemas
│   │   ├── udp.v1.json
│   │   └── graph-processor.v1.json
│   ├── specs/             # OpenAPI specs
│   │   ├── openapi.v3.yaml
│   │   └── component-schema-meta.json
│   └── docs/              # Shared documentation
│       ├── SCHEMA_GENERATION.md
│       └── CONTRACT_TESTING.md
│
├── semstreams/            # Backend
│   ├── cmd/schema-exporter/  # Generates to ../contracts/
│   └── test/contract/        # Tests ../contracts/
│
├── semstreams-ui/         # Frontend
│   ├── src/lib/contract/     # Tests ../contracts/
│   └── src/lib/types/        # Generated from ../contracts/specs/
│
└── semmem/               # Separate repo
    └── test/contract/        # Tests ../contracts/
```

**Changes Required**:
1. Move `schemas/` and `specs/` to `contracts/`
2. Update schema-exporter output paths
3. Update all test paths
4. Update documentation paths

### Option 3: Copy Artifacts During Build (Decoupled)

**Pros**: Each repo self-contained, no runtime dependency
**Cons**: Duplication, sync required

```
semstreams-ui/
├── contracts/             # NEW: Copied from backend
│   ├── schemas/          # Copy from semstreams/schemas/
│   └── specs/            # Copy from semstreams/specs/
├── src/lib/contract/     # Tests local contracts/
└── src/lib/types/        # Generated from local contracts/
```

**Build Script** (`package.json`):
```json
{
  "scripts": {
    "sync-contracts": "rsync -av ../semstreams/schemas/ contracts/schemas/ && rsync -av ../semstreams/specs/ contracts/specs/",
    "generate-types": "npm run sync-contracts && openapi-typescript contracts/specs/openapi.v3.yaml -o src/lib/types/api.generated.ts",
    "test:contract": "npm run sync-contracts && vitest run src/lib/contract"
  }
}
```

**CI Integration**:
```yaml
# .github/workflows/frontend.yml
- name: Sync contract artifacts
  run: npm run sync-contracts

- name: Generate types
  run: npm run generate-types

- name: Run contract tests
  run: npm run test:contract
```

### Option 4: Workspace Configuration (Recommended)

**Pros**: Clear configuration, easy to override, works in any environment
**Cons**: Requires configuration file

Create workspace config that all tools use:

```javascript
// workspace.config.js (root of monorepo)
module.exports = {
  semstreams: {
    root: './semstreams',
    schemas: './semstreams/schemas',
    specs: './semstreams/specs',
    docs: './semstreams/docs'
  },
  semstreamsUi: {
    root: './semstreams-ui',
    contracts: './semstreams-ui/src/lib/contract',
    types: './semstreams-ui/src/lib/types'
  },
  semmem: {
    root: './semmem',
    schemas: './semmem/schemas',
    specs: './semmem/specs'
  }
};
```

**Usage in Tests**:
```typescript
// semstreams-ui/src/lib/contract/openapi.contract.test.ts
import { resolve } from 'path';
import workspaceConfig from '../../../../workspace.config';

const OPENAPI_SPEC_PATH = process.env.OPENAPI_SPEC_PATH ||
  resolve(__dirname, '../../../..', workspaceConfig.semstreams.specs, 'openapi.v3.yaml');
```

**Usage in Go Tests**:
```go
// test/contract/helpers.go
func findWorkspaceRoot() (string, error) {
    // Look for workspace.config.js
    dir, _ := os.Getwd()
    for {
        configPath := filepath.Join(dir, "workspace.config.js")
        if _, err := os.Stat(configPath); err == nil {
            return dir, nil
        }
        parent := filepath.Dir(dir)
        if parent == dir {
            break
        }
        dir = parent
    }
    return "", errors.New("workspace.config.js not found")
}
```

## Recommended Approach

**Phase 1: Quick Wins (Now)**

1. ✅ Add environment variable support (already done)
2. ✅ Document expected directory structure in README
3. ✅ Add clear error messages when paths not found

**Example Error Message**:
```typescript
try {
  readFileSync(OPENAPI_SPEC_PATH, 'utf8');
} catch (err) {
  throw new Error(`
    Failed to load OpenAPI spec from: ${OPENAPI_SPEC_PATH}

    This test requires the semstreams backend repository to be available.

    Expected directory structure:
      /path/to/parent/
        ├── semstreams/specs/openapi.v3.yaml
        └── semstreams-ui/  (current repo)

    You can override the path with environment variable:
      export OPENAPI_SPEC_PATH=/path/to/openapi.v3.yaml

    Or skip these tests:
      npm test -- --exclude "src/lib/contract/**"
  `);
}
```

**Phase 2: Workspace Config (Next)**

1. Create `workspace.config.js` at repo root
2. Update all tools to use workspace config
3. Keep environment variable override support
4. Document in CONTRIBUTING.md

**Phase 3: CI/CD Improvements (Later)**

1. Add artifact caching in CI
2. Publish schemas as build artifacts
3. Consider npm package for schemas (if needed for external consumers)

## Path Resolution Best Practices

### Backend (Go)

```go
// ✅ Good: Walk up to find workspace root
func findRepoRoot(t *testing.T) string {
    t.Helper()
    dir, _ := os.Getwd()

    for {
        // Look for go.mod or .git as marker
        if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
            return dir
        }

        parent := filepath.Dir(dir)
        if parent == dir {
            t.Fatal("Could not find repo root (no go.mod found)")
        }
        dir = parent
    }
}

func TestSchemas(t *testing.T) {
    repoRoot := findRepoRoot(t)
    schemasDir := filepath.Join(repoRoot, "schemas")

    // Use schemasDir...
}
```

### Frontend (TypeScript)

```typescript
// ✅ Good: Try multiple strategies
function findOpenAPISpec(): string {
  // Strategy 1: Environment variable (highest priority)
  if (process.env.OPENAPI_SPEC_PATH) {
    return process.env.OPENAPI_SPEC_PATH;
  }

  // Strategy 2: Workspace config
  try {
    const workspaceConfig = require('../../../../workspace.config.js');
    return resolve(__dirname, '../../../..', workspaceConfig.semstreams.specs, 'openapi.v3.yaml');
  } catch {}

  // Strategy 3: Conventional location
  const conventionalPath = join(__dirname, '../../../../semstreams/specs/openapi.v3.yaml');
  if (existsSync(conventionalPath)) {
    return conventionalPath;
  }

  throw new Error('OpenAPI spec not found. Set OPENAPI_SPEC_PATH or ensure repos are in standard layout.');
}
```

## Documentation Structure

### Current Location: semstreams/docs/

**Pros**:
- ✅ Lives with implementation
- ✅ Versioned with code
- ✅ Easy to keep in sync

**Cons**:
- ❌ Frontend devs may not look here
- ❌ Schema-specific docs mixed with general docs

### Recommendation: Keep Current Structure

The current structure is actually good:

```
semstreams/docs/
├── SCHEMA_GENERATION.md         # Overview (central reference)
├── SCHEMA_TAGS_GUIDE.md         # For backend developers
├── SCHEMA_VERSIONING.md         # For all developers
├── MIGRATION_GUIDE.md           # For backend developers
├── OPENAPI_INTEGRATION.md       # For frontend developers
├── CONTRACT_TESTING.md          # For all developers
└── CI_SCHEMA_INTEGRATION.md     # For DevOps

# Add cross-references from frontend
semstreams-ui/README.md:
  "See ../semstreams/docs/SCHEMA_GENERATION.md for schema documentation"

semstreams-ui/CONTRIBUTING.md:
  "Schema contracts defined in ../semstreams/docs/"
```

## Test Organization

### Current Structure: Good ✅

```
semstreams/test/contract/        # Backend-specific contract tests
  ├── schema_contract_test.go    # Schemas match code
  └── openapi_contract_test.go   # OpenAPI spec valid

semstreams-ui/src/lib/contract/  # Frontend-specific contract tests
  ├── openapi.contract.test.ts   # Can load OpenAPI spec
  └── types.contract.test.ts     # TypeScript types valid

semmem/test/contract/            # Cross-repo contract tests
  └── cross_repo_test.go         # Cross-repo compatibility
```

**Why This Works**:
- Each repo tests its own concerns
- Contract tests are separate from unit/integration tests
- Can run independently or together
- Clear naming convention (*.contract.test.*)

## Summary

### Issues We Hit

| Issue | Severity | Solution |
|-------|----------|----------|
| Fragile relative paths | Medium | Environment variables + workspace config |
| Cross-repo dependencies | Low | Document expected structure, clear errors |
| Path resolution failures | Medium | Multi-strategy resolution + good errors |

### Recommended Changes

**Immediate (Phase 1)**:
1. ✅ Environment variables (done)
2. Add better error messages
3. Document directory structure in README

**Short-term (Phase 2)**:
1. Create `workspace.config.js`
2. Update tools to use config
3. Add path resolution helpers

**Long-term (Phase 3)**:
1. Consider monorepo tooling (Turborepo, Nx) if repos merge
2. Publish schemas as npm package if external consumers need them
3. Add schema CDN/registry for runtime access

### Keep What Works ✅

1. **Docs location**: `semstreams/docs/` is fine
2. **Test location**: `test/contract/` in each repo is good
3. **Schema generation**: `semstreams/schemas/` and `specs/` is correct
4. **Type generation**: `semstreams-ui/src/lib/types/` is appropriate

The current structure is **fundamentally sound**. We just need to:
- ✅ Make path resolution more robust
- ✅ Add better error messages
- ✅ Document expected structure

No major restructuring needed!
