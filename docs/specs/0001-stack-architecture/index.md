# 0001. Stack and architecture for the AI Dev Platform

**Date**: 2026-11-21 · **Updated**: 2026-11-22 (revised toward modular services: one service per ownership boundary, a database per service, REST bindings with no cycles)
**Status**: In Progress

## Summary

The platform is a small set of Go services, each one shaped exactly like a ModularPlatform module, in one `docker compose` stack on a box you own. Release 1 runs three: `forge` (the only holder of GitHub credentials), `scheduler` (owns projects, tasks and runs, and picks the next run, with no outside credentials), and `runner` (the only service that may start containers, each agent run in a gVisor sandbox). A fourth, `models`, arrives with feature #12 to hold the model keys. Each service owns its own database on one Postgres server, and services talk only over REST with one token per binding (a binding is one "service A uses service B" link), always in one direction, so the set drops into the modular platform's graph in Slice 7 without a rewrite. Every split sits on a real boundary (a credential or root equivalent access), not on scale.

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Decision

**Chosen option**: Option 2: modular services cut along ownership boundaries, a database per service, REST bindings that never form a cycle.

Build `forge`, `scheduler` and `runner` (and `models` at #12) as Go modules on ModularPlatform's `sdk.Run`, each with its own catalog entry, its own database and login made by a one shot `provision` step, and its own `migrate` init step. A service that needs another one binds to it over REST with the sdk's per consumer bearer token (`auth: binding-token`). Asynchronous work crosses a binding through a transactional outbox on River in the sender's own database, and flows back by long polling the provider. Agent runs stay the Claude Code CLI inside gVisor sandboxes reached through an allowlisting Docker socket proxy, and Caddy stays the single public door.

**Implementation skills**: `golang-project-layout` · `golang-code-style` · `golang-naming` · `golang-error-handling` · `golang-context` · `golang-concurrency` · `golang-structs-interfaces` · `golang-database` · `golang-security` · `golang-observability` · `golang-lint` · `golang-testing` · `golang-dependency-management` · `golang-continuous-integration` (all `samber/cc-skills-golang`, `.claude/skills/<name>/`) · `openapi-spec-generation` (`wshobson/agents`, `.claude/skills/openapi-spec-generation/`) · `docker-compose-patterns` · `docker-build-strategies` (`docker/skills`, `.claude/skills/<name>/`) · `multi-stage-dockerfile` (`github/awesome-copilot`, `.claude/skills/multi-stage-dockerfile/`) · `supabase-postgres-best-practices` (`supabase/agent-skills`, `.claude/skills/supabase-postgres-best-practices/`) · `go-goose` (`metalagman/agent-skills`, `.claude/skills/go-goose/`) · `github-webhooks` (`hookdeck/webhook-skills`, `.claude/skills/github-webhooks/`)

## Proposed stack

| Layer | Choice | Reason |
|---|---|---|
| Architecture pattern | One service per ownership boundary, each a ModularPlatform module: `forge` (GitHub credentials), `scheduler` (run state, no credentials), `runner` (engine access), `models` (model keys, #12). Inside each service, bounded contexts are packages | Each cut isolates one credential or one root equivalent access, and each service maps to exactly one module entry in Slice 7. No cut exists for scale alone (detail: *Services and boundaries*) |
| Language | Go, language version `go 1.26.8`, toolchain `go1.27.3` (both in `go.work` and each `go.mod`), matching ModularPlatform | Reuses the ModularPlatform sdk and tooling as is; the agent runs as a CLI, so Go limits nothing on the Claude side |
| Framework | Standard library `net/http` ServeMux on `sdk.Run` (`github.com/racherom/modular-platform/sdk`) | Config, secret files, health, readiness, shutdown, binding clients and binding token checks come from the sdk |
| sdk pin | `github.com/racherom/modular-platform/sdk v0.0.0-20261120145312-1eea5a896d43` (ModularPlatform commit `1eea5a8`, which already has `auth/`, `httpclient/` and `provides.<p>.auth: binding-token`); switch to `sdk/vX.Y.Z` tags once they exist | The repo is public, so `go get` works with no credentials; no `replace` to a local folder for the sdk |
| Service config | One catalog entry per service at `catalog/modules/<service>/module.yaml` in the `modular-platform/v1` format. Postgres arrives as a `uses: postgresql` binding; a service another one calls declares one `provides` entry with `auth: binding-token`; its consumers declare a `uses` entry with `client: http`. Own capabilities live in `catalog/capabilities/adp-<service>.yaml`. `sdkgen` writes `internal/config/config.gen.go` | Slice 7 registers these entries instead of writing them; only the sdk reads env vars or `*_FILE` paths |
| Primary DB | PostgreSQL `postgres:18.6-alpine3.24` (same tag as ModularPlatform's catalog), one server, **one database and one login per service** (detail: *Databases and migrations*) | Each service owns its data, as a module on the platform does (ModularPlatform principle 4); still only one stateful container |
| DB access | pgx v5 + sqlc for queries, goose for schema migrations, all per service | Inherited from ModularPlatform managed mode |
| Provisioning | A one shot `provision` service (stock Postgres image, a psql script ported from ModularPlatform's `postgresql-provisioner` job) creates each service's database and login, then exits | Same behavior as the platform's provisioner, so Slice 7 swaps it for the real one |
| Migrations | Each service image ships a `migrate` binary; a one shot `<service>-migrate` container runs goose, then River's migrator, against that service's own database, as that service's login (the database owner), then exits. The service starts only after it succeeded | Each service owns its schema and its release order; the shared code lives in `libs/dbmigrate` |
| Service to service requests | REST, contract first with OpenAPI 3.1 and `oapi-codegen`, one contract per provided capability, written by the first feature that adds an operation (#4, #7). Every call carries the binding token as `Authorization: Bearer` (sdk `httpclient`), checked by the sdk against the provider's clients directory | ModularPlatform principle 3 (REST for requests) and principle 8 (a credential per binding) |
| Binding graph | Calls go one way only: `forge` uses `scheduler`, `scheduler` uses `runner`. No service uses one that uses it back (detail: *Binding graph and flows*) | The platform's checks refuse a `binding_cycle`, so a cycle now would block Slice 7 |
| Async flow | Push: the sender writes its state change and an outbox job in one transaction; River delivers it over REST with retries and an `Idempotency-Key`. Pull: the consumer long polls the provider, claiming commands under a lease or reading a cursor feed. NATS JetStream replaces this once the platform's `nats` capability exists | Transactional, survives restarts, no broker before one is needed (ModularPlatform made the same call for its first managed thread) |
| Background jobs | River `v0.51.2` on pgx v5, in each service's own database, behind a small `queue` interface | Retries and stuck job recovery built in; zero cost when idle |
| Agent loop | Claude Code CLI inside the sandbox image: `claude -p … --output-format stream-json`; runner reads the final `result` event for outcome, tokens and cost | Go never runs an agent loop; no official Go Agent SDK exists (detail: feature #6) |
| Sandbox runtime | gVisor (`runsc`) registered as a Docker runtime on the host; runner sets `runtime: runsc` on every sandbox and refuses to start if the engine does not list it | A user space kernel between agent code and the host from day one (detail: feature #5) |
| Engine access | `wollomatic/socket-proxy` in front of `/var/run/docker.sock`, allowlist per method and path, reachable only on an internal network shared with `runner` | No other service sees the socket; a compromised `runner` cannot call exec, build, or system endpoints |
| GitHub client | `google/go-github` (current major) plus `bradleyfalzon/ghinstallation/v2`, in `forge` only | The standard Go client; app vs token details belong to feature #4 |
| Event intake | Signed GitHub webhooks (HMAC `X-Hub-Signature-256`) arrive at `forge`, which records the delivery and pushes the event to `scheduler`; a code only reconcile in `forge` reads repo state on start and on a manual resync | Catches events missed while the box was down, with no agent runs and no tokens |
| Ingress | Caddy `caddy:2.11.7-alpine` with automatic HTTPS on one hostname, `{$ADP_PUBLIC_HOST}`, routing to `forge`; the only service that publishes host ports (80, 443) (detail: *Public address and HTTPS*) | Same ingress as ModularPlatform; a tunnel can be added later as a compose overlay |
| Run logs | `runner` streams each run's `stream-json` output to a file on the `run-logs` volume; its own database keeps the relative path. Access goes through a `LogStore` interface | Cheap and simple; moving to an S3 capability later changes one adapter |
| Secrets | `deploy/compose/setup.sh`, run once with `sudo`, creates one file per secret per reading service under `secrets/<service>/`, mode `0400`, owned by the uid of the reading container; never overwritten on rerun (detail: *Secrets and config per service*) | ModularPlatform principle 6; compose ignores `uid` and `mode` on secrets outside Swarm, so ownership is set on the host |
| Images | Built on the host with `docker compose build`, one image per service (`adp/forge:dev`, `adp/scheduler:dev`, `adp/runner:dev`); build context is the repo root so the image can include `libs/`; multi stage, distroless static nonroot (uid 65532). Build stage bases pinned by tag and digest, stock images by tag | No registry needed for Release 1 (see Consequences for Slice 7) |
| Hosting | One always on Linux box you own with Docker Engine, the compose plugin and `runsc` registered. Receiving GitHub webhooks needs a public DNS name with ports 80 and 443 reachable, or a tunnel overlay | One admin, one box |
| Admin web and auth | None until features #10 and #11; any admin service then uses `scheduler` (and never the other way round) | Release 1 is GitHub only |
| Observability | `log/slog` JSON plus the sdk's `/healthz` and `/readyz` on `:8080`, probed by each binary's `healthcheck` subcommand; every outbox delivery and every claim logs its binding, path and idempotency key (never a body or token); metrics wait for Slice 3 | One log line per hop makes a cross service flow traceable without a tracing system yet |
| Internal TLS | Off for Release 1. Postgres connections get TLS with #24; service to service calls get TLS in a separate feature after #24 (see Follow-up). Every call still needs its binding token | Keeps certificate wiring off #1's path; tokens cross internal only networks in plain text until then |
| Task runner | Task (`Taskfile.yml`) with `setup`, `generate`, `build`, `test`, `up`, `down`, `check-proxy`, `check-secrets`, `check-bindings` | Same entry points as ModularPlatform |

### Services and boundaries

| Service | Boundary it owns | Bounded contexts inside | Data it owns (own database) |
|---|---|---|---|
| `forge` | The GitHub App key and webhook secret (#4); the only service that talks to `api.github.com` | `forge` (webhook intake, delivery dedup, reconcile, posting questions and PRs) | GitHub deliveries, installation and repo mapping, its outbox |
| `scheduler` | Run state and the deterministic "what runs next" decision; holds no outside credential, only its binding token to `runner` | `runloop`, `board` (question and answer state) | Projects, tasks, runs (as decided), questions and answers, cursors into `runner`'s feed, its outbox |
| `runner` | Root equivalent engine access through the socket proxy; the run logs | `sandbox`, `agent`, `logstore` | Run executions, run outcomes feed, log paths |
| `models` (#12) | Claude and OpenRouter keys; hands out short lived scoped values | `models` | Encrypted model connections |

- What each service's tables look like is feature #3's decision, now one data model per service. This spec only fixes who owns what.
- `models` uses nothing but `postgresql`. Feature #12 decides whether `runner` or `scheduler` uses it; either keeps the graph free of cycles.

### Binding graph and flows

```
caddy ──(route)──▶ forge ──uses──▶ scheduler ──uses──▶ runner ──▶ socket-proxy
                     │                 │                  │
                     └─────────────────┴──── each uses its own postgresql database
```

- **forge uses scheduler.** Push: a verified webhook event goes into `forge`'s outbox in the same transaction that records the `X-GitHub-Delivery` ID, then River delivers it to `scheduler`. Pull: `forge` long polls `scheduler` to claim GitHub commands (post this question, open this PR) under a lease, and marks each one complete.
- **scheduler uses runner.** Push: "start this run" goes into `scheduler`'s outbox in the same transaction that marks the run as dispatched. Pull: `scheduler` long polls `runner`'s outcome feed by cursor, and applies each outcome and its new cursor in one transaction.
- The exact operations, paths and payloads belong to the features that need them (#4, #7, #8, #9). Every operation follows the three patterns below.

**Push (consumer to provider, through the outbox)**:
- The consumer inserts a River job (kind `outbox.deliver`, args: binding, method, path, JSON body, idempotency key) in the same transaction as its state change. The key is a UUIDv7 made at insert time.
- The worker sends it with the sdk `httpclient` (which never retries on its own) and the header `Idempotency-Key`. A 2xx answer completes the job. A 408, 429, 5xx or network error retries with River's backoff. Any other 4xx is a permanent failure: the job is discarded and logged at `error` with the binding, path and key.
- The provider stores each key it accepted in its own database in the same transaction as the effect. A repeated key answers the first result (2xx) and changes nothing.

**Pull commands (claim with a lease)**:
- `POST /v1/<things>/claim` with `wait` up to 15 seconds answers 200 with one item, a lease ID and the lease expiry, or 204 when nothing arrived in time. The consumer calls `POST /v1/<things>/{id}/complete` with the lease ID (idempotent). An expired lease puts the item back. A lease lasts 5 minutes unless the operation's feature sets another.

**Long poll limits**: the sdk server (at the pinned commit) has a 15 second read timeout, a 20 second write timeout and a 10 second shutdown, so `wait` never exceeds 15 seconds and a provider answers before it. The consumer sets its request timeout through its context to `wait` plus 10 seconds (the sdk `httpclient` sets none). Shutdown may cut an open poll; that is safe because claims expire and cursors are applied in the consumer's own transaction.

**Pull feed (cursor)**:
- `GET /v1/<feed>?after=<seq>&wait=15s` answers the items after `seq` in order, or an empty list after the wait. The provider gives each item its `seq` from a counter row it updates in the same transaction as the item (`UPDATE … SET seq = seq + 1 RETURNING seq`), so the commit order is the `seq` order and a reader never skips one. The consumer stores the last `seq` it applied in its own database, in the same transaction as applying it.

**Contracts and versions**:
- Each provided capability has a version in its `provides` entry (`"1.0"` to start). Adding operations or optional fields keeps `1.x`. A breaking change ships as `2.0` under `/v2/`, served next to `/v1/` until every consumer moved. Providers deploy before their consumers.

### Public address and HTTPS

- `setup.sh` asks once for `ADP_PUBLIC_HOST` and writes it, with `DOCKER_GID`, to `deploy/compose/.env`. Neither is a secret. The Caddyfile site address is `{$ADP_PUBLIC_HOST}`, and `forge`'s public origin is `https://` plus that host.
- **Real DNS name**: Caddy gets a Let's Encrypt certificate; the name must resolve to the box and ports 80 and 443 must be reachable. Behind NAT or CGNAT, add a tunnel overlay (Cloudflare Tunnel or Tailscale Funnel) or use the DNS challenge. This is the only mode in which GitHub can deliver webhooks.
- **`localhost` or a private name**: Caddy uses its internal CA. Checks use `curl --cacert` with Caddy's root certificate. For local runs and CI only.
- Routes: `/webhooks/github` accepts `POST` only (other methods answer 405 at Caddy), with `request_body max_size 1MB`, proxied to `forge:8080`. `/healthz` is proxied to `forge`'s liveness path. Every other path answers 404 at Caddy. Until #4 adds the handler, `forge` answers **501** on `POST /webhooks/github`, so a check can tell it reached `forge` (Caddy answers 404 or 405 itself, never 501). `scheduler` and `runner` have no route.
- `forge` trusts exactly one proxy hop for the client IP (principle 11).

### Services and networks

| Service | Image | Networks | Holds | Publishes |
|---|---|---|---|---|
| `caddy` | `caddy:2.11.7-alpine` | `edge` | TLS state volume | 80, 443 |
| `forge` | built here | `edge`, `backend` | `forge` DB password, token for `scheduler` (GitHub App key and webhook secret with #4) | nothing; `:8080` |
| `scheduler` | built here | `backend` | `scheduler` DB password, token for `runner`, clients dir with `forge`'s token | nothing; `:8080` |
| `runner` | built here | `backend`, `engine` | `runner` DB password, clients dir with `scheduler`'s token, `run-logs` volume | nothing; `:8080` |
| `provision` | `postgres:18.6-alpine3.24`, command `sh /provision.sh`, user `70:70` | `backend` | the owner password and each service login's password | nothing |
| `forge-migrate`, `scheduler-migrate`, `runner-migrate` | the service's own image, entrypoint `/app/migrate` | `backend` | that service's DB password (same file as the service) | nothing |
| `socket-proxy` | `wollomatic/socket-proxy:1.14.0` | `engine` (internal) | Docker socket mount; runs as `65534:65534` with the host's docker gid | nothing; `:2375` |
| `postgres` | `postgres:18.6-alpine3.24` | `backend` (internal) | `pgdata` volume, the owner password | nothing |

- `backend` and `engine` are `internal: true`. `edge` is not internal and is `forge`'s way out to `api.github.com`. `scheduler` and `runner` have no internet access. Sandboxes never join any of these networks (detail: feature #5).
- Start order: `postgres` healthy → `provision` exits 0 → the three `<service>-migrate` steps (independent of each other) each exit 0 → `runner` healthy (also after `socket-proxy` healthy) → `scheduler` healthy → `forge` healthy → `caddy`. Providers start before their consumers, matching the binding graph. The sdk enforces the same order on readiness: a consumer's `/readyz` stays 503 until its provider's `/readyz` answered once (sdk RDY-1); a provider going down later does not unready its consumer.
- Compose builds three images, each with the repo root as context and `dockerfile: services/<service>/Dockerfile`, tagged `adp/<service>:dev` by the service entry. Each `<service>-migrate` uses the same tag with `pull_policy: never`. The root `.dockerignore` excludes everything but `libs/` and `services/`.
- `task up` runs `docker compose up -d --wait`, which creates the one shot steps again on every run.
- A `:ro` flag on the socket mount protects nothing (the API still accepts writes), so the spec relies on the allowlist.
- `proxy-check` (`curlimages/curl:8.23.0`, profile `checks`, network `engine`) and `binding-check` (same image, profile `checks`, network `backend`, user `65532`) exist only while their `task check-*` runs.

### Health checks

| Service | Check |
|---|---|
| `forge`, `scheduler`, `runner` | `["CMD", "/app/<service>", "healthcheck"]`: the sdk's subcommand calls `/readyz` on `127.0.0.1:8080`; `runner` is ready only after the `runsc` check passed |
| `postgres` | `pg_isready -h 127.0.0.1 -U adp_owner -d postgres`, over TCP on purpose (the image's first init runs a socket only server that must not count as ready) |
| `caddy` | `wget -qO- http://127.0.0.1:2019/config/` |
| `socket-proxy` | `-allowhealthcheck` plus the bundled `/healthcheck` binary |
| `provision`, `<service>-migrate` | none; each must exit 0 |

### Databases and migrations

- `adp_owner` is the Postgres bootstrap superuser (`POSTGRES_USER=adp_owner`, `POSTGRES_PASSWORD_FILE`, `POSTGRES_DB=postgres` so no extra database is made). Only `provision` uses it after init.
- Per service: database `forge`, `scheduler`, `runner` (later `models`), each owned by its login `adp_forge`, `adp_scheduler`, `adp_runner` (`LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE`).
- `provision` (`deploy/compose/provision.sh`, ported from ModularPlatform's `postgresql-provisioner` script at the sdk pin's commit) runs on every `up`. It reads `ADP_PROVISION_HOST`, `ADP_PROVISION_PORT`, `ADP_PROVISION_OWNER_USER`, `ADP_PROVISION_OWNER_PASSWORD_FILE`, `ADP_PROVISION_SERVICES` (space separated: `forge scheduler runner`) and `ADP_PROVISION_PASSWORD_DIR` (`/run/secrets`, holding `adp_<service>_password`). The port renames the script's `MP_*` names to these, since `MP_` is the sdk's bootstrap prefix. It writes a `PGPASSFILE` in `/tmp` (mode `0600`) instead of exporting `PGPASSWORD`. Under one advisory lock for the whole run, for each service in turn: create the marker role `adp_provisioned` if missing; create the login or update its password (a rerun rotates a changed password; rotate only between `task down` and `task up`, since a running service keeps the old password until it restarts); refuse a login or database of that name it did not make; create the database owned by the login; `REVOKE CONNECT` on it from `PUBLIC` and `GRANT CONNECT` to its login; revoke `CONNECT` on `postgres` and `template1` from `PUBLIC`. Every name reaches SQL through psql variables, and each password is read inside psql from its file, never on a command line.
- `<service>-migrate` connects as the service's own login to its own database and runs `libs/dbmigrate`: wait for the database, goose up (embedded SQL from `services/<service>/migrations/`), then River's migrations (`rivermigrate`), then exit. A rerun applies nothing new and exits 0.
- A login can reach only its own database. No service reads another service's tables; data crosses only through a binding.
- Services connect straight to Postgres with no pooler, which River's LISTEN and NOTIFY need.

### Secrets and config per service

Every secret file is made with `openssl rand -hex 32` on the first `setup.sh` run, mode `0400`, owned by the uid that reads it. `secrets/` is `0700` root. A value read by two services gets two files with the same value (the last column).

| File | Read by (uid) | Mounted at | Same value as |
|---|---|---|---|
| `secrets/postgres/adp_owner_password` | `postgres` (70) | `/run/secrets/adp_owner_password` | `provision/adp_owner_password` |
| `secrets/provision/adp_owner_password` | `provision` (70) | `/run/secrets/adp_owner_password` | `postgres/adp_owner_password` |
| `secrets/provision/adp_forge_password` | `provision` (70) | `/run/secrets/adp_forge_password` | `forge/db_password` |
| `secrets/provision/adp_scheduler_password` | `provision` (70) | `/run/secrets/adp_scheduler_password` | `scheduler/db_password` |
| `secrets/provision/adp_runner_password` | `provision` (70) | `/run/secrets/adp_runner_password` | `runner/db_password` |
| `secrets/forge/db_password` | `forge`, `forge-migrate` (65532) | `/run/secrets/db_password` | `provision/adp_forge_password` |
| `secrets/scheduler/db_password` | `scheduler`, `scheduler-migrate` (65532) | `/run/secrets/db_password` | `provision/adp_scheduler_password` |
| `secrets/runner/db_password` | `runner`, `runner-migrate` (65532) | `/run/secrets/db_password` | `provision/adp_runner_password` |
| `secrets/forge/scheduler_token` | `forge`, `binding-check` (65532) | `/run/secrets/scheduler_token` | `scheduler/clients/forge` |
| `secrets/scheduler/clients/forge` | `scheduler` (65532) | `/run/secrets/clients/forge` | `forge/scheduler_token` |
| `secrets/scheduler/runner_token` | `scheduler`, `binding-check` (65532) | `/run/secrets/runner_token` | `runner/clients/scheduler` |
| `secrets/runner/clients/scheduler` | `runner` (65532) | `/run/secrets/clients/scheduler` | `scheduler/runner_token` |

- Clients files are mounted one by one as compose secrets with target `/run/secrets/clients/<consumer>` (the file modes matter, not the host folder's), so the provider's clients directory holds only its consumers' tokens (sdk rules TOK-1 to TOK-3: file name is the consumer instance, at least 32 characters, an empty or missing directory stops the start).
- The scaffold holds no GitHub App key, App ID or webhook secret. Feature #4 adds them to `forge` only.
- No password or token sits inside a URL or an environment variable.
- Catalog entries (own env names get the `ADP_` prefix; `MP_INSTANCE` is the service name):
  - Every service: a `uses: postgresql` binding mapped to `ADP_DB_URL` (`postgres://postgres:5432`, never with credentials), `ADP_DB_NAME`, `ADP_DB_USER`, `ADP_DB_PASSWORD_FILE`, `ADP_DB_SSLMODE` (`disable` for now).
  - `forge`: config `public-origin` (`ADP_PUBLIC_ORIGIN`, url), `proxy-hops` (`ADP_PROXY_HOPS`, int, default `1`, range 0 to 1); `uses: scheduler` (capability `adp-scheduler`, `client: http`, `ADP_SCHEDULER_URL` = `http://scheduler:8080`, `ADP_SCHEDULER_TOKEN_FILE`).
  - `scheduler`: `provides: scheduler` (capability `adp-scheduler`, version `"1.0"`, `auth: binding-token`, clients dir env `ADP_CLIENTS_DIR` = `/run/secrets/clients`); `uses: runner` (capability `adp-runner`, `client: http`, `ADP_RUNNER_URL` = `http://runner:8080`, `ADP_RUNNER_TOKEN_FILE`).
  - `runner`: `provides: runner` (capability `adp-runner`, version `"1.0"`, `auth: binding-token`, `ADP_CLIENTS_DIR`); config `docker-host` (`ADP_DOCKER_HOST`, `tcp://socket-proxy:2375`), `sandbox-runtime` (`ADP_SANDBOX_RUNTIME`, enum `[runsc]`), `run-logs-dir` (`ADP_RUN_LOGS_DIR`, default `/var/lib/adp/run-logs`).
  - `catalog/capabilities/adp-scheduler.yaml` and `adp-runner.yaml` follow ModularPlatform's `renderer.yaml`: fields `url` (url), `token` (secret, direction both), `ca` (certificate, optional). Their description names the contract file `contracts/openapi/<capability>.yaml`, which arrives with the first operation. The `provides` entries copy `renderer`'s shape too: `fields: {url: {platform: url}, token: {generate: true}, ca: {platform: ca}}`, which the sdk ignores today and the platform's checks need in Slice 7.
  - Each service keeps `internal/config/generate.go` with a `go:generate` line running `sdkgen` from the sdk module, declared with a `tool` directive in its `go.mod`, as `control` and `runner` do today.
  - `migrate` binaries are not catalog modules; they read plain env (`ADP_DB_HOST`, `ADP_DB_PORT`, `ADP_DB_NAME`, `ADP_DB_USER`, `ADP_DB_PASSWORD_FILE`). `provision` reads `ADP_PROVISION_*` env naming the host, owner user, and the services to provision, plus the password files.

### Socket proxy allowlist (starting point; feature #5 tightens it)

Unchanged from the first version of this spec and already built:

- Per method regex flags (`-allowGET`, `-allowPOST`, `-allowDELETE`), each anchored and accepting an optional API version prefix `(/v1\.[0-9]+)?`. Only `runner` and the throwaway `proxy-check` may connect (`-allowfrom=runner,proxy-check`). A method with no allow flag answers 405. A table test in `runner` checks allowed and denied paths against the same regexes.
- Allowed: `GET /_ping`, `/version`, `/info`, `/images/{name}/json`, `/containers/json`, `/containers/{id}/json`, `/containers/{id}/logs`, `/networks`, `/networks/{id}`; `POST /containers/create`, `/containers/{id}/start`, `/containers/{id}/wait`, `/containers/{id}/kill`, `/networks/create`; `DELETE /containers/{id}`, `/networks/{id}`.
- Denied: everything else, explicitly `exec`, `build`, `commit`, `images/create`, `networks/{id}/connect`, `swarm`, `plugins`, `system`, `volumes`.
- Every container and network `runner` creates carries `adp.managed=true` and `adp.run=<run id>`.
- **runsc check**: at start, `runner` calls `GET /info` and looks for `runsc` in `Runtimes`; if missing it logs `runsc runtime not registered; run setup.sh` and exits 1.
- **The proxy filters paths, not request bodies.** `runner` builds every create body from code (never `Privileged`, never host bind mounts, always `runtime: runsc`) and never takes these fields from task or agent input.

### Repository layout

```
go.work                         libs, services/forge, services/scheduler, services/runner (contracts/ joins with #4 or #7)
Taskfile.yml
.dockerignore                   build context is the repo root
catalog/capabilities/postgresql.yaml     copied from ModularPlatform at the sdk pin's commit
catalog/capabilities/adp-scheduler.yaml
catalog/capabilities/adp-runner.yaml
catalog/modules/{forge,scheduler,runner}/module.yaml
libs/                           Go module github.com/racherom/ai-dev-platform/libs
  dbmigrate/                    goose + rivermigrate for one service database (used by every cmd/migrate)
  outbox/                       outbox.deliver worker and enqueue helper (arrives with #4, the first push)
contracts/                      Go module; openapi/<capability>.yaml plus oapi-codegen output (arrives with the first operation)
services/forge/
  cmd/forge/  cmd/migrate/
  internal/config/              sdkgen output, never hand edited
  internal/forge/               from #4
  migrations/                   goose SQL, embedded
  Dockerfile
services/scheduler/
  cmd/scheduler/  cmd/migrate/
  internal/config/
  internal/runloop/  internal/board/   from #7 and #8
  migrations/
  Dockerfile
services/runner/
  cmd/runner/  cmd/migrate/
  internal/config/
  internal/sandbox/  internal/agent/  internal/logstore/
  migrations/
  Dockerfile
images/sandbox/Dockerfile       agent image (features #5, #6; not in the scaffold)
deploy/compose/compose.yaml
deploy/compose/Caddyfile
deploy/compose/setup.sh
deploy/compose/provision.sh
deploy/compose/check-proxy.sh   task check-proxy
deploy/compose/check-bindings.sh  task check-bindings
deploy/compose/check-secrets.sh   task check-secrets
docs/                           scope, specs
```

- Go module paths: `github.com/racherom/ai-dev-platform/services/<name>`, `…/libs`, `…/contracts`. Each service `go.mod` requires `libs` (and later `contracts`) with a `replace` to the relative folder, so an image builds with `GOWORK=off` from the repo root context and ships exactly the versions its own `go.mod` names.
- Every module pins the same versions of shared dependencies (pgx, River, goose); a bump changes all of them in one commit.
- Own config values use the `ADP_` prefix; sdk bootstrap names keep `MP_`.
- ModularPlatform's 12 architecture principles apply where they make sense, notably 3 (standard protocol per interaction), 4 (provisioning capabilities), 6 (secrets born on the host), 8 (per binding credentials), 9 (no image or host names in service code), 11 (client IP by proxy hop count) and 12 (origin from configuration only).

### Scaffold pins (recorded 2026-11-22)

Bump any of these on purpose, and update this table in the same change.

| What | Pin |
|---|---|
| Go | language `go 1.26.8`, toolchain `go1.27.3` |
| ModularPlatform sdk | `v0.0.0-20261120145312-1eea5a896d43` (commit `1eea5a8`); `sdkgen` from the same module via `tool` |
| sdk health | `/healthz` and `/readyz` on `:8080`; `healthcheck` subcommand probes `/readyz` |
| River | `riverqueue/river` and `riverdriver/riverpgxv5` `v0.51.2`, in every service and `libs` |
| pgx | `jackc/pgx/v5` `v5.13.0` in every module |
| goose | `pressly/goose/v3` `v3.30.1` |
| go-github | not added yet; feature #4 pins it with `ghinstallation/v2`, in `forge` only |
| Build base | `golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18` |
| Runtime base | `gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab` |
| Postgres | `postgres:18.6-alpine3.24`, data volume at `/var/lib/postgresql` |
| Caddy | `caddy:2.11.7-alpine` |
| Socket proxy | `wollomatic/socket-proxy:1.14.0` |
| Check client | `curlimages/curl:8.23.0` (`proxy-check`, `binding-check`) |

### Container hardening (carried over from the first scaffold)

Every built service, every one shot step and the proxy run `read_only`, `cap_drop: [ALL]`, `no-new-privileges`, with a `/tmp` tmpfs. Caddy the same plus `NET_BIND_SERVICE`, HTTP/1.1 and HTTP/2 only, host ports overridable with `ADP_HTTP_PORT` and `ADP_HTTPS_PORT` for local runs only.

## Requirements

Scaffold acceptance criteria for feature #1, revised for the service split. `/develop stack & architecture` builds to these, and `/check verify` checks them.

- **AC-1**: After `task setup` and `task up`, `postgres`, `socket-proxy`, `runner`, `scheduler`, `forge` and `caddy` pass the checks in *Health checks*; `provision` exits 0 before any `<service>-migrate` starts, and each `<service>-migrate` exits 0 before its service starts. A second `task up` reruns them all with nothing new applied. On a host without `runsc`, `task setup` stops early with a hint.
- **AC-2**: Only `caddy` publishes host ports (80, 443), as `docker compose ps --format json` shows. `https://$ADP_PUBLIC_HOST/healthz` answers 200 through Caddy from `forge`, `/webhooks/github` answers 405 to anything but `POST` and 501 from `forge` on `POST`, any other path answers 404 at Caddy, and no service answers on `127.0.0.1:8080` from the host.
- **AC-3**: When `GET /info` does not list `runsc`, `runner` logs `runsc runtime not registered; run setup.sh` and exits 1. `task check-proxy` shows a denied call such as `POST /v1.47/containers/x/exec` failing with 403 and `GET /_ping` succeeding.
- **AC-4**: `forge`, `scheduler` and `runner` each connect with their own login to their own database, which holds their goose and River tables. A login connecting to another service's database, or to `postgres`, fails with `permission denied for database`. No service login is superuser or may create databases or roles.
- **AC-5**: With the stack up, `task check-secrets` finds no secret value (passwords or binding tokens) in `docker compose config`, in `docker history` of any built image, in the `docker inspect` environment of any container (one shot steps included), or in `docker compose logs`. Each of the 12 secret files is mode `0400` and owned by the uid of the service that reads it. Rerunning `setup.sh` keeps every value.
- **AC-6**: `forge`, `scheduler` and `runner` each boot on `sdk.Run` from their `catalog/modules/<service>/module.yaml`; a missing or invalid config value stops the service with a problem line, and `scheduler` or `runner` started with an empty clients directory (`docker compose run --rm --no-deps -e ADP_CLIENTS_DIR=/tmp/empty <service>`, with Postgres up and migrated, since the sdk checks it after setup) stops with the sdk's `missing` problem on `provides.adp-scheduler.clients_dir` or `provides.adp-runner.clients_dir`. (`migrate` and `provision` are plain binaries or scripts and are not covered.)
- **AC-7**: `task build` builds `adp/forge:dev`, `adp/scheduler:dev` and `adp/runner:dev` from the repo root context, `task generate` leaves no diff, and `task test` (`go test -race` on every module in `go.work`) passes, including the proxy allowlist table test and a `libs/dbmigrate` test against a real Postgres behind its build tag.
- **AC-8**: The binding graph has no cycle: the catalog entries declare only `forge` uses `scheduler` and `scheduler` uses `runner`. With the stack up, `task check-bindings` (`docker compose --profile checks run --rm binding-check`: `curlimages/curl` as uid 65532 on `backend`, depending on healthy `scheduler` and `runner`, reading the two token files listed in the secrets table) calls `GET /v1/binding-check` on both services. `scheduler` with `forge`'s token and `runner` with `scheduler`'s token answer 404 (token accepted, no route; the scaffold registers no catch all route). On each service, no token, a wrong token, and the other service's token answer 401, and `/healthz` answers 200 without a token. Once #4 or #7 add routes, the script keeps the unrouted probe path.

## Migration plan

**Strategy**: replace directly. There is no live data and no feature code yet (the scaffold holds empty services and a baseline migration), so a strangler would only add work.
**Phases**:
1. Add `libs/` with `dbmigrate` (moved from `services/control/internal/dbsetup`, minus the role and grant steps); point `go.work` at `libs`, add the `replace` to each service `go.mod`, run `go work sync`, and align `runner` to pgx `v5.13.0`.
2. Rename `services/control` to `services/forge` (keeps `public-origin`, `proxy-hops`, the webhook path, its `cmd/migrate` rewritten on `libs/dbmigrate`); add `services/scheduler` and `runner`'s `cmd/migrate`, each with its own baseline migration.
3. Add `deploy/compose/provision.sh` and the capability files; rewrite catalog entries, `compose.yaml` (provision, three migrate steps, start order, clients mounts), `Caddyfile` (`forge:8080`), `setup.sh` (12 files), `check-secrets.sh`, and add `check-bindings.sh` plus `task check-bindings`.
4. On any host that already ran the old scaffold, run `docker compose down -v` once: the old `adp` database and the `adp_control` role hold nothing worth keeping. Then `task setup` creates the new secret files beside the old ones, and you may delete `secrets/control/` and `secrets/migrate/`.
5. Update `verify.md` to these ACs and run `/check verify`.
**Rollback**: revert the commit and run `docker compose down -v` again; no data exists to lose.
**Risks**: a leftover `pgdata` volume from the old layout makes `provision` refuse the database names only if they clash (they do not: `adp` vs `forge`, `scheduler`, `runner`), so the old database just lingers until step 4; a missed secret file shows up in `task check-secrets`.

## Consequences

**Positive**:
- Each credential lives in exactly one small process: GitHub keys only in `forge`, engine access only in `runner`, model keys only in `models`. `scheduler`, the brain, holds nothing an attacker wants beyond one internal token.
- Slice 7 becomes registration: every service is already one module with its own database binding, provides and uses entries, binding tokens checked by the sdk, and a graph the platform's checks accept.
- Services deploy on their own: a contract versioned per capability and providers starting before consumers mean `runner` can change without touching `forge`.
- One Postgres server still backs everything. A `pg_dumpall` plus `run-logs` plus `secrets/` restores the whole platform.
- An idle repo costs zero agent runs and zero tokens: long polls and outbox workers are plain code.

**Negative / tradeoffs**:
- This is more to build and run for one admin: three services, three databases, four one shot steps, twelve secret files and two contracts where the first draft had two services. Each cross service flow is now an outbox, a retry policy, an idempotency table and a contract instead of a function call. The first features (#3, #4, #7) pay this cost.
- No transaction spans two services. "Webhook recorded, so the run is queued" becomes eventual: it holds once the outbox delivers. Every operation must be idempotent, and a permanent delivery failure needs a person to look at the logs (no dead letter view until the console exists).
- Long polling adds up to one poll cycle of delay per pull hop and keeps a few idle connections open. It is cheap at this scale, but it is a second pattern beside push.
- Service to service tokens cross the internal networks in plain text until the internal TLS feature after #24 ships; anyone who can sniff `backend` sees them. Postgres passwords have the same exposure until #24.
- The allowlist still does not stop a compromised `runner` from creating a privileged container with host mounts, which is root on the host. Until #5 decides on body filtering, only `runner`'s own code stands in the way.
- With `scheduler` using `runner`, a second runner is a second binding `scheduler` must choose between; parallel runs (Deferred) owe that design.
- Each service's `migrate` is its own init container. The platform has no per module init step today, so Slice 7 must either gain one or move migrations into service start.
- The `provision` script is a copy of ModularPlatform's provisioner and can drift from it until Slice 7 replaces it.
- Images build from the repo root with a `replace` to `libs/`, so `libs` changes rebuild every image. Images are not reproducible across hosts beyond the two digest pinned bases, and Slice 7 still needs a registry or local tags.
- River is still before 1.0, now in four modules; its pin moves in one commit across all of them.
- gVisor overhead, the `sudo` setup step, the public DNS or tunnel need for webhooks, and run logs on a local volume carry over unchanged.

**Neutral**:
- `control` disappears; its public role passes to `forge`. Docs, the scope and `AGENTS.md` that say `control` need rewording.
- Feature #3 now designs one data model per service, and each service owns its own migrations.
- Feature #23's pgx alignment happens as part of this rework; its River schema check now runs in each service against its own database.
- NATS remains the target for events once the platform offers a `nats` capability; the outbox and feed patterns are what it replaces.

## Follow-up

- [ ] Ask ModularPlatform to start tagging `sdk/vX.Y.Z`, then switch this repo's pin from a pseudo version to a tag.
- [ ] Before Slice 7, decide how images reach the managed platform (a ghcr.io pipeline or local tags).
- [ ] Before Slice 7, decide how a module's `migrate` step runs on the platform (a per module init step in ModularPlatform, or migrations at service start), and replace `provision.sh` with the platform's own `postgresql-provisioner`.
- [ ] Before Slice 7, register `adp-scheduler`, `adp-runner` (and `adp-models`) as capabilities the platform catalog accepts.
- [ ] Feature #3 designs one data model per service (`forge`, `scheduler`, `runner`) and the idempotency key and feed counter tables the patterns above need.
- [ ] Features #4 and #7 write the first contracts (`contracts/openapi/adp-scheduler.yaml`, `adp-runner.yaml`), add the `contracts/` module and `libs/outbox`, and record each `X-GitHub-Delivery` ID once (unique key) so a redelivered webhook does nothing twice.
- [ ] Feature #5 tightens the socket proxy allowlist, defines the per run sandbox network, egress, resource limits and cleanup, and decides on body filtering for container creates (or accepts that risk in writing).
- [ ] Feature #6 decides how the Claude credential reaches a sandbox until `models` exists, and which `claude -p` flags and permission mode runs use.
- [ ] Feature #12 decides whether `runner` or `scheduler` uses `models`, keeping the graph free of cycles.
- [ ] Revisit Postgres TLS (#24) for three logins before any GA feature (#4, #5, #12) ships.
- [ ] Enroll a new feature after #24: TLS for service to service calls (an internal CA such as ModularPlatform's `cert-issuer`, verified by the sdk's `ca` binding field). Run `/scope` to add it.
- [ ] Feature #25 (backup) uses `pg_dumpall` or one `pg_dump` per service database.
- [ ] Every service checks River's schema version at start and stops with a clear line when its `migrate` has not caught up (#23).
- [ ] Keep `catalog/capabilities/postgresql.yaml` and `provision.sh` in step with the sdk pin's commit on every sdk bump.
- [ ] Scope feature #1 and root `AGENTS.md` still name `control` and a one shot `migrate` in the `control` image. Run `/scope` for the feature text and `/sync` for `AGENTS.md` (services, contexts per service, the `libs` module) after the rework lands.
- [ ] Parallel runs (Deferred) owe the design for more than one `runner` behind `scheduler`.
- [x] Pin at scaffold time and record here (done 2026-11-22, see *Scaffold pins*).
- [ ] Connect the MCP servers in your own MCP settings; this spec cannot do that for you.
