# Verify: Stack & architecture · spec 0001 · updated 2026-11-22 (service split: forge, scheduler, runner)
_Steps derived from spec 0001 acceptance criteria. `/check verify` runs these; `/test` locks the durable ones._

Run these on a host with gVisor installed and registered (`docker info --format '{{json .Runtimes}}'` lists `runsc`). For a local run, use `ADP_PUBLIC_HOST=localhost`, and copy Caddy's root certificate out first:
`docker compose -f deploy/compose/compose.yaml cp caddy:/data/caddy/pki/authorities/local/root.crt ./caddy-root.crt`

**Once, on a host that ran the old scaffold** (Migration plan step 4): `docker compose -f deploy/compose/compose.yaml down -v`. The old `adp` database and the `adp_runner` role made by the old `migrate` would make `provision` refuse `adp_runner` as "not made by provision". Afterwards you may delete `secrets/control/` and `secrets/migrate/`.

## Commands

### Setup and boot
- [ ] On a host **without** `runsc`: `task setup` → stops before writing anything, with the gVisor install hint → AC-1
- [ ] On a host with `runsc`: `ADP_PUBLIC_HOST=localhost task setup` → prints `created` (or `kept`) for 12 secret files and writes `deploy/compose/.env` with `ADP_PUBLIC_HOST` and `DOCKER_GID` → AC-1
- [ ] `task build` → builds `adp/forge:dev`, `adp/scheduler:dev` and `adp/runner:dev` from the repo root context → AC-7
- [ ] `task up` → exits 0. `task ps` shows `postgres`, `socket-proxy`, `runner`, `scheduler`, `forge` and `caddy` as `healthy`; `provision`, `forge-migrate`, `scheduler-migrate` and `runner-migrate` as `Exited (0)` → AC-1
- [ ] `docker compose -f deploy/compose/compose.yaml logs provision` → `provision: database <svc> and login adp_<svc>` for forge, scheduler and runner, then `provision: done`; each `<service>-migrate` logs `goose up`, `river migrate`, `migrate done` before its service logs `config loaded` → AC-1
- [ ] Run `task up` a second time → every one shot step exits 0 again; each `<service>-migrate` logs `goose up applied 0` and `river migrate applied 0` → AC-1

### Public door (Caddy)
- [ ] `curl --cacert caddy-root.crt https://localhost/healthz` → 200, from `forge` → AC-2
- [ ] `curl --cacert caddy-root.crt -X GET https://localhost/webhooks/github` (also PUT and DELETE) → 405 with `Allow: POST` → AC-2
- [ ] `curl --cacert caddy-root.crt -X POST https://localhost/webhooks/github` → 501 from `forge` (Caddy never answers 501) → AC-2
- [ ] `curl --cacert caddy-root.crt https://localhost/anything` → 404 at Caddy → AC-2
- [ ] `docker compose -f deploy/compose/compose.yaml ps --format json` → only `caddy` has published ports (80, 443); `curl http://127.0.0.1:8080/healthz` from the host fails to connect → AC-2

### Engine access
- [ ] With runsc unregistered (or on a host without it): `runner` logs `runsc runtime not registered; run setup.sh` and exits 1, and Docker restarts it → AC-3
- [ ] `task check-proxy` → every line `ok`: `GET /_ping` 200, `POST /v1.47/containers/x/exec` 403, build, image pull, network connect, volumes and system all 403, PUT 405 → AC-3

### Databases and logins
- [ ] As each of `adp_forge`, `adp_scheduler`, `adp_runner` (password from `secrets/<svc>/db_password`, for example `docker compose exec -e PGPASSWORD=… postgres psql -h 127.0.0.1 -U adp_<svc> -d <svc>`): the own database holds `goose_db_version` and `river_job` → AC-4
- [ ] Each login connecting to another service's database, or to `postgres` → `FATAL: permission denied for database "<name>"` → AC-4
- [ ] `select rolname, rolsuper, rolcreatedb, rolcreaterole from pg_roles where rolname in ('adp_forge','adp_scheduler','adp_runner')` → all `f` → AC-4

### Secrets
- [ ] `task check-secrets` (with the stack up) → every line `ok`: 12 files at `0400`, uid 70 (`postgres/*`, `provision/*`) or 65532 (the rest); no password or binding token in compose config, image history, image or container env (one shot steps included), or logs; a rerun of setup.sh keeps every value → AC-5

### Config and tests
- [ ] `task generate` then `git diff --exit-code services/*/internal/config` → no diff → AC-7
- [ ] `task test` → passes: unit tests of every `go.work` module (including `TestProxyAllowlist`, which reads the regexes from `compose.yaml`), then `test:integration` runs `libs/dbmigrate` against a real Postgres → AC-7
- [ ] `go test -race -run 'TestRefusesBadConfig|TestRefusesWithoutRunsc|TestBindingAuth' ./services/...` → passes: a missing or invalid value gives a `config: <value> (<ENV>): <code>` line → AC-6
- [ ] With Postgres up and migrated: `docker compose -f deploy/compose/compose.yaml run --rm --no-deps -e ADP_CLIENTS_DIR=/tmp scheduler` (and the same for `runner`) → exits 1 with `config: provides.scheduler.clients_dir (ADP_CLIENTS_DIR): missing` (`provides.runner.…` for runner) → AC-6. _The spec's AC-6 text says `/tmp/empty` and `provides.adp-scheduler…`; a folder that does not exist gives `invalid_binding`, and the sdk names the provide key, not the capability. Owed to `/architect`._

### Bindings
- [ ] `grep -A3 '^uses:' catalog/modules/*/module.yaml` → only `forge` uses `adp-scheduler` and only `scheduler` uses `adp-runner`; no other `uses` but `postgresql` → AC-8
- [ ] `task check-bindings` (with the stack up) → every line `ok`: scheduler answers 404 to forge's token and 401 to no token, a wrong token and scheduler's own token; runner answers 404 to scheduler's token and 401 to no token, a wrong token and forge's token; `/healthz` 200 on both without a token → AC-8

## Acceptance criteria coverage
- AC-1: both setup steps, task up, provision and migrate logs, second task up
- AC-2: the five public door steps
- AC-3: runner refusal, task check-proxy
- AC-4: the three database and login steps
- AC-5: task check-secrets
- AC-6: the config refusal tests, the empty clients directory run
- AC-7: task build, task generate, task test
- AC-8: the catalog grep, task check-bindings

## Last run
2026-11-22, by /develop after the service split, on a host **without** runsc and without passwordless sudo. Smoke run of the real `compose.yaml` under project `adpsmoke`, with throwaway secrets (mode 0444, owned by the user, since `task setup` needs sudo) and a stand in engine that lists `runsc`. Passed: task build, task generate (no diff), task test (unit and integration), every service healthy and every one shot step exited 0, second up applied nothing, all five public door steps, the runner refusal against the real socket proxy, task check-proxy, all three database and login steps, the leak search of AC-5 (no value found in config, history, env or logs), the empty clients directory run (with `/tmp`), task check-bindings. Not run: both `task setup` steps and the mode and owner half of `task check-secrets` (need sudo), and `task up` with a really registered `runsc`.

Earlier runs (old layout with `control` and one `migrate`, superseded by the service split): 2026-11-22 by /develop and by /check verify, both on a host without runsc. They passed everything but the sudo steps and the all healthy `task up`.
