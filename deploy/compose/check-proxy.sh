#!/bin/sh
# Runs inside the proxy-check container (`task check-proxy`, spec 0001 AC-3). It probes the socket
# proxy the way runner would and fails on the first call that is not answered as expected.
set -u
proxy=http://socket-proxy:2375
fail=0

expect() {
	want=$1 method=$2 path=$3
	got=$(curl -s -o /dev/null -w '%{http_code}' -X "$method" "$proxy$path")
	if [ "$got" = "$want" ]; then
		echo "ok    $method $path -> $got"
	else
		echo "FAIL  $method $path -> $got, want $want"
		fail=1
	fi
}

# allowed
expect 200 GET /_ping
expect 200 GET /v1.47/_ping
expect 200 GET /version
expect 200 GET '/v1.47/containers/json?all=1&filters=%7B%22label%22%3A%5B%22adp.managed%3Dtrue%22%5D%7D'
# denied
expect 403 POST /v1.47/containers/x/exec
expect 403 POST /v1.47/build
expect 403 POST /v1.47/images/create
expect 403 POST /v1.47/networks/x/connect
expect 403 GET /v1.47/volumes
expect 403 GET /v1.47/system/df
# a method with no allow flag at all is refused as 405, not 403
expect 405 PUT /v1.47/containers/x/archive

exit $fail
