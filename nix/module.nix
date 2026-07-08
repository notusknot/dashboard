# NixOS module for beacon. Imported from the flake as `nixosModules.default`;
# `self` supplies the default package.
self:
{ config, lib, pkgs, ... }:

let
  cfg = config.services.beacon;
  settingsFormat = pkgs.formats.toml { };
  configFile = settingsFormat.generate "beacon.toml" cfg.settings;

  # Links + http-health providers discovered from the rest of the NixOS config.
  # Lazy — only forced when discovery is enabled in the config block below.
  discovered = import ./discovery.nix { inherit config lib; cfg = cfg.discover; };
in
{
  options.services.beacon = {
    enable = lib.mkEnableOption "beacon, a self-hosted tailnet status dashboard";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      defaultText = lib.literalExpression "beacon.packages.\${system}.default";
      description = "The beacon package to run.";
    };

    listenAddress = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1";
      example = "100.64.0.7";
      description = ''
        Address to bind. Keep it off 0.0.0.0: bind the host's Tailscale IP,
        or keep localhost and expose via `tailscale serve`. There is no
        built-in auth — the tailnet is the trust boundary.
      '';
    };

    port = lib.mkOption {
      type = lib.types.port;
      default = 8383;
      description = "Port to listen on.";
    };

    openFirewall = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Open the port in the firewall.";
    };

    extraPackages = lib.mkOption {
      type = lib.types.listOf lib.types.package;
      default = [ ];
      example = lib.literalExpression "[ pkgs.restic ]";
      description = ''
        Packages added to the service's PATH; the restic provider needs
        `pkgs.restic`, and `command` providers need whatever they invoke.
      '';
    };

    user = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "User to run as. Null uses systemd DynamicUser.";
    };

    group = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      description = "Group to run as. Null uses systemd DynamicUser.";
    };

    settings = lib.mkOption {
      type = settingsFormat.type;
      default = { };
      example = lib.literalExpression ''
        {
          server.staleAfter = "15m";
          providers = [
            {
              id = "backups";
              type = "restic";
              title = "Backups";
              interval = "15m";
              repository = "/srv/backup/restic";
              passwordFile = "/run/credentials/beacon.service/restic-pw";
              staleAfter = "26h";
            }
          ];
        }
      '';
      description = ''
        Rendered to beacon's TOML config file. `server.listen` is derived
        from {option}`listenAddress` and {option}`port`. Keys ending in
        `File` must be paths readable by the service at poll time — use
        systemd credentials (LoadCredential) so secrets stay out of the
        Nix store.
      '';
    };

    discover = {
      enable = lib.mkEnableOption ''
        auto-discovery: read enabled services from the rest of your NixOS
        config and add a launch link plus an http-health liveness card for
        each known one, so beacon tracks your services without hand-listing
        them. Discovered entries are appended to {option}`settings`'';

      domain = lib.mkOption {
        type = lib.types.str;
        default = "";
        example = "start.example.ts.net";
        description = ''
          Base domain for discovered links. Each service links to
          `<scheme>://<subdomain>.<domain>` — its reverse-proxy hostname.
          Required when {option}`discover.enable` is set.
        '';
      };

      scheme = lib.mkOption {
        type = lib.types.str;
        default = "https";
        description = "URL scheme for discovered service links.";
      };

      interval = lib.mkOption {
        type = lib.types.str;
        default = "1m";
        description = "Poll interval for the generated http-health cards.";
      };

      healthTarget = lib.mkOption {
        type = lib.types.enum [ "local" "link" ];
        default = "local";
        description = ''
          Where a generated health check connects. `local` dials
          `127.0.0.1:<port>` on this host — robust, and independent of the
          reverse proxy or any login redirect. `link` checks the public
          `<scheme>://<subdomain>.<domain>` URL instead.
        '';
      };

      exclude = lib.mkOption {
        type = lib.types.listOf lib.types.str;
        default = [ ];
        example = [ "grafana" ];
        description = ''
          Catalog keys to skip — drop a discovered service, or resolve a
          collision with a hand-written `links`/`providers` entry.
        '';
      };

      extraServices = lib.mkOption {
        type = lib.types.listOf (lib.types.attrsOf lib.types.anything);
        default = [ ];
        example = lib.literalExpression ''
          [ { key = "octoprint"; title = "OctoPrint"; icon = "cpu"; port = 5000; } ]
        '';
        description = ''
          Extra catalog entries for services beacon doesn't ship, or
          third-party modules. Same shape as the built-in catalog: `key`
          (required — the provider id and default subdomain), `title`,
          `icon` (an existing dashboard icon name), `port` (for the local
          health check), and optional `enable` (default true), `subdomain`,
          `linkUrl`, `healthUrl`.
        '';
      };
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [{
      assertion = !cfg.discover.enable || cfg.discover.domain != "";
      message = "services.beacon.discover.domain must be set when discover.enable is true.";
    }];

    services.beacon.settings.server.listen =
      lib.mkDefault "${cfg.listenAddress}:${toString cfg.port}";

    networking.firewall.allowedTCPPorts = lib.mkIf cfg.openFirewall [ cfg.port ];

    # Auto-discovery: append generated links/cards to the rendered config.
    # mkAfter places them after any hand-written entries; the TOML list type
    # concatenates the definitions.
    services.beacon.settings.links =
      lib.mkIf cfg.discover.enable (lib.mkAfter discovered.links);
    services.beacon.settings.providers =
      lib.mkIf cfg.discover.enable (lib.mkAfter discovered.providers);

    systemd.services.beacon = {
      description = "beacon status dashboard";
      wantedBy = [ "multi-user.target" ];
      wants = [ "network-online.target" ];
      after = [ "network-online.target" ];
      path = cfg.extraPackages;

      # restic wants a cache directory; point HOME at our private cache.
      environment.HOME = "/var/cache/beacon";

      serviceConfig = {
        ExecStart = "${lib.getExe' cfg.package "beacon"} -config ${configFile}";
        Restart = "on-failure";
        RestartSec = 5;
        CacheDirectory = "beacon";

        DynamicUser = cfg.user == null;
        User = lib.mkIf (cfg.user != null) cfg.user;
        Group = lib.mkIf (cfg.group != null) cfg.group;

        # Hardening. Providers only need outbound network plus read access to
        # configured mounts and credential files.
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectClock = true;
        ProtectControlGroups = true;
        ProtectHostname = true;
        ProtectKernelLogs = true;
        ProtectKernelModules = true;
        ProtectKernelTunables = true;
        ProtectProc = "invisible";
        ProcSubset = "pid";
        RestrictAddressFamilies = [ "AF_UNIX" "AF_INET" "AF_INET6" ];
        RestrictNamespaces = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        LockPersonality = true;
        MemoryDenyWriteExecute = true;
        CapabilityBoundingSet = "";
        SystemCallArchitectures = "native";
        SystemCallFilter = [ "@system-service" "~@privileged" ];
        UMask = "0077";
      };
    };
  };
}
