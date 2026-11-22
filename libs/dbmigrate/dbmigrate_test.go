package dbmigrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	file := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o400); err != nil {
			t.Fatal(err)
		}
		return p
	}
	pw, empty := file("pw", "forge-secret\n"), file("empty", "\n")
	good := func() map[string]string {
		return map[string]string{
			"ADP_DB_HOST":          "postgres",
			"ADP_DB_PORT":          "5432",
			"ADP_DB_NAME":          "forge",
			"ADP_DB_USER":          "adp_forge",
			"ADP_DB_PASSWORD_FILE": pw,
		}
	}
	lookup := func(env map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	}

	s, err := Load(lookup(good()))
	if err != nil {
		t.Fatal(err)
	}
	if s.Host != "postgres" || s.Port != 5432 || s.Database != "forge" || s.User != "adp_forge" || s.Password.Reveal() != "forge-secret" {
		t.Errorf("unexpected settings %+v", s)
	}

	cases := map[string]struct {
		change func(map[string]string)
		want   string
	}{
		"missing host":    {func(e map[string]string) { delete(e, "ADP_DB_HOST") }, "ADP_DB_HOST: missing"},
		"blank user":      {func(e map[string]string) { e["ADP_DB_USER"] = " " }, "ADP_DB_USER: missing"},
		"bad port":        {func(e map[string]string) { e["ADP_DB_PORT"] = "not a port" }, "ADP_DB_PORT: invalid"},
		"zero port":       {func(e map[string]string) { e["ADP_DB_PORT"] = "0" }, "ADP_DB_PORT: invalid"},
		"empty password":  {func(e map[string]string) { e["ADP_DB_PASSWORD_FILE"] = empty }, "is empty"},
		"no such file":    {func(e map[string]string) { e["ADP_DB_PASSWORD_FILE"] = filepath.Join(dir, "nope") }, "cannot be read"},
		"password as env": {func(e map[string]string) { delete(e, "ADP_DB_PASSWORD_FILE"); e["ADP_DB_PASSWORD"] = "x" }, "ADP_DB_PASSWORD_FILE: missing"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			env := good()
			tc.change(env)
			_, err := Load(lookup(env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error with %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "forge-secret") {
				t.Error("the error repeats the password")
			}
		})
	}
}
