// Package web embeds the built frontend. `pnpm --dir web build` writes it to
// web/static; a plain `go build` without that step embeds only .gitkeep.
package web

import "embed"

//go:embed all:static
var Static embed.FS
