// Package server exposes the JSON API and the embedded frontend.
package server

import (
	"encoding/json"
	"mime"
	"net/http"
	"time"

	"beacon/internal/config"
	"beacon/internal/provider"
	"beacon/web"
)

func init() {
	// FileServerFS picks Content-Type from the extension; .webmanifest isn't a
	// built-in so register it, otherwise the manifest is served as octet-stream.
	mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// Source supplies the latest provider entries; the Runner in live mode, a
// fabricated set in --demo mode.
type Source interface {
	Snapshot() []provider.Entry
}

func New(src Source, cfg config.File) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]any{
			"generatedAt":    time.Now(),
			"refreshSeconds": cfg.RefreshSeconds,
			"hostLabel":      cfg.HostLabel,
			"links":          cfg.Links,
			"engines":        cfg.Engines,
			"providers":      src.Snapshot(),
		})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.Handle("/", http.FileServerFS(web.FS))
	return mux
}
