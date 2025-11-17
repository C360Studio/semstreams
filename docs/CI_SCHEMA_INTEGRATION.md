# CI/CD Schema Integration

This document describes the automated schema generation and validation system integrated into the CI/CD pipelines.

## Overview

The schema generation system ensures that JSON Schemas, OpenAPI specifications, and TypeScript types remain synchronized across all repositories. CI workflows automatically validate that committed schemas match the code, preventing schema drift.

## Workflow Structure

### semstreams Repository

**Workflow**: `.github/workflows/ci.yml`

**Schema Validation Job**:
```yaml
schema-validation:
  - Generate schemas from component registry
  - Validate against meta-schema
  - Check for uncommitted changes
  - Fail if schemas are out of sync
```

**Trigger**: Push or PR to `main`/`develop` branches

**What it checks**:
- `semstreams/schemas/*.v1.json` - Component configuration schemas
- `semstreams/specs/openapi.v3.yaml` - OpenAPI 3.0 specification

**Failure conditions**:
- Schema generation produces different output than committed files
- Schema validation against meta-schema fails
- Schema exporter tests fail

### semmem Repository

**Workflow**: `semmem/.github/workflows/ci.yml`

**Schema Validation Job**:
```yaml
schema-validation:
  - Generate schemas from semmem component registry
  - Validate against meta-schema
  - Check for uncommitted changes
  - Fail if schemas are out of sync
```

**Trigger**: Push or PR to `main`/`develop` branches

**What it checks**:
- `semmem/schemas/*.v1.json` - Component configuration schemas
- `semmem/specs/openapi.v3.yaml` - OpenAPI 3.0 specification

**Failure conditions**:
- Schema generation produces different output than committed files
- Schema validation against meta-schema fails
- Schema exporter tests fail

### semstreams-ui Repository

**Workflows**:
- `.github/workflows/ci.yml` - Fast CI checks (lint, types, tests, build)
- `.github/workflows/e2e-tests.yml` - Full E2E tests

**Type Generation Job** (in both workflows):
```yaml
type-generation:
  - Generate TypeScript types from OpenAPI spec
  - Validate types compile
  - Check for uncommitted changes
  - Fail if types are out of sync
```

**Trigger**: Push or PR to `main`/`develop` branches

**What it checks**:
- `src/lib/types/api.generated.ts` - Generated TypeScript types

**Failure conditions**:
- Type generation produces different output than committed file
- Generated types fail to compile
- OpenAPI spec location is invalid

## Local Development Workflow

### Making Schema Changes (semstreams/semmem)

1. **Modify component code**:
   ```go
   type Config struct {
       NewField string `schema:"type:string,desc:New configuration field"`
   }
   ```

2. **Regenerate schemas**:
   ```bash
   task schema:generate
   ```

3. **Verify changes**:
   ```bash
   git diff schemas/ specs/openapi.v3.yaml
   ```

4. **Commit both code and schemas**:
   ```bash
   git add .
   git commit -m "feat(component): add new configuration field"
   ```

### Updating TypeScript Types (semstreams-ui)

1. **After OpenAPI spec changes** (in semstreams or semmem):
   ```bash
   task generate-types
   ```

2. **Verify types**:
   ```bash
   git diff src/lib/types/api.generated.ts
   ```

3. **Commit generated types**:
   ```bash
   git add src/lib/types/api.generated.ts
   git commit -m "chore: regenerate types from OpenAPI spec"
   ```

## CI/CD Integration Details

### Schema Generation in CI

**Install Task runner**:
```bash
sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b /usr/local/bin
```

**Generate schemas**:
```bash
task schema:generate
```

**Validate no drift**:
```bash
git diff --exit-code schemas/ specs/openapi.v3.yaml
```

### Type Generation in CI

**Generate TypeScript types**:
```bash
task generate-types
```

**Validate no drift**:
```bash
git diff --exit-code src/lib/types/api.generated.ts
```

**Validate types compile**:
```bash
npx tsc --noEmit --skipLibCheck src/lib/types/api.generated.ts
```

## Error Messages

### Schema Out of Sync

```
❌ Schema or OpenAPI files have uncommitted changes!

The following files changed after running 'task schema:generate':
  schemas/udp.v1.json
  specs/openapi.v3.yaml

Please run 'task schema:generate' locally and commit the changes.
```

**Fix**: Run `task schema:generate` and commit the result

### Types Out of Sync

```
❌ Generated TypeScript types have uncommitted changes!

The TypeScript types changed after running 'task generate-types'.
This means the OpenAPI spec was updated but types weren't regenerated.

Please run 'task generate-types' locally and commit the changes.
```

**Fix**: Run `task generate-types` and commit the result

### Schema Validation Failure

```
Schema validation failed for udp:
  - required: Invalid type. Expected: array, given: null
```

**Fix**: Ensure component schema has proper structure (check meta-schema requirements)

## Bypassing Schema Checks (Not Recommended)

If absolutely necessary (e.g., during schema format migration):

1. **Temporarily disable check**: Comment out the schema validation job in `.github/workflows/ci.yml`
2. **Create tracking issue**: Document why bypass was needed
3. **Re-enable ASAP**: Once schema issues are resolved

**Note**: This defeats the purpose of schema validation and should only be used in exceptional circumstances.

## Benefits

### 1. **Contract Enforcement**
- Schemas always match code
- Breaking changes visible in git diffs
- Type safety from backend to frontend

### 2. **Automated Validation**
- No manual schema generation step
- Immediate feedback on PRs
- Prevents schema drift

### 3. **Cross-Repository Synchronization**
- semstreams schemas → OpenAPI spec → semstreams-ui types
- Changes propagate with explicit commits
- Full audit trail in git history

### 4. **Developer Experience**
- Single command: `task schema:generate`
- Clear error messages
- Fast feedback loop

## Troubleshooting

### Task Not Found

**Error**: `task: command not found`

**Fix**: Install task locally:
```bash
# macOS
brew install go-task/tap/go-task

# Linux
sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d
```

### OpenAPI Spec Not Found

**Error**: `Can't resolve $ref: ENOENT: no such file or directory`

**Fix**: Check `OPENAPI_SPEC_PATH` environment variable or use default path:
```bash
export OPENAPI_SPEC_PATH=../semstreams/specs/openapi.v3.yaml
task generate-types
```

### Schema Validation Errors

**Error**: Meta-schema validation fails

**Fix**: Examine `specs/component-schema-meta.json` requirements:
- Required fields: `$schema`, `$id`, `type`, `title`, `description`, `properties`, `x-component-metadata`
- Component metadata must include: `name`, `type`, `protocol`, `domain`, `version`
- Schema ID must match pattern: `^[a-z0-9_-]+\.v[0-9]+\.json$`

## Related Documentation

- [Schema Generation System](./SCHEMA_GENERATION.md)
- [OpenAPI Specification](../specs/openapi.v3.yaml)
- [Meta-Schema](../specs/component-schema-meta.json)
- [TypeScript Type Generation](../../semstreams-ui/README.md#type-generation)
