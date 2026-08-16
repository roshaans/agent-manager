{ lib
, buildGoModule
, tmux
, git
, # Set by flake.nix to the git rev this was built from, so `agent-manager
  # --version` identifies a specific fork build rather than just the upstream
  # tag it is based on. Matters while we carry local patches.
  revision ? "unknown"
}:

buildGoModule (finalAttrs: {
  pname = "agent-manager";

  # The upstream tag this fork is rebased onto. Bump when rebasing; the rev
  # suffix below is what actually distinguishes our builds from each other.
  version = "0.29.1";

  # Built from the working tree, not fetchFromGitHub: the point of packaging
  # inside the fork is that a source change and its packaging change land in
  # one commit. fileset keeps README/docs churn from forcing a rebuild.
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../main.go
      ../main_test.go
      ../internal
      ../tools
    ];
  };

  # Regenerate after any go.mod change:
  #   nix run nixpkgs#nix-update -- --flake --version=skip agent-manager
  # Vendoring instead (vendorHash = null) was measured at 154M / 3510 files,
  # almost all modernc.org/libc — too large to carry in git.
  vendorHash = "sha256-W8KwhRMMrQcIGyMsJzGtZkBS8W8ZX/kIo3z5MD+x664=";

  # Matches .goreleaser.yaml, so a nix build and a release build agree.
  env.CGO_ENABLED = 0;
  ldflags = [
    "-s"
    "-w"
    "-X main.version=${finalAttrs.version}+${revision}"
  ];

  # The suite shells out to tmux for session management and to git for the
  # review mailbox tests; neither is on PATH in the build sandbox by default.
  nativeCheckInputs = [ tmux git ];

  # The real-tmux test creates a session in $HOME, which the sandbox otherwise
  # points at the non-existent /homeless-shelter.
  preCheck = ''
    export HOME=$(mktemp -d)
  '';

  # Three tests cannot pass under the build sandbox. All three are sandbox
  # limitations, verified to pass in `nix develop` on a real checkout — they
  # still run in CI, which is where a regression in them would surface.
  #   TestTreesSelf / TestSampleMemoryMatchesVMStatOnDarwin
  #     read the host's real process tree and shell out to macOS's vm_stat.
  #   TestQuickWorktreeToggle
  #     needs a git repo; fileset.toSource deliberately excludes .git, so the
  #     alt+w toggle correctly refuses with "not a git repo".
  #   TestRunIndicatorAppearsThroughAPoll
  #     reads a pane's process tree through ps, which the sandbox restricts
  #     the same way it restricts the two above.
  checkFlags = [
    "-skip=^(TestTreesSelf|TestSampleMemoryMatchesVMStatOnDarwin|TestQuickWorktreeToggle|TestRunIndicatorAppearsThroughAPoll)$"
  ];

  # tools/badges is a CI helper that builds alongside the TUI. Its tests are
  # worth running, so it stays in the build and only the binary is dropped —
  # nothing but agent-manager should land on PATH.
  postInstall = ''
    rm -f $out/bin/badges
  '';

  meta = {
    description = "Terminal UI for managing AI coding-agent sessions in tmux";
    homepage = "https://github.com/roshaans/agent-manager";
    license = lib.licenses.asl20;
    mainProgram = "agent-manager";
    platforms = lib.platforms.unix;
  };
})
