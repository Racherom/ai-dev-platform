// Command runner is the only service that may start containers (spec 0001). The scaffold boots on
// the sdk, refuses to start unless the engine lists the sandbox runtime, and serves health only;
// features #5 and #6 add the sandbox and the agent run.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/racherom/modular-platform/sdk"

	"github.com/racherom/ai-dev-platform/services/runner/internal/config"
	"github.com/racherom/ai-dev-platform/services/runner/internal/sandbox"
)

func main() { sdk.Run(config.Module, setup) }

// setup checks what runner cannot work without, then returns its handler. Every refusal logs its
// own line and wraps sdk.ErrStart, so the process exits 1 before it listens.
func setup(ctx context.Context, m *sdk.Module[config.Config, config.Uses]) (http.Handler, error) {
	host, err := m.Config.DockerHost(ctx)
	if err != nil {
		return nil, fmt.Errorf("docker host: %w", err)
	}
	runtime, err := m.Config.SandboxRuntime(ctx)
	if err != nil {
		return nil, fmt.Errorf("sandbox runtime: %w", err)
	}
	logsDir, err := m.Config.RunLogsDir(ctx)
	if err != nil {
		return nil, fmt.Errorf("run logs dir: %w", err)
	}
	if _, err := m.Uses.Postgresql(ctx); err != nil {
		return nil, fmt.Errorf("postgresql: %w", err)
	}

	engine, err := sandbox.NewEngine(host, nil)
	if err != nil {
		return nil, refuse(ctx, m.Log, "docker host refused", err)
	}
	if err := engine.CheckRuntime(ctx, string(runtime)); err != nil {
		if errors.Is(err, sandbox.ErrRuntimeMissing) {
			m.Log.ErrorContext(ctx, "runsc runtime not registered; run setup.sh")
			return nil, fmt.Errorf("%w: %w", sdk.ErrStart, err)
		}
		return nil, refuse(ctx, m.Log, "docker engine not reachable through the socket proxy", err)
	}
	if err := checkWritable(logsDir); err != nil {
		return nil, refuse(ctx, m.Log, "run logs dir not writable", err)
	}
	return http.NewServeMux(), nil
}

// refuse logs why the start stops and returns an error the sdk does not log a second time.
func refuse(ctx context.Context, log *slog.Logger, msg string, err error) error {
	log.ErrorContext(ctx, msg, slog.Any("error", err))
	return fmt.Errorf("%w: %s: %w", sdk.ErrStart, msg, err)
}

// checkWritable proves runner can create a file in dir, as each run log needs.
func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return err
	}
	name := f.Name()
	return errors.Join(f.Close(), os.Remove(name))
}
