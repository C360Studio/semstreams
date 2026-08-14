# Contract Testing Strategy

## Overview

Contract testing ensures that schemas, OpenAPI specifications, and TypeScript types remain synchronized across the SemStreams ecosystem without requiring the backend to be running. These tests validate that committed artifacts match the source code and that contracts are compatible across repositories.

## Repository ownership

SemStreams contributors and agents change only this repository. Sister repositories may be inspected read-only for
compatibility inventories, but all branches, edits, commits, pushes, pull requests, issues, comments, labels, tags,
releases, and other mutations belong to their downstream owners.

For a breaking contract change, add exact downstream impact, required edits, and validation commands to a
SemStreams-owned migration document. Downstream owners apply those instructions and report their own evidence; a
SemStreams agent does not perform the downstream migration.

## Test Locations

### semstreams
- **Location**: `test/contract/`
- **Framework**: Go testing
- **Coverage**:
  - Schema contract validation (`schema_contract_test.go`)
  - OpenAPI spec validation (`openapi_contract_test.go`)

### semmem
- **Owner**: semmem downstream maintainers; SemStreams agents inspect this surface read-only
- **Location**: `test/contract/`
- **Framework**: Go testing
- **Coverage**:
  - Cross-repo schema validation (`cross_repo_test.go`)

### semstreams-ui
- **Owner**: semstreams-ui downstream maintainers; SemStreams agents inspect this surface read-only
- **Location**: `src/lib/contract/`
- **Framework**: Vitest
- **Coverage**:
  - OpenAPI spec contract tests (`openapi.contract.test.ts`)
  - TypeScript type validation (`types.contract.test.ts`)

## Test Categories

### 1. Message Contract Tests (semstreams)

**File**: `semstreams/test/contract/message_contract_test.go`

**Purpose**: Validate that all registered message payloads serialize correctly and maintain consistency between their `Schema()` methods and registry entries.

**Tests**:
- `TestSchemaRegistrationConsistency`: Verifies payload `Schema()` methods match their registry registration
- `TestBaseMessageRoundTrip`: Validates messages can marshal/unmarshal without data loss
- `TestPayloadValidation`: Ensures `Validate()` doesn't panic on registered payloads
- `TestPayloadMarshalJSON`: Validates `MarshalJSON()` produces valid output

**How to Run**:
```bash
cd semstreams
go test ./test/contract -v -run Message
```

**Why Important**:
- Catches Schema/Registration mismatches that cause deserialization failures
- Validates the contract enforcement in `BaseMessage.MarshalJSON` (invalid payloads fail at serialize time)
- Ensures all registered payloads can round-trip through JSON without data loss
- Prevents silent message drops due to format mismatches

### 2. Component Schema Contract Tests (semstreams)

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

### 3. OpenAPI Spec Tests (semstreams)

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

### 4. Cross-Repo Validation Tests (semmem)

**File**: `semmem/test/contract/cross_repo_test.go`

**Purpose**: Validate compatibility between semmem and semstreams schemas.

**Tests**:
- `TestSchemasValidateAgainstSemStreamsMeta`: Validates semmem schemas against semstreams meta-schema
- `TestMetaSchemasAreCompatible`: Ensures both repos use compatible meta-schemas

**Downstream-owner command** (record in the SemStreams migration guide; SemStreams agents do not execute it):
```bash
cd semmem
go test ./test/contract -v
```

**Why Important**:
- Ensures schemas are interchangeable between repos
- Validates both projects follow the same schema standards
- Catches incompatibilities early

**Note**: These tests require both semstreams and semmem to be in sibling directories.

### 5. UI Contract Tests (semstreams-ui)

**File**: `semstreams-ui/src/lib/contract/openapi.contract.test.ts`

**Purpose**: Validate that the UI can load and use the committed OpenAPI spec without the backend running.

**Tests**:
- OpenAPI spec loading and parsing
- OpenAPI 3.0 structure validation
- Path definitions validation
- ComponentType schema validation
- Schema reference validation

**Downstream-owner command** (record in the SemStreams migration guide; SemStreams agents do not execute it):
```bash
cd semstreams-ui
npm test -- src/lib/contract/openapi.contract.test.ts
```

**Why Important**:
- Frontend can validate contracts without backend
- Catches OpenAPI spec issues early in development
- Validates schema files exist and are valid JSON

### 6. TypeScript Type Validation (semstreams-ui)

**File**: `semstreams-ui/src/lib/contract/types.contract.test.ts`

**Purpose**: Validate generated TypeScript types exist and are valid.

