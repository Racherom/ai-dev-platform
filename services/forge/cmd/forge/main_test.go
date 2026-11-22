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

	"github.com/racherom/ai-dev-platform/services/forge/internal/config"
)

// schedulerToken is forge's binding token for scheduler in these tests.
const schedulerToken = "not-a-real-token-0123456789abcdef0123456789"

// validEnv is a complete config; neither Postgres nor scheduler is ever reached, since both
// clients connect lazily.
func validEnv(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	pw := filepath.Join(dir, "db_password")
	if err := os.WriteFile(pw, []byte("not-a-real-password\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(dir, "scheduler_token")
	if err := os.WriteFile(token, []byte(schedulerToken+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"MP_INSTANCE":              "forge",
		"ADP_PUBLIC_ORIGIN":        "https://adp.example.test",
		"ADP_DB_URL":               "postgres://127.0.0.1:1",
		"ADP_DB_NAME":              "forge",
		"ADP_DB_USER":              "adp_forge",
		"ADP_DB_PASSWORD_FILE":     pw,
		"ADP_DB_SSLMODE":           "disable",
		"ADP_SCHEDULER_URL":        "http://127.0.0.1:1",
		"ADP_SCHEDULER_TOKEN_FILE": token,
	}
}

// emptyFile returns the path of an empty file, as a token file setup.sh has not filled.
func emptyFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(p, nil, 0o400); err != nil {
		t.Fatal(err)
	}
	return p
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

// AC-2, AC-6: a good config boots, serves health without a token, and answers 501 on POST to the
// not yet built webhook path.
func TestBoots(t *testing.T) {
	h, logs, err := build(t, validEnv(t))
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	cases := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodGet, "/readyz", http.StatusServiceUnavailable}, // Postgres and scheduler have not answered
		{http.MethodPost, "/webhooks/github", http.StatusNotImplemented},
		{http.MethodGet, "/webhooks/github", http.StatusMethodNotAllowed},
		{http.MethodGet, "/other", http.StatusNotFound},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.want {
			t.Errorf("%s %s: got %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
	for _, secret := range []string{"not-a-real-password", schedulerToken} {
		if strings.Contains(logs, secret) {
			t.Errorf("a secret reached a log line: %s", logs)
		}
	}
}

// AC-6: a missing or invalid value stops the start with one problem line per value.
func TestRefusesBadConfig(t *testing.T) {
	cases := map[string]struct {
		change func(map[string]string)
		line   string
	}{
		"missing origin": {
			func(e map[string]string) { delete(e, "ADP_PUBLIC_ORIGIN") },
			"config: public-origin (ADP_PUBLIC_ORIGIN): missing",
		},
		"bad origin": {
			func(e map[string]string) { e["ADP_PUBLIC_ORIGIN"] = "not a url" },
			"config: public-origin (ADP_PUBLIC_ORIGIN): invalid",
		},
		"too many hops": {
			func(e map[string]string) { e["ADP_PROXY_HOPS"] = "2" },
			"config: proxy-hops (ADP_PROXY_HOPS): out_of_range",
		},
		"missing db user": {
			func(e map[string]string) { delete(e, "ADP_DB_USER") },
			"config: uses.postgresql.user (ADP_DB_USER): missing",
		},
		"missing scheduler url": {
			func(e map[string]string) { delete(e, "ADP_SCHEDULER_URL") },
			"config: uses.scheduler.url (ADP_SCHEDULER_URL): missing",
		},
		"empty scheduler token": {
			func(e map[string]string) { e["ADP_SCHEDULER_TOKEN_FILE"] = emptyFile(t) },
			"uses.scheduler.token_file (ADP_SCHEDULER_TOKEN_FILE): missing",
		},
		"password as plain env": {
			func(e map[string]string) {
				delete(e, "ADP_DB_PASSWORD_FILE")
				e["ADP_DB_PASSWORD"] = "x"
			},
			"ADP_DB_PASSWORD_FILE",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env := validEnv(t)
			tc.change(env)
			_, logs, err := build(t, env)
			if !errors.Is(err, sdk.ErrStart) {
				t.Fatalf("got %v, want a refused start", err)
			}
			if !strings.Contains(logs, tc.line) {
				t.Errorf("no problem line %q in:\n%s", tc.line, logs)
			}
			if strings.Contains(logs, "config loaded") {
				t.Error("a refused start logged config loaded")
			}
		})
	}
}
