# Scope: AI Dev Platform

A self hosted control plane, in the spirit of Paperclip, that runs the Agent Skills workflow (`/scope`, `/architect`, `/develop`, `/check`, `/test`, `/sync`) headless against your repos. When an agent needs a decision, it asks on GitHub and keeps working on everything else. Built for you as the single admin first. It starts as a `docker compose` stack that follows your modular platform's conventions, and moves onto the modular platform once both are ready.

**Build approach:** Skateboard (ship the thinnest usable whole, then grow it) until Release 1 has the platform building itself; Tracer Bullet (each slice end to end through every layer, one thin strand at a time) from Slice 2 on.
**Workflow:** Beta (after `/develop`: `/check verify`, then `/test`). The project default level of rigor. `/architect` is the recommended first stop for a feature with a real decision, but skippable when you already know the build. Features that hold credentials or run agent code carry `· GA`.
**Principle: code decides, agents do.** No agent ever starts just to look for work. Plain code finds what work exists, what is unblocked and what goes first, and starts an agent only for a real task. Where judgment is truly needed to place work (user issues), a small cheap agent classifies it once, and plain code takes over again.

_These are recommendations to keep your build orderly, not requirements. Skip anything that does not fit: if you already know how to build a feature, use `/develop` and skip `/architect`. You decide when a feature is `done`._

## At a glance

| # | Feature | Phase | Status |
|---|---------|-------|--------|
| 1 | Stack & architecture | Foundation | in-progress |
| 2 | Coding standards & tooling | Foundation | planned |
| 3 | Data model | Foundation | planned |
| 23 | Hardening | Foundation | planned |
| 24 | Internal Postgres TLS | Release 1 | planned |
| 26 | Internal service TLS | Release 1 | planned |
| 4 | GitHub connection & repo onboarding | Release 1 | planned |
| 5 | Run sandbox | Release 1 | planned |
| 6 | Claude agent adapter | Release 1 | planned |
| 7 | Run loop | Release 1 | planned |
| 8 | Board questions on GitHub | Release 1 | planned |
| 25 | Backup and restore | Release 1 | planned |
| 9 | Feature PRs | Release 1 | planned |
| 10 | Admin sign in | Slice 2 | planned |
| 11 | Admin console foundation | Slice 2 | planned |
| 12 | Model connections: Claude and OpenRouter | Slice 2 | planned |
| 13 | Manual task routing | Slice 2 | planned |
| 14 | Usage tracking | Slice 3 | planned |
| 15 | Quotas and limits | Slice 3 | planned |
| 16 | Scope and spec mirror | Slice 4 | planned |
| 17 | New project from the board | Slice 5 | planned |
| 18 | Automatic routing recommendation | Slice 6 | planned |
| 19 | CI failures become fix tasks | Slice 6 | planned |
| 20 | Skill profiles per task type | Slice 6 | planned |
| 22 | User issue triage | Slice 6 | planned |
| 21 | Modular platform modules | Slice 7 | planned |

## Foundations

### 1. Stack & architecture · in-progress
Decide the services and how they fit together (`control` talks to GitHub and picks the next run, `runner` alone starts sandboxed containers, a one shot `migrate`), leaning on your modular platform's sdk so shared libraries are reused. Scaffold a `docker compose` stack that already follows the platform's conventions (Postgres as a capability binding, secrets delivered as files, one HTTP ingress), so the later move is mostly writing module entries. Admin sign in and the admin web move to #10 and #11.
**Done when:** the architecture is recorded in a spec, and `task up` boots the empty services behind one ingress with a passing build, on a host where gVisor (`runsc`) is really registered.
spec [0001](../specs/0001-stack-architecture/index.md) · code in `services/`, `libs/`, `deploy/compose/`, `catalog/`
- [x] Decide the stack (spec): `/architect stack & architecture`
- [x] Scaffold from the decision: `/develop stack & architecture`
- [ ] Verify it: `/check verify stack & architecture` (AC-1 still owed on a host with `runsc`)
- [ ] Test it: `/test stack & architecture`

