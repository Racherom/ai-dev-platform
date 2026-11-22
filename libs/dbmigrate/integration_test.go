//go:build integration

package dbmigrate

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/racherom/modular-platform/sdk"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// postgresImage is the stack's Postgres pin (spec 0001, Scaffold pins).
const postgresImage = "postgres:18.6-alpine3.24"

// TestRunAsServiceLogin migrates a database the way <service>-migrate does: as a login that owns
// its database and is no superuser, as provision leaves it. A second run applies nothing.
func TestRunAsServiceLogin(t *testing.T) {
	ctx := t.Context()
	ctr, err := postgres.Run(ctx, postgresImage,
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("adp_owner"),
		postgres.WithPassword("owner-password"),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatal(err)
	}
	ownerURL, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := pgx.Connect(ctx, ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(context.Background())
	for _, stmt := range []string{
		"CREATE ROLE adp_forge LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD 'forge-password'",
		"CREATE DATABASE forge OWNER adp_forge",
	} {
		if _, err := owner.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}

	endpoint, err := ctr.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	s := Settings{Host: host, Port: uint16(port), Database: "forge", User: "adp_forge", Password: sdk.NewSecret("forge-password")}
	migrations := fstest.MapFS{
		"00001_baseline.sql": {Data: []byte("-- +goose Up\nCREATE TABLE probe (id bigint PRIMARY KEY);\n-- +goose Down\nDROP TABLE probe;\n")},
	}

	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	if err := Run(ctx, s, migrations, log); err != nil {
		t.Fatalf("first run: %v\n%s", err, logs.String())
	}
	logs.Reset()
	if err := Run(ctx, s, migrations, log); err != nil {
		t.Fatalf("second run: %v\n%s", err, logs.String())
	}
	if !strings.Contains(logs.String(), `"msg":"goose up","applied":0`) || !strings.Contains(logs.String(), `"msg":"river migrate","applied":0`) {
		t.Errorf("the second run applied something:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "forge-password") {
		t.Error("the password reached a log line")
	}

	// The tables live in forge, owned by adp_forge.
	q, err := pgx.Connect(ctx, strings.Replace(ownerURL, "/postgres?", "/forge?", 1))
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close(context.Background())
	for _, table := range []string{"goose_db_version", "probe", "river_job", "river_migration"} {
		var tableOwner string
		err := q.QueryRow(ctx, "SELECT tableowner FROM pg_tables WHERE schemaname = 'public' AND tablename = $1", table).Scan(&tableOwner)
		if err != nil {
			t.Errorf("table %s: %v", table, err)
			continue
		}
		if tableOwner != "adp_forge" {
			t.Errorf("table %s is owned by %s, want adp_forge", table, tableOwner)
		}
	}
}
