#!/usr/bin/env bash
# One time host setup for the stack (spec 0001). Run it with sudo, through `task setup`:
#   - stops early unless Docker lists the gVisor runtime (runsc)
#   - writes deploy/compose/.env with ADP_PUBLIC_HOST and DOCKER_GID (neither is a secret)
#   - creates one file per secret per reading service under secrets/<service>/, mode 0400, owned
#     by the uid of the one container that reads it; an existing file is never overwritten
# ADP_PUBLIC_HOST comes from the environment, else from an existing .env, else it asks.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
env_file="$here/.env"
secrets="$root/secrets"

# container uids that read secrets (spec 0001, "Secrets and config per service")
PG_UID=70        # postgres:*-alpine: postgres, provision
APP_UID=65532    # distroless nonroot: forge, scheduler, runner, their migrate steps, binding-check

die() { echo "setup: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run me with sudo (task setup), so each secret can be owned by its reader's uid"
command -v openssl >/dev/null || die "openssl is required"
command -v docker >/dev/null || die "docker is required"

# 1. the sandbox runtime
runtimes=$(docker info --format '{{json .Runtimes}}') || die "docker info failed; is the Docker engine running?"
case "$runtimes" in
*'"runsc"'*) ;;
*) die "Docker does not list the gVisor runtime runsc. Install gVisor and register it:
  https://gvisor.dev/docs/user_guide/install/   (then: sudo runsc install && sudo systemctl restart docker)
and run task setup again." ;;
esac

# 2. .env: public host and the docker group id for the socket proxy
host=${ADP_PUBLIC_HOST:-}
if [ -z "$host" ] && [ -f "$env_file" ]; then
	host=$(sed -n 's/^ADP_PUBLIC_HOST=//p' "$env_file" | tail -n1)
fi
if [ -z "$host" ]; then
	[ -t 0 ] || die "set ADP_PUBLIC_HOST (a DNS name pointing at this box, or localhost for a local run)"
	read -r -p "Public host name (DNS name pointing at this box, or localhost): " host
fi
[[ "$host" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || die "ADP_PUBLIC_HOST '$host' is not a lowercase host name"
docker_gid=$(stat -c %g /var/run/docker.sock) || die "cannot stat /var/run/docker.sock"

{
	echo "# written by deploy/compose/setup.sh; not secret. Rerun setup to change it."
	echo "ADP_PUBLIC_HOST=$host"
	echo "DOCKER_GID=$docker_gid"
	# keep a local port override if you set one (local runs only)
	if [ -f "$env_file" ]; then grep -E '^ADP_HTTPS?_PORT=' "$env_file" || true; fi
} >"$env_file.tmp"
mv "$env_file.tmp" "$env_file"
chmod 0644 "$env_file"
[ -n "${SUDO_UID:-}" ] && chown "$SUDO_UID:${SUDO_GID:-$SUDO_UID}" "$env_file"

# 3. secrets
umask 077
install -d -m 0700 -o 0 -g 0 "$secrets"

# ensure_value <file:uid>...: every listed copy holds the same value. Existing copies are kept;
# a value none of them holds yet is generated once with openssl. Copies that disagree stop setup.
ensure_value() {
	local value="" spec path
	for spec in "$@"; do
		path="$secrets/${spec%%:*}"
		if [ -f "$path" ]; then
			local v
			v=$(cat "$path")
			if [ -n "$value" ] && [ "$v" != "$value" ]; then
				die "secrets disagree: ${spec%%:*} differs from its other copies; fix or remove them by hand"
			fi
			value=$v
		fi
	done
	[ -n "$value" ] || value=$(openssl rand -hex 32)
	for spec in "$@"; do
		path="$secrets/${spec%%:*}"
		local uid=${spec##*:}
		install -d -m 0700 -o 0 -g 0 "$(dirname "$path")"
		if [ ! -f "$path" ]; then
			printf '%s' "$value" >"$path.tmp"
			chown "$uid:$uid" "$path.tmp"
			chmod 0400 "$path.tmp"
			mv "$path.tmp" "$path"
			echo "created  secrets/${spec%%:*}"
		else
			echo "kept     secrets/${spec%%:*}"
		fi
		# ownership and mode are part of the contract, even on a kept file
		chown "$uid:$uid" "$path"
		chmod 0400 "$path"
	done
}

# the owner login, then one login per service, then one token per binding (consumer side first)
ensure_value "postgres/adp_owner_password:$PG_UID" "provision/adp_owner_password:$PG_UID"
for svc in forge scheduler runner; do
	ensure_value "provision/adp_${svc}_password:$PG_UID" "$svc/db_password:$APP_UID"
done
ensure_value "forge/scheduler_token:$APP_UID" "scheduler/clients/forge:$APP_UID"
ensure_value "scheduler/runner_token:$APP_UID" "runner/clients/scheduler:$APP_UID"

# every folder must be traversable by the daemon only; it mounts the files as root
find "$secrets" -type d -exec chown 0:0 {} + -exec chmod 0700 {} +

echo "setup done: ADP_PUBLIC_HOST=$host DOCKER_GID=$docker_gid. Next: task up"
