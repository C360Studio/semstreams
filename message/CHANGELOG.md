# Message Package Changelog

This file documents breaking changes and migrations for the `message` package.

## Phase 2: Structural Improvements (2025-01)

### Breaking Changes

#### 1. Constructor API Simplification

**Changed:** Replaced 5 constructor functions with single functional options-based constructor.

**Before:**
```go
// Multiple constructors
msg := message.NewBaseMessage(msgType, payload, "source")
msg := message.NewBaseMessageWithTime(msgType, payload, "source", timestamp)
msg := message.NewBaseMessageWithMeta(msgType, payload, meta)
msg := message.NewFederatedMessage(msgType, payload, "source", platform)
msg := message.NewFederatedMessageWithTime(msgType, payload, "source", platform, timestamp)
```

**After:**
```go
// Single constructor with functional options
msg := message.NewBaseMessage(msgType, payload, "source")
msg := message.NewBaseMessage(msgType, payload, "source", message.WithTime(timestamp))
msg := message.NewBaseMessage(msgType, payload, "source", message.WithMeta(meta))
msg := message.NewBaseMessage(msgType, payload, "source", message.WithFederation(platform))
msg := message.NewBaseMessage(msgType, payload, "source", message.WithFederationAndTime(platform, timestamp))
```

**Migration:**
- Replace `NewBaseMessageWithTime(...)` with `NewBaseMessage(..., WithTime(timestamp))`
- Replace `NewFederatedMessage(...)` with `NewBaseMessage(..., WithFederation(platform))`
- Replace `NewFederatedMessageWithTime(...)` with `NewBaseMessage(..., WithFederationAndTime(platform, timestamp))`
- Simple `NewBaseMessage(...)` calls require no changes

#### 2. StoredMessage Location

**Changed:** Moved `StoredMessage` from `message` package to `storage/objectstore` package.

**Before:**
```go
import "github.com/c360/semstreams/message"

stored := message.NewStoredMessage(...)
var msg *message.StoredMessage
```

**After:**
```go
import "github.com/c360/semstreams/storage/objectstore"

stored := objectstore.NewStoredMessage(...)
var msg *objectstore.StoredMessage
```

**Migration:**
- Update imports: `message` → `storage/objectstore` for StoredMessage usage
- Update type references: `*message.StoredMessage` → `*objectstore.StoredMessage`
- The `Storable` interface remains in the `message` package (it's a behavioral capability)

**Rationale:** StoredMessage is storage-specific implementation, not a core message concept. Moving it clarifies package boundaries and keeps the message package focused on message semantics.

---

## Phase 1: Interface Clarification (2025-01)

### Breaking Changes

#### 1. GenericPayload Removal

**Changed:** Removed `GenericPayload` in favor of explicit `GenericJSONPayload`.

**Before:**
```go
payload := message.NewGenericPayload(msgType, data)
msg := message.NewBaseMessage(msgType, payload, "source")
```

**After:**
```go
payload := message.NewGenericJSON(data)
msg := message.NewBaseMessage(payload.Schema(), payload, "source")
```

**Migration:**
- Replace `NewGenericPayload(msgType, data)` with `NewGenericJSON(data)`
- Message type now comes from `payload.Schema()` instead of separate parameter
- Payload type string changed from custom types to well-known `"core.json.v1"`

**Rationale:** The well-known type `core.json.v1` makes it explicit that this payload is for generic JSON processing, and having the payload know its own schema is more idiomatic.

#### 2. PayloadType() Method Removed

**Changed:** Removed `PayloadType()` convenience method from payload interface and implementations.

**Before:**
```go
typeStr := payload.PayloadType()  // Returns "domain.category.version"
```

**After:**
```go
typeStr := payload.Schema().String()  // Returns "domain.category.version"
```

**Migration:**
- Replace all `payload.PayloadType()` calls with `payload.Schema().String()`
- The `Schema()` method is part of the required `Payload` interface
- `String()` method on `Type` returns the same dotted notation format

**Rationale:** Consolidates type access through the `Schema()` method, reducing API surface and making the pattern more consistent across the codebase.

---

## Validation Philosophy (Documented in Phase 3)

The message package now documents a clear validation philosophy:

### Structural Validation (Required)
- Check required fields are not empty/nil
- Check basic type constraints
- Use `errors.WrapInvalid()` for consistency

### Semantic Validation (Optional)
- Check business rules and value ranges
- Domain-specific constraints
- May include complex logic

### Example Pattern
```go
func (p *MyPayload) Validate() error {
    // Structural: required fields
    if p.ID == "" {
        return errors.WrapInvalid(errors.ErrInvalidData, "MyPayload", "Validate", "ID is required")
    }

    // Semantic: value ranges
    if p.Value < 0 || p.Value > 100 {
        return errors.WrapInvalid(errors.ErrInvalidData, "MyPayload", "Validate",
            fmt.Sprintf("value must be 0-100, got: %d", p.Value))
    }

    return nil
}
```

---

## Upgrade Guide

### Quick Migration Checklist

For codebases using the message package, follow these steps:

1. **Phase 1 - Interface Changes:**
   - [ ] Replace `NewGenericPayload()` with `NewGenericJSON()`
   - [ ] Update message type access from `PayloadType()` to `Schema().String()`
   - [ ] Update test assertions from custom types to `"core.json.v1"`

2. **Phase 2 - Structural Changes:**
   - [ ] Update imports for `StoredMessage` from `message` to `storage/objectstore`
   - [ ] Update type references to `*objectstore.StoredMessage`
   - [ ] Replace specialized constructors with functional options:
     - `NewBaseMessageWithTime()` → `NewBaseMessage(..., WithTime(...))`
     - `NewFederatedMessage()` → `NewBaseMessage(..., WithFederation(...))`

3. **Phase 3 - Validation Updates:**
   - [ ] Update validation methods to use `errors.WrapInvalid()`
   - [ ] Add comments distinguishing structural vs semantic validation
   - [ ] Follow documented validation pattern in package docs

### Testing Your Migration

Run these tests to verify successful migration:

```bash
# Unit tests
go test ./...

# Integration tests (if applicable)
INTEGRATION_TESTS=1 go test ./...

# Race detector
go test -race ./...
```

### Getting Help

- **Package Documentation:** `go doc message` for comprehensive usage guide
- **Type System:** See "Type System Hierarchy" section in package docs
- **Behavioral Interfaces:** See "Behavioral Interfaces" section for runtime discovery patterns
- **Best Practices:** See "Best Practices" section for payload implementation guidelines

---

## Version History

- **v0.3.0** (2025-01) - Phase 3: Documentation and validation philosophy
- **v0.2.0** (2025-01) - Phase 2: Structural improvements and constructor simplification
- **v0.1.0** (2025-01) - Phase 1: Interface clarification and GenericPayload removal
