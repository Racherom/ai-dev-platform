package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/racherom/modular-platform/sdk"

	"github.com/racherom/ai-dev-platform/services/runner/internal/config"
)

// fakeEngine answers GET /info with the given runtimes JSON.
func fakeEngine(t *testing.T, runtimes string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Runtimes":` + runtimes + `}`))
	}))
	t.Cleanup(srv.Close)
	return "tcp://" + strings.TrimPrefix(srv.URL, "http://")
}

// schedulerToken is scheduler's binding token for runner in these tests.
const schedulerToken = "scheduler-token-for-runner-0123456789abcd"

func validEnv(t *testing.T, dockerHost string) map[string]string {
	t.Helper()
	dir := t.TempDir()
	pw := filepath.Join(dir, "db_password")
	if err := os.WriteFile(pw, []byte("not-a-real-password\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	logs := filepath.Join(dir, "run-logs")
	if err := os.Mkdir(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	clients := filepath.Join(dir, "clients")
	if err := os.Mkdir(clients, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clients, "scheduler"), []byte(schedulerToken+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"MP_INSTANCE":          "runner",
		"ADP_DOCKER_HOST":      dockerHost,
		"ADP_RUN_LOGS_DIR":     logs,
		"ADP_DB_URL":           "postgres://127.0.0.1:1",
		"ADP_DB_NAME":          "runner",
		"ADP_DB_USER":          "adp_runner",
		"ADP_DB_PASSWORD_FILE": pw,
		"ADP_DB_SSLMODE":       "disable",
		"ADP_CLIENTS_DIR":      clients,
	}
}

// syncBuffer is a bytes.Buffer safe for concurrent use. sdk.Build starts a readiness poll per
// required binding, and that goroutine logs a warning while the test reads the logs.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func build(t *testing.T, env map[string]string) (http.Handler, string, error) {
	t.Helper()
	var logs syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h, err := sdk.Build(ctx, config.Module, setup,
		sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	return h, logs.String(), err
}

// AC-6, AC-8: with runsc listed and a good config, runner boots; health needs no token, every
// other path needs scheduler's token, and with it an unrouted path answers 404.
func TestBoots(t *testing.T) {
	h, logs, err := build(t, validEnv(t, fakeEngine(t, `{"runc":{},"runsc":{}}`)))
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	cases := []struct {
		name, path, auth string
		want             int
	}{
		{"health without token", "/healthz", "", http.StatusOK},
		{"scheduler's token", "/v1/binding-check", "Bearer " + schedulerToken, http.StatusNotFound},
		{"no token", "/v1/binding-check", "", http.StatusUnauthorized},
		{"wrong token", "/v1/binding-check", "Bearer " + strings.Repeat("x", 40), http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, c.path, nil)
			if c.auth != "" {
				req.Header.Set("Authorization", c.auth)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Errorf("got %d, want %d", rec.Code, c.want)
			}
		})
	}
	if strings.Contains(logs, schedulerToken) {
		t.Errorf("the token reached a log line: %s", logs)
	}
}

// AC-3: an engine without runsc stops runner with the exact hint line.
func TestRefusesWithoutRunsc(t *testing.T) {
	_, logs, err := build(t, validEnv(t, fakeEngine(t, `{"runc":{}}`)))
	if !errors.Is(err, sdk.ErrStart) {
		t.Fatalf("got %v, want a refused start", err)
	}
	if !strings.Contains(logs, `"msg":"runsc runtime not registered; run setup.sh"`) {
		t.Errorf("no runsc hint in:\n%s", logs)
	}
}

// AC-6: missing or invalid values stop the start with a problem line.
func TestRefusesBadConfig(t *testing.T) {
	cases := map[string]struct {
		change func(map[string]string)
		line   string
	}{
		"missing docker host": {
			func(e map[string]string) { delete(e, "ADP_DOCKER_HOST") },
			"config: docker-host (ADP_DOCKER_HOST): missing",
		},
		"other runtime": {
			func(e map[string]string) { e["ADP_SANDBOX_RUNTIME"] = "runc" },
			"config: sandbox-runtime (ADP_SANDBOX_RUNTIME): not_allowed",
		},
		"relative logs dir": {
			func(e map[string]string) { e["ADP_RUN_LOGS_DIR"] = "run-logs" },
			"config: run-logs-dir (ADP_RUN_LOGS_DIR): no_match",
		},
		"unix socket": {
			func(e map[string]string) { e["ADP_DOCKER_HOST"] = "unix:///var/run/docker.sock" },
			"config: docker-host (ADP_DOCKER_HOST): invalid",
		},
		"other scheme": {
			func(e map[string]string) { e["ADP_DOCKER_HOST"] = "ssh://socket-proxy:2375" },
			"docker host refused",
		},
		"empty clients dir": {
			func(e map[string]string) { e["ADP_CLIENTS_DIR"] = e["ADP_RUN_LOGS_DIR"] },
			"config: provides.runner.clients_dir (ADP_CLIENTS_DIR): missing",
		},
		"logs dir missing": {
			func(e map[string]string) { e["ADP_RUN_LOGS_DIR"] = "/nonexistent/run-logs" },
			"run logs dir not writable",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env := validEnv(t, fakeEngine(t, `{"runsc":{}}`))
			tc.change(env)
			_, logs, err := build(t, env)
			if !errors.Is(err, sdk.ErrStart) {
				t.Fatalf("got %v, want a refused start", err)
			}
			if !strings.Contains(logs, tc.line) {
				t.Errorf("no line %q in:\n%s", tc.line, logs)
			}
		})
	}
}
