# AI Dev Platform

A self hosted control plane that runs the Agent Skills workflow headless against your repos and asks for decisions on GitHub. A `docker compose` stack written like ModularPlatform modules, so it can move onto the modular platform later (Slice 7).

## Stack

- **Language / Runtime**: Go, language `go 1.26.8`, toolchain `go1.27.3` (pinned in `go.work` and each `go.mod`)
- **Framework**: standard library `net/http` ServeMux on ModularPlatform's `sdk.Run` (config, secret files, `/healthz`, `/readyz`, shutdown); no web framework
- **Key dependencies**: `jackc/pgx/v5` + sqlc, `pressly/goose/v3` (migrations), `riverqueue/river` (Postgres job queue), `wollomatic/socket-proxy` + gVisor `runsc` (sandboxes), Caddy (single ingress); `google/go-github` arrives with #4
- **Package manager**: Go modules in a `go.work` workspace (`services/control`, `services/runner`); Task for entry points; Docker Compose plus `runsc` on the host
- **Services**: `control` (GitHub, picks the next run), `runner` (the only one that starts containers), one shot `migrate` (in the control image); Postgres 18 is the only stateful service
- Source of truth: [docs/specs/0001-stack-architecture](docs/specs/0001-stack-architecture/index.md); bump a pin only together with its *Scaffold pins* table

## Build approach

**Skateboard** (ship the thinnest usable whole, then grow it) until Release 1 has the platform building itself; **Tracer Bullet** (each slice end to end through every layer) from Slice 2 on. Principle: code decides, agents do.

## Commands

```bash
task setup        # once per host, with sudo: checks runsc, writes deploy/compose/.env and secrets/
task generate     # sdkgen config getters from catalog/modules/*/module.yaml (output committed)
task build        # build the control and runner images
task test         # go test -race on both services
task up / down    # start the stack and wait for healthy / stop it (volumes and secrets stay)
task check-proxy  # probe the socket proxy allowlist; task check-secrets checks secret file modes
```

## Specs

Stored in `docs/specs/`, one folder per spec: `docs/specs/NNNN-title/index.md` (plus `rationale.md`, `verify.md`). Scope lives in `docs/scope/scope.md`, reviews in `docs/reviews/`.

## Rules

- **Clean Architecture, one package per bounded context**: each context lives in `services/<svc>/internal/<context>` (forge, runloop, board, sandbox, agent, logstore). Dependencies point inward: domain types and use cases never import pgx, River, `net/http`, go-github or Docker types. Adapters implement interfaces the context defines (define interfaces where they are consumed) and sit at the edge; `cmd/` wires everything together.
- **Thin use cases, rich entities**: use cases orchestrate; entities and value objects validate themselves when built. Handlers and River workers only parse input, call a use case and map the result. Plain structs cross boundaries, never pgx rows or go-github types.
- **Layout follows the scaffold**: `cmd/<binary>`, `internal/<context>`, `internal/config` (sdkgen output, never hand edited), `migrations/` (goose SQL, embedded, owned by `control`).
- **Config checked at start**: values come only from the sdkgen getters of `catalog/modules/<svc>/module.yaml`; only the sdk reads env vars or `*_FILE` paths; a bad value stops the service. Own names use the `ADP_` prefix. No secret in a URL, env var, image or log.
- **Errors**: return them, wrap with `fmt.Errorf("...: %w", err)`, sentinel or typed errors where callers branch; no panics for expected failures.
- **Logs**: `log/slog` JSON only; never log request bodies or secret values.
- **Docs**: every exported Go identifier has a doc comment; Go idioms, no stutter.
- **Tests**: unit plus integration. Table tests beside the code, `httptest` for handlers, always `-race`; domain and use cases tested without infrastructure mocks, adapters (Postgres, River, Docker engine) tested against the real thing behind a build tag.
- **Sandbox safety**: `runner` builds every container create body in code (never `Privileged`, never host binds, always `runtime: runsc`) and never takes those fields from task or agent input.
- **Commits**: conventional commits (`feat:`, `fix:`, `chore:`) for commits and PR titles.

## Tooling

