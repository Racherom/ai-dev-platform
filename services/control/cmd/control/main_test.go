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

	"github.com/racherom/ai-dev-platform/services/control/internal/config"
)

// validEnv is a complete config; Postgres is never reached, since the pool connects lazily.
func validEnv(t *testing.T) map[string]string {
	t.Helper()
	pw := filepath.Join(t.TempDir(), "db_password")
	if err := os.WriteFile(pw, []byte("not-a-real-password\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"MP_INSTANCE":          "control",
		"ADP_PUBLIC_ORIGIN":    "https://adp.example.test",
		"ADP_DB_URL":           "postgres://127.0.0.1:1",
		"ADP_DB_NAME":          "adp",
		"ADP_DB_USER":          "adp_control",
		"ADP_DB_PASSWORD_FILE": pw,
		"ADP_DB_SSLMODE":       "disable",
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

// AC-6: a good config boots, serves health, and answers 404 on the not yet built webhook path.
func TestBoots(t *testing.T) {
	h, logs, err := build(t, validEnv(t))
	if err != nil {
		t.Fatalf("start refused: %v\n%s", err, logs)
	}
	for path, want := range map[string]int{
		"/healthz":         http.StatusOK,
		"/readyz":          http.StatusServiceUnavailable, // Postgres has not answered
		"/webhooks/github": http.StatusNotFound,
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if path == "/healthz" || path == "/readyz" {
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		}
		if rec.Code != want {
			t.Errorf("%s: got %d, want %d", path, rec.Code, want)
		}
	}
	if strings.Contains(logs, "not-a-real-password") {
		t.Error("the password reached a log line")
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
