// Package migrations embeds the SQL migration files so internal/migrate
// can build a golang-migrate source from them without a filesystem path
// at runtime. Mirrors scripts/lua/embed.go's rationale.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
