// Command migrate is forge's one shot forge-migrate step (spec 0001): goose and River's migrations
// against forge's own database, as forge's own login, then exit.
package main

import (
	"github.com/racherom/ai-dev-platform/libs/dbmigrate"

	"github.com/racherom/ai-dev-platform/services/forge/migrations"
)

func main() { dbmigrate.Main(migrations.FS) }
