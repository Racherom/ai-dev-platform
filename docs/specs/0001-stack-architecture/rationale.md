# 0001. Stack and architecture: rationale

The decision record for [index.md](index.md). `/develop` does not read this file.

## Context

> ⚠️ Premise note: three services for one admin looks like premature microservices, a known failure pattern (more to build, deploy and debug for no scale benefit). The split here is not about scale. It exists because one process must hold root equivalent access to the Docker engine, and that process should not also hold your GitHub App key and webhook secret. Keep it at exactly this split, and keep each service a single modular monolith inside (bounded contexts as packages). Add no further services until a real ownership or privilege boundary forces one.

The platform runs coding agents headless against your GitHub repos. It decides in plain code what to run next, starts each run in a throwaway container, and reports back on GitHub. One person (you) operates it on one always on box. Release 1 has no admin web app. GitHub is the whole user interface.

Three forces shape the stack. **Reuse**: your ModularPlatform already settled Go, the sdk, Postgres, Caddy, secrets as files and 12 architecture principles, and this platform must later run *as* a set of ModularPlatform modules (Slice 7). Anything that diverges now is rework then. **Blast radius**: agent runs execute untrusted code (code an LLM wrote, in repos it can change), and starting containers needs Docker engine access, which is root equivalent on the host. **Determinism and cost**: "code decides, agents do". An idle repo must cost zero agent runs, and the same state must always pick the same next task.

Without a recorded stack, features #3 to #9 each pick their own pieces, and the move onto the modular platform becomes a rewrite instead of a registration.

## Options considered

### Option 1: Go services on the ModularPlatform sdk, split by privilege, Postgres only (chosen)

`control` and `runner` in Go on `sdk.Run`, River as the queue in Postgres, the Claude Code CLI in gVisor sandboxes behind a socket proxy, Caddy as the ingress.

**Pros**: maximum reuse of proven code and conventions; one stateful dependency; Docker access isolated; Slice 7 is mostly registration.
**Cons**: coupled to ModularPlatform's sdk release cadence; three DB roles and a proxy allowlist to keep correct; gVisor overhead.

### Option 2: One TypeScript service on the Claude Agent SDK

A single Node service with Octokit, the official Agent SDK (TypeScript or Python only), a Postgres queue, and Docker access in the same process.

**Pros**: richest agent hooks with no CLI parsing; one language with a future web app; fastest to a first demo.
**Cons**: nothing from the ModularPlatform sdk carries over, so a second backend stack to maintain; GitHub keys and Docker access share a process; Slice 7 needs a new module runtime for Node.

### Option 3: Go control plane with NATS JetStream and a REST runner

