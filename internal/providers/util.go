// Package providers implements the built-in provider types. Each type lives
// in one file and registers itself via init().
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

const maxBody = 4 << 20

func getBody(ctx context.Context, url string, hdr map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func getJSON(ctx context.Context, url string, hdr map[string]string, out any) error {
	body, code, err := getBody(ctx, url, hdr)
	if err != nil {
		return err
	}
	if code >= 400 {
		return fmt.Errorf("GET %s: HTTP %d", url, code)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("GET %s: invalid JSON: %v", url, err)
	}
	return nil
}

// readSecret reads a *File credential at poll time so rotation works.
func readSecret(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// ── option readers over provider.Config.Options ─────────────────────────────

func reqString(o map[string]any, key string) (string, error) {
	s, err := optString(o, key, "")
	if err != nil {
		return "", err
	}
	if s == "" {
		return "", fmt.Errorf("missing required key %q", key)
	}
	return s, nil
}

func optString(o map[string]any, key, def string) (string, error) {
	v, ok := o[key]
	if !ok {
		return def, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string, got %T", key, v)
	}
	return s, nil
}

func optInt(o map[string]any, key string, def int) (int, error) {
	v, ok := o[key]
	if !ok {
		return def, nil
	}
	switch t := v.(type) {
	case int64:
		return int(t), nil
	case float64:
		return int(t), nil
	}
	return 0, fmt.Errorf("%s must be an integer, got %T", key, v)
}

func optFloat(o map[string]any, key string, def float64) (float64, error) {
	v, ok := o[key]
	if !ok {
		return def, nil
	}
	f, ok := toFloat(v)
	if !ok {
		return 0, fmt.Errorf("%s must be a number, got %T", key, v)
	}
	return f, nil
}

func optDuration(o map[string]any, key string, def time.Duration) (time.Duration, error) {
	s, err := optString(o, key, "")
	if err != nil || s == "" {
		return def, err
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

func optStringSlice(o map[string]any, key string) ([]string, error) {
	v, ok := o[key]
	if !ok {
		return nil, nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a list of strings, got %T", key, v)
	}
	out := make([]string, len(raw))
	for i, e := range raw {
		s, ok := e.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string, got %T", key, i, e)
		}
		out[i] = s
	}
	return out, nil
}

func optStringMap(o map[string]any, key string) (map[string]string, error) {
	v, ok := o[key]
	if !ok {
		return nil, nil
	}
	raw, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a table of strings, got %T", key, v)
	}
	out := make(map[string]string, len(raw))
	for k, e := range raw {
		s, ok := e.(string)
		if !ok {
			return nil, fmt.Errorf("%s.%s must be a string, got %T", key, k, e)
		}
		out[k] = s
	}
	return out, nil
}

// ── formatting helpers ───────────────────────────────────────────────────────

func humanDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h, m := int(d.Hours()), int(d.Minutes())%60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	default:
		days, h := int(d.Hours())/24, int(d.Hours())%24
		if h == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd%dh", days, h)
	}
}

func humanBytes(b float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	i := 0
	for b >= 1024 && i < len(units)-1 {
		b /= 1024
		i++
	}
	if b >= 100 || i == 0 {
		return fmt.Sprintf("%.0f %s", b, units[i])
	}
	return fmt.Sprintf("%.1f %s", b, units[i])
}

func humanCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}
