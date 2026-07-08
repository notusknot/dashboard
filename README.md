# beacon

A self-hosted status dashboard for your tailnet, built to be a browser start
page. One fast page that aggregates the health of your services — and makes
the failure mode that actually hurts impossible to miss: **a dead backup or
broken sync that nobody notices**. Anything unhealthy or stale is loud,
red/amber, and sorted to the top.

- Single static Go binary (~7 MB), frontend embedded. No runtime deps.
- Vanilla JS frontend, no build step, no framework, self-hosted fonts.
- Everything configured from one TOML file; on NixOS, declaratively via the
  bundled module.
- No auth, no database, no history, no alerting. It is a read-only,
  at-a-glance page; the tailnet is the security boundary.

## Quick start

```sh
# demo mode: fabricated data for every provider type and render shape
nix run . -- --demo
# or with a real config
nix run . -- -config ./example.toml
```

Then open http://127.0.0.1:8383. `example.toml` documents every option.

Endpoints: `/` (the page), `/api/status` (JSON), `/healthz`.

## How it works

Everything monitored is a **provider**. A runner polls each provider on its
own goroutine at its configured `interval` and keeps the latest result. The
frontend fetches `/api/status` every `refreshSeconds` and renders cards
generically from the data — adding a provider in config makes a card appear
with zero frontend changes.

Two safety properties matter more than the rest:

- A provider that errors or panics yields an `error` card; it never takes
  the process down.
- **Staleness is first-class.** If a provider's last successful poll is older
  than `staleAfter` (global, per-provider overridable), the card is badged
  STALE and floats up with the errors. A wedged poller looks broken, not
  quietly green. Cards sort `error` → `warn` → `stale` → unknown → healthy.

## Adding a service

### The common path: config only

Most services can be watched with one of the generic providers — no code:

- **`http-health`** — up/down from an HTTP status code or a TCP connect.
- **`http-json`** — GET a JSON API, extract fields by dotted path, map one
  value to a status via `equals` or numeric thresholds, template a summary,
  pick metrics.
- **`command`** — run any command; parse stdout as JSON or with a regex
  (named capture groups become fields); same mapping rules as `http-json`.

```toml
[[providers]]
id = "jellyfin"
type = "http-health"
title = "Jellyfin"
url = "https://jellyfin.your-tailnet.ts.net/health"
```

That's a full new card. See `example.toml` for `http-json` and `command`
examples with status rules, summary templates, and metrics.

### The typed path: one Go file

When a service deserves richer logic, add a typed provider: one file in
`internal/providers/`, self-registered via `init()`. Minimal template:

```go
package providers

import (
    "context"

    "beacon/internal/provider"
)

func init() { provider.Register("myservice", newMyService) }

type myService struct {
    provider.Base
    url string
}

func newMyService(cfg provider.Config) (provider.Provider, error) {
    m := &myService{Base: provider.Base{Cfg: cfg}}
    var err error
    if m.url, err = reqString(cfg.Options, "url"); err != nil {
        return nil, err
    }
    return m, nil
}

func (m *myService) Poll(ctx context.Context) provider.Result {
    var data struct{ Healthy bool `json:"healthy"` }
    if err := getJSON(ctx, m.url+"/api/health", nil, &data); err != nil {
        return provider.Errorf("%v", err)
    }
    if !data.Healthy {
        return provider.Errorf("service reports unhealthy")
    }
    return provider.Result{Status: provider.StatusOK, Summary: "healthy"}
}
```

Helpers you get for free: `reqString`/`optString`/`optInt`/`optFloat`/
`optDuration`/`optStringSlice`/`optStringMap` for options, `getJSON`/
`getBody` for HTTP, `readSecret` for `*File` credentials, and `parseRules`/
`rules.apply` if you want the generic extraction engine.

## Config reference

Global (`[server]`): `listen`, `staleAfter`, `refreshSeconds`, `hostLabel`.
Optional `[[links]]` (quick-launch row: `label`, `url`, `icon`) and
`[[engines]]` (command-bar web search: `name`, `url` prefix).

