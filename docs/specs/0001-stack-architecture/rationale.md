# 0001. Stack and architecture: rationale

The decision record for [index.md](index.md). `/develop` does not read this file.

## Context

> ⚠️ Premise note: microservices for one admin on one box is a known failure pattern. Each extra service costs a contract, a deploy unit, a database, retries and a new class of partial failure, and none of that buys scale this workload needs. The first version of this spec said so and stopped at two services. This revision goes further for two reasons you named: each service should *be* a ModularPlatform module, and parts should deploy independently. Those are real forces, but they only justify a split where a service also owns something real. So every service here sits on an ownership or privilege boundary (a credential, root equivalent engine access, or the run state that must hold no credential at all). Bounded contexts that do not own such a boundary (board, logstore, agent) stay packages inside a service. Add no service that cannot name its boundary.

The platform runs coding agents headless against your GitHub repos. It decides in plain code what to run next, starts each run in a throwaway container, and reports back on GitHub. One person (you) operates it on one always on box. Release 1 has no admin web app. GitHub is the whole user interface.

Four forces shape the stack. **Reuse and fidelity**: your ModularPlatform already settled Go, the sdk, Postgres, Caddy, secrets as files and 12 architecture principles, and this platform must later run *as* a set of ModularPlatform modules (Slice 7). On the platform a module gets its own database from a provisioner (principle 4), calls other modules over REST with a credential per binding (principles 3 and 8), and the platform's checks refuse any binding cycle. Anything that diverges now is rework then. **Blast radius**: agent runs execute untrusted code, starting containers needs root equivalent Docker engine access, and GitHub and model keys are worth stealing. **Determinism and cost**: "code decides, agents do". An idle repo must cost zero agent runs, and the same state must always pick the same next task. **Independent change**: you want to change or redeploy one part (for example the runner) without touching the others.

The first version of this spec (2026-11-21) balanced the first three forces with two services, `control` and `runner`, sharing one database and one River queue. The scaffold was built and partly verified against it (17 Go files, empty services, a baseline migration). That layout puts GitHub keys, the run loop and the board in one process, couples services through shared tables, and has no binding between the services the platform could check, so it serves the fourth force and the module fidelity part of the first one poorly.

Without revising now, features #3, #4 and #7 build on the shared database and the single `control` process, and splitting them later means dividing live data and rewriting every cross context call.

## Options considered (revision, 2026-11-22)

### Option 1: Keep two services split by privilege (the first decision)

`control` (GitHub, run loop, board) and `runner` (engine), one shared database with per service roles, one River queue between them.

**Pros**: already built; the least code and wiring; transactions can span the whole run state; a function call instead of a contract inside `control`.
**Cons**: GitHub keys share a process with the run loop and board; services are coupled through tables and a shared queue, so neither deploys without the other's schema; no binding the platform can check, so Slice 7 still has to invent the service to service contract.

### Option 2: Modular services cut along ownership boundaries, database per service, REST bindings without cycles (chosen)

`forge`, `scheduler`, `runner` in Release 1 and `models` at #12, each a module with its own database (provisioned like the platform does it), its own migrate step, REST contracts with sdk binding tokens, a transactional outbox on River for pushes, long polling for pulls, and a one way binding graph.

**Pros**: one credential per process; each service is exactly one module entry with a graph the platform accepts; services deploy and migrate on their own; the platform's provisioner, binding auth and `httpclient` are reused as they are.
**Cons**: three services, three databases and four one shot steps for one admin; no cross service transactions, so every operation must be idempotent and flows become eventual; two async patterns (push and pull) to build and test; tokens in plain text on internal networks until internal TLS ships; a per module migrate step the platform does not model yet.

### Option 3: One service per bounded context

`forge`, `runloop`, `board`, `sandbox`, `agent`, `logstore` (and `models`) each as their own service.

**Pros**: the most modular; every context deploys alone; maps one to one to the packages `AGENTS.md` already lists.
**Cons**: six or more services where only three boundaries exist; `board` and `runloop` share run state and would talk constantly; `agent` and `sandbox` are one action (start the CLI in the container) split across a network hop; the most contracts and partial failures for no extra isolation.

