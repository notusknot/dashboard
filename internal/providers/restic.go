package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"beacon/internal/provider"
)

func init() { provider.Register("restic", newRestic) }

type restic struct {
	provider.Base
	repository   string
	passwordFile string
	staleAfter   time.Duration
	extraArgs    []string
}

func newRestic(cfg provider.Config) (provider.Provider, error) {
	r := &restic{Base: provider.Base{Cfg: cfg}, staleAfter: cfg.StaleAfter}
	var err error
	if r.repository, err = reqString(cfg.Options, "repository"); err != nil {
		return nil, err
	}
	if r.passwordFile, err = reqString(cfg.Options, "passwordFile"); err != nil {
		return nil, err
	}
	if r.staleAfter <= 0 {
		r.staleAfter = 26 * time.Hour
	}
	if r.extraArgs, err = optStringSlice(cfg.Options, "extraArgs"); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *restic) Poll(ctx context.Context) provider.Result {
	// No --group-by: current restic emits a flat JSON array. --no-lock keeps
	// the check from contending with a running backup.
	args := append([]string{"snapshots", "--json", "--no-lock"}, r.extraArgs...)
	cmd := exec.CommandContext(ctx, "restic", args...)
	cmd.Env = append(os.Environ(),
		"RESTIC_REPOSITORY="+r.repository,
		"RESTIC_PASSWORD_FILE="+r.passwordFile,
	)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return provider.Errorf("restic: %s", resticErr(errb.String(), err))
	}
	var snaps []struct {
		Time time.Time `json:"time"`
	}
	if err := json.Unmarshal(out.Bytes(), &snaps); err != nil {
		return provider.Errorf("restic: unexpected snapshots output: %v", err)
	}
	times := make([]time.Time, len(snaps))
	for i, s := range snaps {
		times[i] = s.Time
	}
	return resticResult(times, r.staleAfter, time.Now())
}

// resticResult maps snapshot times to a status: ok while the newest snapshot
// is within staleAfter, error once it is older or missing.
func resticResult(times []time.Time, staleAfter time.Duration, now time.Time) provider.Result {
	if len(times) == 0 {
		return provider.Errorf("no snapshots in repository")
	}
	latest := times[0]
	for _, t := range times[1:] {
		if t.After(latest) {
			latest = t
		}
	}
	age := now.Sub(latest)
	res := provider.Result{
		Status:  provider.StatusOK,
		Summary: fmt.Sprintf("last snapshot %s ago · %d snapshots", humanDur(age), len(times)),
		Metrics: map[string]any{
			"last snapshot": map[string]any{
				"t":           "age",
				"at":          latest.UnixMilli(),
				"critAfterMs": staleAfter.Milliseconds(),
			},
			"snapshots": len(times),
		},
	}
	if age > staleAfter {
		res.Status = provider.StatusError
		res.Error = fmt.Sprintf("last snapshot %s ago, limit %s", humanDur(age), humanDur(staleAfter))
	}
	return res
}

// resticErr prefers the structured {"message_type":"exit_error"} line restic
// prints on stderr in --json mode, falling back to raw stderr.
func resticErr(stderr string, err error) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var e struct {
			MessageType string `json:"message_type"`
			Message     string `json:"message"`
		}
		if json.Unmarshal([]byte(lines[i]), &e) == nil && e.MessageType == "exit_error" {
			return e.Message
		}
	}
	if s := tail(stderr, 300); s != "" {
		return s
	}
	return err.Error()
}
