// Package schemas embeds agent form schemas for the dashboard.
package schemas

import "embed"

//go:embed *.json
var FS embed.FS
