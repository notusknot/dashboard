package provider

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Entry is one element of the /api/status payload: provider identity plus its
// latest Result and computed staleness.
type Entry struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Title           string `json:"title"`
	Subtitle        string `json:"subtitle,omitempty"`
	URL             string `json:"url,omitempty"`
	Icon            string `json:"icon,omitempty"`
	IntervalSeconds int    `json:"intervalSeconds,omitempty"`
	Result
	Stale bool `json:"stale"`
}

type proc struct {
	p   Provider
	cfg Config
}

// Runner polls every provider on its own goroutine and keeps the latest
// Result per provider.
type Runner struct {
	mu           sync.RWMutex
	results      map[string]Result
	procs        []proc
	defaultStale time.Duration
}

func NewRunner(cfgs []Config, defaultStale time.Duration) (*Runner, error) {
	r := &Runner{results: map[string]Result{}, defaultStale: defaultStale}
	for _, cfg := range cfgs {
		p, err := New(cfg)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", cfg.ID, err)
		}
		r.procs = append(r.procs, proc{p, cfg})
	}
	return r, nil
}

// Start launches one polling goroutine per provider. A provider that errors
// or panics yields an error Result; it never crashes the process.
func (r *Runner) Start(ctx context.Context) {
	for _, pr := range r.procs {
		go r.loop(ctx, pr)
	}
}

func (r *Runner) loop(ctx context.Context, pr proc) {
	t := time.NewTicker(pr.cfg.Interval)
	defer t.Stop()
	for {
		res := safePoll(ctx, pr)
		r.mu.Lock()
		r.results[pr.cfg.ID] = res
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func safePoll(ctx context.Context, pr proc) (res Result) {
	defer func() {
		if v := recover(); v != nil {
			res = Result{Status: StatusError, Summary: "check panicked", Error: fmt.Sprint(v), UpdatedAt: time.Now()}
		}
	}()
	pctx, cancel := context.WithTimeout(ctx, pr.cfg.Interval)
	defer cancel()
	res = pr.p.Poll(pctx)
	if res.UpdatedAt.IsZero() {
		res.UpdatedAt = time.Now()
	}
	return res
}

// Snapshot returns the latest Entry per provider, in config order. A Result
// older than the provider's staleness window (or the server default) is
// flagged Stale — the "dead poller nobody noticed" guard.
func (r *Runner) Snapshot() []Entry {
	now := time.Now()
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, 0, len(r.procs))
	for _, pr := range r.procs {
		e := Entry{
			ID:              pr.cfg.ID,
			Type:            pr.cfg.Type,
			Title:           pr.cfg.Title,
			Subtitle:        pr.cfg.Subtitle,
			URL:             pr.cfg.URL,
			Icon:            pr.cfg.Icon,
			IntervalSeconds: int(pr.cfg.Interval / time.Second),
		}
		if res, ok := r.results[pr.cfg.ID]; ok {
			e.Result = res
			window := pr.cfg.StaleAfter
			if window <= 0 {
				window = r.defaultStale
			}
			e.Stale = window > 0 && now.Sub(res.UpdatedAt) > window
		} else {
			e.Result = Result{Status: StatusUnknown, Summary: "first poll pending"}
		}
		out = append(out, e)
	}
	return out
}
