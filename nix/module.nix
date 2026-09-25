{ config, lib, pkgs, utils, ... }:

let
  cfg = config.services.hd-idle;
  args = [ "-i" (toString cfg.idleTime) ]
    ++ lib.optionals (cfg.wakeWindow != null) [ "-w" cfg.wakeWindow ]
    ++ lib.optionals (cfg.wakeIdleTime != null) [ "-W" (toString cfg.wakeIdleTime) ]
    ++ lib.optionals (cfg.logFile != null) [ "-l" cfg.logFile ]
    ++ lib.concatLists (lib.mapAttrsToList (d: o: [ "-a" d "-i" (toString o.idleTime) ]) cfg.disks)
    ++ cfg.extraArgs;
in
{
  options.services.hd-idle = {
    enable = lib.mkEnableOption "hd-idle disk spindown service";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ../package.nix { };
      defaultText = lib.literalExpression "pkgs.callPackage <hd-idle-flake>/package.nix { }";
      description = "The hd-idle package used by the service.";
    };

    idleTime = lib.mkOption {
      type = lib.types.ints.unsigned;
      default = 600;
      example = 1800;
      description = "Idle time in seconds outside the wake window. Zero disables spindown.";
    };

    wakeWindow = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "00:00-09:00";
      description = "Daily local-time window in HH:MM-HH:MM format.";
    };

    wakeIdleTime = lib.mkOption {
      type = lib.types.nullOr lib.types.ints.unsigned;
      default = null;
      example = 10800;
      description = "Idle time in seconds inside the wake window. Null suppresses spindown inside the window; zero also suppresses spindown.";
    };

    logFile = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = null;
      example = "/var/log/hd-idle.log";
      description = "Log file for spindown events (-l).";
    };

    disks = lib.mkOption {
      type = lib.types.attrsOf (lib.types.submodule {
        options.idleTime = lib.mkOption {
          type = lib.types.ints.unsigned;
          description = "Idle time in seconds for this disk (-a <disk> -i). Zero disables it.";
        };
      });
      default = { };
      example = { "/dev/disk/by-id/ata-..." = { idleTime = 1200; }; };
      description = "Per-device idle times, appended after the global options.";
    };

    extraArgs = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = [ "-a" "sda" "-i" "3600" ];
      description = "Additional hd-idle arguments, for example per-device options.";
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.wakeIdleTime == null || cfg.wakeWindow != null;
        message = "services.hd-idle.wakeIdleTime requires services.hd-idle.wakeWindow.";
      }
    ];

    systemd.services.hd-idle = {
      description = "Spin down idle hard disks";
      wantedBy = [ "multi-user.target" ];
      serviceConfig = {
        ExecStart = "${cfg.package}/bin/hd-idle ${utils.escapeSystemdExecArgs args}";
        Restart = "on-failure";
      };
    };
  };
}