The same Go services, but events and jobs flow over NATS (ModularPlatform's "NATS later" plan), and `control` calls `runner` over REST.

**Pros**: matches ModularPlatform principle 3 for events exactly; the runner holds no DB credentials; scales to many runners.
**Cons**: a broker before Release 1 needs one; keeping queue state and DB state consistent needs an outbox or similar; more failure cases between two hops for one admin's workload.

### Option 4: One Go binary with the Docker socket mounted

`control` and `runner` merged into one process with the socket mounted directly, as `deployer-docker` does today.

**Pros**: smallest scaffold; no proxy, no second role, no internal queue hop.
**Cons**: one bug or one leaked token gives root on the host *and* your GitHub credentials; hard to split later without redoing config and roles.

## Rationale

Reuse decides the language and runtime. Go on `sdk.Run` with catalog entries in the platform's own format means the Slice 7 move is copying entries, not rewriting config, secrets and health handling. That rules out Option 2, whose real advantage (the Agent SDK) mostly disappears once the agent runs as the Claude Code CLI inside the sandbox: Go only needs to parse one final `result` event (basis: Claude Code headless docs; no official Go Agent SDK exists).

Blast radius decides the shape. Untrusted agent code plus root equivalent engine access is the one risk that could cost you the host, so engine access lives in exactly one process (`runner`), behind an allowlisting proxy, with gVisor as a second wall (basis: least privilege; ModularPlatform principle 8, per binding credentials). That rules out Option 4. Note that the proxy filters paths, not request bodies, which is why runner must build container create bodies from code alone. The proxy still lowers the risk (no exec, build, pulls or system calls), but a compromised runner can reach root through a privileged create; feature #5 owns closing that.

Determinism and operating cost decide the queue. A Postgres queue lets `control` enqueue in the same transaction that changes state, so the run loop reads one consistent source of truth, and an idle system does nothing (basis: transactional enqueue; database backed queue first). NATS (Option 3) is the right protocol once several consumers need events, but today it would be a second stateful system for one admin. River gives retries and stuck job recovery that a hand rolled table would have to reinvent. Its pre 1.0 status is contained by a pin and an interface.

You chose to build images on the host rather than in CI. That keeps Release 1 free of a registry, at the cost of reproducibility and of a decision owed before Slice 7 (recorded in index.md, Follow-up).

## Landscape check (2026-11-21)

From the one web check during the design conversation. Versions are as seen then; re-pin at scaffold time.

- Claude Code headless: `claude -p` with `--output-format json` or `stream-json` (`stream-json` needs `--verbose`). The last line is a `result` event with the result text, session id, usage and `total_cost_usd`. Failures during a run still exit with a result, so check its error fields as well as the exit code. SIGTERM exits 143 with no result, so runner must treat a missing result line as a failure. `--bare` skips hooks, CLAUDE.md and MCP discovery.
- Agent SDK: official in Python and TypeScript only. `anthropic-sdk-go` exists for the raw Messages API (no agent loop).
- gVisor: `runsc install` registers it as a Docker runtime, then `--runtime=runsc`. Kata needs virtualization support on the host, and Firecracker needs a containerd wrapper. Rootless Docker or Podman reduce root on the host, but add no kernel boundary.
- River: v0.51.2 seen, `riverpgxv5` driver on a `pgxpool.Pool`, still before 1.0.
- go-github: v93 seen; ghinstallation v2.19.0 seen (a fork, `j178/ghinstallation`, also exists; use `bradleyfalzon`).
- Webhook ingress: GitHub delivers to any public HTTPS URL and signs with HMAC. Tailscale Funnel (beta, ports 443, 8443, 10000) and Cloudflare Tunnel are the options without an open port.
- Not verified: Sysbox and Kata maintenance status, cloudflared version. (`wollomatic/socket-proxy` was pinned at `1.14.0` at scaffold time, see index.md *Scaffold pins*.)

## Cross check (2026-11-22)

An independent read only review on a different model (Sonnet 5.5) checked the spec for decisions the builder would otherwise invent. You chose to apply every recommended fix. What changed in `index.md`:

- **Public address and HTTPS**: where `<origin>` comes from (`ADP_PUBLIC_HOST` in `.env`), Let's Encrypt vs Caddy's internal CA, and the webhook prerequisite (public name, or a tunnel).
- **Health checks**: a `healthcheck` subcommand per Go binary (distroless has no shell or curl), plus a named check for every other service.
- **Database roles and grants**: `adp_owner` as the bootstrap user, how `migrate` creates the app roles and their passwords, the run order, and the grants River needs.
- **Secret files**: `0600` owned by the host user cannot be read by a container running as uid 65532 or 70, and compose ignores `uid` and `mode` outside Swarm. Now `0400`, owned by the reading uid, one copy per reading service.
- **Scaffold secrets**: no GitHub secrets until #4, so AC-6 does not stop `control` at boot; no password inside a URL.
- **Proxy allowlist**: written in the proxy's regex syntax with the versioned API prefix, list endpoints added for a cleanup sweep, no image pulls, runsc checked through `GET /info`.
- **Security claim**: Consequences now say plainly that a privileged create through the proxy is still root on the host.
- **AC checks**: `task check-proxy` and `task check-secrets` give AC-3 and AC-5 a concrete test.

Findings dropped after checking: a `go` line plus a `toolchain` line in `go.mod` is valid, the 21 skill count matches, and the ticked decision box is correct by the scope's own rules.

## References

**Project sources**:
- ModularPlatform `AGENTS.md` (stack, rules, tooling) and spec 0001 (the 12 architecture principles, glossary)
- ModularPlatform `sdk/AGENTS.md` (`sdk.Run`, `sdkgen`, config and secret rules) and `catalog/modules/{api,postgres,caddy}/module.yaml` (entry format, pinned image tags)
- This repo's `docs/scope/scope.md` (feature #1 "Done when", the "code decides, agents do" principle, Skateboard approach)
- Installed skills: `samber/cc-skills-golang`, `docker/skills`, `github/awesome-copilot@multi-stage-dockerfile`, `supabase/agent-skills`, `metalagman/agent-skills@go-goose`, `hookdeck/webhook-skills@github-webhooks`

**Practices & standards**:
- Least privilege and separating privilege across processes
- Transactional enqueue (job and state change in one transaction); database backed queue first
- Defense in depth for untrusted code: user space kernel sandbox plus API allowlist
- HMAC webhook signature verification
- Monolith first; split only on an ownership or privilege boundary

**Links** (web verified during the landscape check):
- Claude Code headless mode: https://code.claude.com/docs/en/headless
- Claude Agent SDK overview: https://code.claude.com/docs/en/agent-sdk/overview.md
- gVisor with Docker, quick start: https://gvisor.dev/docs/user_guide/quick_start/docker/
- River releases: https://github.com/riverqueue/river/releases
- go-github releases: https://github.com/google/go-github/releases
- ghinstallation releases: https://github.com/bradleyfalzon/ghinstallation/releases
- anthropic-sdk-go releases: https://github.com/anthropics/anthropic-sdk-go/releases
- Tailscale Funnel: https://tailscale.com/kb/1223/funnel
- Docker rootless mode: https://docs.docker.com/engine/security/rootless/
