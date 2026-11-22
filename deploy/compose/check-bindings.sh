#!/bin/sh
# Runs inside the binding-check container (`task check-bindings`, spec 0001 AC-8). It holds forge's
# token for scheduler and scheduler's token for runner, and checks that each provider accepts only
# its own consumer's token. /v1/binding-check has no route on purpose: an accepted token answers
# 404, a refused one 401. Keep this path unrouted when #4 and #7 add real routes.
set -u
forge_token=$(cat /run/secrets/scheduler_token)
scheduler_token=$(cat /run/secrets/runner_token)
probe=/v1/binding-check
fail=0

# expect <want> <url> [token]: the token goes in a header read from stdin, never on the command line
expect() {
	want=$1 url=$2 token=${3:-}
	if [ -n "$token" ]; then
		got=$(printf 'Authorization: Bearer %s\n' "$token" | curl -s -o /dev/null -w '%{http_code}' -H @- "$url")
	else
		got=$(curl -s -o /dev/null -w '%{http_code}' "$url")
	fi
	if [ "$got" = "$want" ]; then
		echo "ok    $url ${4:-} -> $got"
	else
		echo "FAIL  $url ${4:-} -> $got, want $want"
		fail=1
	fi
}

for svc in scheduler runner; do
	base=http://$svc:8080
	case $svc in
	scheduler) own=$forge_token other=$scheduler_token own_name=forge other_name=scheduler ;;
	runner) own=$scheduler_token other=$forge_token own_name=scheduler other_name=forge ;;
	esac
	expect 404 "$base$probe" "$own" "($own_name's token)"
	expect 401 "$base$probe" "" "(no token)"
	expect 401 "$base$probe" "not-a-token-0123456789abcdef0123456789abcdef" "(wrong token)"
	expect 401 "$base$probe" "$other" "($other_name's token)"
	expect 200 "$base/healthz" "" "(no token)"
done

exit $fail
