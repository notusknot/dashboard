package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"beacon/internal/provider"
)

func init() { provider.Register("command", newCommand) }

type command struct {
	provider.Base
	argv    []string
	parse   string // "json" | "regex"
	pattern *regexp.Regexp
	rules   rules
}

func newCommand(cfg provider.Config) (provider.Provider, error) {
	c := &command{Base: provider.Base{Cfg: cfg}}
	var err error
	if c.argv, err = optStringSlice(cfg.Options, "command"); err != nil {
		return nil, err
	}
	if len(c.argv) == 0 {
		return nil, fmt.Errorf("missing required key %q (argv list)", "command")
	}
	if c.parse, err = optString(cfg.Options, "parse", "json"); err != nil {
		return nil, err
	}
	switch c.parse {
	case "json":
	case "regex":
		expr, err := reqString(cfg.Options, "regex")
		if err != nil {
			return nil, err
		}
		if c.pattern, err = regexp.Compile(expr); err != nil {
			return nil, fmt.Errorf("regex: %w", err)
		}
	default:
		return nil, fmt.Errorf("parse must be \"json\" or \"regex\", got %q", c.parse)
	}
	if c.rules, err = parseRules(cfg.Options); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *command) Poll(ctx context.Context) provider.Result {
	cmd := exec.CommandContext(ctx, c.argv[0], c.argv[1:]...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return provider.Errorf("%s: %v: %s", c.argv[0], err, tail(errb.String(), 300))
	}
	var data any
	if c.parse == "json" {
		if err := json.Unmarshal(out.Bytes(), &data); err != nil {
			return provider.Errorf("%s: invalid JSON output: %v", c.argv[0], err)
		}
	} else {
		m := c.pattern.FindStringSubmatch(out.String())
		if m == nil {
			return provider.Errorf("%s: regex did not match output", c.argv[0])
		}
		groups := map[string]any{}
		for i, name := range c.pattern.SubexpNames() {
			if name != "" {
				groups[name] = m[i]
			}
		}
		data = groups
	}
	return c.rules.apply(data)
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}
