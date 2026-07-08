// Package web holds the static frontend, embedded so the whole app ships as
// one binary.
package web

import "embed"

//go:embed index.html style.css app.js sw.js manifest.webmanifest fonts
//go:embed icon.svg apple-touch-icon.png icon-192.png icon-512.png
var FS embed.FS
