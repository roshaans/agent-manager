package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/spawn"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

func TestExpandForkCommandQuotesPlaceholders(t *testing.T) {
	got := expandForkCommand("tool --fork {id} --new {new_id} --name {name} --file {session_file}", "source", "new", "Sam's fork", "/store/session.jsonl")
	want := "tool --fork 'source' --new 'new' --name 'Sam'\\''s fork' --file '/store/session.jsonl'"
	if got != want {
		t.Fatalf("fork command = %q, want %q", got, want)
	}
}

func TestForkSelectedSessionCreatesNamedSibling(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("work", dir); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "source", dir, "work")
	m.selectSessionRow(t, "source")
	source := m.rows[m.cursor].sess
	if err := m.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.sessions {
		if m.sessions[i].ID == source.ID {
			m.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	argsFile := filepath.Join(t.TempDir(), "fork-args")
	tool := m.cfg.Tools[source.Tool]
	tool.ForkCommand = "printf '%s\\n' {id} {new_id} {name} > " + tmux.ShellQuote(argsFile) + "; cat"
	m.cfg.Tools[source.Tool] = tool

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(*Model)
	if m.mode != modeFork || m.fork.source.ID != source.ID {
		t.Fatalf("fork mode = %v, source = %q", m.mode, m.fork.source.ID)
	}
	m.fork.name.SetValue("child fork")
	updated, cmd := m.handleForkKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)
	if m.mode != modeList || m.errBar.text != "" {
		t.Fatalf("after fork: mode=%v err=%q", m.mode, m.errBar.text)
	}

	var forkedID string
	for _, sess := range m.sessionRows() {
		if sess.Name != "child fork" {
			continue
		}
		forkedID = sess.ID
		if sess.Tool != source.Tool || sess.Cwd != source.Cwd || sess.Group != source.Group {
			t.Fatalf("forked session = %+v, source = %+v", sess, source)
		}
		if sess.AgentSessionID == "" || sess.AgentSessionID == source.AgentSessionID {
			t.Fatalf("forked conversation id = %q", sess.AgentSessionID)
		}
	}
	if forkedID == "" {
		t.Fatal("forked session not found")
	}
	stored, err := m.store.Get(forkedID)
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var raw []byte
	for time.Now().Before(deadline) {
		raw, err = os.ReadFile(argsFile)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{"source-conversation", stored.AgentSessionID, "child fork"}
	if len(got) != len(want) {
		t.Fatalf("fork args = %q", raw)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fork args = %v, want %v", got, want)
		}
	}
}

func TestForkCopiesManagedWorktreeReference(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	source := store.Session{
		ID:             "managed-source",
		Name:           "source",
		Tool:           "claude",
		Cwd:            dir,
		Status:         status.Idle,
		AgentSessionID: "source-conversation",
		WorktreeRepo:   filepath.Dir(dir),
		WorktreeBranch: "am/source",
	}
	if err := m.store.CreateSession(source); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	m.selectSessionRow(t, "source")
	tool := m.cfg.Tools[source.Tool]
	tool.ForkCommand = "true {id}; cat"
	m.cfg.Tools[source.Tool] = tool

	m.openFork()
	m.fork.name.SetValue("forked")
	updated, cmd := m.handleForkKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)

	var forked store.Session
	for _, sess := range m.sessionRows() {
		if sess.Name == "forked" {
			forked = sess
			break
		}
	}
	if forked.ID == "" {
		t.Fatal("forked session not found")
	}
	if forked.WorktreeRepo != source.WorktreeRepo || forked.WorktreeBranch != source.WorktreeBranch {
		t.Fatalf("forked worktree reference = %+v, source = %+v", forked, source)
	}
}