Common provider keys: `id` (unique, required), `type` (required), `title`,
`subtitle`, `link` (card/pill outbound link; defaults to the provider's own
`url` when it has one), `icon`, `interval` (default `1m`), `staleAfter`.

| Type | Required | Optional |
|------|----------|----------|
| `restic` | `repository`, `passwordFile` | `staleAfter` (snapshot age limit, default `26h`), `extraArgs` |
| `syncthing` | `url`, `apiKeyFile` | |
| `disk` | `mounts` | `warnPercent` (80), `errorPercent` (90) |
| `ntfy` | `url`, `topic` | `limit` (8), `since` (`24h`) |
| `adguard` | `url`, `usernameFile`, `passwordFile` | |
| `http-json` | `url` | `headers`, `bearerTokenFile`, `statusFrom`, `summaryTemplate`, `metrics` |
| `command` | `command` (argv list) | `parse` (`json`\|`regex`), `regex`, `statusFrom`, `summaryTemplate`, `metrics` |
| `http-health` | `url` or `address` | `expectStatus` (200) |

`statusFrom`: `path` plus either `equals` or any of `warnAbove`,
`errorAbove`, `warnBelow`, `errorBelow`. Dotted paths index objects by key
and arrays by number (`disks.0.usedPercent`).

**The `*File` convention (strict).** Any key ending in `File` is a path to a
file containing the secret, read at poll time so rotation just works. There
are no inline secret keys, and there never will be — this keeps secrets out
of the config file, which on NixOS lands in the world-readable store.
Readability of every `*File` path is checked at startup and failures name
the provider and key.

Provider notes:

- `restic` needs the `restic` binary on PATH (on NixOS:
  `services.beacon.extraPackages = [ pkgs.restic ]`). It runs
  `restic snapshots --json --no-lock` with `RESTIC_REPOSITORY` /
  `RESTIC_PASSWORD_FILE`; repo backends needing more env (S3 keys etc.) can
  get it via systemd credentials + a wrapper, or use a `command` provider.
  `staleAfter` doubles as the snapshot-age limit and the poll-staleness
  window — both mean "backups should be newer than this".
- `syncthing` is `ok` only when every non-paused folder is idle with nothing
  to sync, there are no system errors, and every non-paused device is
  connected; syncing/disconnected degrade to `warn`, folder errors to
  `error`.
- `ntfy` status only reflects reachability; the messages themselves render
  as a feed (priority ≥ 5 red, 4 amber).

## NixOS

`flake.nix` exposes `packages.<system>.default`, `nixosModules.default`,
`devShells.<system>.default`, and `checks` (package build + `go vet` +
`go test`). Supported systems: `x86_64-linux`, `aarch64-linux`.

```nix
{
  inputs.beacon.url = "github:you/beacon"; # or a local path

  # in your NixOS configuration:
  imports = [ beacon.nixosModules.default ];

  services.beacon = {
    enable = true;
    listenAddress = "127.0.0.1";
    port = 8383;
    extraPackages = [ pkgs.restic ];
    settings = {
      server.staleAfter = "15m";
      server.hostLabel = "tailnet · start.example.ts.net";
      links = [
        { label = "Grafana"; url = "https://grafana.example.ts.net"; icon = "activity"; }
      ];
      providers = [
        {
          id = "backups"; type = "restic"; title = "Backups";
          interval = "15m"; staleAfter = "26h";
          repository = "/srv/backup/restic";
          passwordFile = "/run/credentials/beacon.service/restic-pw";
        }
        { id = "storage"; type = "disk"; title = "Storage"; mounts = [ "/" "/data" ]; }
      ];
    };
  };
}
```

The service runs hardened: `DynamicUser` (unless you set `user`/`group`),
`ProtectSystem=strict`, `ProtectHome`, `NoNewPrivileges`, `PrivateTmp`,
restricted syscalls and address families. If you monitor mounts under
`/home`, override `ProtectHome`.

### Auto-discovery from your NixOS config

Turn on `discover` and beacon reads the rest of your NixOS config at
rebuild time: every known service you've enabled gets a launch link plus an
`http-health` liveness card, with no duplicated ports to keep in sync. When
you enable a new service and `nixos-rebuild`, it shows up automatically.

