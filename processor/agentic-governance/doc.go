// Package agenticgovernance provides a governance layer processor component
// that enforces content policies for agentic systems. This component implements
// infrastructure-level policy enforcement following the "Two Agentic Loops"
// pattern, where governance is enforced at the outer infrastructure layer
// rather than delegated to agents themselves.
//
// # Architecture
//
// The governance component intercepts agentic messages and applies a configurable
// filter chain before forwarding validated messages to downstream components:
//
//	User Input → Dispatch → [Governance] → Loop → Model → [Governance] → Response
//
// # Filters
//
// The filter chain includes:
//
//   - PII Redaction: Detects and redacts personally identifiable information
//     (emails, phone numbers, SSNs, credit cards, API keys)
//   - Injection Detection: Blocks prompt injection and jailbreak attempts
//   - Content Moderation: Enforces content policies (harmful, illegal content)
//   - Rate Limiting: Token bucket throttling per user/session/global
//
// # NATS Subjects
//
// Input subjects (intercept):
//   - agent.task.* - User task requests
//   - agent.request.* - Outgoing model requests
//   - agent.response.* - Incoming model responses
//
// Output subjects (publish):
//   - agent.task.validated.* - Approved tasks
//   - agent.request.validated.* - Approved requests
//   - agent.response.validated.* - Approved responses
//   - governance.violation.* - Policy violations
//
// # Configuration
//
// Example configuration:
//
//	{
//	  "filter_chain": {
//	    "policy": "fail_fast",
//	    "filters": [
//	      {
//	        "name": "pii_redaction",
//	        "enabled": true,
//	        "pii_config": {
//	          "types": ["email", "phone", "ssn"],
//	          "strategy": "label"
//	        }
//	      },
//	      {
//	        "name": "injection_detection",
//	        "enabled": true
//	      }
//	    ]
//	  },
//	  "violations": {
//	    "store": "GOVERNANCE_VIOLATIONS"
//	  }
//	}
//
// # Violation Policies
//
//   - fail_fast: Stop at first violation (default)
//   - continue: Run all filters, collect all violations
//   - log_only: Log violations but allow all content through
//
// # Usage
//
//	import agenticgovernance "github.com/c360studio/semstreams/processor/agentic-governance"
//
//	// Register with component registry
//	err := agenticgovernance.Register(registry)
//
// # References
//
//   - ADR-016: Agentic Governance Layer
//   - docs/architecture/specs/agentic-governance-spec.md
package agenticgovernance
