# Agentic Governance Component

Infrastructure-level content policy enforcement for agentic systems.

## Overview

The governance component intercepts agentic messages and applies a configurable filter chain for:

- **PII Redaction** - Detect and redact emails, SSNs, credit cards, API keys
- **Injection Detection** - Block prompt injection and jailbreak attempts
- **Content Moderation** - Enforce content policies (harmful, illegal)
- **Rate Limiting** - Token bucket throttling per user/session/global

## Architecture

```
User Input → Dispatch → [Governance] → Loop → Model → [Governance] → Response
                              ↓                            ↓
                        governance.violation.*       (validate output)
```

The component implements the "outer loop" pattern from ADR-016, where governance
is enforced at the infrastructure layer rather than delegated to agents.

## NATS Subjects

**Inputs** (subscribe):
- `agent.task.*` - User task requests
- `agent.request.*` - Outgoing model requests
- `agent.response.*` - Incoming model responses

**Outputs** (publish):
- `agent.task.validated.*` - Approved tasks
- `agent.request.validated.*` - Approved requests
- `agent.response.validated.*` - Approved responses
- `governance.violation.*` - Policy violations

## Configuration

```json
{
  "type": "processor",
  "name": "agentic-governance",
  "config": {
    "filter_chain": {
      "policy": "fail_fast",
      "filters": [
        {
          "name": "pii_redaction",
          "enabled": true,
          "pii_config": {
            "types": ["email", "phone", "ssn", "credit_card", "api_key"],
            "strategy": "label",
            "confidence_threshold": 0.85
          }
        },
        {
          "name": "injection_detection",
          "enabled": true,
          "injection_config": {
            "confidence_threshold": 0.8,
            "enabled_patterns": ["instruction_override", "jailbreak_persona", "system_injection"]
          }
        },
        {
          "name": "content_moderation",
          "enabled": true,
          "content_config": {
            "block_threshold": 0.9,
            "enabled_default": ["harmful", "illegal"]
          }
        },
        {
          "name": "rate_limiting",
          "enabled": true,
          "rate_limit_config": {
            "per_user": {"requests_per_minute": 60, "tokens_per_hour": 100000},
            "algorithm": "token_bucket"
          }
        }
      ]
    },
    "violations": {
      "store": "GOVERNANCE_VIOLATIONS",
      "retention_days": 90,
      "notify_admin_severity": ["critical", "high"]
    }
  }
}
```

## Filter Chain Policies

| Policy | Behavior |
|--------|----------|
| `fail_fast` | Stop at first violation (default) |
| `continue` | Run all filters, collect all violations |
| `log_only` | Log violations but allow all content through |

## PII Types

| Type | Pattern | Validation |
|------|---------|------------|
| `email` | RFC 5322 email addresses | Regex |
| `phone` | US phone numbers | Regex |
| `ssn` | Social Security Numbers | SSN rules validation |
| `credit_card` | Credit card numbers | Luhn algorithm |
| `api_key` | High-entropy strings | Entropy check |
| `ip_address` | IPv4 addresses | Octet validation |

## Injection Patterns

| Pattern | Description | Severity |
|---------|-------------|----------|
| `instruction_override` | "ignore previous instructions" | High |
| `jailbreak_persona` | "you are now DAN" | High |
| `system_injection` | "System:" prefix attacks | Critical |
| `encoded_injection` | base64/hex encoded attacks | Medium |
| `delimiter_injection` | "---END INSTRUCTIONS---" | High |
| `role_confusion` | "your new role is..." | Medium |

## Metrics

```promql
# Filter invocation rate
rate(semstreams_governance_filter_total[5m])

# Violation rate by severity
rate(semstreams_governance_violation_total{severity="high"}[5m])

# PII detection breakdown
sum by (pii_type) (rate(semstreams_governance_pii_detected_total[1h]))

# Success rate
sum(rate(semstreams_governance_messages_processed_total{result="allowed"}[5m]))
  / sum(rate(semstreams_governance_messages_processed_total[5m]))
```

## Deployment

### Phase 1: Observation Mode

Deploy with `log_only` policy to observe without blocking:

```json
{
  "filter_chain": {
    "policy": "log_only"
  }
}
```

### Phase 2: Enable Blocking

Switch to `fail_fast` after tuning thresholds:

```json
{
  "filter_chain": {
    "policy": "fail_fast"
  }
}
```

## References

- [ADR-016: Agentic Governance Layer](../../docs/architecture/adr-016-agentic-governance-layer.md)
- [Governance Specification](../../docs/architecture/specs/agentic-governance-spec.md)