Chosen here, installed by `/develop tooling` (copy ModularPlatform's setup where it fits):

- **Lint and format**: golangci-lint v2 with ModularPlatform's `.golangci.yml` linter set, gofumpt and goimports; run through `task lint` and `task fmt`, the binary pinned via `go run`.
- **Pre commit**: lefthook formats staged Go files and runs golangci-lint; tests stay in CI. Installed with `task hooks`.
- **CI**: GitHub Actions on PRs and pushes to `main` and `dev`: generate diff check, `task lint`, `task test`, `task build`; actions pinned by SHA. Plus a `Branch rules` check for the PR title and branches.
- **Agent skills**: git tracks only `skills-lock.json`; `.claude/skills/` is ignored and restored by `task skills`.

## Git

- integration: on
- branch prefix: feat/
- commit: end-of-build
- branches (as ModularPlatform spec 0032): `main` holds released code, `dev` the next release. Branch `feat/<name>` from `dev`, PR into `dev` with a conventional commit title, squash merge. Release: PR `dev` into `main`, merge commit, never squash. Hotfix: `hotfix/<name>` from `main`.

## Agent skills

- [golang-project-layout](.claude/skills/golang-project-layout/): `samber/cc-skills-golang`, module and package layout, `go.work`
- [golang-code-style](.claude/skills/golang-code-style/): `samber/cc-skills-golang`, Go style
- [golang-naming](.claude/skills/golang-naming/): `samber/cc-skills-golang`, Go names
- [golang-error-handling](.claude/skills/golang-error-handling/): `samber/cc-skills-golang`, wrapping and error types
- [golang-context](.claude/skills/golang-context/): `samber/cc-skills-golang`, `context.Context` use
- [golang-concurrency](.claude/skills/golang-concurrency/): `samber/cc-skills-golang`, goroutines and sync
- [golang-structs-interfaces](.claude/skills/golang-structs-interfaces/): `samber/cc-skills-golang`, types and interfaces
- [golang-database](.claude/skills/golang-database/): `samber/cc-skills-golang`, pgx, sqlc and transactions
- [golang-security](.claude/skills/golang-security/): `samber/cc-skills-golang`, secure Go
- [golang-observability](.claude/skills/golang-observability/): `samber/cc-skills-golang`, slog logs and health
- [golang-lint](.claude/skills/golang-lint/): `samber/cc-skills-golang`, golangci-lint setup
- [golang-testing](.claude/skills/golang-testing/): `samber/cc-skills-golang`, Go tests
- [golang-dependency-management](.claude/skills/golang-dependency-management/): `samber/cc-skills-golang`, `go.mod` and upgrades
- [golang-continuous-integration](.claude/skills/golang-continuous-integration/): `samber/cc-skills-golang`, CI for Go
- [go-goose](.claude/skills/go-goose/): `metalagman/agent-skills`, goose migrations
- [supabase-postgres-best-practices](.claude/skills/supabase-postgres-best-practices/): `supabase/agent-skills`, Postgres schema and SQL
- [github-webhooks](.claude/skills/github-webhooks/): `hookdeck/webhook-skills`, signed GitHub webhooks (#4)
- [openapi-spec-generation](.claude/skills/openapi-spec-generation/): `wshobson/agents`, OpenAPI contracts (from #10)
- [docker-compose-patterns](.claude/skills/docker-compose-patterns/): `docker/skills`, the compose stack
- [docker-build-strategies](.claude/skills/docker-build-strategies/): `docker/skills`, image builds and caching
- [multi-stage-dockerfile](.claude/skills/multi-stage-dockerfile/): `github/awesome-copilot`, multi stage Dockerfiles
- [github-actions-templates](.claude/skills/github-actions-templates/): `wshobson/agents`, GitHub Actions workflow files (CI, Branch rules)
- [docs](.claude/skills/docs/): `minio/skills`, plain language rules (ISO 24495-1) for READMEs, guides and doc comments

Declined: caddy-reverse-proxy, lefthook skill, sqlc skill, taskfile skill, golang-cli, Postgres MCP, Caddy MCP · MCP servers: `github` (connected)

## Context files

<!-- Nested AGENTS.md files are listed here as they are created -->

_Drafted by /audit from the repo, worth a quick human pass. Edit freely: once a line stops matching this draft, later runs treat it as curated and will flag rather than overwrite it._