func TestForkLaunchFailureKeepsSharedWorktree(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	if err := m.spawnSession(spawn.Options{Tool: "claude", Name: "source", Directory: repo, Worktree: true}); err != nil {
		t.Fatal(err)
	}
	sessions, err := m.store.ListSessions(true)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %v, err %v", sessions, err)
	}
	source := sessions[0]
	badConfig := t.TempDir()
	if err := os.WriteFile(filepath.Join(badConfig, "hooks"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Break what the launch actually uses: a hooks directory it cannot write
	// into is the failure this test is about.
	m.spawner = spawn.New(m.cfg, m.store, m.tmux, hooks.NewManager(badConfig), m.gitDrv, nil)

	forked := source
	forked.ID = "failed-fork"
	forked.Name = "forked"
	if err := m.launchNewSession(forked, m.cfg.Tools[forked.Tool], "cat", spawn.LaunchOptions{}); err == nil {
		t.Fatal("launch failure was not reported")
	}
	if _, err := os.Stat(source.Cwd); err != nil {
		t.Fatalf("source worktree was removed: %v", err)
	}
}

func TestOpenForkRequiresConfiguredCommandAndConversationID(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	m.selectSessionRow(t, "source")
	source := m.rows[m.cursor].sess

	tool := m.cfg.Tools[source.Tool]
	tool.ForkCommand = ""
	m.cfg.Tools[source.Tool] = tool
	m.openFork()
	if !strings.Contains(m.errBar.text, "no fork_command") {
		t.Fatalf("missing command error = %q", m.errBar.text)
	}

	tool.ForkCommand = "tool --fork latest"
	m.cfg.Tools[source.Tool] = tool
	m.openFork()
	if !strings.Contains(m.errBar.text, "must reference the source via {id} or {session_file}") {
		t.Fatalf("missing placeholder error = %q", m.errBar.text)
	}

	tool.ForkCommand = "tool --fork {id}"
	m.cfg.Tools[source.Tool] = tool
	m.openFork()
	if !strings.Contains(m.errBar.text, "no captured conversation id") {
		t.Fatalf("missing id error = %q", m.errBar.text)
	}
}

// Every fork_command the manager ships has to satisfy its own validator, so
// a placeholder can never outrun the session store that resolves it.
func TestShippedForkCommandsValidate(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools) < 6 {
		t.Fatalf("built-in tools = %d, expected every shipped CLI", len(cfg.Tools))
	}
	source := store.Session{Name: "source", AgentSessionID: "source-conversation"}
	forkable := 0
	for name, tool := range cfg.Tools {
		if tool.Shell || tool.ForkCommand == "" {
			continue
		}
		forkable++
		if err := validateForkSource(name, tool, source); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if forkable == 0 {
		t.Fatal("no shipped tool defines a fork_command")
	}
}

// {session_file} only resolves for a store that keeps conversations in a
// file. A tool without one would otherwise fork against a gemini path.
func TestOpenForkRejectsSessionFileWithoutAFileBackedStore(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	source := store.Session{
		ID:             "claude-source",
		Name:           "source",
		Tool:           "claude",
		Cwd:            dir,
		Status:         status.Idle,
		AgentSessionID: "claude-conversation",
	}
	if err := m.store.CreateSession(source); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	m.selectSessionRow(t, "source")

	previousResolver := forkSessionFileResolver
	forkSessionFileResolver = func(sessionStore, id string) (string, error) {
		t.Errorf("resolver called for session store %q, id %q", sessionStore, id)
		return "", nil
	}
	t.Cleanup(func() { forkSessionFileResolver = previousResolver })

	tool := m.cfg.Tools["claude"]
	if tool.SessionStore != "" {
		t.Fatalf("claude session_store = %q, want empty", tool.SessionStore)
	}
	tool.ForkCommand = "claude --session-file {session_file}"
	m.cfg.Tools["claude"] = tool

	m.openFork()

	if m.mode == modeFork {
		t.Fatal("fork opened for a tool whose store keeps no session file")
	}
	if !strings.Contains(m.errBar.text, "{session_file}") ||
		!strings.Contains(m.errBar.text, `session_store = "gemini"`) {
		t.Fatalf("err = %q, want it to name the placeholder and the store it needs", m.errBar.text)
	}
}

func TestOpenForkRejectsGroup(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("work", ""); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "work")
	m.openFork()
	if m.errBar.text != "select a session to fork" {
		t.Fatalf("group fork error = %q", m.errBar.text)
	}
}

