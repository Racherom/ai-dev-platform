// Package sandbox talks to the Docker engine through the socket proxy, the only way runner reaches
// it (spec 0001). The scaffold holds the start check; feature #5 adds creating and removing sandboxes.
package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// ErrRuntimeMissing reports an engine that does not list the sandbox runtime.
var ErrRuntimeMissing = errors.New("sandbox runtime not registered")

// Engine is the Docker engine API behind the socket proxy.
type Engine struct {
	base   *url.URL
	client *http.Client
}

// NewEngine returns an Engine for host, like tcp://socket-proxy:2375. Only tcp, http and https
// are accepted, so runner can never be pointed at the raw unix socket.
func NewEngine(host *url.URL, client *http.Client) (*Engine, error) {
	base := *host
	switch base.Scheme {
	case "tcp", "http":
		base.Scheme = "http"
	case "https":
	default:
		return nil, fmt.Errorf("docker host scheme %q: want tcp, http or https", host.Scheme)
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Engine{base: &base, client: client}, nil
}

// CheckRuntime asks the engine (GET /info) whether it lists runtime, and returns an error wrapping
// ErrRuntimeMissing when it does not.
func (e *Engine) CheckRuntime(ctx context.Context, runtime string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.base.JoinPath("info").String(), nil)
	if err != nil {
		return fmt.Errorf("engine info: %w", err)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("engine info: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("engine info: status %d", resp.StatusCode)
	}
	var info struct {
		Runtimes map[string]json.RawMessage `json:"Runtimes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return fmt.Errorf("engine info: %w", err)
	}
	if _, ok := info.Runtimes[runtime]; !ok {
		return fmt.Errorf("%w: %s", ErrRuntimeMissing, runtime)
	}
	return nil
}
