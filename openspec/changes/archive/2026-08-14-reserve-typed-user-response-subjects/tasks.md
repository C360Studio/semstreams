# Tasks: Reserve typed user-response subjects

## 1. SemStreams rule reservation — TDD

- [x] 1.1 Add red tests that definition validation rejects literal and templated reserved subjects for `publish`,
  `publish_agent`, and `approve`, covering every action list including cron actions.
- [x] 1.2 Add red direct-executor tests that wholly dynamic subjects resolving under `user.response.>` fail after
  substitution for all three actions with zero publisher and action-specific side effects.
- [x] 1.3 Implement one private token-aware classifier consumed at both gates; add no exported registry or knob.
- [x] 1.4 Prove unrelated prefixes and valid typed agentic-dispatch publication remain unchanged.

## 2. Governance orphan removal — TDD

- [x] 2.1 Add red config tests for `violations.notify_user` values `true`, `false`, and `null`, proving failure before
  port/filter/NATS construction.
- [x] 2.2 Remove `NotifyUser`, `user_errors`, notification branching and payload construction without replacement.
- [x] 2.3 Prove logs, metrics, KV audit storage under valid `violation.<id>`, admin alerts, and
  `governance.violation.*` publication remain; prove no `violation:<id>` compatibility reader exists because NATS
  never accepted that key.
- [x] 2.4 Remove the shipped `notify_user` key and stale docs; regenerate schema and inspect the exact diff.

## 3. Message-logger and typed response proof

- [x] 3.1 Update the frozen census artifact and assertions to raw 395/243/54, effective 579/380/70, delta
  184/137/16, added NATS outputs 27, and 47 loop/dispatch-only collapses.
- [x] 3.2 Add production-registry evidence that a valid reserved-family fixture decodes to concrete
  `*agentic.UserResponse`; state explicitly that observation is not delivery.
- [x] 3.3 Run the repository-wide subject/payload census and prove no SemStreams flat writer matches
  `user.response.>`.

## 4. SemDev breaking lockstep

- [x] 4.1 Add red contract tests for exact `semdev.park-post.request`, interface
  `semdev.park_post_request/v1`, both USER catalogs, both rule outputs, both conversation inputs, the new durable,
  and every required raw-envelope validation.
- [x] 4.2 Move all nine park rules, configs, comments, docs, manifest rows, and fixtures to the exact subject.
- [x] 4.3 Update conversation-channel to exact-subject routing and raw product decoding. Require a canonical non-empty
  `entity_id`, exact envelope `subject`, RFC3339 `timestamp`, and exact `source == rule_engine`; keep `properties` and
  `related_id` optional, and prove typed responses are not accepted as park requests.
- [x] 4.4 Prove broker-accepted JetStream publication, bounded transient redelivery, and unchanged definitive
  graph-only outcomes.
- [x] 4.5 Run SemDev unit/race/contract gates and the end-to-end park-post proof.

## 5. SemTeams adoption gate

- [ ] 5.1 Delete exactly the two flat `publish` actions in coordinator `03-ask-user` and `03b-respond-direct`, retaining
  their audit triples and adding no replacement subject.
- [ ] 5.2 Remove stale flat-bus docs and tests while retaining typed command producers and USER observation.
- [ ] 5.3 Prove production message-logger decoding returns concrete `*agentic.UserResponse` without claiming delivery.
- [ ] 5.4 Run SemTeams contract and relevant UI/E2E suites; block adoption until green.

## 6. Fresh-state and documentation

- [x] 6.1 Add breaking migration notes naming the SemStreams/SemDev lockstep and SemTeams adoption dependency.
- [ ] 6.2 Prove no bridge, alias, union decoder, dual format, dual subscription, forwarding subject, retained-state
  conversion, or mixed-version path exists.
- [x] 6.3 Replace every provisional ruling row with exact file:line evidence or an explicit SemTeams/tag/adoption
      `PENDING` status.
- [ ] 6.4 Update current capability truth and archive this change only after all cross-repo gates are recorded.

## 7. Final verification and review

- [x] 7.1 Run focused SemStreams unit and race tests for rule, governance, message-logger, dispatch, and schemas.
- [x] 7.2 Run `task lint`, `go test -race ./...`, `task schema:generate`, and
  `go test ./test/contract/...`; inspect generated drift.
- [x] 7.3 Run strict OpenSpec validation.
- [x] 7.4 Run `task e2e:agentic` and record clean teardown before the breaking SemStreams commit lands.
- [x] 7.5 Record the green SemDev end-to-end park-post proof before either breaking side lands.
- [ ] 7.6 Complete independent SemStreams review and downstream reviews with no blocking or high findings.
