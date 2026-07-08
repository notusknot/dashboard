package providers

import (
	"context"
	"fmt"
	"net"
	"time"

	"beacon/internal/provider"
)

func init() { provider.Register("http-health", newHTTPHealth) }

type httpHealth struct {
	provider.Base
	url     string
	address string
	expect  int
}

func newHTTPHealth(cfg provider.Config) (provider.Provider, error) {
	h := &httpHealth{Base: provider.Base{Cfg: cfg}}
	var err error
	if h.url, err = optString(cfg.Options, "url", ""); err != nil {
		return nil, err
	}
	if h.address, err = optString(cfg.Options, "address", ""); err != nil {
		return nil, err
	}
	if (h.url == "") == (h.address == "") {
		return nil, fmt.Errorf("need exactly one of %q or %q", "url", "address")
	}
	if h.expect, err = optInt(cfg.Options, "expectStatus", 200); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *httpHealth) Poll(ctx context.Context) provider.Result {
	start := time.Now()
	if h.address != "" {
		conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", h.address)
		if err != nil {
			return provider.Errorf("tcp connect %s: %v", h.address, err)
		}
		conn.Close()
		return upResult(fmt.Sprintf("tcp connect in %s", roundMS(start)), start)
	}
	_, code, err := getBody(ctx, h.url, nil)
	if err != nil {
		return provider.Errorf("GET %s: %v", h.url, err)
	}
	if code != h.expect {
		res := provider.Errorf("GET %s: HTTP %d, want %d", h.url, code, h.expect)
		res.Metrics = map[string]any{"status": code}
		return res
	}
	res := upResult(fmt.Sprintf("HTTP %d in %s", code, roundMS(start)), start)
	res.Metrics["status"] = code
	return res
}

func upResult(summary string, start time.Time) provider.Result {
	return provider.Result{
		Status:  provider.StatusOK,
		Summary: summary,
		Metrics: map[string]any{"latency": roundMS(start)},
	}
}

func roundMS(start time.Time) string {
	return time.Since(start).Round(time.Millisecond).String()
}
