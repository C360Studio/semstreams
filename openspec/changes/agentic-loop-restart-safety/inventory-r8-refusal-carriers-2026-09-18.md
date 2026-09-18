# R8 refusal-carrier and adopter supplement

base: 68c14c8eb25c512e988f740cbf7ea14b6815976f

Inventory only. Supplements accepted inventory `53fe8a90022dac6dd6a036c32a648109d2c073758c2cd2e85fed5bb4fd9dee6b`.

Owner ruling `5727252562` remains selected: preserve safety and give visible refusal when required history is definitively unavailable. This supplement does not reopen that choice.

## Existing paths and limits

| Caller/path | Existing producer and visible result | Measured limit |
|---|---|---|
| Durable USER submission | Dispatch’s `answerRefusedSubmission` creates registered UserResponse/error; `sendResponse` publishes to the resolved channel response subject and requires PubAck. | Applicable while dispatch has the source UserMessage and its route. Downstream delivery is the channel-response contract, not activity SSE. |
| HTTP submission | `refusedSubmissionResponse` returns UserResponse/error synchronously. Successful submission instead returns an asynchronous-work acknowledgment; its stream mirror remains optional. | A later loop refusal cannot retroactively change that completed HTTP response. |
| Task with retained nonterminal LoopEntity | Existing loop failure builder uses authoritative state/routing; terminal settlement can publish failure and persist its terminal marker. Dispatch consumes ordinary terminal outputs. | Applicable only with sufficient current authority. It cannot justify fabricating a failed LoopEntity when that authority is gone. |
| Raw TaskMessage with user route | Registered UserResponse has error content, InReplyTo and ThreadID, and can represent a delivery refusal without claiming a loop terminal state. | Loop currently has no declared `user.response` output. Reuse would require an explicit producer-port extension and correlation rule. The type has no dedicated TaskID field; InReplyTo currently means message ID or loop ID. |
| Activity SSE caller | Dispatch serves its shared AGENT_LOOPS graph view, including current-loop/completion records and view errors. | It does not consume UserResponse. Publishing that existing payload alone does not establish visibility on this endpoint. |
| Routeless rule/raw-task producer | TaskMessage permits omitted channel routing. | UserResponse requires ChannelType and ChannelID, so it cannot represent every such result unchanged. The measured registry has no dedicated task-delivery refusal payload; no existing producer-to-consumer refusal route for this case has been established. |

Quarantine is not an answer to these carrier gaps: it requests owner stop, rather than returning a caller-visible terminal result. Known history loss must not silently strand the caller or stop unrelated work under that name.

The complete measured agentic registry includes model responses, tool results, loop lifecycle events, context events, approval messages and UserResponse. Registry-name absence alone does **not** prove that a new payload is necessary. Existing UserResponse reuse is viable for routed callers, subject to declared wiring; routeless callers and activity SSE need an explicitly named result path and consumer.

## Adopter findings

1. A channel client can already display UserResponse/error without understanding restart internals.
2. An HTTP submitter receives only submission-time results synchronously; later visibility must be named separately.
3. An activity-SSE client must not be told that a UserResponse publication reaches its current subscription.
4. A component author publishing valid routeless TaskMessage must not acquire an undocumented requirement to invent a user channel.
5. The missing proof is the concrete refusal route for those latter consumers—not whether refusal is an acceptable product outcome.
6. No new payload, bucket, field, or API is selected by this supplement.

## Verifier pins

- `agentic/payload_registry.go:37` — `{Domain: Domain, Category: CategoryUserResponse, Version: SchemaVersion, Description: "User response to channel", Factory: func() any { return &UserResponse{} }, IndexingProfile: content},`
- `agentic/user_types.go:229` — `type UserResponse struct {`
- `agentic/user_types.go:236` — `InReplyTo string `json:"in_reply_to,omitempty"` // message_id or loop_id`
- `agentic/user_types.go:255` — `if r.ChannelType == "" {`
- `agentic/user_types.go:258` — `if r.ChannelID == "" {`
- `agentic/user_types.go:330` — `// User routing info (optional, for error notifications)`
- `processor/agentic-dispatch/component.go:893` — `func (c *Component) answerRefusedSubmission(ctx context.Context, msg agentic.UserMessage, refusal error) error {`
- `processor/agentic-dispatch/component.go:899` — `Type:        agentic.ResponseTypeError,`
- `processor/agentic-dispatch/component.go:1041` — `subject, err := component.ResolveSubject(c.outputPortDefs(), "user.response", resp.ChannelType+"."+resp.ChannelID)`
- `processor/agentic-dispatch/component.go:1045` — `if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {`
- `processor/agentic-dispatch/http.go:312` — `func refusedSubmissionResponse(msg agentic.UserMessage, refusal error) agentic.UserResponse {`
- `processor/agentic-dispatch/http.go:325` — `// The actual task execution happens asynchronously via NATS.`
- `processor/agentic-dispatch/http.go:421` — `c.sendResponse(ctx, resp)`
- `processor/agentic-dispatch/http.go:423` — `return resp, nil`
- `processor/agentic-dispatch/http_activity.go:175` — `// ensureActivityView returns the component's ONE shared AGENT_LOOPS view,`
- `processor/agentic-dispatch/http_activity.go:347` — `// from the component's ONE shared AGENT_LOOPS graph view (ADR-081) instead`
- `processor/agentic-loop/handlers.go:2784` — `entity, err := h.loopManager.GetLoop(loopID)`
- `processor/agentic-loop/handlers.go:2842` — `failureSubject, err := component.ResolveSubject(h.config.Ports.Outputs, "agent.failed", loopID)`
- `processor/agentic-loop/config.go:417` — `Outputs: []component.PortDefinition{`
- `natsclient/delivery_settlement.go:408` — `ownerStopNeeded: work.decision == DeliveryDecisionQuarantine,`

## Searches and read boundary

1. Read complete `agentic/payload_registry.go`; inspected UserResponse and TaskMessage routing declarations.
2. Searched `type (UserResponse|.*Refus.*|.*Reject.*|.*Status.*|.*Notification.*)`, matching category names and DeliveryDecisionQuarantine in agentic/natsclient production Go.
3. Located UserResponse, sendResponse and refusal helpers in dispatch component/HTTP sources; inspected the cited implementations.
4. Searched `user.response|CategoryUserResponse|\*agentic.UserResponse` in dispatch, input, output, gateway and loop config. Truncated locator output was not treated as a complete production-consumer census.
5. Inspected the complete loop default output-port list: it contains no `user.response`.
6. Inspected the activity view/SSE source and settlement owner-stop branch.
7. Initial guessed paths `agentic/user_message.go` and shell glob `natsclient/settlement*.go` failed; corrected to `agentic/user_types.go` and `natsclient/delivery_settlement.go`. No absence claim relies on those failed locators.

No tests or mutations. Stop for independent inventory review. The conditional facts-only mechanical outline remains separate and unpromoted.
