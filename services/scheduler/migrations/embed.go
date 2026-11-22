// Package migrations holds scheduler's goose SQL migrations, embedded into its migrate binary (spec
// 0001). Add files as NNNNN_name.sql; feature #3 adds scheduler's first tables.
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS
