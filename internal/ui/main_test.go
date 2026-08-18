package ui

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/YoanWai/agent-manager/internal/git"
)

// testSocket is an isolated tmux server for this package's tests, so they
// never touch the default socket where the user's shell tmux and live agents
// live. TestMain tears it down before and after the run.
const testSocket = "amuitest"

// TestMain kills any leftover test server so each run starts and ends clean.
// The anchor session then holds the server up for the whole run: tests kill
// their sessions in cleanup, and a server whose last session dies begins an
// exit-empty shutdown that takes the next test's fresh session down with it
// ("server exited unexpectedly", the recurring CI failure in
// TestFocusWatchReportsCursor).
func TestMain(m *testing.M) {
	unsignCommits()
	// No test reaches a network. A seeded repository names a remote it
	// cannot talk to, and a pass that really fetched would hang on it.
	fetchRemote = func(*git.Driver, string) {}
	// kill-server fails whenever no server is up, which is the normal case.
	tmuxCmd("kill-server").Run()
	// Without tmux the run still starts: each test skips through its own
	// requireTmux. With tmux, a run that could not plant the anchor would
	// pass or flake on luck, so it stops instead.
	if _, err := exec.LookPath("tmux"); err == nil {
		if out, err := tmuxCmd("new-session", "-d", "-s", "anchor").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "anchor session: %v: %s\n", err, out)
			os.Exit(1)
		}
	}
	code := m.Run()
	tmuxCmd("kill-server").Run()
	os.Exit(code)
}

// unsignCommits stops the machine's own git config from signing the commits
// these tests make, for every git the run reaches — the helpers here and the
// ones the code under test shells out to. A signing agent that has locked
// asks for a passphrase, and a prompt nobody is there to answer hangs the
// run instead of failing it.
func unsignCommits() {
	os.Setenv("GIT_CONFIG_COUNT", "1")
	os.Setenv("GIT_CONFIG_KEY_0", "commit.gpgsign")
	os.Setenv("GIT_CONFIG_VALUE_0", "false")
}

// tmuxCmd builds a raw tmux command aimed at the test socket, matching the
// socket buildModel's driver runs on.
func tmuxCmd(args ...string) *exec.Cmd {
	return exec.Command("tmux", append([]string{"-L", testSocket}, args...)...)
}
