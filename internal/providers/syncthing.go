package providers

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"beacon/internal/provider"
)

func init() { provider.Register("syncthing", newSyncthing) }

type syncthing struct {
	provider.Base
	url        string
	apiKeyFile string
}

func newSyncthing(cfg provider.Config) (provider.Provider, error) {
	s := &syncthing{Base: provider.Base{Cfg: cfg}}
	var err error
	if s.url, err = reqString(cfg.Options, "url"); err != nil {
		return nil, err
	}
	if s.apiKeyFile, err = reqString(cfg.Options, "apiKeyFile"); err != nil {
		return nil, err
	}
	s.url = strings.TrimRight(s.url, "/")
	return s, nil
}

func (s *syncthing) Poll(ctx context.Context) provider.Result {
	key, err := readSecret(s.apiKeyFile)
	if err != nil {
		return provider.Errorf("apiKeyFile: %v", err)
	}
	hdr := map[string]string{"X-API-Key": key}

	var sysErr struct {
		Errors []struct {
			When    time.Time `json:"when"`
			Message string    `json:"message"`
		} `json:"errors"` // may be null when empty
	}
	var sysStatus struct {
		MyID string `json:"myID"`
	}
	var folders []struct {
		ID     string `json:"id"`
		Label  string `json:"label"`
		Paused bool   `json:"paused"`
	}
	var devices []struct {
		DeviceID string `json:"deviceID"`
		Name     string `json:"name"`
		Paused   bool   `json:"paused"`
	}
	var conns struct {
		Connections map[string]struct {
			Connected bool `json:"connected"`
		} `json:"connections"`
	}
	for path, out := range map[string]any{
		"/rest/system/error":       &sysErr,
		"/rest/system/status":      &sysStatus,
		"/rest/config/folders":     &folders,
		"/rest/config/devices":     &devices,
		"/rest/system/connections": &conns,
	} {
		if err := getJSON(ctx, s.url+path, hdr, out); err != nil {
			return provider.Errorf("%v", err)
		}
	}

	expected, connected := 0, 0
	var disconnected []string
	for _, d := range devices {
		if d.Paused || d.DeviceID == sysStatus.MyID {
			continue
		}
		expected++
		if conns.Connections[d.DeviceID].Connected {
			connected++
		} else {
			disconnected = append(disconnected, deviceName(d.Name, d.DeviceID))
		}
	}

	active, inSync, needTotal := 0, 0, 0
	var busy, broken []string
	for _, f := range folders {
		if f.Paused {
			continue
		}
		active++
		var db struct {
			State          string `json:"state"`
			NeedTotalItems int    `json:"needTotalItems"`
		}
		if err := getJSON(ctx, s.url+"/rest/db/status?folder="+url.QueryEscape(f.ID), hdr, &db); err != nil {
			return provider.Errorf("%v", err)
		}
		name := f.Label
		if name == "" {
			name = f.ID
		}
		needTotal += db.NeedTotalItems
		switch {
		case db.State == "error":
			broken = append(broken, name)
		case db.State == "idle" && db.NeedTotalItems == 0:
			inSync++
		default:
			busy = append(busy, fmt.Sprintf("%s (%s)", name, db.State))
		}
	}

	res := provider.Result{
		Status: provider.StatusOK,
		Metrics: map[string]any{
			"folders in sync":   fmt.Sprintf("%d / %d", inSync, active),
			"devices connected": fmt.Sprintf("%d / %d", connected, expected),
			"out of sync items": needTotal,
		},
	}
	switch {
	case len(sysErr.Errors) > 0:
		res.Status = provider.StatusError
		res.Summary = "system errors reported"
		res.Error = sysErr.Errors[len(sysErr.Errors)-1].Message
	case len(broken) > 0:
		res.Status = provider.StatusError
		res.Summary = "folder in error state"
		res.Error = strings.Join(broken, ", ")
	case len(busy) > 0:
		res.Status = provider.StatusWarn
		res.Summary = strings.Join(busy, ", ")
	case len(disconnected) > 0:
		res.Status = provider.StatusWarn
		res.Summary = fmt.Sprintf("disconnected: %s", strings.Join(disconnected, ", "))
	default:
		res.Summary = fmt.Sprintf("%d folders in sync · %d devices connected", inSync, connected)
	}
	return res
}

func deviceName(name, id string) string {
	if name != "" {
		return name
	}
	if len(id) > 7 {
		return id[:7]
	}
	return id
}
