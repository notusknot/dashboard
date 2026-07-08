package providers

import (
	"context"
	"encoding/json"

	"beacon/internal/provider"
)

func init() { provider.Register("http-json", newHTTPJSON) }

type httpJSON struct {
	provider.Base
	url        string
	headers    map[string]string
	bearerFile string
	rules      rules
}

func newHTTPJSON(cfg provider.Config) (provider.Provider, error) {
	j := &httpJSON{Base: provider.Base{Cfg: cfg}}
	var err error
	if j.url, err = reqString(cfg.Options, "url"); err != nil {
		return nil, err
	}
	if j.headers, err = optStringMap(cfg.Options, "headers"); err != nil {
		return nil, err
	}
	if j.bearerFile, err = optString(cfg.Options, "bearerTokenFile", ""); err != nil {
		return nil, err
	}
	if j.rules, err = parseRules(cfg.Options); err != nil {
		return nil, err
	}
	return j, nil
}

func (j *httpJSON) Poll(ctx context.Context) provider.Result {
	hdr := map[string]string{}
	for k, v := range j.headers {
		hdr[k] = v
	}
	if j.bearerFile != "" {
		tok, err := readSecret(j.bearerFile)
		if err != nil {
			return provider.Errorf("bearerTokenFile: %v", err)
		}
		hdr["Authorization"] = "Bearer " + tok
	}
	body, code, err := getBody(ctx, j.url, hdr)
	if err != nil {
		return provider.Errorf("GET %s: %v", j.url, err)
	}
	if code >= 400 {
		return provider.Errorf("GET %s: HTTP %d", j.url, code)
	}
	var data any
	if err := json.Unmarshal(body, &data); err != nil {
		return provider.Errorf("GET %s: invalid JSON: %v", j.url, err)
	}
	return j.rules.apply(data)
}
