package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"beacon/internal/provider"
)

func init() { provider.Register("openibex", newOpenibex) }

// openibex reads an OpenIbex instance's read-only HTTP API (/api/v1) to show a
// live training-load card: form (TSB), fitness (CTL) with an 84-day sparkline,
// fatigue (ATL), weekly TSS, and a readiness pill. Deep fatigue (low form) or
// monotonous training flip the card to warn — the "ease off" nudge.
//
// Enable the API in OpenIbex by setting API_TOKEN; point apiTokenFile at that
// same token. See https://github.com/notusknot/openibex (README → HTTP API).
type openibex struct {
	provider.Base
	url               string
	apiTokenFile      string
	formWarnBelow     float64
	monotonyWarnAbove float64
}

func newOpenibex(cfg provider.Config) (provider.Provider, error) {
	o := &openibex{Base: provider.Base{Cfg: cfg}}
	var err error
	if o.url, err = reqString(cfg.Options, "url"); err != nil {
		return nil, err
	}
	if o.apiTokenFile, err = reqString(cfg.Options, "apiTokenFile"); err != nil {
		return nil, err
	}
	if o.formWarnBelow, err = optFloat(cfg.Options, "formWarnBelow", -30); err != nil {
		return nil, err
	}
	if o.monotonyWarnAbove, err = optFloat(cfg.Options, "monotonyWarnAbove", 2.0); err != nil {
		return nil, err
	}
	o.url = strings.TrimRight(o.url, "/")
	return o, nil
}

func (o *openibex) Poll(ctx context.Context) provider.Result {
	tok, err := readSecret(o.apiTokenFile)
	if err != nil {
		return provider.Errorf("apiTokenFile: %v", err)
	}
	hdr := map[string]string{"Authorization": "Bearer " + tok}

	// OpenIbex formats some KPI fields as display strings (monotony is "1.42",
	// or "—" when it can't be computed yet), so decode every numeric field as a
	// jsonNum that accepts a number or a numeric string.
	var sum struct {
		Fitness   jsonNum `json:"fitness"` // CTL — chronic (42-day) load
		Fatigue   jsonNum `json:"fatigue"` // ATL — acute (7-day) load
		Form      jsonNum `json:"form"`    // TSB — CTL minus ATL
		WeekTss   jsonNum `json:"weekTss"`
		Monotony  jsonNum `json:"monotony"`
		Readiness struct {
			Label string `json:"label"`
		} `json:"readiness"`
	}
	if err := getJSON(ctx, o.url+"/api/v1/summary", hdr, &sum); err != nil {
		return provider.Errorf("%v", err)
	}

	ctl, atl, tsb := round(float64(sum.Fitness)), round(float64(sum.Fatigue)), round(float64(sum.Form))
	metrics := map[string]any{
		"form":     map[string]any{"t": "stat", "value": fmt.Sprintf("%+d", tsb), "unit": "TSB"},
		"fitness":  o.fitnessMetric(ctx, hdr, ctl),
		"fatigue":  atl,
		"week TSS": round(float64(sum.WeekTss)),
	}
	if sum.Readiness.Label != "" {
		metrics["readiness"] = map[string]any{"t": "pill", "value": sum.Readiness.Label, "kind": readinessKind(sum.Readiness.Label)}
	}

	res := provider.Result{Status: provider.StatusOK, Metrics: metrics}
	res.Summary = fmt.Sprintf("form %+d · fitness %d", tsb, ctl)
	if sum.Readiness.Label != "" {
		res.Summary += " · " + strings.ToLower(sum.Readiness.Label)
	}
	mono := float64(sum.Monotony) // NaN when OpenIbex sent "—" (not yet computable)
	switch {
	case float64(sum.Form) < o.formWarnBelow:
		res.Status = provider.StatusWarn
		res.Summary = fmt.Sprintf("form %+d — high fatigue, ease off", tsb)
	case o.monotonyWarnAbove > 0 && !math.IsNaN(mono) && mono > o.monotonyWarnAbove:
		res.Status = provider.StatusWarn
		res.Summary = fmt.Sprintf("monotony %.1f — vary the training load", mono)
	}
	return res
}

// fitnessMetric renders CTL as a sparkline of the 84-day series when that
// endpoint is reachable, falling back to a plain stat if it isn't — a series
// hiccup shouldn't blank the whole card.
func (o *openibex) fitnessMetric(ctx context.Context, hdr map[string]string, ctl int) map[string]any {
	var s struct {
		Series []struct {
			Ctl float64 `json:"ctl"`
		} `json:"series"`
	}
	if err := getJSON(ctx, o.url+"/api/v1/series", hdr, &s); err == nil && len(s.Series) > 1 {
		pts := make([]int, len(s.Series))
		for i, p := range s.Series {
			pts[i] = round(p.Ctl)
		}
		return map[string]any{"t": "spark", "points": pts, "value": fmt.Sprintf("%d", ctl)}
	}
	return map[string]any{"t": "stat", "value": fmt.Sprintf("%d", ctl)}
}

// readinessKind maps an OpenIbex readiness label to a pill colour. The labels
// are free text, so match common substrings and fall back to a neutral pill.
func readinessKind(label string) string {
	l := strings.ToLower(label)
	switch {
	case containsAny(l, "fresh", "ready", "optimal", "good", "high"):
		return "done"
	case containsAny(l, "fatigued", "tired", "low", "poor"):
		return "failed"
	default:
		return "info"
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func round(f float64) int {
	if math.IsNaN(f) {
		return 0
	}
	return int(math.Round(f))
}

// jsonNum decodes a JSON number OR a numeric string ("1.42"). OpenIbex formats
// some KPI fields as display strings — monotony is "1.42", or "—" when it can't
// be computed yet (needs ~7 days of variance) — so a plain float64 field would
// fail to unmarshal. An unparseable value (e.g. "—", null) decodes to NaN;
// callers treat NaN as "unavailable".
type jsonNum float64

func (n *jsonNum) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if f, ok := toFloat(v); ok {
		*n = jsonNum(f)
		return nil
	}
	*n = jsonNum(math.NaN())
	return nil
}
