# Sister-repository adoption checklist

SemStreams agents mutate only SemStreams. Downstream owners implement and validate their own adoption.

For a published breaking SemStreams version:

1. Update the downstream's owned literals, patterns, configuration, schemas, tools, fixtures, seed data, and queries.
2. Start the adopting deployment on newly provisioned NATS storage.
3. Do not migrate, preserve, wipe, or reseed absent state as part of release adoption.
4. If retained deployed NATS state is discovered, stop only that adoption. Perform no destructive action; obtain a
   separate owner-reviewed migration or recovery design.
5. Prove cold-start readiness and run the downstream product's native contract and E2E gates.

SemStreams provides no compatibility alias, dual reader, dual writer, online conversion, or rollback lane for these
pre-v1 clean breaks.

Historical destructive cutover documents remain evidence of earlier beta procedures. They are not current
stable-release adoption instructions.
