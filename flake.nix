{
  description = "Arkadia, a strict functional language with algebraic effects";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { nixpkgs, ... }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      # `nix develop`: everything needed to develop Arkadia. Go builds the
      # compiler and the generated programs, git fetches git dependencies of
      # projects, just runs the tasks (justfile) and Node.js builds the VS Code
      # extension (editors/vscode).
      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [ pkgs.go pkgs.git pkgs.just pkgs.nodejs ];
          # generated programs are built without cgo
          CGO_ENABLED = "0";
        };
      });
    };
}
