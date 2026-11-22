// Package dbmigrate brings one service database to the schema its service expects (spec 0001,
// "Databases and migrations"). Every service image ships a migrate binary built on it; the one
// shot <service>-migrate step runs it as the service's own login, which owns the database, so no
// step but provision ever holds the Postgres owner login.
package dbmigrate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/racherom/modular-platform/sdk"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// waitFor is how long Run waits for Postgres to answer before it gives up.
const waitFor = 30 * time.Second

// Settings is where migrate connects. The password arrives as a file path and stays an
// sdk.Secret until the connection uses it.
type Settings struct {
	Host     string
	Port     uint16
	Database string
	User     string
	Password sdk.Secret
}

// Load reads the settings from ADP_DB_HOST, ADP_DB_PORT, ADP_DB_NAME, ADP_DB_USER and
// ADP_DB_PASSWORD_FILE through lookup. It reports every bad value at once and never repeats a
// secret in its error.
func Load(lookup func(string) (string, bool)) (Settings, error) {
	var (
		s    Settings
		errs []error
	)
	need := func(env string) string {
		v, ok := lookup(env)
		v = strings.TrimSpace(v)
		if !ok || v == "" {
			errs = append(errs, fmt.Errorf("%s: missing", env))
		}
		return v
	}
	s.Host = need("ADP_DB_HOST")
	s.Database = need("ADP_DB_NAME")
	s.User = need("ADP_DB_USER")
	if p := need("ADP_DB_PORT"); p != "" {
		port, err := strconv.ParseUint(p, 10, 16)
		if err != nil || port == 0 {
			errs = append(errs, fmt.Errorf("ADP_DB_PORT: invalid port %q", p))
		}
		s.Port = uint16(port)
	}
	if path := need("ADP_DB_PASSWORD_FILE"); path != "" {
		data, err := os.ReadFile(path)
		v := strings.TrimRight(string(data), " \t\r\n")
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("ADP_DB_PASSWORD_FILE: the file %s cannot be read", path))
		case v == "":
			errs = append(errs, fmt.Errorf("ADP_DB_PASSWORD_FILE: the file %s is empty", path))
		default:
			s.Password = sdk.NewSecret(v)
		}
	}
	return s, errors.Join(errs...)
}

// Main is the whole migrate binary: it loads the settings from the process env, runs Run with
// migrations, logs the result as JSON and exits 1 on any failure.
func Main(migrations fs.FS) {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := func() error {
		s, err := Load(os.LookupEnv)
		if err != nil {
			return err
		}
		return Run(ctx, s, migrations, log)
	}()
	if err != nil {
		log.ErrorContext(ctx, "migrate failed", slog.Any("error", err))
		stop()
		os.Exit(1)
	}
	log.InfoContext(ctx, "migrate done")
}

// Run waits up to 30 seconds for the database, then applies goose up from migrations and River's
// migrations. A second run applies nothing and succeeds.
func Run(ctx context.Context, s Settings, migrations fs.FS, log *slog.Logger) error {
	cfg, err := pgxpool.ParseConfig("")
	if err != nil {
		return fmt.Errorf("pool config: %w", err)
	}
	cc := cfg.ConnConfig
	cc.Host, cc.Port, cc.Database, cc.User = s.Host, s.Port, s.Database, s.User
	cc.Password = s.Password.Reveal()
	cc.TLSConfig = nil // internal network, TLS arrives with feature #24
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
	if err := gooseUp(ctx, pool, migrations, log); err != nil {
		return err
	}
	return riverUp(ctx, pool, log)
}

// waitForDB pings until Postgres answers, since provision may have only just made the database.
func waitForDB(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	deadline := time.Now().Add(waitFor)
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

func gooseUp(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS, log *slog.Logger) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations)
	if errors.Is(err, goose.ErrNoMigrations) {
		log.InfoContext(ctx, "goose: no migrations")
		return nil
	}
	if err != nil {
		return fmt.Errorf("goose: %w", err)
	}
	results, err := p.Up(ctx)
	if err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	version, err := p.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("goose version: %w", err)
	}
	log.InfoContext(ctx, "goose up", slog.Int("applied", len(results)), slog.Int64("version", version))
	return nil
}

func riverUp(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("river migrator: %w", err)
	}
	res, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("river migrate: %w", err)
	}
	log.InfoContext(ctx, "river migrate", slog.Int("applied", len(res.Versions)))
	return nil
}
