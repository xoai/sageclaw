package web

import "embed"

// DistFS contains the built Preact dashboard.
// Build with: cd web && npm run build
//
//go:embed dist/*
var DistFS embed.FS
