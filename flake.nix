{
  description = "beacon — self-hosted tailnet status dashboard";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs, ... }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forAllSystems (pkgs: rec {
        default = beacon;
        beacon = pkgs.buildGoModule {
          pname = "beacon";
          version = "0.1.0";
          src = nixpkgs.lib.fileset.toSource {
            root = ./.;
            fileset = nixpkgs.lib.fileset.unions [
              ./cmd
              ./internal
              ./web
              ./vendor
              ./go.mod
              ./go.sum
            ];
          };
          vendorHash = null; # dependencies are committed under vendor/
          subPackages = [ "cmd/beacon" ];
          env.CGO_ENABLED = 0;
          ldflags = [ "-s" "-w" ];
          meta.mainProgram = "beacon";
        };
      });

      nixosModules.default = import ./nix/module.nix self;

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [ go restic ];
        };
      });

      checks = forAllSystems (pkgs:
        let beacon = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
        in {
          package = beacon;
          go-checks = beacon.overrideAttrs (old: {
            pname = "beacon-go-checks";
            buildPhase = ''
              go vet ./...
              go test ./...
            '';
            installPhase = "touch $out";
            doCheck = false;
            doInstallCheck = false;
          });
        });
    };
}
