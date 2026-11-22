package main

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
	env := map[string]string{
		"ADP_DB_HOST":                  "postgres",
		"ADP_DB_PORT":                  "5432",
		"ADP_DB_NAME":                  "adp",
		"ADP_DB_OWNER_USER":            "adp_owner",
		"ADP_DB_OWNER_PASSWORD_FILE":   file("owner", "owner-secret\n"),
		"ADP_DB_CONTROL_PASSWORD_FILE": file("control", "control-secret\n"),
		"ADP_DB_RUNNER_PASSWORD_FILE":  file("runner", "runner-secret"),
	}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	s, err := load(lookup)
	if err != nil {
		t.Fatal(err)
	}
	if s.port != 5432 || s.ownerPassword.Reveal() != "owner-secret" || s.roles[0].Password.Reveal() != "control-secret" {
		t.Errorf("unexpected settings %+v", s)
	}

	env["ADP_DB_PORT"] = "not a port"
	env["ADP_DB_RUNNER_PASSWORD_FILE"] = file("empty", "\n")
	delete(env, "ADP_DB_HOST")
	_, err = load(lookup)
	if err == nil {
		t.Fatal("bad settings were accepted")
	}
	for _, want := range []string{"ADP_DB_HOST: missing", "ADP_DB_PORT: invalid", "ADP_DB_RUNNER_PASSWORD_FILE: the file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	for _, secret := range []string{"owner-secret", "control-secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error repeats a secret")
		}
	}
}
