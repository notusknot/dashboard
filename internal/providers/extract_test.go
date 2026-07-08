package providers

import (
	"encoding/json"
	"testing"
	"time"

	"beacon/internal/provider"
)

func decode(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestLookup(t *testing.T) {
	data := decode(t, `{"a":{"b":[10,{"c":"deep"}]},"top":5}`)
	cases := []struct {
		path string
		want any
		ok   bool
	}{
		{"top", 5.0, true},
		{"a.b.0", 10.0, true},
		{"a.b.1.c", "deep", true},
		{"a.missing", nil, false},
		{"a.b.9", nil, false},
		{"top.deeper", nil, false},
	}
	for _, c := range cases {
		got, ok := lookup(data, c.path)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("lookup(%q) = %v,%v; want %v,%v", c.path, got, ok, c.want, c.ok)
		}
	}
}

func TestStatusThresholds(t *testing.T) {
	mk := func(warn, crit float64) rules {
		r, err := parseRules(map[string]any{
			"statusFrom": map[string]any{"path": "pct", "warnAbove": warn, "errorAbove": crit},
		})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := mk(80, 90)
	for pct, want := range map[float64]provider.Status{
		50: provider.StatusOK,
		85: provider.StatusWarn,
		95: provider.StatusError,
	} {
		got := r.apply(map[string]any{"pct": pct})
		if got.Status != want {
			t.Errorf("pct=%v: status %s, want %s", pct, got.Status, want)
		}
	}
	// string values (regex capture groups) coerce to numbers
	if got := r.apply(map[string]any{"pct": "97.5"}); got.Status != provider.StatusError {
		t.Errorf("string pct: status %s, want error", got.Status)
	}
	// missing path -> unknown
	if got := r.apply(map[string]any{}); got.Status != provider.StatusUnknown {
		t.Errorf("missing path: status %s, want unknown", got.Status)
	}
}

func TestStatusBelow(t *testing.T) {
	r, err := parseRules(map[string]any{
		"statusFrom": map[string]any{"path": "free", "errorBelow": int64(10)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.apply(map[string]any{"free": 5.0}); got.Status != provider.StatusError {
		t.Errorf("below: status %s, want error", got.Status)
	}
	if got := r.apply(map[string]any{"free": 50.0}); got.Status != provider.StatusOK {
		t.Errorf("above: status %s, want ok", got.Status)
	}
}

func TestStatusEquals(t *testing.T) {
	r, err := parseRules(map[string]any{
		"statusFrom": map[string]any{"path": "state", "equals": "up"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.apply(map[string]any{"state": "up"}); got.Status != provider.StatusOK {
		t.Errorf("equal: status %s, want ok", got.Status)
	}
	got := r.apply(map[string]any{"state": "degraded"})
	if got.Status != provider.StatusError || got.Error == "" {
		t.Errorf("unequal: status %s error %q, want error with detail", got.Status, got.Error)
	}
	// numeric equals across TOML int64 / JSON float64
	rn, err := parseRules(map[string]any{
		"statusFrom": map[string]any{"path": "code", "equals": int64(200)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := rn.apply(decode(t, `{"code":200}`)); got.Status != provider.StatusOK {
		t.Errorf("int64 vs float64 equals: status %s, want ok", got.Status)
	}
}

func TestSummaryAndMetrics(t *testing.T) {
	r, err := parseRules(map[string]any{
		"summaryTemplate": "{name} at {load.0}% ({bogus})",
		"metrics":         map[string]any{"load": "load.0", "host": "name"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := r.apply(decode(t, `{"name":"srv1","load":[42.0,13]}`))
	if got.Summary != "srv1 at 42% ({bogus})" {
		t.Errorf("summary = %q", got.Summary)
	}
	if got.Metrics["load"] != 42.0 || got.Metrics["host"] != "srv1" {
		t.Errorf("metrics = %v", got.Metrics)
	}
	if got.Status != provider.StatusOK {
		t.Errorf("status = %s, want ok (no rule)", got.Status)
	}
}

func TestParseRulesRejectsEmptyStatusFrom(t *testing.T) {
	_, err := parseRules(map[string]any{"statusFrom": map[string]any{"path": "x"}})
	if err == nil {
		t.Fatal("want error for statusFrom without equals/thresholds")
	}
}

func TestGradePct(t *testing.T) {
	if gradePct(79, 80, 90) != provider.StatusOK ||
		gradePct(80, 80, 90) != provider.StatusWarn ||
		gradePct(90, 80, 90) != provider.StatusError {
		t.Fatal("gradePct thresholds wrong")
	}
}

func TestResticResult(t *testing.T) {
	now := time.Now()
	fresh := resticResult([]time.Time{now.Add(-40 * time.Hour), now.Add(-2 * time.Hour)}, 26*time.Hour, now)
	if fresh.Status != provider.StatusOK {
		t.Errorf("fresh: status %s, want ok (%s)", fresh.Status, fresh.Error)
	}
	stale := resticResult([]time.Time{now.Add(-30 * time.Hour)}, 26*time.Hour, now)
	if stale.Status != provider.StatusError {
		t.Errorf("stale: status %s, want error", stale.Status)
	}
	empty := resticResult(nil, 26*time.Hour, now)
	if empty.Status != provider.StatusError {
		t.Errorf("empty: status %s, want error", empty.Status)
	}
}
