# Verify: Stack & architecture · spec 0001 · updated 2026-11-22
_Steps derived from spec 0001 acceptance criteria. `/check verify` runs these; `/test` locks the durable ones._

Run these on a host with gVisor installed and registered (`docker info --format '{{json .Runtimes}}'` lists `runsc`). For a local run, use `ADP_PUBLIC_HOST=localhost`, and copy Caddy's root certificate out first:
`docker compose -f deploy/compose/compose.yaml cp caddy:/data/caddy/pki/authorities/local/root.crt ./caddy-root.crt`

## Commands

### Setup and boot
- [ ] On a host **without** `runsc`: `task setup` → stops before writing anything, with the gVisor install hint → AC-1
- [ ] On a host with `runsc`: `ADP_PUBLIC_HOST=localhost task setup` → prints `created` for 6 secret files and writes `deploy/compose/.env` with `ADP_PUBLIC_HOST` and `DOCKER_GID` → AC-1
- [x] `task build` → builds `adp/control:dev` and `adp/runner:dev` on the host → AC-7
- [ ] `task up` → exits 0. `task ps` shows `postgres`, `socket-proxy`, `control`, `runner` and `caddy` as `healthy`, and `migrate` as `Exited (0)` → AC-1
- [x] `docker compose -f deploy/compose/compose.yaml logs migrate` → `role ready` ×2, `goose up`, `river migrate`, `migrate done`, all logged before `control` and `runner` log `config loaded` → AC-1
- [x] Run `task up` a second time → `migrate` exits 0 again, with `goose up applied 0` and `river migrate applied 0` → AC-1

### Public door (Caddy)
- [x] `curl --cacert caddy-root.crt https://localhost/healthz` → 200 → AC-2
- [x] `curl --cacert caddy-root.crt -X GET https://localhost/webhooks/github` (also PUT and DELETE) → 405 with `Allow: POST` → AC-2
- [x] `curl --cacert caddy-root.crt -X POST https://localhost/webhooks/github` → reaches control, which answers 404 until feature #4 → AC-2
- [x] `curl --cacert caddy-root.crt https://localhost/readyz` and `/anything` → 404 at Caddy → AC-2
- [x] `docker compose -f deploy/compose/compose.yaml ps --format json` → only `caddy` has published ports (80, 443); `curl http://127.0.0.1:8080/healthz` from the host fails to connect → AC-2

### Engine access
- [x] With runsc unregistered (or on a host without it): `runner` logs `runsc runtime not registered; run setup.sh` and exits 1, and Docker restarts it → AC-3
- [x] `task check-proxy` → every line `ok`: `GET /_ping` 200, `POST /v1.47/containers/x/exec` 403, build, image pull, network connect, volumes and system all 403, PUT 405 → AC-3

### Database roles
- [x] As `adp_control` with `secrets/control/db_password` (for example a throwaway `postgres:18.6-alpine3.24` container on `adp_backend`): `create table nope (id int)` → `permission denied for schema public` → AC-4
- [x] Same as `adp_runner` with `secrets/runner/db_password` → `permission denied for schema public` → AC-4
- [x] As each role: insert, update, select and delete a `river_job` row, and insert and delete a `river_queue` row → all succeed; `drop table river_job` → `must be owner` → AC-4
- [x] `select rolsuper, rolcreatedb, rolcreaterole from pg_roles where rolname in ('adp_control','adp_runner')` → all `f` → AC-4

### Secrets
- [ ] `task check-secrets` (with the stack up) → every line `ok`: 6 files at `0400` with uid 70 (postgres) or 65532 (the rest), no value in compose config, image history, image or container env, or logs, and a rerun of setup.sh keeps every value → AC-5

### Config and tests
- [x] `task test` → passes, including `TestProxyAllowlist` (which reads the regexes from `compose.yaml`) → AC-7
- [x] `go test -race -run 'TestRefusesBadConfig|TestRefusesWithoutRunsc' ./services/control/... ./services/runner/...` → passes: a missing or invalid value gives a `config: <value> (<ENV>): <code>` line and no `config loaded` → AC-6
- [x] Remove `ADP_PUBLIC_ORIGIN` from `control` in a scratch copy of compose.yaml and start it → the container exits 1 with `config: public-origin (ADP_PUBLIC_ORIGIN): missing` → AC-6

## Acceptance criteria coverage
- AC-1: setup without runsc, setup, task up, migrate logs, migrate rerun
- AC-2: the five public door steps
- AC-3: runner refusal, task check-proxy
- AC-4: the four database role steps
- AC-5: task check-secrets
- AC-6: the two config refusal steps
- AC-7: task build, task test

## Last run
2026-11-22, by /develop on a host **without** runsc. Passed: AC-2, AC-3, AC-4, AC-5, AC-6 and AC-7, plus AC-1 for every service except `runner`, which refused to start as AC-3 requires. Setup was run as root in a container, with a stand-in that reported `runsc` to `docker info`. Still owed: one full `task up` on a host where `runsc` is really registered.

2026-11-22, by /check verify on a host **without** runsc and without passwordless sudo. Ran and passed: task build, migrate logs and rerun, all five public door steps, the runner refusal, task check-proxy, all four database role steps, task test, both config refusal steps. Not run: both `task setup` steps and `task check-secrets` (need sudo), and the all healthy `task up` (needs runsc; `runner` refused as AC-3 requires).
