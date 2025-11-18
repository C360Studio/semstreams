# SemStreams Implementation Documentation

## Work In Progress

**All repos in ecosystem are under heavy and active dev.**

This directory contains **implementation-specific** documentation for the SemStreams service.

## 📚 Documentation Organization

### Ecosystem Documentation → [semdocs](https://github.com/c360/semdocs)

**For architecture, concepts, guides, deployment**: See the centralized [semdocs repository](https://github.com/c360/semdocs).

Ecosystem docs migrated to semdocs:

- Architecture overview
- Federation guide
- PathRAG guide
- Embedding strategies
- Schema tags guide
- Deployment guides (production, operations, TLS, ACME)
- API integration (GraphQL, REST, NATS)

### Implementation Documentation (this directory)

**Package-level and implementation details**:

- **CI_SCHEMA_INTEGRATION.md** - CI/CD schema validation
- **CONTRACT_TESTING.md** - Contract testing implementation
- **ERROR_MESSAGE_IMPROVEMENTS.md** - Error handling patterns
- **FOLDER_STRUCTURE_ANALYSIS.md** - Codebase organization
- **SCHEMA_GENERATION.md** - Schema code generation
- **architecture/GRAPHRAG_LESSONS_LEARNED.md** - GraphRAG implementation learnings
- **components/GATEWAY.md** - Gateway implementation
- **design/WEBSOCKET_INPUT.md** - WebSocket design proposal

### Package Documentation

For package-level documentation, see `pkg/*/README.md` files:
- `pkg/cache/README.md` - Caching implementation
- `pkg/graphclustering/README.md` - Graph clustering algorithms
- `pkg/retry/README.md` - Retry logic
- `pkg/worker/README.md` - Worker pools
- And others...

### Archived Documentation

Historical and migrated docs are in `archive/`:
- `archive/ecosystem-docs/` - Docs migrated to semdocs
- `archive/historical/` - Outdated planning docs
- `archive/pre-merge/` - Pre-monorepo-merge docs

## Quick Links

- **Ecosystem Docs**: [semdocs](https://github.com/c360/semdocs)
- **Contributing**: [semdocs/development/contributing.md](https://github.com/c360/semdocs/docs/development/contributing.md)
- **Testing**: [semdocs/development/testing.md](https://github.com/c360/semdocs/docs/development/testing.md)
- **API Reference**: [semdocs/integration/](https://github.com/c360/semdocs/docs/integration/)

## Principle

**semdocs** = WHAT and WHY (ecosystem concepts)
**This directory** = HOW (implementation details)
