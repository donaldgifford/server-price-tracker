// Package web embeds and serves the alert review UI assets.
//
// Templates live as *.templ files compiled to *_templ.go via the templ CLI
// (`make templ-generate`). HTMX, Alpine, and the page CSS live in static/
// and are served as-is. Embedding lets the binary ship as a single file.
package web

import "embed"

// StaticFS holds the static JS/CSS assets served at /static/*.
//
//go:embed static/*
var StaticFS embed.FS
