package providers

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"beacon/internal/provider"
)

func init() { provider.Register("adguard", newAdguard) }

type adguard struct {
	provider.Base
	url          string
	usernameFile string
	passwordFile string
}

func newAdguard(cfg provider.Config) (provider.Provider, error) {
	a := &adguard{Base: provider.Base{Cfg: cfg}}
	var err error
	if a.url, err = reqString(cfg.Options, "url"); err != nil {
		return nil, err
	}
	if a.usernameFile, err = reqString(cfg.Options, "usernameFile"); err != nil {
		return nil, err
	}
	if a.passwordFile, err = reqString(cfg.Options, "passwordFile"); err != nil {
		return nil, err
	}
	a.url = strings.TrimRight(a.url, "/")
	return a, nil
}

func (a *adguard) Poll(ctx context.Context) provider.Result {
	user, err := readSecret(a.usernameFile)
	if err != nil {
		return provider.Errorf("usernameFile: %v", err)
	}
	pass, err := readSecret(a.passwordFile)
	if err != nil {
		return provider.Errorf("passwordFile: %v", err)
	}
	hdr := map[string]string{
		"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass)),
	}

	var status struct {
		Running           bool   `json:"running"`
		ProtectionEnabled bool   `json:"protection_enabled"`
		Version           string `json:"version"`
	}
	if err := getJSON(ctx, a.url+"/control/status", hdr, &status); err != nil {
		return provider.Errorf("%v", err)
	}
	var stats struct {
		NumDNSQueries       int64 `json:"num_dns_queries"`
		NumBlockedFiltering int64 `json:"num_blocked_filtering"`
	}
	if err := getJSON(ctx, a.url+"/control/stats", hdr, &stats); err != nil {
		return provider.Errorf("%v", err)
	}

	blockPct := 0.0
	if stats.NumDNSQueries > 0 {
		blockPct = float64(stats.NumBlockedFiltering) / float64(stats.NumDNSQueries) * 100
	}
	res := provider.Result{
		Status:  provider.StatusOK,
		Summary: fmt.Sprintf("%s queries · %.1f%% blocked", humanCount(stats.NumDNSQueries), blockPct),
		Metrics: map[string]any{
			"queries": map[string]any{"t": "stat", "value": humanCount(stats.NumDNSQueries)},
			"blocked": fmt.Sprintf("%.1f%%", blockPct),
			"version": status.Version,
		},
	}
	switch {
	case !status.Running:
		res.Status = provider.StatusError
		res.Summary = "AdGuard Home is not running"
		res.Error = "status reports running=false"
	case !status.ProtectionEnabled:
		res.Status = provider.StatusWarn
		res.Summary = "protection disabled"
	}
	return res
}