### Option 4: The same split, one shared database and a shared River queue

`forge`, `scheduler`, `runner` as separate processes, but all in database `adp` with a role each, handing work over by inserting River jobs into each other's queues.

**Pros**: keeps transactional enqueue across services; no contracts, no outbox, no long polling; the least new code beyond Option 1.
**Cons**: services stay coupled through tables and job argument types; a shared queue is not a protocol the platform can bind, so Slice 7 still rewrites every interaction; per role grants on shared tables are the wiring that drifts first.

## Rationale

Fidelity to the platform decides the shape. You want each service to be a ModularPlatform module, and a module there owns its database, reaches others only through bindings with their own credential, and lives in a graph with no cycles. Option 4 splits processes but keeps the shared database and queue the platform cannot express, and Option 1 does not split at all, so both leave the real migration work for Slice 7 (basis: ModularPlatform spec 0001, principles 3, 4 and 8; spec 0002, the `binding_cycle` rule). Option 2 builds that shape now, while the services are empty and splitting costs a rename and some compose wiring rather than a data migration.

Blast radius decides where the cuts go, and stops them going further. GitHub keys, engine access and model keys are three different things to steal, so each gets the only process that holds it, and the brain (`scheduler`) holds none (basis: least privilege; separating privilege across processes). Option 3 splits along package lines that own nothing, adding network hops between contexts that share state (board and run loop) or form one action (agent and sandbox), which is the premature microservices failure the premise note warns about (basis: monolith first, split only on an ownership or privilege boundary).

Determinism and cost decide the flow. Without a shared database, a cross service handoff cannot be one transaction, so each sender writes its outbox job with its own state change, and River retries delivery with an idempotency key the receiver records in its own transaction. That keeps "the same state picks the same next task" and "a redelivered event never starts a second run" true across services (basis: transactional outbox; idempotency keys). The one way graph means half the flows must be pulled instead of pushed. Long polling keeps an idle system free of agent runs and tokens, and a commit ordered feed counter keeps cursor reads from skipping an item. You chose the mixed direction (`forge` uses `scheduler`, `scheduler` uses `runner`) over making `scheduler` a pure server; that keeps each interaction direct, at the cost of two patterns and of a second runner becoming a second binding (recorded in Consequences). NATS JetStream is the right event protocol on the platform, but before its `nats` capability exists it would be a second stateful system that still needs an outbox to stay consistent with each database; ModularPlatform made the same call for its first managed thread (basis: ModularPlatform spec 0004, managed mode first).

You chose to defer service to service TLS to a separate feature after #24. Tokens therefore cross internal only networks in plain text for a while; every call still needs its token, which keeps a service on the wrong network from calling in.

### Calls made while writing (pick, why, runner up)

- **Provisioning**: a psql script ported from ModularPlatform's `postgresql-provisioner` job, run in the stock Postgres image. Why: identical behavior to what Slice 7 runs, and it already handles reruns, password rotation and foreign names. Runner up: keep the Go `dbsetup` code and extend it per service.
- **Migrate step form**: a separate `cmd/migrate` binary per image, run as a `<service>-migrate` one shot container. Why: keeps DDL out of the long running process's start path and matches today's `migrate`. Runner up: an sdk subcommand, which the sdk reserves for `healthcheck`.
- **Names**: databases `forge`, `scheduler`, `runner`; logins `adp_<service>`. Why: database names read as the owner; the `adp_` prefix keeps logins apart from other tenants of a shared server later. Runner up: `adp_<service>` for both.
- **Clients directory**: compose secrets with target `/run/secrets/clients/<consumer>`. Why: keeps the one file per reader rule and the sdk's TOK rules with no directory bind. Runner up: bind mount the `clients/` folder read only.
- **Feed ordering**: a counter row updated in the item's transaction. Why: commit order equals `seq` order, so a cursor never skips; writers serialize, which costs nothing at this volume. Runner up: identity column plus a "safe horizon" delay.
- **Long poll wait**: at most 15 seconds, client timeout `wait` plus 10 seconds, lease 5 minutes. Why: the sdk server's 15 second read and 20 second write timeouts cap it (found by the cross check; the first draft said 25 seconds, which the sdk would cut off); an idle poll still costs almost nothing. Runner up: extend the deadline per handler with `http.ResponseController`, which works but fights the sdk's defaults.
- **Build context**: repo root with a `replace` to `libs/` per service `go.mod`, built with `GOWORK=off`. Why: each image ships exactly its own `go.mod` versions. Runner up: tag and publish `libs`.
- **Proving bindings before contracts**: `task check-bindings` expects 404 for a right token and 401 otherwise. Why: the sdk checks the token before routing, so auth is provable with no operation. Runner up: wait for #4.