// Tools split into two fork shapes: those that mint their own conversation id
// (opencode, codex) leave {new_id} out, so the fork starts without one until
// the session store captures it; those that take an agent-chosen id (grok,
// claude) include {new_id} and the fork launches with it already set.
func TestForkAgentSessionIDFollowsNewIDPlaceholder(t *testing.T) {
	for _, tc := range []struct {
		name      string
		tool      string
		forkCmd   string
		wantNewID bool
	}{
		{"opencode_session_store", "opencode", "true {id}; cat", false},
		{"grok_id_flag", "grok", "true {id} {new_id}; cat", true},
		{"pi_id_flag", "pi", "true {id} {new_id}; cat", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			dir := t.TempDir()
			source := store.Session{
				ID:             "source-" + tc.tool,
				Name:           "source",
				Tool:           tc.tool,
				Cwd:            dir,
				Status:         status.Idle,
				AgentSessionID: "source-conversation",
			}
			if err := m.store.CreateSession(source); err != nil {
				t.Fatal(err)
			}
			loadStoredRows(t, m)
			m.selectSessionRow(t, "source")

			tool := m.cfg.Tools[tc.tool]
			tool.ForkCommand = tc.forkCmd
			tool.MCP = "none"
			m.cfg.Tools[tc.tool] = tool

			m.openFork()
			if m.errBar.text != "" {
				t.Fatalf("openFork error = %q", m.errBar.text)
			}
			m.fork.name.SetValue("forked")
			updated, cmd := m.handleForkKey(tea.KeyMsg{Type: tea.KeyEnter})
			m = updated.(*Model)
			m.applyCmd(t, cmd)
			if m.mode != modeList || m.errBar.text != "" {
				t.Fatalf("after fork: mode=%v err=%q", m.mode, m.errBar.text)
			}

			var forked store.Session
			for _, sess := range m.sessionRows() {
				if sess.Name == "forked" {
					forked = sess
					break
				}
			}
			if forked.ID == "" {
				t.Fatal("forked session not found")
			}
			if tc.wantNewID {
				if forked.AgentSessionID == "" || forked.AgentSessionID == source.AgentSessionID {
					t.Fatalf("forked AgentSessionID = %q, want a fresh id", forked.AgentSessionID)
				}
			} else if forked.AgentSessionID != "" {
				t.Fatalf("forked AgentSessionID = %q, want empty until the session store captures it", forked.AgentSessionID)
			}
		})
	}
}

// gemini forks by handing `--session-file` the source's on-disk session file;
// gemini imports it as a brand-new conversation, so the fork launches with no
// conversation id and the resolver's path must reach the command line intact.
func TestForkGeminiResolvesSessionFile(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	source := store.Session{
		ID:             "gemini-source",
		Name:           "source",
		Tool:           "gemini",
		Cwd:            dir,
		Status:         status.Idle,
		AgentSessionID: "gemini-conversation",
	}
	if err := m.store.CreateSession(source); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	m.selectSessionRow(t, "source")

	const sourceFile = "/store/gemini/session-source.jsonl"
	previousResolver := forkSessionFileResolver
	forkSessionFileResolver = func(sessionStore, id string) (string, error) {
		if sessionStore != "gemini" {
			t.Errorf("resolver session store = %q, want gemini", sessionStore)
		}
		if id != source.AgentSessionID {
			t.Errorf("resolver id = %q, want %q", id, source.AgentSessionID)
		}
		return sourceFile, nil
	}
	t.Cleanup(func() { forkSessionFileResolver = previousResolver })

	argsFile := filepath.Join(t.TempDir(), "fork-args")
	tool := m.cfg.Tools["gemini"]
	tool.ForkCommand = "printf '%s\\n' {session_file} > " + tmux.ShellQuote(argsFile) + "; cat"
	tool.SessionStore = "gemini"
	tool.MCP = "none"
	m.cfg.Tools["gemini"] = tool

	m.openFork()
	if m.errBar.text != "" {
		t.Fatalf("openFork error = %q", m.errBar.text)
	}
	m.fork.name.SetValue("forked")
	updated, cmd := m.handleForkKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)
	if m.mode != modeList || m.errBar.text != "" {
		t.Fatalf("after fork: mode=%v err=%q", m.mode, m.errBar.text)
	}

	var forked store.Session
	for _, sess := range m.sessionRows() {
		if sess.Name == "forked" {
			forked = sess
			break
		}
	}
	if forked.ID == "" {
		t.Fatal("forked session not found")
	}
	if forked.AgentSessionID != "" {
		t.Fatalf("forked AgentSessionID = %q, want empty until the gemini store captures it", forked.AgentSessionID)
	}

	deadline := time.Now().Add(2 * time.Second)
	var raw []byte
	var err error
	for time.Now().Before(deadline) {
		raw, err = os.ReadFile(argsFile)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(raw)); got != sourceFile {
		t.Fatalf("session file passed to fork = %q, want %q", got, sourceFile)
	}
}

