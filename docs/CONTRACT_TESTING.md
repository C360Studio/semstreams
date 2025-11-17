# Contract Testing Strategy

## Overview

Contract testing ensures that schemas, OpenAPI specifications, and TypeScript types remain synchronized across the SemStreams ecosystem without requiring the backend to be running. These tests validate that committed artifacts match the source code and that contracts are compatible across repositories.

## Test Locations

### semstreams
- **Location**: `test/contract/`
- **Framework**: Go testing
- **Coverage**:
  - Schema contract validation (`schema_contract_test.go`)
  - OpenAPI spec validation (`openapi_contract_test.go`)

### semmem
- **Location**: `test/contract/`
- **Framework**: Go testing
- **Coverage**:
  - Cross-repo schema validation (`cross_repo_test.go`)

### semstreams-ui
- **Location**: `src/lib/contract/`
- **Framework**: Vitest
- **Coverage**:
  - OpenAPI spec contract tests (`openapi.contract.test.ts`)
  - TypeScript type validation (`types.contract.test.ts`)

## Test Categories

### 1. Schema Contract Tests (semstreams)

**File**: `semstreams/test/contract/schema_contract_test.go`

**Purpose**: Validate that committed schemas match the component registration code.

**Tests**:
- `TestCommittedSchemasMatchCode`: Ensures committed schemas are identical to generated schemas
- `TestCommittedSchemasValidStructure`: Validates schema structure matches meta-schema requirements
- `TestNoOrphanedSchemaFiles`: Ensures no schema files exist without corresponding components

**How to Run**:
```bash
cd semstreams
go test ./test/contract -v
```

**Why Important**:
- Prevents schema drift between code and committed artifacts
- Catches when developers forget to run `task schema:generate` after changing components
- Ensures all schemas have valid structure

### 2. OpenAPI Spec Tests (semstreams)

**File**: `semstreams/test/contract/openapi_contract_test.go`

**Purpose**: Validate the OpenAPI specification structure and completeness.

**Tests**:
- `TestCommittedOpenAPISpecValid`: Validates OpenAPI 3.0.3 structure
- `TestOpenAPISpecContainsAllComponents`: Ensures all registered components are in the spec
- `TestOpenAPISpecPaths`: Validates required API paths exist
- `TestOpenAPISchemaReferences`: Validates all schema references point to existing files

**How to Run**:
```bash
cd semstreams
go test ./test/contract -v -run TestOpenAPI
```

**Why Important**:
- Ensures OpenAPI spec stays in sync with component registry
- Validates API contract is complete and correct
- Catches broken schema references

### 3. Cross-Repo Validation Tests (semmem)

**File**: `semmem/test/contract/cross_repo_test.go`

**Purpose**: Validate compatibility between semmem and semstreams schemas.

**Tests**:
- `TestSchemasValidateAgainstSemStreamsMeta`: Validates semmem schemas against semstreams meta-schema
- `TestMetaSchemasAreCompatible`: Ensures both repos use compatible meta-schemas

**How to Run**:
```bash
cd semmem
go test ./test/contract -v
```

**Why Important**:
- Ensures schemas are interchangeable between repos
- Validates both projects follow the same schema standards
- Catches incompatibilities early

**Note**: These tests require both semstreams and semmem to be in sibling directories.

### 4. UI Contract Tests (semstreams-ui)

**File**: `semstreams-ui/src/lib/contract/openapi.contract.test.ts`

**Purpose**: Validate that the UI can load and use the committed OpenAPI spec without the backend running.

**Tests**:
- OpenAPI spec loading and parsing
- OpenAPI 3.0 structure validation
- Path definitions validation
- ComponentType schema validation
- Schema reference validation

**How to Run**:
```bash
cd semstreams-ui
npm test -- src/lib/contract/openapi.contract.test.ts
```

**Why Important**:
- Frontend can validate contracts without backend
- Catches OpenAPI spec issues early in development
- Validates schema files exist and are valid JSON

### 5. TypeScript Type Validation (semstreams-ui)

**File**: `semstreams-ui/src/lib/contract/types.contract.test.ts`

**Purpose**: Validate generated TypeScript types exist and are valid.

**Tests**:
- Generated types file exists
- Valid TypeScript syntax
- Contains expected type definitions
- Has proper exports

**How to Run**:
```bash
cd semstreams-ui
npm test -- src/lib/contract/types.contract.test.ts
```

**Why Important**:
- Ensures type generation succeeded
- Validates types are usable by the UI
- Catches type generation failures

## CI Integration

Contract tests run automatically in CI for all repositories:

### semstreams CI
```yaml
schema-validation:
  - Run: task schema:generate
  - Run: go test ./cmd/schema-exporter
  - Check: git diff --exit-code schemas/ specs/openapi.v3.yaml
  - Run: go test ./test/contract
```

### semmem CI
```yaml
schema-validation:
  - Run: task schema:generate
  - Run: go test ./cmd/schema-exporter
  - Check: git diff --exit-code schemas/ specs/openapi.v3.yaml
  - Run: go test ./test/contract
```

