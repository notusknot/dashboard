package main

import (
	"time"

	"beacon/internal/config"
	"beacon/internal/provider"
)

// demoSource fabricates one entry per render shape — healthy, warning,
// errored, stale, offline, pending, feed, metric grid — so the frontend can
// be reviewed without live services.
type demoSource struct{}

func demoConfig() config.File {
	cfg := config.Default()
	cfg.HostLabel = "demo · beacon"
	cfg.Links = []config.Link{
		{Label: "Home Assistant", URL: "#", Icon: "home"},
		{Label: "Immich", URL: "#", Icon: "image"},
		{Label: "Miniflux", URL: "#", Icon: "book"},
		{Label: "Navidrome", URL: "#", Icon: "music"},
		{Label: "Gitea", URL: "#", Icon: "terminal"},
		{Label: "Grafana", URL: "#", Icon: "activity"},
		{Label: "Vaultwarden", URL: "#", Icon: "shield"},
	}
	cfg.Engines = []config.Engine{
		{Name: "SearXNG", URL: "https://searxng.example.ts.net/search?q="},
		{Name: "Startpage", URL: "https://www.startpage.com/sp/search?query="},
	}
	return cfg
}

func (demoSource) Snapshot() []provider.Entry {
	now := time.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	e := func(id, typ, title, subtitle, icon string, interval time.Duration, res provider.Result, stale bool) provider.Entry {
		return provider.Entry{
			ID: id, Type: typ, Title: title, Subtitle: subtitle, Icon: icon,
			URL: "#", IntervalSeconds: int(interval / time.Second), Result: res, Stale: stale,
		}
	}
	return []provider.Entry{
		e("restic", "restic", "Backups", "restic", "shield", 15*time.Minute, provider.Result{
			Status:  provider.StatusError,
			Summary: "last snapshot 3d4h ago · 214 snapshots",
			Error:   "offsite snapshot has not run in 3 days",
			Metrics: map[string]any{
				"last snapshot": map[string]any{"t": "age", "at": ago(76 * time.Hour).UnixMilli(), "critAfterMs": (26 * time.Hour).Milliseconds()},
				"repo size":     "412 GB",
				"snapshots":     214,
			},
			UpdatedAt: ago(4 * time.Minute),
		}, false),
		e("disk", "disk", "Storage", "disk usage", "hdd", 5*time.Minute, provider.Result{
			Status:  provider.StatusWarn,
			Summary: "3 mounts · worst /data at 83%",
			Metrics: map[string]any{
				"/":        map[string]any{"t": "bar", "value": 41, "max": 100, "valueLabel": "41 / 100 GB", "warnAt": 80, "critAt": 92},
				"/data":    map[string]any{"t": "bar", "value": 83, "max": 100, "valueLabel": "3.7 / 4.4 TB", "warnAt": 80, "critAt": 92},
				"/backups": map[string]any{"t": "bar", "value": 58, "max": 100, "valueLabel": "2.6 / 4.4 TB", "warnAt": 80, "critAt": 92},
			},
			UpdatedAt: ago(90 * time.Second),
		}, false),
		e("syncthing", "syncthing", "Syncthing", "file sync", "sync", 30*time.Second, provider.Result{
			Status:  provider.StatusOK,
			Summary: "3 folders in sync · 4 devices connected",
			Metrics: map[string]any{
				"folders in sync":   "3 / 3",
				"devices connected": "4 / 4",
				"out of sync items": 0,
			},
			UpdatedAt: ago(2 * time.Hour), // stale: poller stopped 2h ago
		}, true),
		e("adguard", "adguard", "AdGuard Home", "dns filter", "filter", time.Minute, provider.Result{
			Status:  provider.StatusOK,
			Summary: "48.2k queries · 31.4% blocked",
			Metrics: map[string]any{
				"queries":  map[string]any{"t": "stat", "value": "48.2k"},
				"blocked":  "31.4%",
				"upstream": map[string]any{"t": "pill", "value": "Online", "kind": "done"},
			},
			UpdatedAt: ago(30 * time.Second),
		}, false),
		e("ntfy", "ntfy", "ntfy", "alert feed", "bell", 15*time.Second, provider.Result{
			Status:  provider.StatusOK,
			Summary: "4 messages in last 24h",
			Items: []provider.FeedItem{
				{Time: ago(40 * time.Minute), Title: "restic offsite backup failed", Priority: 5},
				{Time: ago(3 * time.Hour), Title: "/data usage above 80%", Priority: 4},
				{Time: ago(5 * time.Hour), Title: "syncthing: laptop connected", Priority: 3},
				{Time: ago(9 * time.Hour), Title: "nightly snapshot complete", Priority: 3},
			},
			UpdatedAt: ago(8 * time.Second),
		}, false),
		e("fitness", "openibex", "OpenIbex", "training load", "activity", 15*time.Minute, provider.Result{
			Status:  provider.StatusOK,
			Summary: "form -9 · fitness 62 · moderate",
			Metrics: map[string]any{
				"form":      map[string]any{"t": "stat", "value": "-9", "unit": "TSB"},
				"fitness":   map[string]any{"t": "spark", "points": []int{41, 43, 44, 46, 48, 49, 51, 53, 55, 57, 58, 60, 61, 62}, "value": "62"},
				"fatigue":   71,
				"week TSS":  512,
				"readiness": map[string]any{"t": "pill", "value": "moderate", "kind": "info"},
			},
			UpdatedAt: ago(6 * time.Minute),
		}, false),
		e("llm", "http-health", "Local LLM", "ollama", "cpu", time.Minute, provider.Result{
			Status:    provider.StatusUnknown,
			Summary:   "check failed",
			Error:     "no response from ollama host",
			UpdatedAt: ago(22 * time.Minute),
		}, false),
		e("uptime", "command", "Uptime", "reachability", "globe", 30*time.Second, provider.Result{
			Status:    provider.StatusOK,
			Summary:   "6 / 7 hosts reachable",
			Metrics:   map[string]any{"reachable": "6 / 7", "worst latency": "42ms"},
			UpdatedAt: ago(15 * time.Second),
		}, false),
		e("immich", "http-health", "Immich", "photos", "image", 5*time.Minute, provider.Result{
			Status:  provider.StatusUnknown,
			Summary: "first poll pending",
		}, false),
	}
}
