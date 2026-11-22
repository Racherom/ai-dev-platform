// Command scheduler owns run state and decides which run goes next (spec 0001). It holds no
// outside credential: only its binding token to runner, and the clients directory that names the
// one consumer allowed to call it (forge). The scaffold boots on the sdk, opens its own Postgres
// binding and its binding to runner, and serves health only; features #7 and #8 add the run loop
// and the board.
package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/racherom/modular-platform/sdk"

	"github.com/racherom/ai-dev-platform/libs/dbmigrate"
	"github.com/racherom/ai-dev-platform/services/scheduler/internal/config"
)

func main() { sdk.Run(config.Module, setup) }

// setup opens the bindings scheduler needs and returns its handler. The sdk serves /healthz and
// /readyz in front of it and checks forge's binding token on every other path; /readyz waits
// until Postgres and runner have answered once.
func setup(ctx context.Context, m *sdk.Module[config.Config, config.Uses]) (http.Handler, error) {
	pool, err := m.Uses.Postgresql(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgresql: %w", err)
	}
	if err := dbmigrate.RequireRiver(ctx, pool, m.Log, "scheduler-migrate"); err != nil {
		return nil, err
	}
	if _, err := m.Uses.Runner(ctx); err != nil {
		return nil, fmt.Errorf("runner: %w", err)
	}
	// No routes yet: a call with forge's token answers 404 until features #4 and #7 add them.
	return http.NewServeMux(), nil
}
