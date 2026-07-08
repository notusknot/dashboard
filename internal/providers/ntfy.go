package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"beacon/internal/provider"
)

func init() { provider.Register("ntfy", newNtfy) }

type ntfy struct {
	provider.Base
	url   string
	topic string
	limit int
	since string
}

func newNtfy(cfg provider.Config) (provider.Provider, error) {
	n := &ntfy{Base: provider.Base{Cfg: cfg}}
	var err error
	if n.url, err = reqString(cfg.Options, "url"); err != nil {
		return nil, err
	}
	if n.topic, err = reqString(cfg.Options, "topic"); err != nil {
		return nil, err
	}
	if n.limit, err = optInt(cfg.Options, "limit", 8); err != nil {
		return nil, err
	}
	if n.since, err = optString(cfg.Options, "since", "24h"); err != nil {
		return nil, err
	}
	n.url = strings.TrimRight(n.url, "/")
	return n, nil
}

func (n *ntfy) Poll(ctx context.Context) provider.Result {
	u := fmt.Sprintf("%s/%s/json?poll=1&since=%s", n.url, url.PathEscape(n.topic), url.QueryEscape(n.since))
	body, code, err := getBody(ctx, u, nil)
	if err != nil {
		return provider.Errorf("GET %s: %v", u, err)
	}
	if code >= 400 {
		return provider.Errorf("GET %s: HTTP %d", u, code)
	}

	// Response is NDJSON: one event object per line, oldest first.
	var items []provider.FeedItem
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var msg struct {
			Event    string   `json:"event"`
			Time     int64    `json:"time"` // unix seconds
			Title    string   `json:"title"`
			Message  string   `json:"message"`
			Tags     []string `json:"tags"`
			Priority int      `json:"priority"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) != nil || msg.Event != "message" {
			continue
		}
		items = append(items, provider.FeedItem{
			Time:     time.Unix(msg.Time, 0),
			Title:    msg.Title,
			Message:  msg.Message,
			Tags:     msg.Tags,
			Priority: msg.Priority,
		})
	}

	total := len(items)
	// newest first, capped at limit
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	if len(items) > n.limit {
		items = items[:n.limit]
	}
	summary := fmt.Sprintf("%d messages in last %s", total, n.since)
	if total == 0 {
		summary = "no messages in last " + n.since
	}
	return provider.Result{Status: provider.StatusOK, Summary: summary, Items: items}
}