## Options weighed in the first version (2026-11-21)

Kept as history; Option 1 there is Option 1 above.

- **Go services on the ModularPlatform sdk, split by privilege, Postgres only** (chosen then): maximum reuse; coupled to the sdk's release cadence; gVisor overhead.
- **One TypeScript service on the Claude Agent SDK**: richest agent hooks and one language with a future web app; nothing from the sdk carries over, and GitHub keys and Docker access share a process. Rejected because the agent runs as the Claude Code CLI in the sandbox, so Go only parses one final `result` event.
- **Go control plane with NATS JetStream and a REST runner**: matches principle 3 for events exactly; a broker before Release 1 needs one, plus an outbox to stay consistent. Its REST half returns in this revision; its NATS half waits for the platform's `nats` capability.
- **One Go binary with the Docker socket mounted**: smallest scaffold; one bug gives root on the host and your GitHub credentials. Rejected on blast radius.

The first version chose a Postgres queue for transactional enqueue and zero idle cost, and host built images to keep Release 1 free of a registry; both still hold, now per service.

## Landscape check (2026-11-21)

From the one web check during the first design conversation. Versions are as seen then; the scaffold pins in index.md are what is built.

- Claude Code headless: `claude -p` with `--output-format json` or `stream-json` (`stream-json` needs `--verbose`). The last line is a `result` event with the result text, session id, usage and `total_cost_usd`. Failures during a run still exit with a result, so check its error fields as well as the exit code. SIGTERM exits 143 with no result, so runner must treat a missing result line as a failure. `--bare` skips hooks, CLAUDE.md and MCP discovery.
- Agent SDK: official in Python and TypeScript only. `anthropic-sdk-go` exists for the raw Messages API (no agent loop).
- gVisor: `runsc install` registers it as a Docker runtime, then `--runtime=runsc`. Kata needs virtualization support on the host, and Firecracker needs a containerd wrapper. Rootless Docker or Podman reduce root on the host, but add no kernel boundary.
- River: v0.51.2 seen, `riverpgxv5` driver on a `pgxpool.Pool`, still before 1.0.
- go-github: v93 seen; ghinstallation v2.19.0 seen (use `bradleyfalzon`, not the `j178` fork).
- Webhook ingress: GitHub delivers to any public HTTPS URL and signs with HMAC. Tailscale Funnel and Cloudflare Tunnel are the options without an open port.
- Not verified: Sysbox and Kata maintenance status, cloudflared version.

## ModularPlatform check (2026-11-22, this revision)

Read from your local ModularPlatform checkout during the revision; no web fetch.

- `catalog/capabilities/postgresql-provisioner.yaml` yields a `postgresql` capability per consumer: one database owned by one generated login, `CONNECT` revoked from `PUBLIC`, under an advisory lock. `provision.sh` ports this script.
- `catalog/capabilities/renderer.yaml` and `catalog/modules/renderer/module.yaml` show the shape for an own REST capability: `url`, `token` (secret, both directions), optional `ca`; the provider declares `auth: binding-token` and a clients dir env; the consumer declares `client: http`.
- `sdk/CONTRACT.md` TOK-1 to TOK-3: the clients directory holds one file per consumer instance, tokens of at least 32 characters, and an empty or missing directory stops the start. CAT-6: an sdk module has at most one provide. Both exist at the pinned commit `1eea5a8`.
- `libs/check` and spec 0002 refuse a `binding_cycle`; spec 0015 resolved its own cycle by having backends bind to the ingress.
- ModularPlatform spec 0001 principle 3 (REST for requests, NATS for events, PostgreSQL wire for data) and spec 0004 (the first managed thread runs without NATS; `api` polls the deployer).