```nix
services.beacon = {
  enable = true;
  discover.enable = true;
  discover.domain = "start.example.ts.net";  # links go to <name>.<domain>
};
```

Enabling `services.grafana` and `services.adguardhome` then yields links to
`https://grafana.start.example.ts.net` / `https://adguard.start.example.ts.net`
and two health cards. Discovered entries are **appended** to whatever you
write by hand in `settings.links` / `settings.providers`.

How it works and its knobs:

- Discovery is a curated catalog (grafana, prometheus, adguardhome,
  syncthing, jellyfin, home-assistant, immich, paperless, forgejo, gitea,
  vaultwarden, radarr/sonarr/prowlarr/bazarr, miniflux, audiobookshelf,
  photoprism, transmission, uptime-kuma). A service not in the catalog is
  simply not discovered — it needs a one-line `discover.extraServices` entry.
- Links point at the reverse-proxy subdomain `<scheme>://<name>.<domain>`
  (`discover.scheme` defaults to `https`). The health check, by contrast,
  dials the service **locally** (`127.0.0.1:<port>`, `discover.healthTarget =
  "local"`) so it's a real liveness check independent of the proxy or a login
  redirect; set `healthTarget = "link"` to check the public URL instead.
- `discover.exclude = [ "grafana" ]` drops an entry (also the way to resolve a
  name collision with a hand-written link/provider).
- `discover.extraServices` adds services beacon doesn't ship:

  ```nix
  discover.extraServices = [
    { key = "octoprint"; title = "OctoPrint"; icon = "cpu"; port = 5000; }
    # optional per-entry: enable, subdomain, linkUrl, healthUrl
  ];
  ```

Only generic reachability is auto-configured. Rich providers (restic,
syncthing folders, adguard stats, …) need secrets, so wire those by hand in
`settings.providers` as above.

### Secrets: systemd credentials

Wire secrets with `LoadCredential` so they never enter the Nix store. The
module needs nothing special — beacon just reads file paths, and
`/run/credentials/beacon.service/<name>` is a stable path only the service
can read:

```nix
systemd.services.beacon.serviceConfig.LoadCredential = [
  "restic-pw:/run/agenix/restic-pw"          # agenix
  "syncthing-api:${config.sops.secrets.syncthing-api.path}"  # sops-nix
];
```

Then point the `*File` keys at `/run/credentials/beacon.service/restic-pw`
etc. Any file readable by the service works too (e.g. agenix secrets owned
by a fixed `user`); `LoadCredential` is just the cleanest fit for
`DynamicUser`.

### Exposing it on the tailnet

There is **no built-in auth** — the tailnet is the trust boundary. Two
options, firewall closed by default:

1. **Bind the Tailscale IP**: set `listenAddress` to the host's `100.x.y.z`
   address (plus `openFirewall = true` if your firewall filters
   `tailscale0`). Reachable as `http://host:8383` on the tailnet only.
2. **Bind localhost + `tailscale serve`** (nicer: HTTPS + a clean name):

   ```sh
   tailscale serve --bg --https=443 http://127.0.0.1:8383
   ```

Do not port-forward it to the internet.

## Development

```sh
nix develop          # Go toolchain + restic
go run ./cmd/beacon --demo
go vet ./... && go test ./...
nix build && nix flake check
```

Layout: `cmd/beacon` (main + demo data), `internal/provider` (model,
registry, runner), `internal/providers` (one file per type + shared
extraction engine), `internal/config`, `internal/server`, `web/` (embedded
frontend), `nix/module.nix`.

## Dependencies

One third-party Go module: **`github.com/BurntSushi/toml`** (vendored,
~170 KB). The stdlib has no TOML parser, and TOML is the config format the
NixOS module generates through `pkgs.formats.toml`; a hand-rolled parser
would be the riskiest code in the project. Everything else is stdlib.

The frontend has zero dependencies; Archivo and JetBrains Mono (latin,
variable weight) are embedded in the binary.
