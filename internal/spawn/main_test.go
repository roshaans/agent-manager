package spawn

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// testSocket is an isolated tmux server for this package's tests, so they
// never touch the default socket where the user's shell tmux and live agents
// live. TestMain tears it down before and after the run.
const testSocket = "amspawntest"

// TestMain kills any leftover test server so each run starts and ends clean.
// The anchor session then holds the server up for the whole run: tests kill
// their sessions in cleanup, and a server whose last session dies begins an
// exit-empty shutdown that takes the next test's fresh session down with it.
func TestMain(m *testing.M) {
	tmuxCmd("kill-server").Run()
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

func tmuxCmd(args ...string) *exec.Cmd {
	return exec.Command("tmux", append([]string{"-L", testSocket}, args...)...)
}

func testConfig() config.Config {
	return config.Config{Tools: map[string]config.Tool{
		"claude": {Command: "cat", DefaultStatus: status.Idle},
		"claude-hooked": {
			Command:       "cat",
			StatusSource:  hooks.StatusSourceClaude,
			DefaultStatus: status.Idle,
		},
		"send-tool": {Command: "cat", PromptMode: "send", DefaultStatus: status.Idle},
		"id-tool":   {Command: "cat", SessionIDFlag: "--session-id", DefaultStatus: status.Idle},
		// The block T spawns, carrying the shell flag the way the generated
		// config ships it.
		"terminal": {Shell: true, DefaultStatus: status.Idle},
	}}
}

func newSpawner(t *testing.T) *Spawner {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	driver, err := tmux.NewWithSocket(testSocket)
	if err != nil {
		t.Fatalf("tmux: %v", err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	gitDriver, _ := git.New()
	s := New(testConfig(), st, driver, hooks.NewManager(t.TempDir()), gitDriver, nil)
	t.Cleanup(func() {
		sessions, _ := st.ListSessions(true)
		for _, sess := range sessions {
			_ = driver.Kill(sess.ID)
		}
		_ = st.Close()
	})
	return s
}

// seedRepo builds a committed repo the worktree tests can branch from. It
// sits one level inside the temp directory so the sibling "<name>-worktrees"
// tree is cleaned up with it.
func seedRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
		{"add", "."},
		{"commit", "-m", "seed"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func writeProjectSettings(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".agent-manager")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
