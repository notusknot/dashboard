// Package web holds the static frontend, embedded so the whole app ships as
// one binary.
package web

import "embed"

//go:embed index.html style.css app.js fonts
var FS embed.FS