// A fork whose source file cannot be resolved reports the error and does not
// launch, leaving the session list unchanged.
func TestForkGeminiResolverFailureReportsError(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	source := store.Session{
		ID:             "gemini-source",
		Name:           "source",
		Tool:           "gemini",
		Cwd:            dir,
		Status:         status.Idle,
		AgentSessionID: "gemini-conversation",
	}
	if err := m.store.CreateSession(source); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	m.selectSessionRow(t, "source")

	previousResolver := forkSessionFileResolver
	forkSessionFileResolver = func(_, id string) (string, error) {
		return "", fmt.Errorf("no gemini session file found for conversation %s", id)
	}
	t.Cleanup(func() { forkSessionFileResolver = previousResolver })

	tool := m.cfg.Tools["gemini"]
	tool.ForkCommand = "gemini --session-file {session_file}"
	tool.SessionStore = "gemini"
	m.cfg.Tools["gemini"] = tool

	before := len(m.sessionRows())
	m.openFork()
	m.fork.name.SetValue("forked")
	updated, _ := m.handleForkKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if !strings.Contains(m.errBar.text, "no gemini session file") {
		t.Fatalf("resolver error = %q", m.errBar.text)
	}
	if got := len(m.sessionRows()); got != before {
		t.Fatalf("session rows = %d, want %d (no fork launched)", got, before)
	}
}

// A shell has no fork_command, but saying so names a config field for a
// row that was never going to hold a conversation.
func TestForkRefusesAShellInItsOwnTerms(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	sess := spawnTerminal(t, m)
	m.selectSessionRow(t, sess.Name)

	m.openFork()

	if m.mode == modeFork {
		t.Fatal("fork should not open on a shell row")
	}
	if !strings.Contains(m.errBar.text, "is a shell") ||
		!strings.Contains(m.errBar.text, "no conversation to fork") {
		t.Fatalf("err = %q, want it to name the row as a shell", m.errBar.text)
	}
	if strings.Contains(m.errBar.text, "fork_command") {
		t.Fatalf("err = %q should not name a config field", m.errBar.text)
	}
}

// The bug this fixes: a fork used to be handed its source's directory, so two
// agents wrote to one checkout — the thing AGENTS.md warns against, reached
// by pressing one key.
func TestForkOfAWorktreeSessionGetsItsOwn(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	source := forkableSource(t, m, "feature", repo, true)

	m.openFork()
	if !m.fork.worktree {
		t.Fatal("forking a worktree session did not default to a worktree of its own")
	}
	m.fork.name.SetValue("feature-fork")
	_, cmd := m.submitFork()
	m.applyCmd(t, cmd)

	forked := sessionNamed(t, m, "feature-fork")
	if forked.Cwd == source.Cwd {
		t.Fatalf("the fork shares its source's checkout: %s", forked.Cwd)
	}
	if forked.WorktreeBranch == source.WorktreeBranch {
		t.Fatalf("the fork shares its source's branch: %s", forked.WorktreeBranch)
	}
	if forked.WorktreeRepo != source.WorktreeRepo {
		t.Fatalf("the fork was cut from %q, want the source's repository %q",
			forked.WorktreeRepo, source.WorktreeRepo)
	}
	if !isDir(forked.Cwd) {
		t.Fatalf("the fork's worktree was never made: %s", forked.Cwd)
	}
}