## Cross check (2026-11-22, first version)

An independent read only review on a different model checked the first version for decisions the builder would otherwise invent. You applied every recommended fix: the public address and HTTPS modes, a `healthcheck` subcommand per binary, the bootstrap user and role creation order, secret files owned by the reading uid, no GitHub secrets until #4, the proxy allowlist in the proxy's regex syntax with no image pulls, a plain statement that a privileged create is still root on the host, and `task check-proxy` and `task check-secrets` as concrete AC checks. All of these carry over to this revision.

## Cross check (2026-11-22, this revision)

A read only review on Sonnet 5.5 checked the revision against the pinned sdk. It confirmed that binding tokens are checked before routing (so 401 vs 404 proves auth with no route) and the clients dir problem path. You applied every recommended fix: long poll `wait` capped at 15 seconds by the sdk's write timeout; `forge` answers 501 on the webhook path so AC-2 can tell it from Caddy; the `binding-check` path, matrix and token mounts; the `provision` env names, marker role and `PGPASSFILE`; a "same value as" column and mount paths for every secret file; the compose build entries and `docker compose up -d --wait`; how AC-2, AC-5 and AC-6 are checked; renderer's `provides` field shape; password rotation only between down and up; readiness following the binding graph; `socket-proxy` in the start order.

## References

**Project sources**:
- ModularPlatform `AGENTS.md` and spec 0001 (the 12 architecture principles), spec 0002 (binding cycles), spec 0004 (managed mode first, no NATS yet), spec 0011 and 0015 (capability contract, backends bind to the ingress)
- ModularPlatform `sdk/AGENTS.md` and `sdk/CONTRACT.md` (`sdk.Run`, `sdkgen`, `auth/`, `httpclient/`, TOK and CAT rules)
- ModularPlatform `catalog/capabilities/{postgresql,postgresql-provisioner,renderer,http}.yaml` and `catalog/modules/{api,renderer,postgres,caddy}/module.yaml`
- This repo's `docs/scope/scope.md` (feature #1, "code decides, agents do", Skateboard then Tracer Bullet), `AGENTS.md` (bounded contexts, Clean Architecture rules), and the scaffold under `services/`, `deploy/compose/`, `catalog/`
- Installed skills: `samber/cc-skills-golang`, `docker/skills`, `github/awesome-copilot@multi-stage-dockerfile`, `supabase/agent-skills`, `metalagman/agent-skills@go-goose`, `hookdeck/webhook-skills@github-webhooks`, `wshobson/agents@openapi-spec-generation`

**Practices & standards**:
- Least privilege and separating privilege across processes
- Monolith first; split only on an ownership or privilege boundary
- Database per service
- Transactional outbox; idempotency keys for retried requests
- Long polling with leases; commit ordered cursors
- Defense in depth for untrusted code: user space kernel sandbox plus API allowlist
- HMAC webhook signature verification

**Links** (web verified during the first landscape check):
- Claude Code headless mode: https://code.claude.com/docs/en/headless
- Claude Agent SDK overview: https://code.claude.com/docs/en/agent-sdk/overview.md
- gVisor with Docker, quick start: https://gvisor.dev/docs/user_guide/quick_start/docker/
- River releases: https://github.com/riverqueue/river/releases
- go-github releases: https://github.com/google/go-github/releases
- ghinstallation releases: https://github.com/bradleyfalzon/ghinstallation/releases
- anthropic-sdk-go releases: https://github.com/anthropics/anthropic-sdk-go/releases
- Tailscale Funnel: https://tailscale.com/kb/1223/funnel
- Docker rootless mode: https://docs.docker.com/engine/security/rootless/