**Tests**:
- Generated types file exists
- Valid TypeScript syntax
- Contains expected type definitions
- Has proper exports

**Downstream-owner command** (record in the SemStreams migration guide; SemStreams agents do not execute it):
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
  - Run: go test ./cmd/component-schema-exporter
  - Check: git diff --exit-code schemas/ specs/openapi.v3.yaml
  - Run: go test ./test/contract
```

### semmem CI (downstream-owned)

This is a read-only inventory of the downstream gate. SemStreams agents record the expected result in the migration
guide; semmem owners run and maintain this workflow.

```yaml
schema-validation:
  - Run: task schema:generate
  - Run: go test ./cmd/component-schema-exporter
  - Check: git diff --exit-code schemas/ specs/openapi.v3.yaml
  - Run: go test ./test/contract
```

### semstreams-ui CI (downstream-owned)

This is a read-only inventory of the downstream gate. SemStreams agents record the expected result in the migration
guide; semstreams-ui owners run and maintain this workflow.

```yaml
type-generation:
  - Run: task generate-types
  - Check: git diff --exit-code src/lib/types/api.generated.ts
  - Run: npx tsc --noEmit --skipLibCheck src/lib/types/api.generated.ts
  - Run: npm test -- src/lib/contract
```

See [CI Integration](./05-ci-integration.md) for detailed CI configuration.

## Development Workflow

### Making Schema Changes

These steps apply only to SemStreams-owned changes in this repository:

1. **Modify SemStreams component code**:
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

4. **Commit the SemStreams code and schemas**:
   ```bash
   git add .
   git commit -m "feat(component): add new field"
   ```

### Updating Frontend Types

After an OpenAPI change, the SemStreams contributor records the affected downstream repository, required generated
artifacts, exact validation commands, and expected results in the applicable SemStreams migration guide. The
semstreams-ui owner then performs these steps in that repository:

1. **Downstream owner regenerates types**:
   ```bash
   cd semstreams-ui
   task generate-types
   ```

2. **Downstream owner runs contract tests**:
   ```bash
   npm test -- src/lib/contract
   ```

3. **Downstream owner commits generated types**:
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

**Downstream-owner fix**:

Record these commands and the expected clean generated diff in the SemStreams migration guide. The semstreams-ui
owner executes and publishes the change; a SemStreams agent does not enter or modify that repository.

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
Regenerate the SemStreams artifacts here, then document the downstream command and expected result in the applicable
SemStreams migration guide. Do not enter or modify the downstream repository:
```bash
task schema:generate
```

The downstream owner independently regenerates and validates its artifacts in its own repository.

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
Before pushing SemStreams changes, run the SemStreams contract tests:
```bash
go test ./test/contract
```

Record any downstream contract gates in the SemStreams migration guide for the relevant repository owners to run.

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
Use `task schema:generate` for SemStreams artifacts. When downstream type generation is required, record the
downstream-owned `task generate-types` command in the SemStreams migration guide.

### 5. **Document Breaking Changes**
When schemas change in breaking ways, document the migration path.

## Troubleshooting

### Tests Pass Locally But Fail in CI

**Cause**: Uncommitted schema/type files

**Fix**:
```bash
git status
git add schemas/ specs/
git commit --amend
```

### Cross-Repo Tests Skip

**Cause**: Repos not in sibling directories

**Inventory note**: Some downstream-owned tests expect the following checkout layout. SemStreams agents may inspect
this layout read-only, but do not create, move, or modify sister repositories to satisfy the test:
```
/code/semstreams/
  semstreams/
  semmem/
  semstreams-ui/
```

Record the skipped gate and expected downstream-owner validation in the applicable SemStreams migration guide.

### Path Resolution Failures

**Cause**: Test running from unexpected directory

**Fix**: Tests use `findRepoRoot()` helper that walks up from cwd. Ensure running from within repo.

## Related Documentation

- [CI Integration](./05-ci-integration.md)
- [Schema Generation](./03-schema-generation.md)
- [Component Meta-Schema](../specs/component-schema-meta.json)
- [OpenAPI Specification](../specs/openapi.v3.yaml)

## Summary

Contract testing ensures:
- ✅ Message payloads serialize/deserialize correctly
- ✅ Payload Schema() methods match registry entries
- ✅ Committed component schemas match source code
- ✅ OpenAPI specs are complete and valid
- ✅ TypeScript types are synchronized
- ✅ Cross-repo compatibility maintained
- ✅ All contracts validated in CI
- ✅ Fast feedback without running backend

This approach provides confidence that all contract layers (component schemas → OpenAPI spec → TypeScript types) stay synchronized throughout the development lifecycle.
