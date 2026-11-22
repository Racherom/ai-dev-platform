# Review, stack-architecture (whole feature, no git), 2026-11-22

**Reviewed by**: Sonnet 5.5 (author model not stated)
**Scope**: 38 files, whole feature read directly (not a git repository, so no diff)
**Verdict**: Changes requested

## Summary
The scaffold matches spec 0001 closely. Two services run on the sdk, a one shot migrate runs before both, Postgres arrives as a binding, secrets are files owned by the reading uid, and Caddy is the only published door. `go vet` and `go test -race` pass for both modules, and `docker compose config` parses. The headline issue is the engine trust boundary: the socket proxy allowlist filters paths only, so it does not scope kill, delete, start, inspect or logs to the platform's own containers, and the create call is unfiltered. The spec accepts the create risk in writing, but nothing in the code or tests guards it yet. The remaining findings are small hardening and workflow gaps. No secrets were found in `deploy/compose/.env`, and `.gitignore` covers both `/secrets/` and `/deploy/compose/.env`.

## Major
### 🟠 Engine boundary is weaker than the summary claims, `deploy/compose/compose.yaml:147-149`
**Problem**: The allowlist matches paths only. Any container id or name that fits the character class is accepted for `POST /containers/{id}/kill`, `/start`, `DELETE /containers/{id}` and `GET /containers/{id}/json|logs`. Query strings such as `force=true` are not checked. So a compromised runner can kill or force remove `postgres` or `control`, and can read the inspect output and logs of any container. The create body is also unfiltered, which the spec already records as root on the host.
**Why it matters**: The architecture's main argument is that a runner compromise is contained. Today that rests only on runner's own code, and the spec's caveat covers the create body but not the unscoped kill and delete of platform containers. `engine.go` has no create code yet, so the risk is latent, but nothing enforces the rule "never Privileged, never bind mounts" when #5 adds it.
**Suggested fix**: Keep this tracked as a hard gate for feature #5: no create call ships without either a body filtering shim in front of the proxy or a written acceptance. In the meantime, add to the spec's caveat that kill, delete, start and inspect are not scoped to `adp.managed` containers. When #5 adds the create builder, add a test that fails if the body ever sets Privileged, host binds, or a runtime other than runsc.

## Minor
### 🟡 `task up` can run stale images, `Taskfile.yml:35-38`
**Problem**: `up -d` builds only when an image is missing. After a code change, `task up` starts the old `adp/control:dev` and `adp/runner:dev`, and `migrate` (which reuses the control tag) runs old migrations.
**Why it matters**: Silent drift between source and what runs, and a likely source of "works after task build" confusion.
**Suggested fix**: Add `--build` to `up`, or make `up` depend on `build`.

### 🟡 Password can reach Postgres logs on a failed role statement, `services/control/internal/dbsetup/dbsetup.go:71-74`
**Problem**: The password is sent as a literal inside `CREATE/ALTER ROLE`. The client hides the error text, but Postgres's default `log_min_error_statement=error` writes a failing statement, password included, to the server log. `docker compose logs` then shows it, which AC-5 says must never happen.
**Why it matters**: A failed run (for example a transient error) leaks a live password into logs.
**Suggested fix**: Send a precomputed SCRAM-SHA-256 verifier instead of the clear password, or wrap the statement so statement logging is off for the session. Add a note to `check-secrets.sh` to scan the postgres log after a forced failure.

### 🟡 App roles can edit migration history, `services/control/internal/dbsetup/dbsetup.go:131`
**Problem**: The blanket `GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES` also covers `goose_db_version` and River's `river_migration`. A compromised app role can rewrite which migrations count as applied.
**Why it matters**: It lets a service role influence the next owner run of DDL. It breaks the "apps never hold DDL rights" intent at the edges.
**Suggested fix**: Revoke the app roles' write rights on the two migration bookkeeping tables after the grant step (feature #3 already narrows grants per table, so fold it in there if preferred).

### 🟡 Docker host setting accepts any tcp host, `services/runner/internal/sandbox/engine.go:26-39`
**Problem**: `NewEngine` blocks `unix:` but accepts any `tcp://` or `http://` host. The catalog text says "never the raw socket", but a tcp exposed raw engine would pass.
**Why it matters**: A config slip bypasses the proxy and its allowlist.
**Suggested fix**: Optionally check that the host is the proxy (a catalog pattern on the value, or refuse port 2375 on any name except the compose proxy). Also cap the `/info` body read with `io.LimitReader`.

### 🟡 Tested pgx differs from shipped pgx, `services/runner/go.mod:8`
**Problem**: In the workspace `task test` uses pgx 5.13.0 for both modules, while the runner image builds with `GOWORK=off` and ships 5.12.0.
**Why it matters**: The tested and shipped dependency sets differ. It is already tracked as follow-up and feature #23, so this is a reminder, not new.
**Suggested fix**: Do #23, or add a CI step that runs `GOWORK=off go test ./...` per module.

