# Upstream consumer context check

This read-only check supplements the accepted inventory; it does not amend its measured recurrence claims.

The older upstream [issue 1431](https://github.com/nats-io/nats.go/issues/1431) describes a large-bucket Watch
operation expiring at five seconds despite a longer caller context. A maintainer points to
[PR 1835](https://github.com/nats-io/nats.go/pull/1835), merged 2025-03-25, which passes the subscription's context
option into consumer creation. The motivating report used about three million keys and SDK 1.30.2.

Commands on 2026-09-29:

```sh
gh pr view 1835 --repo nats-io/nats.go --json title,body,mergedAt,url,files
gh pr diff 1835 --repo nats-io/nats.go --color never
rg -n -C 6 'upsertConsumer\(stream, consName|upsertConsumer\(jsi.stream|ctx:.*o.ctx' \
  /Users/coby/go/pkg/mod/github.com/nats-io/nats.go@v1.52.0/js.go
```

Installed SDK 1.52.0 already contains the initial creation fix at `js.go:1950–1955`, with the subscription context
recorded at line 1918. The separate recovery branch at lines 2284–2289 selects `js.opts.ctx`. These are observations,
not evidence that either branch caused the current valid-filter, 5,000-key recurrence. An SDK upgrade to obtain
PR 1835 is therefore not a supported repair: this checkout already has it.

The exact installed SDK file hashes remain recorded in `../inventory.md`. No upstream write, test, local SDK
modification, dependency change or production change was performed.
