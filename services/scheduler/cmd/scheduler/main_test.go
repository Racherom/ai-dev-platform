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
	"testing"

	"github.com/racherom/modular-platform/sdk"

	"github.com/racherom/ai-dev-platform/services/scheduler/internal/config"
)

// The binding tokens in these tests: forge's token for scheduler, and scheduler's own for runner.
const (
	forgeToken  = "forge-token-for-scheduler-0123456789abcdef"
	runnerToken = "scheduler-token-for-runner-0123456789abcd"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o400); err != nil {
		t.Fatal(err)
	}
}

// validEnv is a complete config; neither Postgres nor runner is ever reached, since both clients
// connect lazily.
func validEnv(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "db_password"), "not-a-real-password\n")
	writeFile(t, filepath.Join(dir, "runner_token"), runnerToken+"\n")
	writeFile(t, filepath.Join(dir, "clients", "forge"), forgeToken+"\n")
	return map[string]string{
		"MP_INSTANCE":           "scheduler",
		"ADP_DB_URL":            "postgres://127.0.0.1:1",
		"ADP_DB_NAME":           "scheduler",
		"ADP_DB_USER":           "adp_scheduler",
		"ADP_DB_PASSWORD_FILE":  filepath.Join(dir, "db_password"),
		"ADP_DB_SSLMODE":        "disable",
		"ADP_RUNNER_URL":        "http://127.0.0.1:1",
		"ADP_RUNNER_TOKEN_FILE": filepath.Join(dir, "runner_token"),
		"ADP_CLIENTS_DIR":       filepath.Join(dir, "clients"),
	}
}

func build(t *testing.T, env map[string]string) (http.Handler, string, error) {
	t.Helper()
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h, err := sdk.Build(ctx, config.Module, setup,
		sdk.WithEnv(env), sdk.WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	return h, logs.String(), err
}

// AC-6, AC-8: a good config boots; health needs no token, every other path needs forge's token,
// and with it an unrouted path answers 404.
func TestBindingAuth(t *testing.T) {
	h, logs, err := build(t, validEnv(t))
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	cases := []struct {
		name, path, auth string
		want             int
	}{
		{"health without token", "/healthz", "", http.StatusOK},
		{"forge's token", "/v1/binding-check", "Bearer " + forgeToken, http.StatusNotFound},
		{"no token", "/v1/binding-check", "", http.StatusUnauthorized},
		{"wrong token", "/v1/binding-check", "Bearer " + strings.Repeat("x", 40), http.StatusUnauthorized},
		{"runner's token", "/v1/binding-check", "Bearer " + runnerToken, http.StatusUnauthorized},
		{"not a bearer", "/v1/binding-check", forgeToken, http.StatusUnauthorized},
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
	for _, secret := range []string{"not-a-real-password", forgeToken, runnerToken} {
		if strings.Contains(logs, secret) {
			t.Errorf("a secret reached a log line: %s", logs)
		}
	}
}

// AC-6: missing or invalid values stop the start with a problem line, an empty clients directory
// included.
func TestRefusesBadConfig(t *testing.T) {
	cases := map[string]struct {
		change func(t *testing.T, e map[string]string)
		line   string
	}{
		"empty clients dir": {
			func(t *testing.T, e map[string]string) { e["ADP_CLIENTS_DIR"] = t.TempDir() },
			"config: provides.scheduler.clients_dir (ADP_CLIENTS_DIR): missing",
		},
		"no clients dir": {
			func(_ *testing.T, e map[string]string) { delete(e, "ADP_CLIENTS_DIR") },
			"config: provides.scheduler.clients_dir (ADP_CLIENTS_DIR): missing",
		},
		"short client token": {
			func(t *testing.T, e map[string]string) {
				dir := t.TempDir()
				writeFile(t, filepath.Join(dir, "forge"), "short")
				e["ADP_CLIENTS_DIR"] = dir
			},
			"config: provides.scheduler.clients_dir (ADP_CLIENTS_DIR): invalid_binding",
		},
		"missing runner url": {
			func(_ *testing.T, e map[string]string) { delete(e, "ADP_RUNNER_URL") },
			"config: uses.runner.url (ADP_RUNNER_URL): missing",
		},
		"missing db user": {
			func(_ *testing.T, e map[string]string) { delete(e, "ADP_DB_USER") },
			"config: uses.postgresql.user (ADP_DB_USER): missing",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env := validEnv(t)
			tc.change(t, env)
			_, logs, err := build(t, env)
			if !errors.Is(err, sdk.ErrStart) {
				t.Fatalf("got %v, want a refused start", err)
			}
			if !strings.Contains(logs, tc.line) {
				t.Errorf("no problem line %q in:\n%s", tc.line, logs)
			}
		})
	}
}