### 2. Coding standards & tooling
Capture conventions and tooling from the real scaffold, reusing what already works in your modular platform (task runner, conventional commit PR titles, branch rules), then install lint, format, pre commit hooks and CI.
**Done when:** root `AGENTS.md` reflects the real stack, and lint, format, pre commit and CI run clean.
- [ ] Capture conventions + tooling choices: `/audit`
- [ ] Install the tooling: `/develop tooling`
- [ ] Check it runs clean: `/test`

### 3. Data model · needs a decision
Only the entities Release 1 needs: project, repository, task (a runnable box from the repo's scope or a spec's build plan), run, question and answer, and each run's link to its commit and PR. Models, routing rules and usage join in their own slices.
**Done when:** the entities carry Release 1 end to end, the run result columns `adp_runner` may write and the grants `migrate` applies are named, and later slices (routing, usage, limits) add tables without a breaking migration.
- [ ] Design it (spec): `/architect data model`

### 23. Hardening
Small fixes the scaffold left behind: align pgx in `runner` with `control` (`v5.13.0`), and have both services check River's schema version at start.
**Done when:** both modules build with the same pgx version, and a service started before `migrate` caught up stops with one clear line instead of failing later.
- [ ] Build it: `/develop hardening`

## Release 1: The platform builds itself

The thinnest usable whole. You point the platform at this repo and start a run from GitHub. The agent works in a fresh container, asks on GitHub when it needs a decision, keeps going where it can, commits each run atomically, and opens one PR per feature. Release 1 is done when the platform builds a real feature of this scope and you merge its PR.

