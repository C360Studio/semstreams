## ADDED Requirements

### Requirement: Reply decide actions SHALL be a framework contract with one classifier

The framework SHALL reserve the decide actions `respond_direct` and `ask_user` as user-facing reply actions and SHALL
classify them through exactly one exported function in `agentic`. The decide tool's description SHALL remain
vocabulary-agnostic and SHALL NOT enumerate the reserved names. The deployment-level `restricted_decide_actions` policy
SHALL continue to bar either reserved name when configured. The decide tool name SHALL have one exported constant home
in `agentic`, consumed by agentic-tools and agentic-loop.

#### Scenario: reserved names classify as user-facing

- **WHEN** the classifier is asked about `respond_direct` and `ask_user`
- **THEN** it reports user-facing for both
- **AND** it reports not user-facing for `autoresearch`, `research`, `needs_clarification`, and the empty string

#### Scenario: description stays agnostic

- **WHEN** the decide tool lists itself
- **THEN** its description names no action value

#### Scenario: restriction still bars a reserved name

- **GIVEN** `restricted_decide_actions` contains `ask_user`
- **WHEN** a coordinator calls decide with `ask_user`
- **THEN** the call is rejected with invalid arguments as before

#### Scenario: one tool-name home

- **WHEN** the repository is searched for the decide tool-name literal in non-test Go sources
- **THEN** exactly one definition exists in `agentic`
