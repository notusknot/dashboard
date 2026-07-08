package provider

import (
	"fmt"
	"sort"
	"strings"
)

// Constructor builds a Provider from its config table.
type Constructor func(cfg Config) (Provider, error)

var registry = map[string]Constructor{}

// Register makes a provider type available to config. Call from init().
func Register(typ string, c Constructor) {
	if _, dup := registry[typ]; dup {
		panic("duplicate provider type: " + typ)
	}
	registry[typ] = c
}

// New builds the provider for cfg.Type, failing on unknown types.
func New(cfg Config) (Provider, error) {
	c, ok := registry[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("unknown provider type %q (known: %s)", cfg.Type, strings.Join(Types(), ", "))
	}
	return c(cfg)
}

// Types lists registered provider types, sorted.
func Types() []string {
	out := make([]string, 0, len(registry))
	for t := range registry {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
