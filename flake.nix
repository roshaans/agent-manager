{
  description = "agent-manager — terminal UI for managing AI coding-agent sessions in tmux";

  # This flake has its own nixpkgs pin, deliberately independent of the one in
  # rosh's dotfiles. `nix develop` here must not change its Go toolchain
  # because an unrelated `dru` bumped the system flake.
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      # Mirrors the goreleaser build matrix (darwin + linux, amd64 + arm64).
      systems = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
      forAllSystems = f:
        nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      # Traceable in `--version` whether built from a clean checkout or a
      # dirty working tree mid-change.
      revision = self.shortRev or self.dirtyShortRev or "dirty";
    in
    {
      packages = forAllSystems (pkgs: rec {
        agent-manager = pkgs.callPackage ./nix/package.nix { inherit revision; };
        default = agent-manager;
      });

      # `nix develop` — the inner loop stays plain `go build`; this only
      # supplies the toolchain. tmux is here because the tests drive it.
      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            go-tools
            tmux
            git
            nix-update # bumps vendorHash after a go.mod change
          ];
          shellHook = ''
            echo "agent-manager dev shell — go $(go version | cut -d' ' -f3)"
          '';
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixpkgs-fmt);
    };
}
