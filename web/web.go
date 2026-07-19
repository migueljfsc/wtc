// Package web embeds the timeline UI. Toolchain-free by design:
// hand-written HTML/CSS/vanilla JS, no node, no bundler.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var static embed.FS

// FS returns the embedded UI rooted at the static directory.
func FS() fs.FS {
	sub, err := fs.Sub(static, "static")
	if err != nil {
		panic("web: embedded static tree missing: " + err.Error()) // impossible: compile-time embed
	}
	return sub
}
