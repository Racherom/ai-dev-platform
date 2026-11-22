// Package migrations holds control's goose SQL migrations, embedded into the migrate binary
// (spec 0001). Add files as NNNNN_name.sql; feature #3 adds the first tables.
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS
