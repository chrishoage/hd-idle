{
  description = "hd-idle with configurable NixOS wake-window timeouts";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      linuxSystems = [ "x86_64-linux" "aarch64-linux" "i686-linux" "armv7l-linux" ];
      forEachSystem = f: nixpkgs.lib.genAttrs linuxSystems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      packages = forEachSystem (pkgs: {
        default = pkgs.callPackage ./package.nix { };
        hd-idle = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      });

      nixosModules.default = import ./nix/module.nix;
    };
}
