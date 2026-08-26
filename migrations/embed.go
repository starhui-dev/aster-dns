package migrations

import "embed"

const LatestVersion uint = 4

// Files contains the ordered SQL migrations used by the server migrate command.
//
//go:embed *.sql
var Files embed.FS