### 24. Internal Postgres TLS · GA · needs a decision
Encrypt traffic between the services and Postgres on the internal network before the first feature that holds credentials (#4, #5, #12) ships.
**Done when:** `control`, `runner` and `migrate` connect only over verified TLS, a plain connection is refused, and the certificates come from the stack's own setup with no manual step.
- [ ] Design it (spec): `/architect internal Postgres TLS`

### 26. Internal service TLS · GA · needs a decision · from spec 0001
Encrypt the REST calls between services (`forge` to `scheduler`, `scheduler` to `runner`), which carry binding tokens in plain text on the internal networks until now. Builds on #24's certificates.
**Done when:** every service to service call runs over verified TLS from the stack's own CA, a plain HTTP call is refused, and no manual certificate step is needed.
- [ ] Design it (spec): `/architect internal service TLS`

### 4. GitHub connection & repo onboarding · GA · needs a decision
Connect one GitHub repo with narrowly scoped write access, behind a thin forge interface so GitLab can follow later, and pick up the repo's existing scope, specs and `AGENTS.md`.
**Done when:** you register this repo, the platform reads its scope features and spec build plans, its credentials can touch only the repos you allowed, and each webhook delivery is recorded once, so a redelivery does nothing twice.
- [ ] Design it (spec): `/architect GitHub connection & repo onboarding`

### 5. Run sandbox · GA · needs a decision
Every run starts in a throwaway container with only its repo checkout at the exact branch and commit, the pinned skill set, and short lived scoped credentials. Nothing survives the run except its commit, log and outcome.
**Done when:** two runs share no files or credentials, a run cannot reach the host or other repos (including through a privileged container create, or that risk is accepted in writing), each run gets its own network with controlled egress under `runsc`, and the container is gone after the run (a startup sweep clears leftovers) while its log stays.
- [ ] Design it (spec): `/architect run sandbox`

### 6. Claude agent adapter · needs a decision
Run a coding agent CLI headless with the workflow skills on your Claude connection, and turn its result into a structured outcome (done, question asked, failed) with its log and token usage.
**Done when:** the platform starts a skill command (for example `/develop <feature>`) headless in the sandbox with a short lived, file mounted Claude credential, and always gets back an outcome, a log and token usage, also on a crash or timeout.
- [ ] Design it (spec): `/architect Claude agent adapter`

### 7. Run loop · needs a decision
Deterministic code around every agent run, with no AI heartbeat: code alone notices new work (a GitHub event, a changed scope, an answered question), finds the unblocked tasks in the scope and spec build plans, and orders them by fixed rules (dependencies, phase, scope order). Only then does it check out the target, inject only the relevant spec and files, run the agent, and stage and commit atomically with a conventional commit message and update state.
**Done when:** you start the loop from GitHub, it picks the right next task, each run lands exactly one well named commit, a blocked task is skipped while independent ones go ahead, the same state always yields the same next task, and an idle repo costs zero agent runs and zero tokens, and a redelivered event never starts a second run.
- [ ] Design it (spec): `/architect run loop`

### 8. Board questions on GitHub · needs a decision
When a skill would stop for a decision (an `/architect` question, a `/develop` gate), the run posts the decision panel as a GitHub question with its options and recommendation instead of waiting. Your answer triggers a fresh run with a clean context that includes it.
**Done when:** a question appears on GitHub with its options, you answer it from your phone, a new clean run picks up the answer, and tasks not waiting on it kept running meanwhile.
- [ ] Design it (spec): `/architect board questions on GitHub`

### 25. Backup and restore
A `task backup` that takes a consistent `pg_dump` and keeps a copy of `secrets/` and the run logs off the box, so the platform survives losing its host before it starts committing real work.
**Done when:** `task backup` writes a dated dump plus secrets and logs to a place off the box, and a restore onto a fresh host brings back the same projects, runs and secrets.
- [ ] Build it: `/develop backup and restore`

### 9. Feature PRs · needs a decision
Each scope feature gets its own branch. Once its build milestones are done, the platform opens a PR, and merging it is how you accept the feature.
**Done when:** a finished feature arrives as one PR with its atomic commits and a summary drawn from the real diff, the repo's own CI runs on it, and merging records the acceptance on the feature in the scope.
- [ ] Design it (spec): `/architect feature PRs`

## Slice 2: Models and routing

From here on, Tracer Bullet: each slice runs end to end, and the platform builds these slices on itself.

### 10. Admin sign in · needs a decision
For now, sign in to the admin console with GitHub as the identity provider (you already live there, and the platform already connects to it). Keep the console speaking OpenID Connect, so moving to your modular platform's own identity provider later is a configuration change, not new auth code. Heads up for the spec: GitHub's user sign in is plain OAuth, not full OpenID Connect (no ID token, no discovery), so the spec decides how to bridge that gap.
**Done when:** only your allowlisted GitHub account can sign in, every admin action is recorded with who made it, and pointing sign in at a different OpenID Connect provider needs no code change.
- [ ] Design it (spec): `/architect admin sign in`

### 11. Admin console foundation · needs a decision
A small web console for admin work only (models, routing, runs, limits); day to day work stays on GitHub. Reuse your modular platform's design system where it fits.
**Done when:** `design.md` covers the console, base components handle focus and keyboard, and a run list shows each run with its outcome, log and links.
- [ ] Design it (spec): `/architect admin console foundation`

### 12. Model connections: Claude and OpenRouter · GA · needs a decision
Connect Claude and an OpenRouter key as model sources, stored encrypted and handed to runs only as short lived scoped values, plus an agent path that can run on OpenRouter models.
**Done when:** you add, test and remove both connections in the console, a run can use either one, and no key ever shows in a log, an environment dump or a commit.
- [ ] Design it (spec): `/architect model connections`

### 13. Manual task routing
Assign a model per task type (scope, architect, develop, check, test, review, document), with your override per project or feature.
**Done when:** each run uses the model its task type maps to, your override wins, and the run report names the model that ran.
- [ ] Build it: `/develop manual task routing`

## Slice 3: Usage and limits

### 14. Usage tracking · needs a decision
Record tokens and cost of every run by model, project and task type, so you see where spend goes.
**Done when:** every run shows its tokens and cost, the console totals them by model and project per day, and a run with unknown pricing is flagged, never counted as zero.
- [ ] Design it (spec): `/architect usage tracking`

### 15. Quotas and limits · needs a decision
Read remaining quota and rate limits from providers that expose them, and enforce your hard and soft limits per model and project (per user once there are more users).
**Done when:** crossing a soft limit warns you on GitHub, a hard limit stops new runs and queues them, and the platform never starts a run it already knows would break a limit.
- [ ] Design it (spec): `/architect quotas and limits`

## Slice 4: Scope board on GitHub

### 16. Scope and spec mirror · needs a decision
Mirror the repo's scope and specs to GitHub so you follow progress natively: features as issues, milestones as sub issues, dependencies as blocked by links, phase and status on a Projects board. The repo files stay the source of truth.
**Done when:** a scope change in the repo shows on the board within minutes, dependencies show as blocked by links, and the board never silently drifts from the files.
- [ ] Design it (spec): `/architect scope and spec mirror`

## Slice 5: Scope a new project on the go

### 17. New project from the board · needs a decision
Start a new project from a short idea on GitHub: the platform creates the repo, runs `/scope` headless, and every decision panel arrives as a board question you answer on the go until the scope is committed.
**Done when:** from your phone you start a project from an idea, answer its scoping questions, and find a committed scope ready for its first `/architect` run.
- [ ] Design it (spec): `/architect new project from the board`

## Slice 6: Smarter runs

### 18. Automatic routing recommendation · needs a decision
A setup run weighs your connected models by capability, context window, cost and remaining limits, then recommends (or applies, if you allow it) the model per task type. Your manual override always wins.
**Done when:** after you add a model, you get a routing recommendation with a short reason per task type, and applying it changes routing only where you have no override.
- [ ] Design it (spec): `/architect automatic routing recommendation`

### 19. CI failures become fix tasks · needs a decision
Read the repo's CI results on a run's commit or PR. A failing build, lint or test becomes a follow up fix task carrying the structured error log, so no model judges correctness on its own.
**Done when:** a red check creates exactly one fix task with the failing output, its run fixes it on the same branch, and the PR turns green without you stepping in.
- [ ] Design it (spec): `/architect CI failures become fix tasks`

### 20. Skill profiles per task type · needs a decision
Each task type gets a pinned skill set: the workflow skills plus a small curated list of extras (ECC candidates: `verification-loop`, `conventional-commit-review`, `git-workflow`), locked per project.
**Done when:** a run loads only its task type's skills at the locked versions, and changing a profile is one reviewed change in the repo.
- [ ] Design it (spec): `/architect skill profiles per task type`

### 22. User issue triage · needs a decision
When a user files a GitHub issue, code picks it up without any agent polling. A small cheap model reads it once and places it: task type, which model it goes to, and which scope feature it belongs to (or a new one). From there it is an ordinary task the run loop orders by its fixed rules.
**Done when:** a new user issue gets exactly one triage run, its answer is limited to the fixed set of task types, models and features, an unclear or unsafe issue is sent to you as a board question instead of being guessed, and no triage run happens without a new or edited issue.
- [ ] Design it (spec): `/architect user issue triage`

## Slice 7: Move onto the modular platform

Starts when both sides are ready: this platform is good enough to depend on, and it has pushed your modular platform far enough to host it.

### 21. Modular platform modules · needs a decision
Describe each service as a module entry that uses the platform's `oidc`, `postgresql` and `http-ingress` capabilities, and run through managed mode instead of the stack's own compose file. Admin sign in switches from GitHub to the platform's own identity provider here.
**Done when:** images reach the managed platform by a decided path (a registry pipeline or local tags), the managed platform deploys this platform from its graph, sign in moves to the platform's identity provider by configuration alone, data carries over, and the run sandbox works within the platform's host access rules (or the platform gained a safe way to allow it).
- [ ] Design it (spec): `/architect modular platform modules`

## Deferred
Out of scope for the current build pass, kept so the plan stays honest.
- **Existing repo audit**: onboard a repo that does not use the workflow yet by running an automated audit first · needs a decision
- **GitLab support**: a second forge behind the same interface · needs a decision
- **Parallel runs**: several independent runs at once, each in its own isolated workspace (ECC candidate: `team-agent-orchestration`) · needs a decision
- **More model sources**: direct OpenAI and other APIs, local engines like Ollama or vLLM · needs a decision
- **Own agent loop**: call model APIs directly for cheap or local tasks · needs a decision
- **Team users and roles**: more users, roles and per user limits · needs a decision · GA
- **Least privilege skills and MCP gateway**: agents request extra skills or tools, one central MCP gateway enforces per request limits · needs a decision · GA
- **Shadow learning**: models learn your decision style from your answers, reviews and ticket calls (ECC candidate: `continuous-learning-v2` instincts) · needs a decision
- **Consensus council**: three models propose, vote, merge overlapping findings and escalate splits to you (ECC candidate: `council`) · needs a decision · GA
- **Dreaming mode**: idle or scheduled ideation runs that post proposals to the board and stay within budget · needs a decision
- **Own web board**: only if GitHub cannot carry a day to day workflow · needs a decision

## Legend

**The decision box.** Every feature carries exactly one, the sub task whose label ends with `(spec)`. Its wording varies (`Design it (spec)` normally, `Decide the stack (spec)` on Stack & architecture), so skills locate it by that `(spec)` suffix, never by an exact label. Every other box is an execution box, and `/architect` never ticks one.

**Feature lifecycle**: the scope updates as a feature moves; each row is what it shows and who sets it:

| State | Set by | The feature shows |
|---|---|---|
| `planned` · needs a decision | `/scope` | one box: `Design it (spec): /architect <feature>` |
| `in-progress` (designed) | **`/architect` at spec capture** | `Design it` ticked; spec linked; `Build it: /develop <feature>` + **2 to 5 milestones**; the tier's closing boxes (`Verify it` Alpha+, `Test it` Beta+, `Review it` + `Document it` GA); any surfaced follow up enrolled |
| `in-progress` (building) | `/develop` | milestone boxes tick one by one; code pointer filled |
| `in-progress` (verified) | `/check verify` | `Build it` + milestones ticked; `Verify it` ticked |
| `done` | **you, when you decide it is** (any skill sets it when you say so); `/sync` reconciles | boxes you ran ticked, skipped ones marked skipped; the tier's last stage (`Prototype` after `/develop`; `Alpha` after `/check verify`; `Beta`/`GA` after `/test`) is the suggested point to call it done; `/sync` captures conventions |

- **Next step** = the first unticked box (always a command or a tracked milestone).
- **needs a decision** = run `/architect` first; otherwise straight to `/develop` (or `/audit` for standards & tooling). The tag drops once the spec is captured.
- **Atomic build tasks live in the spec's `## Build plan`, not here**: the scope carries only the milestone rollup.
- **Status** `planned`, then `in-progress`, then `done`, plus `existing` (predates the workflow) and `dropped` (taken out of scope, kept for history).
- **Approach tag** beside a heading (e.g. `· Facade`) overrides the project default for that feature; no tag means it inherits it.
- **Workflow tier tag** beside a heading (e.g. `· GA`, `· Prototype`) sets that one feature's rigor above or below the project default; no tag inherits the default. It decides the feature's check boxes and each skill's next suggestion.
- **Workflow** (header line) is the project default, what runs after `/develop`: **Prototype** = nothing (trust develop's own build time self check); **Alpha** = `/check verify`; **Beta** = `/check verify` then `/test`; **GA** = adds a fresh model `/check review` then `/document`. A feature built on an unratified decision (an `Assumed` spec) stays flagged, but that never blocks `done`.
- **Pointer line** (`spec <n> · code in <path>`): the spec link added by `/architect`, the code path by `/develop`.
