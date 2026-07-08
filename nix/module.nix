# NixOS module for beacon. Imported from the flake as `nixosModules.default`;
# `self` supplies the default package.
self:
{ config, lib, pkgs, ... }:

let
  cfg = config.services.beacon;
  settingsFormat = pkgs.formats.toml { };
  configFile = settingsFormat.generate "beacon.toml" cfg.settings;
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
  };

  config = lib.mkIf cfg.enable {
    services.beacon.settings.server.listen =
      lib.mkDefault "${cfg.listenAddress}:${toString cfg.port}";

    networking.firewall.allowedTCPPorts = lib.mkIf cfg.openFirewall [ cfg.port ];

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
