#!/bin/sh
# The one shot provision step (spec 0001, "Databases and migrations"): one database and one login
# per service, made with the Postgres owner login, then exit. Ported from ModularPlatform's
# postgresql-provisioner script (catalog/capabilities/postgresql-provisioner.yaml at the sdk pin's
# commit 1eea5a8), with its MP_* names renamed to ADP_PROVISION_*. Slice 7 swaps it for the real one.
#
# Every name reaches SQL through psql variables (:'v' literal, format('%I') identifier) and \gexec,
# never through shell strings; each password is read inside psql from its file, so it never sits
# on a command line or in the env. One advisory lock covers the whole run.
#
# Env: ADP_PROVISION_HOST, ADP_PROVISION_PORT, ADP_PROVISION_OWNER_USER,
# ADP_PROVISION_OWNER_PASSWORD_FILE, ADP_PROVISION_SERVICES (space separated, like
# "forge scheduler runner"), ADP_PROVISION_PASSWORD_DIR (holds adp_<service>_password).
set -eu

die() { echo "provision: $*" >&2; exit 1; }

for v in ADP_PROVISION_HOST ADP_PROVISION_PORT ADP_PROVISION_OWNER_USER \
	ADP_PROVISION_OWNER_PASSWORD_FILE ADP_PROVISION_SERVICES ADP_PROVISION_PASSWORD_DIR; do
	eval "[ -n \"\${$v:-}\" ]" || die "$v: missing"
done

# The service names become database and login names, so they are checked before anything runs.
for s in $ADP_PROVISION_SERVICES; do
	case "$s" in
	*[!a-z0-9_]* | [!a-z]*) die "service name '$s' must match [a-z][a-z0-9_]*" ;;
	esac
	[ -r "$ADP_PROVISION_PASSWORD_DIR/adp_${s}_password" ] ||
		die "cannot read $ADP_PROVISION_PASSWORD_DIR/adp_${s}_password"
	[ -s "$ADP_PROVISION_PASSWORD_DIR/adp_${s}_password" ] ||
		die "$ADP_PROVISION_PASSWORD_DIR/adp_${s}_password is empty"
done

# The owner password goes into a PGPASSFILE on the /tmp tmpfs (mode 0600), never into PGPASSWORD.
# In that file a backslash and a colon need a backslash in front.
umask 077
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
owner_pw=$(cat "$ADP_PROVISION_OWNER_PASSWORD_FILE") || die "cannot read ADP_PROVISION_OWNER_PASSWORD_FILE"
[ -n "$owner_pw" ] || die "ADP_PROVISION_OWNER_PASSWORD_FILE is empty"
printf '*:*:*:%s:%s\n' "$ADP_PROVISION_OWNER_USER" "$(printf '%s' "$owner_pw" | sed 's/[\\:]/\\&/g')" >"$work/pgpass"
unset owner_pw
export PGPASSFILE="$work/pgpass" PGHOST="$ADP_PROVISION_HOST" PGPORT="$ADP_PROVISION_PORT"
export PGUSER="$ADP_PROVISION_OWNER_USER" PGDATABASE=postgres PGSSLMODE=disable PGCONNECT_TIMEOUT=10
export PGAPPNAME=adp-provision

# What one service gets, run once per service with :login, :db and :pwfile set.
cat >"$work/one.sql" <<'SQL'
\set pw `cat :'pwfile'`
SELECT CASE
    WHEN r.oid IS NULL THEN 'create'
    WHEN EXISTS (SELECT FROM pg_auth_members m JOIN pg_roles g ON g.oid = m.roleid
                 WHERE m.member = r.oid AND g.rolname = :'marker') THEN 'update'
    ELSE 'foreign' END AS role_state
  FROM (SELECT CAST(:'login' AS name) AS n) x LEFT JOIN pg_roles r ON r.rolname = x.n \gset
SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L IN ROLE %I',
              :'login', :'pw', :'marker')
  WHERE :'role_state' = 'create' \gexec
SELECT format('ALTER ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L', :'login', :'pw')
  WHERE :'role_state' = 'update' \gexec
SELECT format('DO $adp$ BEGIN RAISE EXCEPTION %L; END $adp$',
              format('role %s exists and was not made by provision', :'login'))
  WHERE :'role_state' = 'foreign' \gexec
\unset pw
SELECT CASE
    WHEN d.oid IS NULL THEN 'create'
    WHEN o.rolname = :'login' THEN 'keep'
    ELSE 'foreign' END AS db_state
  FROM (SELECT CAST(:'db' AS name) AS n) x
  LEFT JOIN pg_database d ON d.datname = x.n LEFT JOIN pg_roles o ON o.oid = d.datdba \gset
SELECT format('CREATE DATABASE %I OWNER %I', :'db', :'login') WHERE :'db_state' = 'create' \gexec
SELECT format('DO $adp$ BEGIN RAISE EXCEPTION %L; END $adp$',
              format('database %s exists and is not owned by %s', :'db', :'login'))
  WHERE :'db_state' = 'foreign' \gexec
SELECT format('REVOKE CONNECT ON DATABASE %I FROM PUBLIC', :'db') \gexec
SELECT format('GRANT CONNECT ON DATABASE %I TO %I', :'db', :'login') \gexec
SQL

# The whole run: lock, marker role, each service in turn, then close the shared databases.
{
	echo "SELECT pg_advisory_lock(hashtext('adp provision')) \\g /dev/null"
	echo "SELECT format('CREATE ROLE %I NOLOGIN', :'marker')"
	echo "  WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'marker') \\gexec"
	for s in $ADP_PROVISION_SERVICES; do
		echo "\\echo provision: database $s and login adp_$s"
		echo "\\set db $s"
		echo "\\set login adp_$s"
		echo "\\set pwfile $ADP_PROVISION_PASSWORD_DIR/adp_${s}_password"
		echo "\\i $work/one.sql"
	done
	echo "REVOKE CONNECT ON DATABASE postgres FROM PUBLIC;"
	echo "REVOKE CONNECT ON DATABASE template1 FROM PUBLIC;"
} >"$work/run.sql"

psql -X -q -v ON_ERROR_STOP=1 -v marker=adp_provisioned -f "$work/run.sql"
echo "provision: done ($ADP_PROVISION_SERVICES)"
