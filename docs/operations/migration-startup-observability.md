# Startup diagnostic listener timing

The shared Manager HTTP listener now binds after composition seals and before
service startup. When the built-in Metrics service is configured, it is the only
source of its existing scrape configuration, but Manager owns and binds that
listener before ordinary service lifecycle begins. Service Start order and
reverse-registration Stop order do not change. Both diagnostic ports are
observable while a later service or component `Start` is still in flight; the
fail-closed startup barriers and `StartAll` return contract do not change.

No product code or configuration change is required. Existing probes that wait
for HTTP 200 from `/readyz` continue to work, and the response bodies remain
exactly `READY` and `NOT READY`. A deployment that treats successful TCP connect
as readiness must switch to the `/readyz` HTTP status because TCP now becomes
reachable earlier.

During startup the shared listener serves only:

- `/health`, `/healthz`, `/readyz`, `/services`, and `/services/health`;
- `/components/health`, `/components/list`, and
  `/components/status/{name}` when the framework ComponentManager is present.

Other routes return 503 `NOT READY` until all fallible boot work succeeds and
Manager commits the full route set. Stop clears commitment before child cleanup.
Product middleware configured before `StartAll` applies to both phases,
including any product-owned probe authentication policy.

`GET /services` adds a count-only `startup` object. Prometheus adds
`semstreams_startup_units{owner,stage}` with fixed `services`/`components`
owners and fixed stage labels. Neither surface exposes service/component names,
contexts, deadlines, NATS subjects, or retained lifecycle state.
