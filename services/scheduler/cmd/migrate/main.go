// Command migrate is scheduler's one shot scheduler-migrate step (spec 0001): goose and River's migrations
// against scheduler's own database, as scheduler's own login, then exit.
package main

import (
	"github.com/racherom/ai-dev-platform/libs/dbmigrate"

	"github.com/racherom/ai-dev-platform/services/scheduler/migrations"
)

func main() { dbmigrate.Main(migrations.FS) }
