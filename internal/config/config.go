// Package config loads and validates beacon's TOML config file.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"beacon/internal/provider"
)

// Link is a quick-launch entry shown below the command bar.
type Link struct {
	Label string `toml:"label" json:"label"`
	URL   string `toml:"url" json:"url"`
	Icon  string `toml:"icon" json:"icon,omitempty"`
}

// Engine is a web search engine for the command bar. URL is a prefix the
// query gets appended to.
type Engine struct {
	Name string `toml:"name" json:"name"`
	URL  string `toml:"url" json:"url"`
}

// File is the fully parsed and validated config.
type File struct {
	Listen         string
	StaleAfter     time.Duration
	RefreshSeconds int
	HostLabel      string
	Links          []Link
	Engines        []Engine
	Providers      []provider.Config
}

func Default() File {
	return File{
		Listen:         "127.0.0.1:8383",
		StaleAfter:     15 * time.Minute,
		RefreshSeconds: 30,
	}
}

type rawFile struct {
	Server struct {
		Listen         string `toml:"listen"`
		StaleAfter     string `toml:"staleAfter"`
		RefreshSeconds int    `toml:"refreshSeconds"`
		HostLabel      string `toml:"hostLabel"`
	} `toml:"server"`
	Links     []Link           `toml:"links"`
	Engines   []Engine         `toml:"engines"`
	Providers []map[string]any `toml:"providers"`
}

// Load reads, parses and validates the config, failing fast with a precise
// message on any problem.
func Load(path string) (File, error) {
	f := Default()
	var raw rawFile
	meta, err := toml.DecodeFile(path, &raw)
	if err != nil {
		return f, fmt.Errorf("%s: %w", path, err)
	}
	for _, k := range meta.Undecoded() {
		// provider tables decode into maps; their nested keys are consumed by
		// the provider constructors, not here.
		if strings.HasPrefix(k.String(), "providers.") {
			continue
		}
		return f, fmt.Errorf("%s: unknown config key %q", path, k.String())
	}

	if raw.Server.Listen != "" {
		f.Listen = raw.Server.Listen
	}
	if raw.Server.RefreshSeconds > 0 {
		f.RefreshSeconds = raw.Server.RefreshSeconds
	}
	f.HostLabel = raw.Server.HostLabel
	if raw.Server.StaleAfter != "" {
		d, err := time.ParseDuration(raw.Server.StaleAfter)
		if err != nil {
			return f, fmt.Errorf("server.staleAfter: %w", err)
		}
		f.StaleAfter = d
	}
	f.Links = raw.Links
	f.Engines = raw.Engines

	seen := map[string]bool{}
	for i, m := range raw.Providers {
		cfg, err := parseProvider(m)
		if err != nil {
			return f, fmt.Errorf("providers[%d]: %w", i, err)
		}
		if seen[cfg.ID] {
			return f, fmt.Errorf("providers[%d]: duplicate id %q", i, cfg.ID)
		}
		seen[cfg.ID] = true
		if err := checkSecretFiles(cfg.Options); err != nil {
			return f, fmt.Errorf("providers[%d] (id=%s): %w", i, cfg.ID, err)
		}
		f.Providers = append(f.Providers, cfg)
	}
	return f, nil
}

func parseProvider(m map[string]any) (provider.Config, error) {
	cfg := provider.Config{Interval: time.Minute, Options: m}
	var err error
	if cfg.ID, err = popString(m, "id"); err != nil {
		return cfg, err
	}
	if cfg.ID == "" {
		return cfg, fmt.Errorf("missing required key %q", "id")
	}
	if cfg.Type, err = popString(m, "type"); err != nil {
		return cfg, err
	}
	if cfg.Type == "" {
		return cfg, fmt.Errorf("missing required key %q", "type")
	}
	if cfg.Title, err = popString(m, "title"); err != nil {
		return cfg, err
	}
	if cfg.Title == "" {
		cfg.Title = cfg.ID
	}
	if cfg.Subtitle, err = popString(m, "subtitle"); err != nil {
		return cfg, err
	}
	if cfg.URL, err = popString(m, "link"); err != nil {
		return cfg, err
	}
	if cfg.URL == "" {
		// fall back to the provider's own endpoint (syncthing GUI, AdGuard
		// UI, …) so cards link somewhere useful by default.
		if u, ok := m["url"].(string); ok {
			cfg.URL = u
		}
	}
	if cfg.Icon, err = popString(m, "icon"); err != nil {
		return cfg, err
	}
	for key, dst := range map[string]*time.Duration{"interval": &cfg.Interval, "staleAfter": &cfg.StaleAfter} {
		s, err := popString(m, key)
		if err != nil {
			return cfg, err
		}
		if s == "" {
			continue
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return cfg, fmt.Errorf("%s: %w", key, err)
		}
		if d <= 0 {
			return cfg, fmt.Errorf("%s must be positive", key)
		}
		*dst = d
	}
	return cfg, nil
}

func popString(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", nil
	}
	delete(m, key)
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string, got %T", key, v)
	}
	return s, nil
}

// checkSecretFiles verifies every *File option points at a readable file, so
// a bad credential path fails loudly at startup instead of silently at poll
// time. The files themselves are (re-)read at poll time so rotation works.
func checkSecretFiles(v any) error {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if s, ok := val.(string); ok && strings.HasSuffix(k, "File") {
				fh, err := os.Open(s)
				if err != nil {
					return fmt.Errorf("%s: %v", k, err)
				}
				fh.Close()
				continue
			}
			if err := checkSecretFiles(val); err != nil {
				return err
			}
		}
	case []any:
		for _, val := range t {
			if err := checkSecretFiles(val); err != nil {
				return err
			}
		}
	}
	return nil
}
