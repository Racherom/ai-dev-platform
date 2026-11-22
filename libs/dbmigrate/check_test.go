package dbmigrate

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/racherom/modular-platform/sdk"
)

// TestRequireRiverUnreachable lets a service start when Postgres does not answer yet: /readyz
// waits for it, so the start check warns instead of refusing.
func TestRequireRiverUnreachable(t *testing.T) {
	ctx := t.Context()
	pool, err := connect(ctx, Settings{Host: "127.0.0.1", Port: 1, Database: "forge", User: "adp_forge", Password: sdk.NewSecret("forge-password")})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var logs bytes.Buffer
	if err := RequireRiver(ctx, pool, slog.New(slog.NewJSONHandler(&logs, nil)), "forge-migrate"); err != nil {
		t.Fatalf("got %v, want nil", err)
	}
	if !strings.Contains(logs.String(), `"level":"WARN","msg":"river schema not checked: postgres not reachable yet"`) {
		t.Errorf("no warning line in:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "forge-password") {
		t.Error("the password reached a log line")
	}
}
