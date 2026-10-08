package main

import "embed"

// webFS embeds the single-page management UI.
//
//go:embed all:web
var webFS embed.FS

// examplesFS embeds frp's commented example configurations for reference.
//
//go:embed all:examples
var examplesFS embed.FS
