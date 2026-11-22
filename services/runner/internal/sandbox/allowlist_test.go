package sandbox

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// composeFile is the stack file that holds the socket proxy's allowlist flags.
const composeFile = "../../../../deploy/compose/compose.yaml"

// proxyAllowlist reads the -allow<METHOD>= flags of the socket-proxy service, so this test checks
// the very regexes the proxy runs with (spec 0001, "Socket proxy allowlist").
func proxyAllowlist(t *testing.T) map[string]*regexp.Regexp {
	t.Helper()
	data, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatal(err)
	}
	var stack struct {
		Services map[string]struct {
			Command []string `json:"command"`
		} `json:"services"`
	}
	if err := yaml.Unmarshal(data, &stack); err != nil {
		t.Fatal(err)
	}
	proxy, ok := stack.Services["socket-proxy"]
	if !ok {
		t.Fatal("no socket-proxy service in " + composeFile)
	}
	allow := map[string]*regexp.Regexp{}
	for _, arg := range proxy.Command {
		flag, expr, found := strings.Cut(arg, "=")
		method, isAllow := strings.CutPrefix(flag, "-allow")
		if !found || !isAllow || method != strings.ToUpper(method) {
			continue // not a per method allow flag
		}
		expr = strings.ReplaceAll(expr, "$$", "$") // compose's escape
		if !strings.HasPrefix(expr, "^") || !strings.HasSuffix(expr, "$") {
			t.Errorf("-allow%s is not anchored: %s", method, expr)
		}
		allow[method] = regexp.MustCompile(expr)
	}
	return allow
}

func TestProxyAllowlist(t *testing.T) {
	allow := proxyAllowlist(t)
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		if allow[m] == nil {
			t.Fatalf("no -allow%s flag", m)
		}
	}
	if len(allow) != 3 {
		t.Errorf("want exactly GET, POST and DELETE allowed, got %d methods", len(allow))
	}

	cases := []struct {
		method, path string
		allowed      bool
	}{
		// allowed, with and without a version prefix
		{"GET", "/_ping", true},
		{"GET", "/v1.47/_ping", true},
		{"GET", "/version", true},
		{"GET", "/v1.51/info", true},
		{"GET", "/v1.47/images/adp-sandbox:dev/json", true},
		{"GET", "/v1.47/images/ghcr.io/racherom/adp-sandbox@sha256:abc/json", true},
		{"GET", "/v1.47/containers/json", true},
		{"GET", "/v1.47/containers/3f2a9c/json", true},
		{"GET", "/v1.47/containers/adp-run-42/logs", true},
		{"GET", "/v1.47/networks", true},
		{"GET", "/v1.47/networks/adp-run-42", true},
		{"POST", "/v1.47/containers/create", true},
		{"POST", "/v1.47/containers/3f2a9c/start", true},
		{"POST", "/v1.47/containers/3f2a9c/wait", true},
		{"POST", "/v1.47/containers/3f2a9c/kill", true},
		{"POST", "/v1.47/networks/create", true},
		{"DELETE", "/v1.47/containers/3f2a9c", true},
		{"DELETE", "/v1.47/networks/adp-run-42", true},

		// denied: the spec names each of these
		{"POST", "/v1.47/containers/x/exec", false},
		{"POST", "/containers/x/exec", false},
		{"POST", "/v1.47/exec/abc/start", false},
		{"POST", "/v1.47/build", false},
		{"POST", "/v1.47/commit", false},
		{"POST", "/v1.47/images/create", false},
		{"POST", "/v1.47/networks/x/connect", false},
		{"POST", "/v1.47/swarm/init", false},
		{"GET", "/v1.47/plugins", false},
		{"GET", "/v1.47/system/df", false},
		{"GET", "/v1.47/volumes", false},
		{"POST", "/v1.47/volumes/create", false},
		{"DELETE", "/v1.47/volumes/x", false},
		{"DELETE", "/v1.47/images/x", false},
		// denied: anchoring, wrong method, version shape, traversal by suffix
		{"GET", "/v1.47/_ping/../containers/x/exec", false},
		{"GET", "/v1.47/containers/x/archive", false},
		{"GET", "/v1.47/containers/x/attach/ws", false},
		{"POST", "/v1.47/containers/x/attach", false},
		{"POST", "/v1.47/containers/x/update", false},
		{"POST", "/v1.47/_ping", false},
		{"DELETE", "/v1.47/containers/x/exec", false},
		{"GET", "/v2.0/_ping", false},
		{"GET", "/x/v1.47/_ping", false},
		{"PUT", "/v1.47/containers/x/archive", false},
		{"HEAD", "/_ping", false},
	}
	for _, c := range cases {
		re := allow[c.method]
		got := re != nil && re.MatchString(c.path)
		if got != c.allowed {
			t.Errorf("%s %s: allowed=%v, want %v", c.method, c.path, got, c.allowed)
		}
	}
}
