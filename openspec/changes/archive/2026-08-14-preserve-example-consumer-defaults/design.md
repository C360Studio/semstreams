## Context

At baseline `1002a051`, the document and IoT example processors hard-coded delivery `all`, explicit acknowledgement,
and maximum delivery `5`. At current head `809f1807`, both call `component.GetConsumerConfig`, whose shared empty/zero
defaults are delivery `new`, explicit acknowledgement, and maximum delivery `3`. The statistical configuration leaves
those fields empty. File inputs and processor consumers start concurrently, so delivery `new` can permanently exclude
retained raw messages published before consumer creation.

The independently reviewed inventory body is
`3e48022be1b262518fd75de78df04bd3323abd7fb3307039e52466f582fb3d68`. The independently reviewed design body is
`452b7a96fcdf86d011ded4dc6a5491d3bb6b23c1be0d3764c165e4692404fe05`.

## Decisions

1. Each affected package resolves canonical consumer configuration, then applies only its local historical defaults.
2. Empty delivery resolves locally to `all`; explicit valid delivery wins.
3. Ack remains the canonical explicit default; explicit valid acknowledgement wins.
4. Runtime zero `max_deliver` resolves locally to `5`. Zero represents both JSON omission and explicit JSON zero, and
   implementation does not distinguish them. Only a positive value overrides `5`.
5. `MaxAckPending` forwards unchanged and remains governed by the existing observation lifecycle.
6. No global `GetConsumerConfig`, `consumerConfigFromFacts`, `buildConsumerConfig`, schema, or config change is allowed.
7. The cold-start proof publishes before component start and waits through explicit synchronization, never a sleep.

## Adopter seam

An adopter chooses only explicit intent. Empty delivery replays retained input; empty or explicit-zero `max_deliver`
means five attempts; a positive value overrides it. The adopter does not predict component startup order or know the
shared extractor's internal defaults. Runtime policy observation continues to explain requested and effective
`MaxAckPending`.

## Risks and mitigations

- A local helper could accidentally become global policy. Keep one private resolver in each affected package.
- A test could merely resample scheduler order. Publish the discriminating raw message before starting the component.
- Restoring one field could overwrite another. Table-test every field independently, including positive and `-1`
  `MaxAckPending`.
- Integration setup could lose the processed output. Bind a replay-safe output observer and use bounded context/state.
