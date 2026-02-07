package ui

import (
	"embed"
	"io/fs"
)

//go:embed web/templates/*.html
var TemplateFS embed.FS

//go:embed web/static
var staticFS embed.FS

// StaticFS holds the sub-filesystem for static assets starting from web/static
var StaticFS fs.FS

func init() {
	var err error
	StaticFS, err = fs.Sub(staticFS, "web/static")
	if err != nil {
		panic("failed to initialize static filesystem: " + err.Error())
	}
}