// A fork continues its source's work, so it branches from where the source is
// rather than from the repository's base.
func TestForkBranchesFromItsSource(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	source := forkableSource(t, m, "feature", repo, true)
	// Work the source has committed but never merged.
	writeCommit(t, source.Cwd, "only-here.txt", "x")

	m.openFork()
	m.fork.name.SetValue("feature-fork")
	_, cmd := m.submitFork()
	m.applyCmd(t, cmd)

	forked := sessionNamed(t, m, "feature-fork")
	if _, err := os.Stat(filepath.Join(forked.Cwd, "only-here.txt")); err != nil {
		t.Fatalf("the fork did not carry the source's commits: %v", err)
	}
}

// Sharing the directory is still reachable, for the times that is what you
// meant — but now it is a choice rather than the only behaviour.
func TestForkCanStillShareTheDirectory(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	source := forkableSource(t, m, "feature", repo, true)

	m.openFork()
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune{'w'}})
	*m = *updated.(*Model)
	if m.fork.worktree {
		t.Fatal("alt+w did not turn the worktree off")
	}
	m.fork.name.SetValue("feature-fork")
	_, cmd := m.submitFork()
	m.applyCmd(t, cmd)

	if forked := sessionNamed(t, m, "feature-fork"); forked.Cwd != source.Cwd {
		t.Fatalf("the fork ran in %q, want its source's directory %q", forked.Cwd, source.Cwd)
	}
}

// A session that is not in a repository at all has nowhere to cut a worktree,
// and the fork has to land in its directory rather than refuse.
func TestForkOutsideARepositorySharesTheDirectory(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	forkableSource(t, m, "loose", dir, false)

	m.openFork()
	if m.forkWorktreeOn() {
		t.Fatal("offered a worktree in a directory that cannot host one")
	}
	m.fork.name.SetValue("loose-fork")
	_, cmd := m.submitFork()
	m.applyCmd(t, cmd)

	if forked := sessionNamed(t, m, "loose-fork"); forked.Cwd != dir {
		t.Fatalf("the fork ran in %q, want %q", forked.Cwd, dir)
	}
}

// forkable gives the test config's agent a fork command, without which
// openFork refuses before any of this is reached.
func forkable(t *testing.T, m *Model, toolName string) {
	t.Helper()
	tool := m.cfg.Tools[toolName]
	tool.ForkCommand = "true {id}; cat"
	m.cfg.Tools[toolName] = tool
}

// forkableSource is a session a fork can actually be taken from: a real
// worktree to branch off, a tool that knows how to fork, and the captured
// conversation id openFork insists on.
func forkableSource(t *testing.T, m *Model, name, dir string, worktree bool) store.Session {
	t.Helper()
	forkable(t, m, "claude")
	if worktree {
		createWorktreeSession(t, m, name, dir)
	} else {
		createSession(t, m, name, dir, "")
	}
	source := sessionNamed(t, m, name)
	if err := m.store.SetAgentSessionID(source.ID, "conversation"); err != nil {
		t.Fatalf("set conversation id: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, name)
	return sessionNamed(t, m, name)
}

func sessionNamed(t *testing.T, m *Model, name string) store.Session {
	t.Helper()
	for _, sess := range m.sessions {
		if sess.Name == name {
			return sess
		}
	}
	t.Fatalf("no session named %q", name)
	return store.Session{}
}
