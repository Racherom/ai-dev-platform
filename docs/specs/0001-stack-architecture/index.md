# 0001. Stack and architecture for the AI Dev Platform

**Date**: 2026-11-21 · **Updated**: 2026-11-22 (scaffold pins and six differences recorded)
**Status**: In Progress

## Summary

The platform is two long running Go services, a one shot `migrate` step, and three stock helpers (Postgres, a Docker socket proxy, Caddy) in one `docker compose` stack on a box you own: `control` (talks to GitHub and decides what runs next), `runner` (the only service that may start containers, and starts each agent run in a gVisor sandbox), and Caddy as the single public door. Work moves between them through a job queue that lives in Postgres, so nothing extra has to run. Every service is written the way your ModularPlatform modules are (same sdk, same catalog entry format, secrets as files), so moving onto the modular platform later is mostly copying entries. The admin web app waits until feature #11.

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Decision

**Chosen option**: Option 1: Go services on the ModularPlatform sdk, split by privilege, Postgres as the only stateful dependency.

Build `control` and `runner` as Go modules that run on ModularPlatform's `sdk.Run`, hand work between them through River (a Postgres job queue), start agent runs as the Claude Code CLI inside gVisor sandboxes reached through an allowlisting Docker socket proxy, and serve everything public through Caddy.

**Implementation skills**: `golang-project-layout` · `golang-code-style` · `golang-naming` · `golang-error-handling` · `golang-context` · `golang-concurrency` · `golang-structs-interfaces` · `golang-database` · `golang-security` · `golang-observability` · `golang-lint` · `golang-testing` · `golang-dependency-management` · `golang-continuous-integration` (all `samber/cc-skills-golang`, `.claude/skills/<name>/`) · `openapi-spec-generation` (`wshobson/agents`, `.claude/skills/openapi-spec-generation/`) · `docker-compose-patterns` · `docker-build-strategies` (`docker/skills`, `.claude/skills/<name>/`) · `multi-stage-dockerfile` (`github/awesome-copilot`, `.claude/skills/multi-stage-dockerfile/`) · `supabase-postgres-best-practices` (`supabase/agent-skills`, `.claude/skills/supabase-postgres-best-practices/`) · `go-goose` (`metalagman/agent-skills`, `.claude/skills/go-goose/`) · `github-webhooks` (`hookdeck/webhook-skills`, `.claude/skills/github-webhooks/`)

## Proposed stack

