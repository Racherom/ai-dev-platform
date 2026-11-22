// Package dbsetup brings the database to the state both apps expect, as the one shot migrate step
// (spec 0001, "Database roles and grants"). Only it ever holds the owner login and DDL rights.
package dbsetup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/racherom/modular-platform/sdk"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// The app roles; their names are part of the spec, not configuration.
const (
	ControlRole = "adp_control"
	RunnerRole  = "adp_runner"
)

// AppRole is one login migrate creates or updates.
type AppRole struct {
	Name     string
	Password sdk.Secret
}

// Run applies, in order and on every start: the app roles (a changed password is rotated), the
// schema lockdown, goose up from migrations, River's migrations, and the grants.
func Run(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS, roles []AppRole, log *slog.Logger) error {
	for _, r := range roles {
		if err := ensureRole(ctx, pool, r); err != nil {
			return err
		}
		log.InfoContext(ctx, "role ready", slog.String("role", r.Name))
	}
	if _, err := pool.Exec(ctx, "REVOKE CREATE ON SCHEMA public FROM PUBLIC"); err != nil {
		return fmt.Errorf("revoke create on public: %w", err)
	}
	if err := gooseUp(ctx, pool, migrations, log); err != nil {
		return err
	}
	if err := riverUp(ctx, pool, log); err != nil {
		return err
	}
	return grant(ctx, pool, roles)
}

// ensureRole creates the role, or alters it so its flags and password match.
func ensureRole(ctx context.Context, pool *pgxpool.Pool, r AppRole) error {
	pw := r.Password.Reveal()
	if pw == "" {
		return fmt.Errorf("role %s: empty password", r.Name)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", r.Name).Scan(&exists); err != nil {
		return fmt.Errorf("role %s: %w", r.Name, err)
	}
	verb := "CREATE"
	if exists {
		verb = "ALTER"
	}
	// Role DDL takes no bind parameters, so the password goes in as a quoted literal. The
	// statement is never logged, and Postgres logs no DDL by default.
	stmt := fmt.Sprintf("%s ROLE %s WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %s",
		verb, pgx.Identifier{r.Name}.Sanitize(), quoteLiteral(pw))
	if _, err := pool.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("%s role %s failed", strings.ToLower(verb), r.Name) // err may echo the statement
	}
	return nil
}

// quoteLiteral quotes s as an SQL string literal; Postgres keeps standard_conforming_strings on,
// so only the single quote needs doubling.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
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

// grant gives every app role row access to all tables and sequences, now and for tables the owner
// creates later. Feature #3 narrows the runner role to the columns it names.
func grant(ctx context.Context, pool *pgxpool.Pool, roles []AppRole) error {
	names := make([]string, len(roles))
	for i, r := range roles {
		names[i] = pgx.Identifier{r.Name}.Sanitize()
	}
	to := strings.Join(names, ", ")
	for _, stmt := range []string{
		"GRANT USAGE ON SCHEMA public TO " + to,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO " + to,
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO " + to,
		"ALTER DEFAULT PRIVILEGES FOR ROLE CURRENT_USER IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO " + to,
		"ALTER DEFAULT PRIVILEGES FOR ROLE CURRENT_USER IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO " + to,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("grant: %w", err)
		}
	}
	return nil
}
