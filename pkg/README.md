# Util

Utility packages providing common functionality across the SemStreams platform.

## Overview

The util package contains a collection of utility subpackages that provide common, reusable functionality used throughout the SemStreams codebase. Each subpackage focuses on a specific utility domain, maintaining clear separation of concerns.

## Subpackages

### timestamp

High-precision timestamp utilities for temporal operations and synchronization.

```go
import "github.com/c360/semstreams/pkg/util/timestamp"
```

Key features:

- Microsecond precision timestamps
- RFC3339 formatting with microseconds
- Time comparison utilities
- Clock synchronization helpers
- Temporal indexing support

See [timestamp README](./timestamp/README.md) for detailed documentation.

## Architecture

The util package follows a subpackage organization pattern where:

- **Single Responsibility**: Each subpackage handles one specific utility domain
- **No Cross-Dependencies**: Utility subpackages don't depend on each other
- **Zero External Dependencies**: Utilities rely only on Go standard library where possible
- **Performance First**: Utilities are optimized for high-frequency use

## Usage Pattern

Utility subpackages are imported directly by packages that need specific functionality:

```go
import (
    "github.com/c360/semstreams/pkg/util/timestamp"
)

// Use timestamp utilities
now := timestamp.Now()
formatted := timestamp.Format(now)
```

## Design Decisions

**Subpackage Organization**: We organize utilities into subpackages rather than a flat structure because:

- Prevents util from becoming a "junk drawer" of unrelated functions
- Makes dependencies explicit and trackable
- Allows utilities to evolve independently
- Improves code organization and discoverability

**Minimal Dependencies**: Utilities minimize external dependencies to:

- Reduce coupling across the codebase
- Improve testability
- Ensure predictable performance
- Simplify deployment and compilation

## Guidelines for New Utilities

When adding new utility subpackages:

1. **Single Purpose**: Each subpackage should have one clear purpose
2. **No Business Logic**: Utilities should be domain-agnostic
3. **Well Tested**: Minimum 90% test coverage with edge cases
4. **Documented**: Complete API documentation with examples
5. **Performance**: Benchmark critical paths

## Related Packages

- [`pkg/message`](../message): Uses timestamp utilities for message timing
- [`pkg/processor/graph`](../processor/graph): Uses timestamp for temporal indexing
- [`pkg/metric`](../metric): Uses timestamp for metric sampling

## License

MIT
