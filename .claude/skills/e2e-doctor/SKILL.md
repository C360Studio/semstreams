---
name: e2e-doctor
description: Diagnose and clear Docker substrate problems before running e2e/integration tests — disk starvation, leaked testcontainers, port conflicts. Use before an e2e tier, or when testcontainers time out (~60s "port 4222/tcp not found") and you suspect infra not code.
argument-hint: [optional: tier name, e.g. structural]
---

# E2E doctor — Docker substrate preflight

semstreams e2e/integration spins up many NATS testcontainers fast. Under Docker disk/IO pressure,
container startup blows past the ~60s ready deadline and fails with
`mapped port: ... port "4222/tcp" not found, ctx err: context deadline exceeded` — which **reads
exactly like a code/test failure but is infra** (the beta.115 trap: a 46h-leaked container + 29 GB
build cache caused two integration packages to "fail"; both passed clean after reclaim). Check this
FIRST when integration/e2e goes red at ~60s.

## Step 1 — Disk pressure

```bash
docker system df
```

Flags:
- **Build Cache** reclaimable more than ~10 GB → `docker builder prune -f` (safe; just rebuilds slower).
- **Images** huge with few ACTIVE → most are likely *other projects'*; see Step 4 before pruning.

## Step 2 — Leaked testcontainers / orphans

```bash
docker ps -a --format '{{.Names}}\t{{.Status}}\t{{.Image}}'
```

Look for old NATS/ryuk/e2e containers **up for hours** (testcontainers' ryuk should reap them but
sometimes doesn't — e.g. `*-e2e-nats-*`, `*-nats-*`). Kill stragglers:

```bash
docker rm -f <name>            # leaked container holding a port / disk
```

## Step 3 — Port conflicts

```bash
task e2e:check-ports
```

If a port is held, find and stop the holder (often a previous tier left `... up -d` running —
`task e2e:<tier>:down` or `docker compose -f docker/compose/tiered.yml down -v`).

## Step 4 — Safe reclaim recipe

Default safe sweep (frees the most for the least risk):

```bash
docker rm -f <leaked containers from Step 2>
docker builder prune -f          # reclaim build cache
docker image prune -f            # dangling images only
docker system df                 # confirm the drop
```

**Do NOT blanket `docker image prune -a`.** On this laptop the big images are sister-project /
sim artifacts (e.g. `seminstruct:qwen3-8b` ~10 GB, `px4-gazebo` ~10 GB, semspec/semteams sandboxes)
— expensive to re-pull and not needed for semstreams tiers. Remove a specific giant only if the
human confirms.

## Step 5 — Clean teardown after

e2e tiers end with `... down -v --timeout 15`; confirm `docker ps -aq | wc -l` is back to 0 so the
next run starts clean. Leftover containers from a killed run are the #1 source of the next flake
(`feedback_substrate_flake_discipline`).
