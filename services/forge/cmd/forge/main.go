// Command forge is the only service that holds GitHub credentials (spec 0001). The scaffold boots
// on the sdk, opens its own Postgres binding and its binding to scheduler, and serves health plus
// a placeholder webhook route; feature #4 adds the webhook intake and the GitHub client.
package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/racherom/modular-platform/sdk"

	"github.com/racherom/ai-dev-platform/services/forge/internal/config"
)

func main() { sdk.Run(config.Module, setup) }

// setup loads the values forge needs and returns its handler. The sdk serves /healthz and /readyz
// in front of it; /readyz waits until Postgres and scheduler have answered once.
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
	if _, err := m.Uses.Scheduler(ctx); err != nil {
		return nil, fmt.Errorf("scheduler: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhooks/github", webhookNotBuilt)
	return mux, nil
}

// webhookNotBuilt answers 501 until feature #4 adds the handler, so a check can tell a request
// reached forge: Caddy itself only ever answers 404 or 405.
func webhookNotBuilt(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "the GitHub webhook arrives with feature #4", http.StatusNotImplemented)
}
