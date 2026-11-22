#!/usr/bin/env bash
# `task check-secrets` (spec 0001, AC-5). Run with sudo: it has to read the secret files. It never
# prints a value; a hit names only the file and where it showed up.
#   1. no secret value in `docker compose config`, image history, container env or compose logs
#   2. each secret file is mode 0400 and owned by the one uid that reads it
#   3. rerunning setup.sh keeps every secret unchanged
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
secrets="$root/secrets"
compose=(docker compose -f "$here/compose.yaml")
fail=0

[ "$(id -u)" -eq 0 ] || { echo "check-secrets: run me with sudo (task check-secrets)" >&2; exit 1; }

declare -A owner=(
	[postgres/adp_owner_password]=70
	[migrate/adp_owner_password]=65532
	[migrate/adp_control_password]=65532
	[migrate/adp_runner_password]=65532
	[control/db_password]=65532
	[runner/db_password]=65532
)

# 2. mode and owner
for f in "${!owner[@]}"; do
	p="$secrets/$f"
	if [ ! -f "$p" ]; then echo "FAIL  missing secrets/$f"; fail=1; continue; fi
	got=$(stat -c '%a %u' "$p")
	if [ "$got" = "400 ${owner[$f]}" ]; then echo "ok    secrets/$f is 0400, uid ${owner[$f]}"
	else echo "FAIL  secrets/$f is '$got', want '400 ${owner[$f]}'"; fail=1; fi
done

# 1. leaks: gather every place a value could surface, then search it for each value
haystack=$(mktemp)
trap 'rm -f "$haystack"' EXIT
{
	echo "== compose config"; "${compose[@]}" config
	for img in adp/control:dev adp/runner:dev; do
		echo "== history $img"; docker history --no-trunc --format '{{.CreatedBy}}' "$img"
		echo "== image env $img"; docker image inspect --format '{{json .Config.Env}}' "$img"
	done
	for id in $("${compose[@]}" ps -aq); do
		echo "== env $id"; docker inspect --format '{{json .Config.Env}}' "$id"
	done
	echo "== logs"; "${compose[@]}" logs --no-color
} >"$haystack" 2>&1
for f in "${!owner[@]}"; do
	[ -f "$secrets/$f" ] || continue
	if grep -qF -- "$(cat "$secrets/$f")" "$haystack"; then
		echo "FAIL  the value of secrets/$f shows up in compose config, an image, an env or the logs"
		fail=1
	else
		echo "ok    secrets/$f appears nowhere"
	fi
done

# 3. rerun keeps every value
before=$(cd "$secrets" && sha256sum $(printf '%s ' "${!owner[@]}" | tr ' ' '\n' | sort))
"$here/setup.sh" >/dev/null
after=$(cd "$secrets" && sha256sum $(printf '%s ' "${!owner[@]}" | tr ' ' '\n' | sort))
if [ "$before" = "$after" ]; then echo "ok    rerunning setup.sh kept every secret"
else echo "FAIL  rerunning setup.sh changed a secret"; fail=1; fi

exit $fail
