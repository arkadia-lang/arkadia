{
  description = "Arkadia, a strict functional language with algebraic effects";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { nixpkgs, ... }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      # `nix develop`: the tools to build and test the compiler (make check,
      # make test, make bootstrap)
      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [ pkgs.go pkgs.gnumake pkgs.git ];
          # generated programs are built without cgo
          CGO_ENABLED = "0";
        };
      });
    };
}
