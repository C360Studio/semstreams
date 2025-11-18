# Vocabulary Examples

**These are reference implementations only** - NOT part of the core SemStreams framework.

## Purpose

These examples demonstrate how applications should:

1. Define domain-specific vocabulary in their own packages
2. Register predicates with the vocabulary registry
3. Configure alias semantics for entity resolution

## Usage in Applications

### Step 1: Create Your Vocabulary Package

```go
// In your application: myapp/vocabulary/mydom

ain.go
package vocabulary

import "github.com/c360/semstreams/vocabulary"

const (
    MyDomainIdentifierDeviceID = "mydomain.identifier.deviceid"
    MyDomainLabelFriendlyName  = "mydomain.label.friendlyname"
)

func init() {
    vocabulary.RegisterPredicate(vocabulary.PredicateMetadata{
        Name:          MyDomainIdentifierDeviceID,
        Description:   "Device ID from our system",
        DataType:      "string",
        Domain:        "mydomain",
        Category:      "identifier",
        IsAlias:       true,
        AliasType:     vocabulary.AliasTypeExternal,
        AliasPriority: 0,
    })

    vocabulary.RegisterPredicate(vocabulary.PredicateMetadata{
        Name:          MyDomainLabelFriendlyName,
        Description:   "User-friendly display name",
        DataType:      "string",
        Domain:        "mydomain",
        Category:      "label",
        IsAlias:       true,
        AliasType:     vocabulary.AliasTypeLabel,  // NOT resolvable!
        AliasPriority: 999,
    })
}
```

### Step 2: Import in Your Main Package

```go
package main

import (
    _ "myapp/vocabulary"  // Registers predicates via init()
    "github.com/c360/semstreams/processor/graph/indexmanager"
)

func main() {
    // AliasIndex will automatically discover registered predicates
    // No hardcoded predicates in framework!
}
```

## Alias Types

- **AliasTypeIdentity**: Entity equivalence (owl:sameAs) - **resolvable**
- **AliasTypeLabel**: Display names (skos:prefLabel) - **NOT resolvable**
- **AliasTypeAlternate**: Secondary identifiers - **resolvable**
- **AliasTypeExternal**: External system IDs - **resolvable**
- **AliasTypeCommunication**: Network/radio IDs - **resolvable**

## Priority

Lower number = higher priority for conflict resolution.

- Priority 0: Most authoritative (e.g., primary IDs)
- Priority 1-10: Standard aliases
- Priority 999: Display-only labels

## Best Practices

1. **Domain Scoping**: Use your domain prefix (e.g., `robotics.*`, `iot.*`)
2. **Three Levels**: Always use `domain.category.property` format
3. **Type Semantics**: Choose AliasType carefully - labels are NOT resolvable
4. **Priority Strategy**: Primary IDs get priority 0, decreasing importance
5. **Registration**: Use `init()` functions for automatic registration