| Layer | Choice | Reason |
|---|---|---|
| Architecture pattern | Three processes split by privilege: `control`, `runner`, plus `migrate` (one shot); each process is a small modular monolith inside | The split follows who may hold what (GitHub keys vs root equivalent engine access), not scale; inside each service, bounded contexts are packages, not services |
| Language | Go, language version `go 1.26.8`, toolchain `go1.27.3` (both in `go.work` and each `go.mod`), matching ModularPlatform | Reuses the ModularPlatform sdk and tooling as is; the agent runs as a CLI, so Go limits nothing on the Claude side |
| Framework | Standard library `net/http` ServeMux on `sdk.Run` (`github.com/racherom/modular-platform/sdk`) | Same as ModularPlatform: config, secret files, health, readiness, graceful shutdown and binding auth come from the sdk |
| sdk pin | `github.com/racherom/modular-platform/sdk v0.0.0-20261120145312-1eea5a896d43` (ModularPlatform commit `1eea5a8`); switch to `sdk/vX.Y.Z` tags once they exist | The repo is public, so `go get` works with no credentials; no `replace` to a local folder |
| Service config | One catalog entry per service at `catalog/modules/<service>/module.yaml` in the `modular-platform/v1` format; Postgres arrives as a `uses: postgresql` binding typed by `catalog/capabilities/postgresql.yaml` (copied from ModularPlatform at the sdk pin's commit); `sdkgen` writes `internal/config/config.gen.go` | Slice 7 then registers these entries instead of writing them; only the sdk reads env vars or `*_FILE` paths |
| Primary DB | PostgreSQL `postgres:18.6-alpine3.24` (same tag as ModularPlatform's catalog), database `adp`, schema `public`, three roles (detail: *Database roles and grants*) | The relational default; the only stateful service in the stack |
| DB access | pgx v5 + sqlc for queries, goose for schema migrations | Inherited from ModularPlatform managed mode |
| Migrations | A one shot `migrate` binary (`cmd/migrate`, shipped in the `control` image, a plain `main` that uses the sdk's secret file reader but not `sdk.Run`) runs on every `up`: it creates or updates the app roles, runs goose, then River's migrator (`rivermigrate`), then applies grants, then exits. Both apps start with `depends_on: condition: service_completed_successfully` (detail: *Database roles and grants*) | Apps never hold DDL rights; same init pattern as ModularPlatform principle 4 |
| Background jobs | River `v0.51.2` on pgx v5, wrapped behind a small `queue` interface in each service | Enqueue happens in the same transaction as the state change, so the run loop stays deterministic; retries and stuck job recovery come built in; zero cost when idle |
| control ↔ runner | No direct calls. `control` enqueues run jobs; `runner` is a River worker that writes the run outcome, usage and log path with its own limited Postgres role | Both sides survive restarts; one credential per binding (ModularPlatform principle 8) |
| Agent loop | Claude Code CLI inside the sandbox image: `claude -p … --output-format stream-json`; runner reads the final `result` event for outcome, tokens and cost | Go never runs an agent loop; no official Go Agent SDK exists; the workflow skills assume Claude Code (detail: feature #6) |
| Sandbox runtime | gVisor (`runsc`) registered as a Docker runtime on the host; runner sets `runtime: runsc` on every sandbox and refuses to start if the engine does not list it | A user space kernel between agent code and the host from day one (detail: feature #5) |
| Engine access | `wollomatic/socket-proxy` in front of `/var/run/docker.sock`, allowlist per method and path, reachable only on an internal network shared with `runner` | `control` never sees the socket; a compromised `runner` cannot call exec, build, or system endpoints |
| GitHub client | `google/go-github` (current major) plus `bradleyfalzon/ghinstallation/v2` for GitHub App installation tokens | The standard Go client; app vs token details belong to feature #4 |
| Event intake | Signed GitHub webhooks (HMAC `X-Hub-Signature-256`) trigger work; a code only reconcile reads repo state on `control` start and on a manual resync | Catches events missed while the box was down, with no agent runs and no tokens |
| Ingress | Caddy `caddy:2.11.7-alpine` with automatic HTTPS on one hostname, `{$ADP_PUBLIC_HOST}`; the only service that publishes host ports (80, 443) (detail: *Public address and HTTPS*) | Same ingress as ModularPlatform; a tunnel can be added later as a compose overlay with no service change |
| Run logs | `runner` streams each run's `stream-json` output to a file on the `run-logs` volume; Postgres keeps the relative path. Access goes through a `LogStore` interface | Cheap and simple; moving to an S3 capability later changes one adapter |
| Secrets | `deploy/compose/setup.sh`, run once with `sudo`, creates one file per secret per reading service under `secrets/<service>/`, mode `0400`, owned by the uid of the one container that reads it; never overwritten on rerun. Compose mounts each service's own files only (detail: *Secrets and config per service*) | ModularPlatform principle 6; no secret in git, env vars, images or logs. Compose ignores `uid` and `mode` on secrets outside Swarm, so ownership is set on the host |
| Images | Built on the host from source with `docker compose build`; multi stage builds, distroless static nonroot (uid 65532) for Go services. The two build stage bases (`golang`, `distroless`) are pinned by tag and digest; stock service images (`postgres`, `caddy`, `socket-proxy`, `curl`) by tag only. The scaffold builds only `control` and `runner`; the sandbox image comes with #5 and #6 | Your pick: no registry needed for Release 1 (see Consequences for what this costs at Slice 7) |
| Hosting | One always on Linux box you own (home server or VPS) with Docker Engine, the compose plugin and `runsc` registered (`setup.sh` stops with a hint if `docker info` does not list it). Receiving GitHub webhooks needs a public DNS name with ports 80 and 443 reachable, or a tunnel overlay | One admin, one box; runs survive your laptop sleeping |
| Admin web | None until feature #11 (then React + Vite + TanStack, as in ModularPlatform `web/`) | Release 1 is GitHub only; the scaffold stays thinner |
| Admin auth | None until feature #10; Release 1 exposes no admin surface | The only public route is the signed webhook |
| API contract | OpenAPI 3.1, contract first, `oapi-codegen`, from the first own endpoint (#10/#11); the webhook handler uses `go-github` event types | No own API exists in Release 1, so no empty contract file |
| Observability | `log/slog` JSON plus the sdk's `/healthz` (liveness) and `/readyz` (readiness) on `:8080`, probed by a `healthcheck` subcommand in each Go binary (detail: *Health checks*); metrics wait for Slice 3 | Run outcomes and logs already live in Postgres and files; distroless images have no shell or curl to probe with |
| Task runner | Task (`Taskfile.yml`) with `build`, `test`, `up`, `setup`; lint, hooks and CI come with feature #2 | Same entry points as ModularPlatform |

### Services and networks

| Service | Image | Networks | Holds | Publishes |
|---|---|---|---|---|
| `caddy` | `caddy:2.11.7-alpine` | `edge` | TLS state volume | 80, 443 |
| `control` | built here | `edge`, `backend` | `control` DB password (GitHub App key and webhook secret arrive with #4) | nothing; listens on `:8080` |
| `migrate` | same image as `control`, command `migrate` | `backend` | its own copies of the `adp_owner`, `adp_control` and `adp_runner` passwords | nothing |
| `runner` | built here | `backend`, `engine` | `runner` DB password, `run-logs` volume (read write) | nothing; health only, on the sdk's `:8080` |
| `socket-proxy` | `wollomatic/socket-proxy:1.14.0` | `engine` (internal) | Docker socket mount; runs as `65534:65534` with `group_add` set to the host's docker gid | nothing; listens on `:2375` |
| `postgres` | `postgres:18.6-alpine3.24` | `backend` (internal) | `pgdata` volume, its own copy of the `adp_owner` password | nothing |

- `backend` and `engine` are `internal: true`. `edge` is not internal and is `control`'s intended way out to `api.github.com`; `runner` has no internet access and needs none. Sandboxes never join any of these networks; runner creates a separate network per run (detail: feature #5).
- A `:ro` flag on the socket mount protects nothing (the API still accepts writes), so the spec relies on the allowlist, not on the mount flag.
- TLS on the internal network is off for Release 1 (see Follow-up).
- `proxy-check` (`curlimages/curl:8.23.0`, profile `checks`, network `engine`) exists only while `task check-proxy` runs.

### Public address and HTTPS

- `setup.sh` asks once for `ADP_PUBLIC_HOST` and writes it, with `DOCKER_GID` (from `stat -c %g /var/run/docker.sock`), to `deploy/compose/.env`. Neither is a secret. The Caddyfile site address is `{$ADP_PUBLIC_HOST}`, and `control`'s public origin is `https://` plus that host.
- **Real DNS name**: Caddy gets a Let's Encrypt certificate. The name must resolve to the box and ports 80 and 443 must be reachable from the internet. Behind NAT or CGNAT with no port forwarding, add a tunnel overlay (Cloudflare Tunnel or Tailscale Funnel) or use the DNS challenge; neither changes a service. This is the only mode in which GitHub can deliver webhooks.
- **`localhost` or a private name**: Caddy uses its internal CA. Checks use `curl --cacert` with Caddy's root certificate copied out of its data volume. For local runs and CI only.
- Routes: `/webhooks/github` accepts `POST` only (other methods answer 405 at Caddy), with `request_body max_size 1MB`, proxied to `control:8080`. `/healthz` is proxied to `control`'s liveness path. Every other path answers 404 at Caddy. Until #4 adds the handler, `control` answers 404 on the webhook path.
- `control` trusts exactly one proxy hop for the client IP (principle 11).

### Health checks

| Service | Check |
|---|---|
| `control` | `["CMD", "/app/control", "healthcheck"]`: the sdk's built in subcommand calls `/readyz` on `127.0.0.1:8080` and exits 0 or 1 |
| `runner` | `["CMD", "/app/runner", "healthcheck"]`, same on `127.0.0.1:8080`; ready only after the `runsc` check passed |
| `postgres` | `pg_isready -h 127.0.0.1 -U adp_owner -d adp`, over TCP on purpose: the image's first init runs a socket only server that must not count as ready |
| `caddy` | `wget -qO- http://127.0.0.1:2019/config/` (busybox `wget` on the alpine image, admin API bound to localhost) |
| `socket-proxy` | `-allowhealthcheck` plus the bundled `/healthcheck` binary, confirmed on `1.14.0` |
| `migrate` | none; it must exit 0 |

- The sdk (at the pinned commit) serves `/healthz` (liveness) and `/readyz` (readiness) in front of each service's own handler, on one fixed plain listener `:8080`. `healthcheck` is a reserved subcommand of every `sdk.Run` binary and probes `/readyz`.

### Database roles and grants

- Database `adp`, schema `public`. `adp_owner` is the Postgres bootstrap user (`POSTGRES_USER=adp_owner`, `POSTGRES_PASSWORD_FILE`), so it is the one superuser and only `migrate` uses it after init.
- `migrate` does, in this order, on every run:
  1. Create or alter `adp_control` and `adp_runner` (`LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE`) with passwords from its own file copies. Running it again also rotates a changed password.
  2. `REVOKE CREATE ON SCHEMA public FROM PUBLIC`.
  3. goose up (embedded SQL from `services/control/migrations/`).
  4. River's migrations through `rivermigrate` (the River tables live with `control`'s migrations; `runner` does not import goose).
  5. `GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public` and `GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public` to both app roles, plus matching `ALTER DEFAULT PRIVILEGES FOR ROLE adp_owner`.
- Grants are per table for now. Feature #3 narrows `adp_runner` to the run result columns it names.
- Both services connect straight to Postgres with no pooler in between, which River's LISTEN and NOTIFY need.

### Secrets and config per service

Every file is generated with `openssl rand` on the first `setup.sh` run, mode `0400`, owned by the uid that reads it. `secrets/` itself is `0700` owned by root. A value read by two services gets two files with the same value, so each service mounts only its own.

| File | Read by (uid) |
|---|---|
| `secrets/postgres/adp_owner_password` | `postgres` (70) |
| `secrets/migrate/adp_owner_password` | `migrate` (65532) |
| `secrets/migrate/adp_control_password` | `migrate` (65532) |
| `secrets/migrate/adp_runner_password` | `migrate` (65532) |
| `secrets/control/db_password` | `control` (65532) |
| `secrets/runner/db_password` | `runner` (65532) |

- The scaffold holds no GitHub App key, App ID or webhook secret. Feature #4 adds them to `control`'s catalog entry and to `setup.sh` (the key read from a file path you give, the webhook secret generated and printed once for the GitHub App settings).
- No password ever sits inside a URL or an environment variable. Config carries host, port, database and user; the password is a `*_FILE` the sdk reads.
- Catalog entries (env names get the `ADP_` prefix):
  - Both services: a `uses: postgresql` binding mapped to `ADP_DB_URL` (`postgres://postgres:5432`, never with credentials), `ADP_DB_NAME`, `ADP_DB_USER`, `ADP_DB_PASSWORD_FILE` and `ADP_DB_SSLMODE` (`disable` for now). No listen address key: the sdk fixes `:8080`. `MP_INSTANCE` names the instance.
  - `control` config: `public-origin` (`ADP_PUBLIC_ORIGIN`, url), `proxy-hops` (`ADP_PROXY_HOPS`, int, default `1`, range 0 to 1).
  - `runner` config: `docker-host` (`ADP_DOCKER_HOST`, `tcp://socket-proxy:2375`), `sandbox-runtime` (`ADP_SANDBOX_RUNTIME`, enum `[runsc]`), `run-logs-dir` (`ADP_RUN_LOGS_DIR`, default `/var/lib/adp/run-logs`). The sandbox image tag comes with #5.
  - `migrate` is not a catalog module; it reads plain env (`ADP_DB_HOST`, `ADP_DB_PORT`, `ADP_DB_NAME`, `ADP_DB_OWNER_USER`, and one `*_PASSWORD_FILE` per role).

### Socket proxy allowlist (starting point; feature #5 tightens it)

- Written as the proxy's per method regex flags (`-allowGET`, `-allowPOST`, `-allowDELETE`), each regex anchored (`^…$`) and accepting an optional API version prefix `(/v1\.[0-9]+)?`, because the Docker Go client sends versioned paths. Only `runner` and the throwaway `proxy-check` client may connect (`-allowfrom=runner,proxy-check`). A method with no allow flag at all (for example `PUT`) answers 405, not 403. A table test in `runner` checks allowed and denied paths against the same regexes.
- Allowed: `GET /_ping`, `/version`, `/info`, `/images/{name}/json`, `/containers/json` (list, for the cleanup sweep), `/containers/{id}/json`, `/containers/{id}/logs`, `/networks`, `/networks/{id}`; `POST /containers/create`, `/containers/{id}/start`, `/containers/{id}/wait`, `/containers/{id}/kill`, `/networks/create`; `DELETE /containers/{id}`, `/networks/{id}`.
- Denied by default: everything else, explicitly `exec`, `build`, `commit`, `images/create` (no pulls: the sandbox image is built on the host and must exist by tag), `networks/{id}/connect` (a sandbox's network goes in the create body's `NetworkingConfig`), `swarm`, `plugins`, `system`, `volumes`.
- Every container and network `runner` creates carries the labels `adp.managed=true` and `adp.run=<run id>`, so a startup sweep can find and remove what a crashed run left behind.
- **runsc check**: at start, `runner` calls `GET /info` and looks for `runsc` in `Runtimes`. If it is missing, it logs `runsc runtime not registered; run setup.sh` and exits 1. Compose restarts it with `restart: on-failure` and a delay.
- **The proxy filters paths, not request bodies.** A compromised `runner` can therefore still create a privileged container with host mounts through the allowed create call, which is root on the host. Until #5 decides on body filtering, the only guard is that `runner` builds every create body from code (never `Privileged`, never host bind mounts, always `runtime: runsc`) and never takes these fields from task or agent input.

### Repository layout

```
go.work                         services/control, services/runner
Taskfile.yml
catalog/capabilities/postgresql.yaml   copied from ModularPlatform at the sdk pin's commit; Slice 7 uses the platform's own copy
catalog/modules/control/module.yaml
catalog/modules/runner/module.yaml
services/control/
  cmd/control/  cmd/migrate/
  internal/config/              sdkgen output, never hand edited
  internal/dbsetup/             roles, goose, River migrations and grants, used by migrate
  internal/...                  bounded contexts as packages (forge, runloop, board) as features add them
  migrations/                   goose SQL, embedded
  Dockerfile
services/runner/
  cmd/runner/
  internal/config/
  internal/...                  sandbox, agent, logstore
  Dockerfile
images/sandbox/Dockerfile       agent image with the claude CLI and pinned skills (feature #5, #6; not in the scaffold)
deploy/compose/compose.yaml
deploy/compose/Caddyfile
deploy/compose/setup.sh
deploy/compose/check-proxy.sh     run inside proxy-check by task check-proxy
deploy/compose/check-secrets.sh   task check-secrets
docs/                           scope, specs
```

- Go module path: `github.com/racherom/ai-dev-platform/services/<name>`.
- Own config values use the `ADP_` prefix (for example `ADP_PUBLIC_ORIGIN`, `ADP_DB_HOST`); sdk bootstrap names keep the sdk's own `MP_` names.
- ModularPlatform's 12 architecture principles apply here where they make sense, notably 3 (standard protocols), 6 (secrets born on the host), 8 (per binding credentials), 9 (no image or host names in service code), 11 (client IP by proxy hop count, via the sdk's `httpkit`) and 12 (origin from configuration only).

### Scaffold pins (recorded 2026-11-22)

What the scaffold actually builds and runs with. Bump any of these on purpose, and update this table in the same change.

| What | Pin |
|---|---|
| Go | language `go 1.26.8`, toolchain `go1.27.3` |
| ModularPlatform sdk | `v0.0.0-20261120145312-1eea5a896d43` (commit `1eea5a8`); `sdkgen` from the same module via `tool` |
| sdk health | `/healthz` and `/readyz` on `:8080`; `healthcheck` subcommand probes `/readyz` |
| River | `riverqueue/river` and `riverdriver/riverpgxv5` `v0.51.2` (`control` only; `runner` adds it with #3 or #7) |
| pgx | `jackc/pgx/v5` `v5.13.0` in `control`, `v5.12.0` in `runner` (see Follow-up) |
| goose | `pressly/goose/v3` `v3.30.1` |
| go-github | not added yet; feature #4 pins it with `ghinstallation/v2` |
| Build base | `golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18` |
| Runtime base | `gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab` |
| Postgres | `postgres:18.6-alpine3.24`, data volume at `/var/lib/postgresql` (the Postgres 18 layout) |
| Caddy | `caddy:2.11.7-alpine` |
| Socket proxy | `wollomatic/socket-proxy:1.14.0`, health check mode present |
| Proxy check client | `curlimages/curl:8.23.0` |

### Where the scaffold differs from the first draft (recorded 2026-11-22)

The sections above already describe the scaffold as built. These six points changed against the first draft of this spec, and each one is accepted as the decision.

| # | First draft said | Scaffold does | Why it stays |
|---|---|---|---|
| 1 | Separate catalog keys for DB host, port, name, user, password file | A `uses: postgresql` binding (`ADP_DB_URL` without credentials, plus name, user, password file, sslmode), typed by a copied `catalog/capabilities/postgresql.yaml` | This is how ModularPlatform modules get Postgres, so Slice 7 binds the real capability instead of rewriting config |
| 2 | `control` on `:8080` from a listen key, `runner` health on `:8081` | Both on `:8080`, no listen key | The sdk fixes the plain listener at `:8080`; each service is its own container, so the same port never clashes |
| 3 | Base images pinned by tag, nothing pinned by digest | Build stage bases pinned by tag and digest; stock images by tag | Same pins as ModularPlatform's own Dockerfiles; a digest keeps a rebuild on the same host from silently changing its base |
| 4 | Only `runner` may reach the socket proxy | `-allowfrom=runner,proxy-check`; unflagged methods answer 405 | AC-3 needs a probe client on the `engine` network; `proxy-check` exists only under the `checks` profile while `task check-proxy` runs |
| 5 | `pg_isready -U adp_owner -d adp` | Same, over TCP (`-h 127.0.0.1`) | `migrate` waits on `postgres: service_healthy`; over the socket the check passes during first init, so `migrate` could start against a server that is about to restart |
| 6 | No container hardening named | Every built service and the proxy run `read_only`, `cap_drop: [ALL]`, `no-new-privileges`, with a `/tmp` tmpfs; Caddy the same plus `NET_BIND_SERVICE`, HTTP/1.1 and HTTP/2 only, host ports overridable with `ADP_HTTP_PORT` and `ADP_HTTPS_PORT` for local runs | Least privilege at no cost to any AC; the port override never applies in production |

## Requirements

Scaffold acceptance criteria, derived from the scope's "Done when" for feature #1. `/develop stack & architecture` builds to these, and `/check verify` checks them.

- **AC-1**: After `task setup` (runs `deploy/compose/setup.sh` with `sudo`) and `task up` (`docker compose -f deploy/compose/compose.yaml up`), `postgres`, `socket-proxy`, `control`, `runner` and `caddy` pass the checks in *Health checks*, and `migrate` exits 0 before `control` and `runner` start. On a host without `runsc`, `task setup` stops early with a hint.
- **AC-2**: Only `caddy` publishes host ports (80, 443). `https://$ADP_PUBLIC_HOST/healthz` answers 200 through Caddy (with `--cacert` for a local host), `/webhooks/github` answers 405 to anything but `POST`, any other path answers 404 at Caddy, and `control` cannot be reached on a host port.
- **AC-3**: When `GET /info` does not list `runsc`, `runner` logs `runsc runtime not registered; run setup.sh` and exits 1. `task check-proxy` (a throwaway curl container on the `engine` network) shows a denied call such as `POST /v1.47/containers/x/exec` failing with 403 and `GET /_ping` succeeding.
- **AC-4**: `control` and `runner` each connect with their own Postgres role and password file. Neither role can create a table (`CREATE TABLE` fails with permission denied for schema `public`), and both can read and write the River tables.
- **AC-5**: `task check-secrets` finds no secret value in `docker compose config`, in `docker history` of any built image, in any environment variable, or in `docker compose logs`. Each secret file is mode `0400` and owned by the uid of the one service that reads it. Rerunning `setup.sh` keeps existing secrets unchanged.
- **AC-6**: `control` and `runner` each boot on `sdk.Run` from their `catalog/modules/<service>/module.yaml`, and a missing or invalid config value stops the service with a problem line. (`migrate` is a plain binary and is not covered.)
- **AC-7**: `task build` builds the `control` and `runner` images on the host, and `task test` (`go test -race ./...`) passes, including the proxy allowlist table test.

## Consequences

**Positive**:
- The Docker engine (root equivalent) is reachable only from `runner`, and only through a path allowlist that blocks exec, build, pulls and system calls. GitHub credentials never share a process with it.
- One stateful dependency (Postgres). A `pg_dump` plus `run-logs` plus `secrets/` restores the whole platform.
- An idle repo costs zero agent runs and zero tokens: webhooks and a code only reconcile are the only triggers.
- Slice 7 becomes mostly registration: the services already run on `sdk.Run` with catalog entries in the platform's format.

**Negative / tradeoffs**:
- The allowlist does not stop a compromised `runner` from creating a privileged container with host mounts, which is root on the host. Until #5 decides on body filtering, only `runner`'s own code stands in the way.
- Receiving webhooks needs a public DNS name with ports 80 and 443 reachable, or a tunnel overlay. A box behind CGNAT needs the tunnel from day one.
- `setup.sh` needs `sudo` once, to give each secret file to the uid of the container that reads it.
- This repo now depends on ModularPlatform's sdk. A breaking sdk change means a coordinated bump here, and the untagged pseudo version pin is opaque until `sdk/` tags exist.
- gVisor adds syscall and I/O overhead to every run (noticeable on large `git` checkouts and test suites), and some syscalls or network features may not work inside a sandbox. You have to install `runsc` on the host.
- Building images on the host means the box needs network access to the Go module proxy and base image registries at build time. Images are not reproducible across hosts: only the two build stage bases are pinned by digest, and Go modules plus stock images resolve by version and tag. Module entries in Slice 7 name an image, so a registry (or local tags the managed platform accepts) is needed by then.
- River is still before 1.0. Upgrades may change its API, so it stays behind an interface and pinned.
- Run logs on a local volume are lost with the host unless you back them up.
- Three Postgres roles and a socket proxy allowlist are extra wiring to keep correct, and each new runner need means an allowlist edit.

**Neutral**:
- No admin web app or admin sign in exists until features #10 and #11; Release 1 is driven from GitHub alone.
- Postgres TLS is off on the internal network for now.
- Feature #2 (coding standards and tooling) adds lint, format, hooks and CI on top of this scaffold.

## Follow-up

- [ ] Ask ModularPlatform to start tagging `sdk/vX.Y.Z`, then switch this repo's pin from a pseudo version to a tag.
- [ ] Before Slice 7, decide how images reach the managed platform (a ghcr.io pipeline like ModularPlatform's, or local tags), since module entries name an image.
- [ ] Feature #5 tightens the socket proxy allowlist and defines the per run sandbox network, egress, resource limits and cleanup.
- [ ] Feature #6 decides how the Claude credential reaches a sandbox (short lived, file mounted, never in env dumps) and which `claude -p` flags and permission mode runs use.
- [ ] Feature #3 names the run result columns `adp_runner` may write, and the grants the `migrate` step applies.
- [ ] Revisit Postgres TLS on the internal network before any GA feature (#4, #5, #12) ships.
- [ ] Root `AGENTS.md` does not exist yet. `/audit` (feature #2) should capture this stack and list the 21 skills installed in `.claude/skills/` (tracked in `skills-lock.json`) in its `## Agent skills` section, with the Go skills in root `## Rules`, and the MCP servers chosen (`github/github-mcp-server`, `crystaldba/postgres-mcp`) on its `MCP servers:` line.
- [ ] Feature #5 decides whether `runner`'s container create calls get body filtering (for example a small shim of your own in front of the proxy) or whether the risk is accepted in writing; proves that `runsc` works with one network per run and controlled egress; and builds the startup sweep by the `adp.managed` label.
- [ ] Features #4 and #7: record each GitHub `X-GitHub-Delivery` ID once (unique key) and enqueue with River's unique job options, so a redelivered webhook does nothing twice.
- [ ] Add `task backup` using `pg_dump` (a file copy of a live `pgdata` is not consistent), and keep a copy of `secrets/` off the box.
- [ ] Both services check River's schema version at start and stop with a clear line when `migrate` has not caught up.
- [x] Pin at scaffold time and record here (done 2026-11-22, see *Scaffold pins*). The go-github major moves to feature #4, which adds the client.
- [ ] Align pgx across both modules (move `runner` to `v5.13.0`, as in `control`). Images build with `GOWORK=off`, so the workspace hides this in `task test` but each image ships its own version.
- [ ] Keep `catalog/capabilities/postgresql.yaml` in step with the sdk pin on every sdk bump.
- [ ] Scope feature #1 still names OpenID Connect sign in and a small admin web, which this spec moves to #10 and #11. Run `/scope` to reword it.
- [ ] Slice 7: the module entries will also need the `oidc`, `postgresql` and `http-ingress` capabilities, so the config shape carries over but each entry still needs those added.
- [ ] Connect the two MCP servers in your own MCP settings (`claude mcp add …`); this spec cannot do that for you.
