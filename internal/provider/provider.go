// Package provider defines beacon's provider model: everything monitored
// implements Provider, is built by a registered constructor, and is polled
// by the Runner.
package provider

import (
	"context"
	"fmt"
	"time"
)

type Status string

const (
	StatusOK      Status = "ok"
	StatusWarn    Status = "warn"
	StatusError   Status = "error"
	StatusUnknown Status = "unknown"
)

// FeedItem is one entry of a feed-style provider (e.g. an ntfy message).
type FeedItem struct {
	Time     time.Time `json:"time"`
	Title    string    `json:"title,omitempty"`
	Message  string    `json:"message,omitempty"`
	Tags     []string  `json:"tags,omitempty"`
	Priority int       `json:"priority,omitempty"`
}

// Result is the outcome of one poll.
//
// Metrics values may be plain scalars (rendered as label/value rows) or small
// objects like {"t":"bar","value":83,"max":100} that the frontend renders as
// richer primitives (bar, age, stat, spark, pill).
type Result struct {
	Status    Status         `json:"status"`
	Summary   string         `json:"summary"`
	Metrics   map[string]any `json:"metrics,omitempty"`
	Items     []FeedItem     `json:"items,omitempty"`
	Error     string         `json:"error,omitempty"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type Provider interface {
	ID() string
	Title() string
	Poll(ctx context.Context) Result
}

// Config is one [[providers]] table from the TOML config. Keys the core does
// not consume stay in Options for the type's constructor.
type Config struct {
	ID         string
	Type       string
	Title      string
	Subtitle   string
	URL        string
	Icon       string
	Interval   time.Duration
	StaleAfter time.Duration // 0 = use the server-wide default
	Options    map[string]any
}

// Base implements ID/Title from a Config; embed it in provider structs.
type Base struct{ Cfg Config }

func (b Base) ID() string    { return b.Cfg.ID }
func (b Base) Title() string { return b.Cfg.Title }

// Errorf builds a failed-poll Result.
func Errorf(format string, args ...any) Result {
	return Result{Status: StatusError, Summary: "check failed", Error: fmt.Sprintf(format, args...)}
}
