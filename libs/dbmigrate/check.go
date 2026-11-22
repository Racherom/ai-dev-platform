package dbmigrate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/racherom/modular-platform/sdk"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// ErrRiverBehind reports that the database lacks River migrations this build expects, so the
// service's migrate step has not caught up yet.
var ErrRiverBehind = errors.New("river schema behind")

// CheckRiver reports whether every River migration this build knows has been applied to the
// database behind pool. It returns an error wrapping ErrRiverBehind when one is missing, and a
// plain error when the database cannot be asked.
func CheckRiver(ctx context.Context, pool *pgxpool.Pool) error {
	quiet := slog.New(slog.DiscardHandler)
	m, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Logger: quiet})
	if err != nil {
		return fmt.Errorf("river migrator: %w", err)
	}
	res, err := m.Validate(ctx, nil)
	if err != nil {
		return fmt.Errorf("river validate: %w", err)
	}
	if !res.OK {
		return fmt.Errorf("%w: %s", ErrRiverBehind, strings.Join(res.Messages, "; "))
	}
	return nil
}

// checkTimeout bounds the start check, so a database that never answers cannot stall a start.
const checkTimeout = 10 * time.Second

// RequireRiver runs CheckRiver at service start. When the schema is behind, it logs one line
// naming the step to run and returns an error wrapping sdk.ErrStart, so the sdk exits 1 without
// logging it a second time. step is the one shot compose step that migrates this service, such
// as "scheduler-migrate".
//
// A database that does not answer yet is not a refusal: the service may start before Postgres
// is up, and its /readyz waits for it (spec 0001). RequireRiver then logs a warning and returns
// nil. In the compose stack, the migrate step has already reached the database by then.
func RequireRiver(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, step string) error {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		log.WarnContext(ctx, "river schema not checked: postgres not reachable yet", slog.Any("error", err))
		return nil
	}
	err := CheckRiver(ctx, pool)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrRiverBehind):
		log.ErrorContext(ctx, "river schema behind; run "+step+" first", slog.Any("error", err))
		return fmt.Errorf("%w: %w", sdk.ErrStart, err)
	default:
		return fmt.Errorf("river schema check: %w", err)
	}
}
