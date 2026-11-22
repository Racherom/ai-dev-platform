// Command migrate is the one shot step every `up` runs before control and runner start (spec
// 0001): it creates or updates the app roles, runs goose and River's migrations, applies the
// grants, then exits 0. It is a plain main, not sdk.Run; it holds the owner login, and each
// password stays an sdk.Secret from its file to the statement that sets it.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/racherom/modular-platform/sdk"

	"github.com/racherom/ai-dev-platform/services/control/internal/dbsetup"
	"github.com/racherom/ai-dev-platform/services/control/migrations"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.LookupEnv, log); err != nil {
		log.ErrorContext(ctx, "migrate failed", slog.Any("error", err))
		stop()
		os.Exit(1)
	}
	log.InfoContext(ctx, "migrate done")
}

// settings is what migrate reads from its env; every password arrives as a file path.
type settings struct {
	host, database, owner string
	port                  uint16
	ownerPassword         sdk.Secret
	roles                 []dbsetup.AppRole
}

func load(lookup func(string) (string, bool)) (settings, error) {
	var (
		s    settings
		errs []error
	)
	need := func(env string) string {
		v, ok := lookup(env)
		if !ok || strings.TrimSpace(v) == "" {
			errs = append(errs, fmt.Errorf("%s: missing", env))
		}
		return strings.TrimSpace(v)
	}
	secret := func(env string) sdk.Secret {
		path := need(env)
		if path == "" {
			return sdk.Secret{}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: the file %s cannot be read", env, path))
			return sdk.Secret{}
		}
		v := strings.TrimRight(string(data), " \t\r\n")
		if v == "" {
			errs = append(errs, fmt.Errorf("%s: the file %s is empty", env, path))
		}
		return sdk.NewSecret(v)
	}
	s.host = need("ADP_DB_HOST")
	s.database = need("ADP_DB_NAME")
	s.owner = need("ADP_DB_OWNER_USER")
	if p := need("ADP_DB_PORT"); p != "" {
		port, err := strconv.ParseUint(p, 10, 16)
		if err != nil || port == 0 {
			errs = append(errs, fmt.Errorf("ADP_DB_PORT: invalid port %q", p))
		}
		s.port = uint16(port)
	}
	s.ownerPassword = secret("ADP_DB_OWNER_PASSWORD_FILE")
	s.roles = []dbsetup.AppRole{
		{Name: dbsetup.ControlRole, Password: secret("ADP_DB_CONTROL_PASSWORD_FILE")},
		{Name: dbsetup.RunnerRole, Password: secret("ADP_DB_RUNNER_PASSWORD_FILE")},
	}
	return s, errors.Join(errs...)
}

func run(ctx context.Context, lookup func(string) (string, bool), log *slog.Logger) error {
	s, err := load(lookup)
	if err != nil {
		return err
	}
	cfg, err := pgxpool.ParseConfig("")
	if err != nil {
		return fmt.Errorf("pool config: %w", err)
	}
	cc := cfg.ConnConfig
	cc.Host, cc.Port, cc.Database, cc.User = s.host, s.port, s.database, s.owner
	cc.Password = s.ownerPassword.Reveal()
	cc.TLSConfig = nil // internal network, TLS off for Release 1 (spec 0001, Follow-up)
	cc.Fallbacks = nil
	cfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("pool: %w", err)
	}
	defer pool.Close()
	if err := waitForDB(ctx, pool, log); err != nil {
		return err
	}
	return dbsetup.Run(ctx, pool, migrations.FS, s.roles, log)
}

// waitForDB retries for up to 30 seconds, since Postgres may still be finishing its first init.
func waitForDB(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := pool.Ping(ctx)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("postgres not reachable: %w", err)
		}
		log.WarnContext(ctx, "postgres not ready yet", slog.Any("error", err))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
