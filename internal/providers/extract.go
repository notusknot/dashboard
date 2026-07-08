package providers

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"beacon/internal/provider"
)

// rules is the shared extraction/threshold engine behind the generic
// providers (http-json, command): pull values out of decoded data by dotted
// path, map one value to a status, render a summary template, and collect
// metrics.
type rules struct {
	status  *statusRule
	summary string            // template with {dotted.path} placeholders
	metrics map[string]string // display label -> dotted path
}

type statusRule struct {
	path                  string
	equals                any
	hasEquals             bool
	warnAbove, errorAbove *float64
	warnBelow, errorBelow *float64
}

func parseRules(o map[string]any) (rules, error) {
	r := rules{}
	var err error
	if r.summary, err = optString(o, "summaryTemplate", ""); err != nil {
		return r, err
	}
	if r.metrics, err = optStringMap(o, "metrics"); err != nil {
		return r, err
	}
	raw, ok := o["statusFrom"]
	if !ok {
		return r, nil
	}
	tbl, ok := raw.(map[string]any)
	if !ok {
		return r, fmt.Errorf("statusFrom must be a table, got %T", raw)
	}
	s := &statusRule{}
	if s.path, err = reqString(tbl, "path"); err != nil {
		return r, fmt.Errorf("statusFrom: %w", err)
	}
	if v, ok := tbl["equals"]; ok {
		s.equals = v
		s.hasEquals = true
	}
	for key, dst := range map[string]**float64{
		"warnAbove": &s.warnAbove, "errorAbove": &s.errorAbove,
		"warnBelow": &s.warnBelow, "errorBelow": &s.errorBelow,
	} {
		v, ok := tbl[key]
		if !ok {
			continue
		}
		f, ok := toFloat(v)
		if !ok {
			return r, fmt.Errorf("statusFrom.%s must be a number, got %T", key, v)
		}
		*dst = &f
	}
	if !s.hasEquals && s.warnAbove == nil && s.errorAbove == nil && s.warnBelow == nil && s.errorBelow == nil {
		return r, fmt.Errorf("statusFrom needs equals or a warn/error threshold")
	}
	r.status = s
	return r, nil
}

// apply maps extracted data to a Result. data is decoded JSON (maps, slices,
// scalars) or a flat map from regex capture groups.
func (r rules) apply(data any) provider.Result {
	res := provider.Result{Status: provider.StatusOK, Summary: "ok"}
	if r.status != nil {
		st, detail := r.status.eval(data)
		res.Status = st
		if detail != "" {
			res.Error = detail
			res.Summary = detail
		}
	}
	if r.summary != "" {
		res.Summary = renderTemplate(r.summary, data)
	}
	if len(r.metrics) > 0 {
		res.Metrics = map[string]any{}
		for label, path := range r.metrics {
			if v, ok := lookup(data, path); ok {
				res.Metrics[label] = v
			} else {
				res.Metrics[label] = "path not found"
			}
		}
	}
	return res
}

func (s *statusRule) eval(data any) (provider.Status, string) {
	v, ok := lookup(data, s.path)
	if !ok {
		return provider.StatusUnknown, fmt.Sprintf("path %q not found", s.path)
	}
	if s.hasEquals {
		if looseEqual(v, s.equals) {
			return provider.StatusOK, ""
		}
		return provider.StatusError, fmt.Sprintf("%s = %s, want %s", s.path, fmtVal(v), fmtVal(s.equals))
	}
	f, ok := toFloat(v)
	if !ok {
		return provider.StatusUnknown, fmt.Sprintf("%s is not numeric: %v", s.path, v)
	}
	switch {
	case s.errorAbove != nil && f > *s.errorAbove:
		return provider.StatusError, fmt.Sprintf("%s = %s (above %s)", s.path, fmtVal(f), fmtVal(*s.errorAbove))
	case s.errorBelow != nil && f < *s.errorBelow:
		return provider.StatusError, fmt.Sprintf("%s = %s (below %s)", s.path, fmtVal(f), fmtVal(*s.errorBelow))
	case s.warnAbove != nil && f > *s.warnAbove:
		return provider.StatusWarn, fmt.Sprintf("%s = %s (above %s)", s.path, fmtVal(f), fmtVal(*s.warnAbove))
	case s.warnBelow != nil && f < *s.warnBelow:
		return provider.StatusWarn, fmt.Sprintf("%s = %s (below %s)", s.path, fmtVal(f), fmtVal(*s.warnBelow))
	}
	return provider.StatusOK, ""
}

// lookup walks decoded data by dotted path; numeric segments index arrays.
func lookup(data any, path string) (any, bool) {
	cur := data
	for _, seg := range strings.Split(path, ".") {
		switch t := cur.(type) {
		case map[string]any:
			v, ok := t[seg]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(t) {
				return nil, false
			}
			cur = t[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

var tmplRe = regexp.MustCompile(`\{([^{}]+)\}`)

// renderTemplate replaces {dotted.path} placeholders with extracted values.
// Unresolvable placeholders are left as-is so typos stay visible.
func renderTemplate(tmpl string, data any) string {
	return tmplRe.ReplaceAllStringFunc(tmpl, func(m string) string {
		if v, ok := lookup(data, m[1:len(m)-1]); ok {
			return fmtVal(v)
		}
		return m
	})
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int64:
		return float64(t), true
	case int:
		return float64(t), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	}
	return 0, false
}

// looseEqual compares across JSON/TOML numeric types via string form, so
// equals = 5 matches a JSON 5.0 and equals = "up" matches "up".
func looseEqual(a, b any) bool {
	return fmtVal(a) == fmtVal(b)
}

func fmtVal(v any) string {
	if f, ok := v.(float64); ok && f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatFloat(f, 'f', 0, 64)
	}
	return fmt.Sprintf("%v", v)
}
