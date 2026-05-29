{
  description = "Rummy scorekeeper";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "rummy";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-JlQWPfcNpIgag1LHDcvz1wlxo/RcdN02J3zKXFd1tvc=";

          meta = with pkgs.lib; {
            description = "Rummy scorekeeper web app";
            homepage = "https://github.com/arjungandhi/rummy";
            license = licenses.mit;
            mainProgram = "rummy";
          };
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
          ];
        };
      }
    )
    // {
      nixosModules.default =
        {
          config,
          lib,
          pkgs,
          ...
        }:
        let
          cfg = config.services.rummy;
        in
        {
          options.services.rummy = {
            enable = lib.mkEnableOption "Rummy scorekeeper";

            dataDir = lib.mkOption {
              type = lib.types.str;
              default = "/var/lib/rummy";
              description = "Directory holding the SQLite database (rummy.db).";
            };

            host = lib.mkOption {
              type = lib.types.str;
              default = "127.0.0.1";
              description = "Address to bind.";
            };

            port = lib.mkOption {
              type = lib.types.port;
              default = 8084;
              description = "Port to listen on.";
            };

            user = lib.mkOption {
              type = lib.types.str;
              default = "rummy";
              description = "User to run the service as.";
            };

            group = lib.mkOption {
              type = lib.types.str;
              default = "rummy";
              description = "Group to run the service as.";
            };
          };

          config = lib.mkIf cfg.enable {
            users.users = lib.mkIf (cfg.user == "rummy") {
              rummy = {
                isSystemUser = true;
                group = cfg.group;
                home = cfg.dataDir;
              };
            };
            users.groups = lib.mkIf (cfg.group == "rummy") { rummy = { }; };

            systemd.services.rummy = {
              description = "Rummy scorekeeper";
              wantedBy = [ "multi-user.target" ];
              after = [ "network.target" ];
              environment = {
                RUMMY_DIR = cfg.dataDir;
                RUMMY_HOST = cfg.host;
                RUMMY_PORT = toString cfg.port;
              };
              serviceConfig = {
                ExecStart = "${self.packages.${pkgs.system}.default}/bin/rummy";
                User = cfg.user;
                Group = cfg.group;
                StateDirectory = lib.mkIf (lib.hasPrefix "/var/lib/" cfg.dataDir) (
                  lib.removePrefix "/var/lib/" cfg.dataDir
                );
                Restart = "always";
                RestartSec = "5s";
              };
            };
          };
        };
    };
}
