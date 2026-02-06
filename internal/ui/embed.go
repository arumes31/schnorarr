package ui

import "embed"

//go:embed web/templates/*.html
var TemplateFS embed.FS
