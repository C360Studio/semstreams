# GitHub #865/#866 Terminal Settlement Metrics Addendum

The accepted design in `gh865-866-terminal-event-design.md` remains immutable at
its recorded body SHA-256. This adjacent clarification resolves how its
implementation-order statement applies to terminal metrics.

The statement that metrics follow successful required work applies to semantic
business metrics such as completion-received counters and the active-loop
gauge. Those metrics are updated only after the corresponding projection or
publication work succeeds.

The diagnostic terminal-settlement counter has different semantics: it records
exactly one final bounded disposition for every delivery attempt. A normalized
terminal is therefore represented by `response_settled` or
`route_less_settled` only after required work succeeds. There is no additional
`accepted` increment. Failed attempts record their single final validation,
routing, projection, or publication disposition.
