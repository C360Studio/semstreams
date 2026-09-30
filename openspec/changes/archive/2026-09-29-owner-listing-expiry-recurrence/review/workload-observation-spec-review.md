# Actual-workload observation spec review

Independent reviewer: **APPROVE** narrow spec/scope review for delta SHA-256
`c6665b257a69f48d56719013a7a5b1cd28cb05140600a9d9bb45b48f8f1b11a6`.

The added requirement matches the approved design: five-attempt evidence, fatal-exit retention, honest snapshot
classification, unchanged listing behavior and bounded callback ownership. It extends the existing harness-evidence
requirement without changing production contracts.

Proposal, tasks and scope reconciliation accurately leave cause and repair on #1421, distinguish required verification
from the single experiment, and reject transferring the previous waiver. No findings. Implementation review remains
pending; the landing PR must reference rather than close #1421.
