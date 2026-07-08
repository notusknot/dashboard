package providers

import (
	"context"
	"fmt"
	"math"
	"syscall"
	"time"

	"beacon/internal/provider"
)

func init() { provider.Register("disk", newDisk) }

type disk struct {
	provider.Base
	mounts  []string
	warnPct float64
	errPct  float64
}

func newDisk(cfg provider.Config) (provider.Provider, error) {
	d := &disk{Base: provider.Base{Cfg: cfg}}
	var err error
	if d.mounts, err = optStringSlice(cfg.Options, "mounts"); err != nil {
		return nil, err
	}
	if len(d.mounts) == 0 {
		return nil, fmt.Errorf("missing required key %q", "mounts")
	}
	if d.warnPct, err = optFloat(cfg.Options, "warnPercent", 80); err != nil {
		return nil, err
	}
	if d.errPct, err = optFloat(cfg.Options, "errorPercent", 90); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *disk) Poll(ctx context.Context) provider.Result {
	res := provider.Result{Status: provider.StatusOK, Metrics: map[string]any{}, UpdatedAt: time.Now()}
	worstMount, worstPct := "", -1.0
	for _, m := range d.mounts {
		var st syscall.Statfs_t
		if err := syscall.Statfs(m, &st); err != nil {
			res.Status = provider.StatusError
			res.Error = joinLines(res.Error, fmt.Sprintf("%s: %v", m, err))
			continue
		}
		bs := float64(st.Bsize)
		used := float64(st.Blocks-st.Bfree) * bs
		total := used + float64(st.Bavail)*bs // df semantics: used + available to non-root
		pct := 0.0
		if total > 0 {
			pct = used / total * 100
		}
		res.Metrics[m] = map[string]any{
			"t":          "bar",
			"value":      math.Round(pct*10) / 10,
			"max":        100,
			"valueLabel": fmt.Sprintf("%s / %s", humanBytes(used), humanBytes(total)),
			"warnAt":     d.warnPct,
			"critAt":     d.errPct,
		}
		res.Status = worse(res.Status, gradePct(pct, d.warnPct, d.errPct))
		if pct > worstPct {
			worstMount, worstPct = m, pct
		}
	}
	if worstMount != "" {
		res.Summary = fmt.Sprintf("%d mounts · worst %s at %.0f%%", len(d.mounts), worstMount, worstPct)
	} else {
		res.Summary = "no mounts readable"
	}
	return res
}

func gradePct(pct, warn, crit float64) provider.Status {
	switch {
	case pct >= crit:
		return provider.StatusError
	case pct >= warn:
		return provider.StatusWarn
	}
	return provider.StatusOK
}

var statusRank = map[provider.Status]int{
	provider.StatusError:   0,
	provider.StatusWarn:    1,
	provider.StatusUnknown: 2,
	provider.StatusOK:      3,
}

// worse returns the more severe of two statuses.
func worse(a, b provider.Status) provider.Status {
	if statusRank[a] < statusRank[b] {
		return a
	}
	return b
}

func joinLines(a, b string) string {
	if a == "" {
		return b
	}
	return a + "\n" + b
}
