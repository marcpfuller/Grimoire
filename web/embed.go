// Package web provides the embedded production build of the Grimoire UI.
package web

import "embed"

// Dist holds the compiled frontend assets under the "dist/" sub-tree.
// Callers should use [io/fs.Sub] to strip the "dist" prefix before serving.
//
//go:embed all:dist
var Dist embed.FS
