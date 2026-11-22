// Command control talks to GitHub and decides which run goes next (spec 0001). The scaffold only
// boots on the sdk, opens its own Postgres binding and serves health; feature #4 adds the webhook
// handler and feature #7 the run loop.
package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/racherom/modular-platform/sdk"

	"github.com/racherom/ai-dev-platform/services/control/internal/config"
)

func main() { sdk.Run(config.Module, setup) }

// setup loads the values control needs and returns its handler. The sdk serves /healthz and
// /readyz in front of it; /readyz waits until Postgres has answered once.
func setup(ctx context.Context, m *sdk.Module[config.Config, config.Uses]) (http.Handler, error) {
	if _, err := m.Config.PublicOrigin(ctx); err != nil {
		return nil, fmt.Errorf("public origin: %w", err)
	}
	if _, err := m.Config.ProxyHops(ctx); err != nil {
		return nil, fmt.Errorf("proxy hops: %w", err)
	}
	if _, err := m.Uses.Postgresql(ctx); err != nil {
		return nil, fmt.Errorf("postgresql: %w", err)
	}
	// No routes yet: /webhooks/github answers 404 until feature #4 adds its handler.
	return http.NewServeMux(), nil
}