### 🟡 Allowlist image regex allows dot dot segments, `deploy/compose/compose.yaml:147`
**Problem**: The `images/...` branch allows `/`, `.` and so `a/../../plugins/x/json` matches. The engine cleans paths so the reachable set is small (read only inspect calls), but the regex says more than intended.
**Why it matters**: Defense in depth for a boundary that is meant to be strict.
**Suggested fix**: Disallow `..` in that class, or restrict names to one `/` free segment plus an optional registry part, and add a table case for it.

### 🟡 Caddy is tied to control health, `deploy/compose/compose.yaml:176-178`
**Problem**: Caddy waits for `control` to be healthy. If control is down, no host answers at all, including the 404 and 405 routes, and ACME renewal is delayed on restart.
**Why it matters**: Small, but a control crash loop takes the door down with it.
**Suggested fix**: Use `condition: service_started`, and let Caddy return 502 while control is away.

### 🟡 Secret edge cases in setup, `deploy/compose/setup.sh:66-76,81`
**Problem**: A kept but empty secret file is never replaced, and while peers get a fresh value the empty copy stays empty. Also, deleting only one of the postgres owner password files after first init regenerates a value the already initialised database no longer accepts.
**Why it matters**: Confusing failure that shows up as a migrate login error.
**Suggested fix**: Treat an empty file as missing, and print a warning when a postgres owner password is created while `pgdata` already exists.

### 🟡 Migration and role logic has no integration test, `services/control/internal/dbsetup/dbsetup_test.go`
**Problem**: Only `quoteLiteral` is tested. `Run`, role rotation, the grants and the AC-4 behaviour are checked by hand in verify.md. `migrate.run` and `waitForDB` are also untested.
**Why it matters**: The privilege model is the part most likely to regress, and `/test` is the next step in scope. Test signal is none-yet, so this is noted once here.
**Suggested fix**: Add one test against a throwaway Postgres (testcontainers or the compose service) that asserts CREATE TABLE is denied, River tables are writable, and a second run applies zero changes.

## Nits
- ⚪ `.gitignore:1-3`, add `.claude/settings.local.json`, `.obsidian/`, `*.tmp`, and a pattern for nested `.env` files (for example `**/.env`).
- ⚪ `services/control/.dockerignore:1-2` and `services/runner/.dockerignore:1-2`, add `.env*` and `*.md` so a stray local file never enters the build context.
- ⚪ `services/control/cmd/control/main_test.go:49-58`, `TestBoots` sends a POST to `/healthz` and `/readyz` and then overwrites the recorder with a GET; drop the dead first call.
- ⚪ `deploy/compose/compose.yaml:74`, `ADP_PUBLIC_ORIGIN` ignores `ADP_HTTPS_PORT`, so a local run on a custom port gets a wrong origin.
- ⚪ `Taskfile.yml:18`, `sudo --preserve-env=ADP_PUBLIC_HOST` fails on hosts whose sudoers forbids preserving env; the script already falls back to a prompt, so say so in the desc.
- ⚪ `services/control/internal/dbsetup/dbsetup.go:43`, the `REVOKE ... FROM PUBLIC` also leaves default TEMP rights on the database for app roles; fine for now, revisit in #3.

## Strengths
- Strong privilege split: only `runner` is on `engine`, `control` never sees the socket, `backend` and `engine` are internal, and every built container is read only with all caps dropped.
- Secrets handling is careful: one file per reader, owned by that uid at 0400, never overwritten, passwords never in URLs or env, `sdk.Secret` kept to the statement, and a check script that scans config, history, env and logs without printing values.
- The allowlist test reads the real flags out of `compose.yaml`, so the test and the proxy cannot drift, and it covers anchoring, versions and denied families.
- Migrate flow is idempotent and ordered as the spec says, with role rotation and a lockdown of the public schema; compose waits on healthy Postgres and a completed migrate.
- Runner refusal path is exact (hint line plus `sdk.ErrStart`), and tests drive the real `sdk.Build` for both services.
- Caddy routes are minimal and exact (405 with Allow, 1MB body cap, 404 default), and the spec's six deviations are recorded.

## Test coverage
Test signal is none-yet, noted once. Existing tests are real and meaningful: config refusal for both services, the runsc refusal, engine runtime parsing, the unix socket refusal, migrate settings loading with a no secrets in errors check, and the allowlist table. Gaps: the database role and grant logic (`dbsetup.Run`), `migrate.run`, the Caddy routes, and `setup.sh` and `check-secrets.sh` have no automated test. AC-1 on a host with real `runsc` and the sudo only steps remain unverified, as the verify file already says.
