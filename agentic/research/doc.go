// Package research defines the payload types for the ADR-045 graph
// search rule chain: Intent, SearchResult, and RouteDecision.
//
// These are the wire shapes the chain's coordinated components and the
// continuation rule pass through the payload registry. Phase 1 PR 1 lands
// the registry surface so PRs 2-6 can land components and the rule chain
// without churn on the schema contract.
//
// See docs/adr/045-graph-search-rule-chain.md for the architecture and
// docs/operations/22-adr045-phase1-plan.md for the PR sequence.
package research
