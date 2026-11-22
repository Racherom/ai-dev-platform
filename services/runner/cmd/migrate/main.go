// Command migrate is runner's one shot runner-migrate step (spec 0001): goose and River's migrations
// against runner's own database, as runner's own login, then exit.
package main

import (
	"github.com/racherom/ai-dev-platform/libs/dbmigrate"

	"github.com/racherom/ai-dev-platform/services/runner/migrations"
)

func main() { dbmigrate.Main(migrations.FS) }
