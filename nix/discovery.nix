# Service auto-discovery for beacon. Reads the rest of the NixOS config and
# turns each enabled, known service into a launch link and an http-health card,
# so beacon tracks your services without hand-listing their ports.
#
# Imported by module.nix as:
#   import ./discovery.nix { inherit config lib; cfg = cfg.discover; }
# where `cfg` is `config.services.beacon.discover`. Returns { links; providers; }.
{ config, lib, cfg }:

let
  inherit (lib) attrByPath filter map elem splitString last toInt length;

  # Read an option from the wider NixOS config, defaulting if the path (or the
  # whole service module) is absent — so a catalog entry for an uninstalled
  # service evaluates to disabled instead of erroring.
  attr = path: default: attrByPath path default config;

  # Pull the port out of a "host:port" address (syncthing/miniflux bundle both),
  # falling back when it isn't a plain host:port (e.g. a unix socket).
  portOf = addr: default:
    let parts = splitString ":" (toString addr);
    in if length parts >= 2 then toInt (last parts) else default;

  # Built-in catalog. Each entry: key (provider id + default subdomain), enable
  # (read from config), title, icon (an existing dashboard icon symbol), and a
  # `port` for the local TCP health check. Port option paths were taken from
  # nixpkgs; a wrong/renamed path just falls back to the listed default.
  catalog = [
    { key = "grafana";        enable = attr [ "services" "grafana" "enable" ] false;        title = "Grafana";        icon = "activity"; port = attr [ "services" "grafana" "settings" "server" "http_port" ] 3000; }
    { key = "prometheus";     enable = attr [ "services" "prometheus" "enable" ] false;     title = "Prometheus";     icon = "activity"; port = attr [ "services" "prometheus" "port" ] 9090; }
    { key = "adguardhome";    enable = attr [ "services" "adguardhome" "enable" ] false;    title = "AdGuard Home";   icon = "filter";   subdomain = "adguard"; port = attr [ "services" "adguardhome" "port" ] 3000; }
    { key = "syncthing";      enable = attr [ "services" "syncthing" "enable" ] false;      title = "Syncthing";      icon = "sync";     port = portOf (attr [ "services" "syncthing" "guiAddress" ] "127.0.0.1:8384") 8384; }
    { key = "jellyfin";       enable = attr [ "services" "jellyfin" "enable" ] false;       title = "Jellyfin";       icon = "music";    port = 8096; }
    { key = "home-assistant"; enable = attr [ "services" "home-assistant" "enable" ] false; title = "Home Assistant"; icon = "home";     port = attr [ "services" "home-assistant" "config" "http" "server_port" ] 8123; }
    { key = "immich";         enable = attr [ "services" "immich" "enable" ] false;         title = "Immich";         icon = "image";    port = attr [ "services" "immich" "port" ] 2283; }
    { key = "paperless";      enable = attr [ "services" "paperless" "enable" ] false;      title = "Paperless";      icon = "file";     port = attr [ "services" "paperless" "port" ] 28981; }
    { key = "forgejo";        enable = attr [ "services" "forgejo" "enable" ] false;        title = "Forgejo";        icon = "book";     port = attr [ "services" "forgejo" "settings" "server" "HTTP_PORT" ] 3000; }
    { key = "gitea";          enable = attr [ "services" "gitea" "enable" ] false;          title = "Gitea";          icon = "book";     port = attr [ "services" "gitea" "settings" "server" "HTTP_PORT" ] 3000; }
    { key = "vaultwarden";    enable = attr [ "services" "vaultwarden" "enable" ] false;    title = "Vaultwarden";    icon = "shield";   port = attr [ "services" "vaultwarden" "config" "ROCKET_PORT" ] 8222; }
    { key = "radarr";         enable = attr [ "services" "radarr" "enable" ] false;         title = "Radarr";         icon = "image";    port = attr [ "services" "radarr" "settings" "server" "port" ] 7878; }
    { key = "sonarr";         enable = attr [ "services" "sonarr" "enable" ] false;         title = "Sonarr";         icon = "image";    port = attr [ "services" "sonarr" "settings" "server" "port" ] 8989; }
    { key = "prowlarr";       enable = attr [ "services" "prowlarr" "enable" ] false;       title = "Prowlarr";       icon = "search";   port = attr [ "services" "prowlarr" "settings" "server" "port" ] 9696; }
    { key = "bazarr";         enable = attr [ "services" "bazarr" "enable" ] false;         title = "Bazarr";         icon = "file";     port = attr [ "services" "bazarr" "settings" "server" "port" ] 6767; }
    { key = "miniflux";       enable = attr [ "services" "miniflux" "enable" ] false;       title = "Miniflux";       icon = "book";     port = portOf (attr [ "services" "miniflux" "config" "LISTEN_ADDR" ] "localhost:8080") 8080; }
    { key = "audiobookshelf"; enable = attr [ "services" "audiobookshelf" "enable" ] false; title = "Audiobookshelf"; icon = "book";     port = attr [ "services" "audiobookshelf" "port" ] 8000; }
    { key = "photoprism";     enable = attr [ "services" "photoprism" "enable" ] false;     title = "PhotoPrism";     icon = "image";    port = attr [ "services" "photoprism" "port" ] 2342; }
    { key = "transmission";   enable = attr [ "services" "transmission" "enable" ] false;   title = "Transmission";   icon = "down";     port = attr [ "services" "transmission" "settings" "rpc-port" ] 9091; }
    { key = "uptime-kuma";    enable = attr [ "services" "uptime-kuma" "enable" ] false;    title = "Uptime Kuma";    icon = "activity"; port = attr [ "services" "uptime-kuma" "settings" "PORT" ] 3001; }
  ];

  # User-supplied entries (same shape) may omit `enable`, meaning always on.
  all = catalog ++ cfg.extraServices;
  active = filter (e: (e.enable or true) && !(elem e.key cfg.exclude)) all;

  # Browser-facing URL: the service's reverse-proxy subdomain, unless overridden.
  urlOf = e: e.linkUrl or "${cfg.scheme}://${e.subdomain or e.key}.${cfg.domain}";

  linkOf = e: { label = e.title or e.key; url = urlOf e; icon = e.icon or "external"; };

  # Liveness card. Health dials the local port by default (proxy- and
  # auth-independent); an explicit healthUrl, `healthTarget = "link"`, or a
  # port-less entry checks the public URL instead.
  providerOf = e: {
    id = e.key;
    type = "http-health";
    title = e.title or e.key;
    icon = e.icon or "globe";
    link = urlOf e;
    interval = cfg.interval;
  } // (
    if e ? healthUrl then { url = e.healthUrl; }
    else if cfg.healthTarget == "link" || !(e ? port) then { url = urlOf e; }
    else { address = "127.0.0.1:${toString e.port}"; }
  );
in
{
  links = map linkOf active;
  providers = map providerOf active;
}
