package sessioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/spawn"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/google/uuid"
)

type sessionHarness struct {
	driver   *tmux.Driver
	store    *store.Store
	sessions *Sessions
	caller   store.Session
}

// newSessionHarness stands up a manager the way a real one is: a config with
// agent CLIs and a shell block, a database, a tmux server, and one session
// already running that the commands are called from.
func newSessionHarness(t *testing.T) *sessionHarness {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	configDir := t.TempDir()
	configText := `[tools.claude]
command = "cat"
mcp = "none"
default_status = "idle"

[tools.codex]
command = "cat"
mcp = "none"
default_status = "idle"

[tools.terminal]
command = ""
shell = true
default_status = "idle"
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configText), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	driver, err := tmux.NewWithSocket("amsesstest-" + uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("tmux driver: %v", err)
	}
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	callerDir := t.TempDir()
	caller := store.Session{
		ID:     spawn.NewID(),
		Name:   "calling-agent",
		Tool:   "claude",
		Cwd:    callerDir,
		Group:  "backend",
		Status: status.Idle,
	}
	if err := st.CreateGroup("backend", callerDir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := driver.Create(caller.ID, caller.Cwd, "", nil, 80, 24); err != nil {
		t.Fatalf("create caller pane: %v", err)
	}
	if err := st.CreateSession(caller); err != nil {
		_ = driver.Kill(caller.ID)
		t.Fatalf("create caller row: %v", err)
	}
	h := &sessionHarness{driver: driver, store: st, caller: caller}
	h.sessions = newSessions(configDir, func() (*tmux.Driver, error) { return driver, nil })
	t.Cleanup(func() {
		sessions, _ := st.ListSessions(true)
		for _, sess := range sessions {
			_ = driver.Kill(sess.ID)
		}
		if out, err := exec.Command("tmux", "-L", driver.SocketName(), "kill-server").CombinedOutput(); err != nil &&
			!strings.Contains(string(out), "no server running") {
			t.Errorf("kill test tmux server: %v: %s", err, strings.TrimSpace(string(out)))
		}
		_ = st.Close()
	})
	return h
}

// The call an agent makes with nothing but a task lands beside it: its group,
// its directory, and the CLI the manager is configured to spawn.
func TestCreateSessionDefaultsToTheCallersPlace(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: "build the api"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Group != h.caller.Group {
		t.Fatalf("group = %q, want the caller's %q", created.Group, h.caller.Group)
	}
	if !sameTerminalPath(created.Directory, h.caller.Cwd) {
		t.Fatalf("directory = %q, want the caller's %q", created.Directory, h.caller.Cwd)
	}
	if created.Tool != "claude" {
		t.Fatalf("tool = %q, want the configured default", created.Tool)
	}
	if created.Status != status.Starting {
		t.Fatalf("status = %q, want %q", created.Status, status.Starting)
	}
	if created.Name == "" || created.Branch != "" {
		t.Fatalf("created = %+v, want a placeholder name and no worktree", created)
	}
	stored, err := h.store.Get(created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// The session that asked for it, so the manager can say where it came from.
	if stored.ParentID != h.caller.ID {
		t.Fatalf("parent = %q, want the caller %q", stored.ParentID, h.caller.ID)
	}
	if !h.driver.Exists(created.ID) {
		t.Fatal("no tmux session for the created row")
	}
}

func TestCreateSessionHonoursAnExplicitTarget(t *testing.T) {
	h := newSessionHarness(t)
	parentDir := t.TempDir()
	explicitDir := t.TempDir()
	if err := h.store.CreateGroup("platform", parentDir); err != nil {
		t.Fatal(err)
	}
	if err := h.store.CreateGroup("platform/api", ""); err != nil {
		t.Fatal(err)
	}

	group := "platform/api"
	inherited, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: "do it", Group: &group})
	if err != nil {
		t.Fatalf("create inherited: %v", err)
	}
	if inherited.Group != group || !sameTerminalPath(inherited.Directory, parentDir) {
		t.Fatalf("inherited target = %+v", inherited)
	}

	root := ""
	explicit, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{
		Prompt: "do it", Group: &root, Directory: explicitDir, Tool: "codex", Name: "named-by-hand",
	})
	if err != nil {
		t.Fatalf("create explicit: %v", err)
	}
	if explicit.Group != "" || !sameTerminalPath(explicit.Directory, explicitDir) {
		t.Fatalf("explicit target = %+v", explicit)
	}
	if explicit.Tool != "codex" || explicit.Name != "named-by-hand" {
		t.Fatalf("explicit tool/name = %+v", explicit)
	}
}

// Every refusal has to leave the list as it found it: a row for a session
// that never launched is worse than the error.
func TestCreateSessionRefusalsLeaveNothingBehind(t *testing.T) {
	h := newSessionHarness(t)
	missing := "missing/group"
	archived := "backend"
	worktree := true
	for name, tc := range map[string]struct {
		opts CreateSessionOptions
		want string
	}{
		"no prompt":        {CreateSessionOptions{}, "needs a first task"},
		"blank prompt":     {CreateSessionOptions{Prompt: "   "}, "needs a first task"},
		"flag-like prompt": {CreateSessionOptions{Prompt: "--version"}, `cannot start with "-"`},
		"unknown group":    {CreateSessionOptions{Prompt: "do it", Group: &missing}, "does not exist"},
		"unknown tool":     {CreateSessionOptions{Prompt: "do it", Tool: "nope"}, "not configured"},
		"a shell block":    {CreateSessionOptions{Prompt: "do it", Tool: "terminal"}, "opens a shell, not an agent"},
		"missing dir":      {CreateSessionOptions{Prompt: "do it", Directory: filepath.Join(t.TempDir(), "gone")}, "directory"},
		// The caller's own directory is not a repository, so isolation cannot
		// be given and must not be silently withheld.
		"worktree outside a repo": {CreateSessionOptions{Prompt: "do it", Worktree: &worktree}, "need a git repository"},
	} {
		t.Run(name, func(t *testing.T) {
			before, _ := h.store.ListSessions(true)
			_, err := h.sessions.Create(h.caller.ID, tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Create = %v, want an error containing %q", err, tc.want)
			}
			after, _ := h.store.ListSessions(true)
			if len(after) != len(before) {
				t.Fatalf("a refused create added a row: before=%d after=%d", len(before), len(after))
			}
		})
	}

	if err := h.store.SetGroupArchived("backend", true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: "do it", Group: &archived}); err == nil ||
		!strings.Contains(err.Error(), "archived") {
		t.Fatalf("archived group error = %v", err)
	}
}

func TestCreateSessionNeedsALiveCaller(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := h.sessions.Create("", CreateSessionOptions{Prompt: "do it"}); err == nil ||
		!strings.Contains(err.Error(), "not inside an agent-manager session") {
		t.Fatalf("empty caller error = %v", err)
	}
	if _, err := h.sessions.Create("deadbeef", CreateSessionOptions{Prompt: "do it"}); err == nil ||
		!strings.Contains(err.Error(), "no longer exists") {
		t.Fatalf("unknown caller error = %v", err)
	}
}

// Hiding a CLI keeps it out of the manager's own pickers. A caller that names
// it is not using a picker, so the name still works; only the default follows
// the setting, since a default is the pick nobody made here.
func TestCreateSessionDefaultToolFollowsSettingsButNamedToolsAlwaysWork(t *testing.T) {
	h := newSessionHarness(t)
	// The config a manager loads is the written one plus the CLIs it backfills,
	// so what counts as "the next enabled tool" is read off it rather than
	// assumed from the fixture.
	runtime, err := h.sessions.open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer runtime.store.Close()
	all := runtime.cfg.AgentTools()
	if len(all) < 2 {
		t.Fatalf("harness needs more than one agent CLI, got %v", all)
	}

	if err := h.store.SetSetting(store.SettingDefaultTool, "codex"); err != nil {
		t.Fatal(err)
	}
	chosen, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: "do it"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if chosen.Tool != "codex" {
		t.Fatalf("tool = %q, want the stored choice", chosen.Tool)
	}

	// A stored choice the user has since hidden is not a tool to spawn with.
	if err := h.store.SetSetting(store.SettingHiddenTools, "codex"); err != nil {
		t.Fatal(err)
	}
	var wantFallback string
	for _, name := range all {
		if name != "codex" {
			wantFallback = name
			break
		}
	}
	fallen, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: "do it"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if fallen.Tool != wantFallback {
		t.Fatalf("tool = %q, want the first enabled one %q", fallen.Tool, wantFallback)
	}

	named, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: "do it", Tool: "codex"})
	if err != nil {
		t.Fatalf("named create: %v", err)
	}
	if named.Tool != "codex" {
		t.Fatalf("tool = %q, want the CLI the caller named", named.Tool)
	}

	if err := h.store.SetSetting(store.SettingHiddenTools, strings.Join(all, ",")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: "do it"}); err == nil ||
		!strings.Contains(err.Error(), "no agent CLI is enabled") {
		t.Fatalf("all hidden error = %v", err)
	}
}

func TestCreateSessionWorktreeFollowsTheGroupUntilItIsAsked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newSessionHarness(t)
	repo := seedSessionRepo(t)
	if err := h.store.CreateGroup("repos", repo); err != nil {
		t.Fatal(err)
	}
	group := "repos"

	// Nobody has asked for worktrees anywhere, so a repository alone is not a
	// reason to branch one.
	plain, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: "do it", Group: &group})
	if err != nil {
		t.Fatalf("create plain: %v", err)
	}
	if plain.Branch != "" {
		t.Fatalf("unasked worktree = %+v", plain)
	}

	if err := h.store.SetGroupWorktree("repos", "on"); err != nil {
		t.Fatal(err)
	}
	inherited, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: "do it", Group: &group, Name: "wt-inherited"})
	if err != nil {
		t.Fatalf("create inherited: %v", err)
	}
	if inherited.Branch == "" || sameTerminalPath(inherited.Directory, repo) {
		t.Fatalf("group default did not branch a worktree: %+v", inherited)
	}

	// An explicit no beats the group, the way the manager's own toggle does.
	off := false
	refused, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: "do it", Group: &group, Worktree: &off})
	if err != nil {
		t.Fatalf("create opted out: %v", err)
	}
	if refused.Branch != "" {
		t.Fatalf("explicit worktree=false still branched: %+v", refused)
	}
}

// seedSessionRepo builds a committed repo a worktree can branch from. It sits
// one level inside the temp directory so the sibling "<name>-worktrees" tree
// is cleaned up with it.
func seedSessionRepo(t *testing.T) string {
	t.Helper()
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