### semstreams-ui CI
```yaml
type-generation:
  - Run: task generate-types
  - Check: git diff --exit-code src/lib/types/api.generated.ts
  - Run: npx tsc --noEmit --skipLibCheck src/lib/types/api.generated.ts
  - Run: npm test -- src/lib/contract
```

See `docs/CI_SCHEMA_INTEGRATION.md` for detailed CI configuration.

## Development Workflow

### Making Schema Changes

1. **Modify component code** (in semstreams or semmem):
   ```go
   type Config struct {
       NewField string `schema:"type:string,desc:New field"`
   }
   ```

2. **Regenerate schemas**:
   ```bash
   task schema:generate
   ```

3. **Run contract tests**:
   ```bash
   go test ./test/contract -v
   ```

4. **Commit both code and schemas**:
   ```bash
   git add .
   git commit -m "feat(component): add new field"
   ```

### Updating Frontend Types

After OpenAPI spec changes:

1. **Regenerate types**:
   ```bash
   cd semstreams-ui
   task generate-types
   ```

2. **Run contract tests**:
   ```bash
   npm test -- src/lib/contract
   ```

3. **Commit generated types**:
   ```bash
   git add src/lib/types/api.generated.ts
   git commit -m "chore: regenerate types from OpenAPI spec"
   ```

## Common Error Scenarios

### Schema Drift Detected

**Error**:
```
TestCommittedSchemasMatchCode failed:
  Schema mismatch for udp (-committed +generated)
```

**Fix**:
```bash
cd semstreams
task schema:generate
git add schemas/
git commit -m "chore: regenerate schemas"
```

### OpenAPI Spec Out of Sync

**Error**:
```
❌ Schema or OpenAPI files have uncommitted changes!
```

**Fix**:
```bash
task schema:generate
git add specs/openapi.v3.yaml
git commit -m "chore: update OpenAPI spec"
```

### TypeScript Types Out of Date

**Error**:
```
❌ Generated TypeScript types have uncommitted changes!
```

**Fix**:
```bash
cd semstreams-ui
task generate-types
git add src/lib/types/api.generated.ts
git commit -m "chore: regenerate TypeScript types"
```

### Cross-Repo Incompatibility

**Error**:
```
TestSchemasValidateAgainstSemStreamsMeta failed:
  Schema validation failed for decision
```

**Fix**:
Ensure both repos have compatible meta-schemas and regenerate schemas in both:
```bash
cd semstreams && task schema:generate
cd ../semmem && task schema:generate
```

## Benefits of Contract Testing

### 1. **No Backend Required**
- Frontend tests can validate contracts without running backend
- Faster feedback loop during development
- Tests run independently

### 2. **Early Detection**
- Schema drift detected immediately
- Type mismatches caught before runtime
- Cross-repo incompatibilities found early

### 3. **Automated Enforcement**
- CI fails if contracts are out of sync
- No manual schema validation needed
- Pull requests validated automatically

### 4. **Cross-Repository Safety**
- semmem and semstreams schemas stay compatible
- Frontend types stay synchronized with backend
- Full contract chain validated

### 5. **Documentation as Tests**
- Tests serve as executable documentation
- Contract structure validated automatically
- API changes visible in test diffs

## Best Practices

### 1. **Always Run Locally First**
Before pushing, run contract tests:
```bash
# Backend
cd semstreams && go test ./test/contract

# Frontend
cd semstreams-ui && npm test -- src/lib/contract
```

### 2. **Commit Schemas with Code**
Never commit code changes without regenerating schemas:
```bash
task schema:generate
git add .
git commit -m "feat: add new field (with regenerated schemas)"
```

### 3. **Check CI Before Merging**
Always wait for CI to validate contracts before merging PRs.

### 4. **Use Task Commands**
Always use `task schema:generate` and `task generate-types` instead of running tools directly.

### 5. **Document Breaking Changes**
When schemas change in breaking ways, document the migration path.

## Troubleshooting

### Tests Pass Locally But Fail in CI

**Cause**: Uncommitted schema/type files

**Fix**:
```bash
git status
git add schemas/ specs/ src/lib/types/
git commit --amend
```

### Cross-Repo Tests Skip

**Cause**: Repos not in sibling directories

**Fix**: Ensure directory structure:
```
/code/semstreams/
  semstreams/
  semmem/
  semstreams-ui/
```

### Path Resolution Failures

**Cause**: Test running from unexpected directory

**Fix**: Tests use `findRepoRoot()` helper that walks up from cwd. Ensure running from within repo.

## Related Documentation

- [CI Schema Integration](./CI_SCHEMA_INTEGRATION.md)
- [Schema Generation System](./SCHEMA_GENERATION.md)
- [Component Meta-Schema](../specs/component-schema-meta.json)
- [OpenAPI Specification](../specs/openapi.v3.yaml)

## Summary

Contract testing ensures:
- ✅ Committed schemas match source code
- ✅ OpenAPI specs are complete and valid
- ✅ TypeScript types are synchronized
- ✅ Cross-repo compatibility maintained
- ✅ All contracts validated in CI
- ✅ Fast feedback without running backend

This approach provides confidence that all contract layers (component schemas → OpenAPI spec → TypeScript types) stay synchronized throughout the development lifecycle.
