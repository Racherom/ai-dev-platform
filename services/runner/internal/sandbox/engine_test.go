package sandbox

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func engineAnswering(t *testing.T, status int, body string) *Engine {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/info" {
			t.Errorf("unexpected call %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	host, _ := url.Parse(srv.URL)
	host.Scheme = "tcp"
	e, err := NewEngine(host, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestCheckRuntime(t *testing.T) {
	cases := map[string]struct {
		status      int
		body        string
		wantMissing bool
		wantErr     bool
	}{
		"listed":       {200, `{"Runtimes":{"runc":{},"runsc":{"path":"/usr/bin/runsc"}}}`, false, false},
		"not listed":   {200, `{"Runtimes":{"runc":{}}}`, true, true},
		"no runtimes":  {200, `{}`, true, true},
		"proxy denies": {403, `Forbidden`, false, true},
		"bad body":     {200, `not json`, false, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := engineAnswering(t, tc.status, tc.body).CheckRuntime(context.Background(), "runsc")
			if (err != nil) != tc.wantErr {
				t.Fatalf("got %v, want error %v", err, tc.wantErr)
			}
			if errors.Is(err, ErrRuntimeMissing) != tc.wantMissing {
				t.Errorf("got %v, want ErrRuntimeMissing %v", err, tc.wantMissing)
			}
		})
	}
}

func TestNewEngineRefusesUnixSocket(t *testing.T) {
	host, _ := url.Parse("unix:///var/run/docker.sock")
	if _, err := NewEngine(host, nil); err == nil {
		t.Fatal("a unix socket host was accepted")
	}
}
