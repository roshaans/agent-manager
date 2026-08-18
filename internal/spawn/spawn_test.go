package spawn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

// Every caller's prompt reaches a command line, so the checks that keep one
// off it belong here rather than at each caller.
func TestCreateRefusesWhatCannotBecomeASession(t *testing.T) {
	s := newSpawner(t)
	dir := t.TempDir()
	for name, tc := range map[string]struct {
		opts Options
		want string
	}{
		"unknown tool":   {Options{Tool: "nope", Directory: dir}, "not configured"},
		"a shell block":  {Options{Tool: "terminal", Directory: dir}, "opens a shell, not an agent"},
		"missing dir":    {Options{Tool: "claude", Directory: filepath.Join(dir, "gone")}, "does not exist"},
		"flag-like task": {Options{Tool: "claude", Directory: dir, Prompt: "--version"}, `cannot start with "-"`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.Create(tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Create = %v, want an error containing %q", err, tc.want)
			}
			sessions, listErr := s.store.ListSessions(true)
			if listErr != nil {
				t.Fatal(listErr)
			}
			if len(sessions) != 0 {
				t.Fatalf("a refused spawn left %d rows behind", len(sessions))
			}
		})
	}
}

// An unnamed session is named for now and asked to rename itself; a named one
// is already at the name its user chose.
func TestCreateNamesOnlyWhatWasNotNamed(t *testing.T) {
	s := newSpawner(t)
	dir := t.TempDir()

	auto, err := s.Create(Options{Tool: "claude", Directory: dir, Prompt: "build the api"})
	if err != nil {
		t.Fatalf("auto-named create: %v", err)
	}
	if !auto.AutoNamed {
		t.Fatal("a session created without a name has one generated and is asked to rename")
	}
	if !strings.HasPrefix(auto.Session.Name, "claude-") {
		t.Fatalf("generated name = %q, want it to say which CLI it is", auto.Session.Name)
	}

	named, err := s.Create(Options{Tool: "claude", Name: "  fix-auth  ", Directory: dir, Prompt: "build the api"})
	if err != nil {
		t.Fatalf("named create: %v", err)
	}
	if named.AutoNamed || named.Session.Name != "fix-auth" {
		t.Fatalf("named session = %+v, want the trimmed name and no rename request", named)
	}
}

// A prompt that cannot open with the directive carries it as its own message
// instead, and only for a session that was asked to rename at all.
func TestCreateDefersTheDirectiveItCannotEmbed(t *testing.T) {
	s := newSpawner(t)
	dir := t.TempDir()
	for name, tc := range map[string]struct {
		opts Options
		want bool
	}{
		"slash command": {Options{Tool: "claude", Directory: dir, Prompt: "/compact"}, true},
		"no prompt":     {Options{Tool: "claude", Directory: dir}, true},
		"plain prompt":  {Options{Tool: "claude", Directory: dir, Prompt: "do things"}, false},
		"named session": {Options{Tool: "claude", Name: "custom", Directory: dir, Prompt: "/compact"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := s.Create(tc.opts)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			queued := strings.Join(result.Session.PendingInputs, "\n")
			if got := strings.Contains(queued, deferredRenameDirective); got != tc.want {
				t.Fatalf("deferred directive queued = %v, want %v (inputs %q)", got, tc.want, queued)
			}
		})
	}
}

// A send-mode tool takes its first task by typing rather than on the command
// line, so the prompt has to survive as a queued input instead.
func TestCreateQueuesTheFirstTaskForSendModeTools(t *testing.T) {
	s := newSpawner(t)
	result, err := s.Create(Options{Tool: "send-tool", Name: "custom", Directory: t.TempDir(), Prompt: "do the work"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	queued := strings.Join(result.Session.PendingInputs, "\n")
	if !strings.Contains(queued, "do the work") {
		t.Fatalf("send-mode prompt was not queued: %q", queued)
	}
	if !strings.Contains(queued, renameAvailableNote) {
		t.Fatalf("the queued prompt lost its launch note: %q", queued)
	}
}

// A tool that accepts a chosen conversation id launches with one, so a later
// revive resumes this exact conversation rather than the directory's most
// recent one.
func TestCreateMintsAConversationIDWhenTheToolTakesOne(t *testing.T) {
	s := newSpawner(t)
	dir := t.TempDir()

	withFlag, err := s.Create(Options{Tool: "id-tool", Name: "with-id", Directory: dir})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if withFlag.Session.AgentSessionID == "" {
		t.Fatal("a tool with a session_id_flag should launch on an id we chose")
	}

	without, err := s.Create(Options{Tool: "claude", Name: "no-id", Directory: dir})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if without.Session.AgentSessionID != "" {
		t.Fatalf("a tool with no flag mints its own id later, got %q", without.Session.AgentSessionID)
	}
}

func TestCreateStoresTheRowItLaunched(t *testing.T) {
	s := newSpawner(t)
	result, err := s.Create(Options{
		Tool: "claude", Name: "listed", Group: "backend",
		Directory: t.TempDir(), Prompt: "do things", ParentID: "abcd1234",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	stored, err := s.store.Get(result.Session.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Name != "listed" || stored.Group != "backend" || stored.Tool != "claude" {
		t.Fatalf("stored row = %+v", stored)
	}
	if stored.Status != status.Starting {
		t.Fatalf("status = %q, want %q until the poller sees the pane", stored.Status, status.Starting)
	}
	// The session that asked for this one, recorded the way a terminal
	// records the session it was opened for.
	if stored.ParentID != "abcd1234" {
		t.Fatalf("parent = %q, want the caller", stored.ParentID)
	}
	if !s.tmux.Exists(stored.ID) {
		t.Fatal("no tmux session for a created row")
	}
}

func TestCreateWorktreeBranchesAndRecordsIt(t *testing.T) {
	s := newSpawner(t)
	repo := seedRepo(t)
	result, err := s.Create(Options{Tool: "claude", Name: "wt-feat", Directory: repo, Worktree: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	sess := result.Session
	if sess.WorktreeRepo == "" || sess.WorktreeBranch == "" {
		t.Fatalf("worktree session did not record its repo and branch: %+v", sess)
	}
	if sess.Cwd == repo {
		t.Fatal("a worktree session should work in the worktree, not the repo it branched from")
	}
	if _, err := os.Stat(sess.Cwd); err != nil {
		t.Fatalf("worktree dir missing: %v", err)
	}
}

func TestCreateWorktreeNeedsARepository(t *testing.T) {
	s := newSpawner(t)
	_, err := s.Create(Options{Tool: "claude", Name: "wt-fail", Directory: t.TempDir(), Worktree: true})
	if err == nil {
		t.Fatal("a directory outside any repository cannot host a worktree")
	}
	sessions, listErr := s.store.ListSessions(true)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(sessions) != 0 {
		t.Fatal("no session row should exist after a blocked spawn")
	}
}

// The worktree is made before anything that can still fail, so every later
// failure has to take it back with it.
func TestCreateRollsBackTheWorktreeItMade(t *testing.T) {
	repo := seedRepo(t)
	worktreePath := filepath.Join(filepath.Dir(repo), filepath.Base(repo)+"-worktrees", "wt-rollback")

	t.Run("settings that will not parse", func(t *testing.T) {
		s := newSpawner(t)
		writeProjectSettings(t, repo, "port_base = 70000\n")
		t.Cleanup(func() { os.RemoveAll(filepath.Join(repo, ".agent-manager")) })

		if _, err := s.Create(Options{Tool: "claude", Name: "wt-rollback", Directory: repo, Worktree: true}); err == nil {
			t.Fatal("unreadable project settings must block the spawn")
		}
		if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
			t.Fatal("worktree must be rolled back when its project settings cannot be read")
		}
	})

	t.Run("a launch that cannot be built", func(t *testing.T) {
		s := newSpawner(t)
		hooksDir := s.hooks.Dir()
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(hooksDir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(hooksDir, 0o755) })

		if _, err := s.Create(Options{Tool: "claude", Name: "wt-rollback", Directory: repo, Worktree: true}); err == nil {
			t.Fatal("a launch command that cannot be built must block the spawn")
		}
		if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
			t.Fatal("worktree must be rolled back when the launch command cannot be built")
		}
	})
}

// A launch that went through returns the row it stored and says nothing else.
// Warnings are for what a created session survives — a status-bar label that
// could not be written — so an empty one here is the caller's signal that
// there is nothing to report beyond the session itself.
func TestLaunchReturnsTheStoredSessionAndNoWarnings(t *testing.T) {
	s := newSpawner(t)
	sess := store.Session{
		ID: NewID(), Name: "labelled", Tool: "claude",
		Cwd: t.TempDir(), Status: status.Starting,
	}
	launched, warnings, err := s.Launch(sess, s.cfg.Tools["claude"], "cat", LaunchOptions{})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("a healthy launch carries no warnings: %v", warnings)
	}
	if launched.CreatedAt.IsZero() || launched.LastStatusAt.IsZero() {
		t.Fatalf("the returned session should carry the times it was stamped with: %+v", launched)
	}
	stored, err := s.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("launched session is not in the store: %v", err)
	}
	if stored.Name != launched.Name || stored.Cwd != launched.Cwd {
		t.Fatalf("returned %+v but stored %+v", launched, stored)
	}
}

// The setup script runs in the pane ahead of the agent, and the flags the
// agent needs belong inside the wrapper's success branch rather than dangling
// after its fi.
func TestCreateWrapsTheAgentInTheProjectSetup(t *testing.T) {
	s := newSpawner(t)
	repo := seedRepo(t)
	writeProjectSettings(t, repo, "setup = \"touch setup-ran\"\n")

	result, err := s.Create(Options{Tool: "claude-hooked", Name: "wt-setup", Directory: repo, Worktree: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	script, err := os.ReadFile(filepath.Join(os.TempDir(), "am-launch-"+result.Session.ID+".sh"))
	if err != nil {
		t.Fatalf("launch script: %v", err)
	}
	body := string(script)
	if !strings.Contains(body, "if touch setup-ran") {
		t.Fatalf("setup script did not open the wrapper:\n%s", body)
	}
	settingsAt := strings.Index(body, "--settings ")
	fiAt := strings.LastIndex(body, "\nfi")
	if settingsAt < 0 || fiAt < 0 || settingsAt > fiAt {
		t.Fatalf("the agent's flags must sit inside the wrapper's success branch:\n%s", body)
	}
	if !strings.Contains(body, hooks.EnvSessionID+"=") {
		t.Fatalf("the pane did not get its session id:\n%s", body)
	}
}

// One server refusing to come up is not a reason to withhold the others, and
// none of them is a reason to withhold the agent they were started beside.
func TestCreateStartsAutoRunsBesideTheAgent(t *testing.T) {
	s := newSpawner(t)
	repo := seedRepo(t)
	writeProjectSettings(t, repo, "[run.dev]\ncommand = \"cat\"\nauto = true\n")

	result, err := s.Create(Options{Tool: "claude", Name: "wt-auto", Directory: repo, Worktree: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Returned as well as stored: the caller's list has to show these rows
	// now, not on its next poll.
	if len(result.AutoRuns) != 1 || !strings.HasPrefix(result.AutoRuns[0].Name, "dev-") {
		t.Fatalf("AutoRuns = %+v, want the started script", result.AutoRuns)
	}
	sessions, err := s.store.ListSessions(true)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, sess := range sessions {
		if sess.ID == result.Session.ID {
			continue
		}
		if strings.HasPrefix(sess.Name, "dev-") && sess.Cwd == result.Session.Cwd {
			found = true
		}
	}
	if !found {
		t.Fatalf("no auto-run session beside the agent, got %d rows", len(sessions))
	}
}

func TestWithPromptComposesPerToolStyle(t *testing.T) {
	plain := config.Tool{Command: "cat"}
	if got := withPrompt(plain, plain.Command, "fix the bug"); got != "cat 'fix the bug'" {
		t.Fatalf("positional compose = %q", got)
	}
	flagged := config.Tool{Command: "opencode", PromptFlag: "--prompt"}
	if got := withPrompt(flagged, flagged.Command, "do it"); got != "opencode --prompt 'do it'" {
		t.Fatalf("flagged compose = %q", got)
	}
	if got := withPrompt(plain, plain.Command, ""); got != "cat" {
		t.Fatalf("empty prompt should leave the command untouched, got %q", got)
	}
	// A send-mode tool is typed into after it boots, so its prompt must not
	// reach the command line at all.
	sent := config.Tool{Command: "hermes --cli", PromptMode: "send"}
	if got := withPrompt(sent, sent.Command, "do it"); got != sent.Command {
		t.Fatalf("send-mode prompt changed launch command to %q", got)
	}
}

func TestBuildLaunchCarriesSessionIDAndHookWiring(t *testing.T) {
	s := newSpawner(t)
	plain := s.cfg.Tools["claude"]
	command, env, err := s.BuildLaunch("claude", plain, withPrompt(plain, plain.Command, "fix the bug"), "abcd1234")
	if err != nil {
		t.Fatalf("BuildLaunch: %v", err)
	}
	if env[hooks.EnvSessionID] != "abcd1234" {
		t.Fatalf("plain tool env = %v, want session id", env)
	}
	if !strings.HasPrefix(command, "cat 'fix the bug' --mcp-config '") {
		t.Fatalf("command = %q", command)
	}

	hooked := s.cfg.Tools["claude-hooked"]
	command, env, err = s.BuildLaunch("claude-hooked", hooked, hooked.Command, "abcd1234")
	if err != nil {
		t.Fatalf("BuildLaunch hooked: %v", err)
	}
	if env[hooks.EnvSessionID] != "abcd1234" || env[hooks.EnvStatusFile] == "" {
		t.Fatalf("hooked tool env = %v, want session id and status file", env)
	}
	if !strings.Contains(command, "--settings '") {
		t.Fatalf("hooked command = %q, want the generated settings file", command)
	}
}

// A project cannot shadow the wiring a session needs to report its status.
func TestLaunchEnvDoesNotLetAProjectOverrideTheHookWiring(t *testing.T) {
	s := newSpawner(t)
	sess := store.Session{ID: NewID(), Name: "env", Tool: "claude", Cwd: t.TempDir(), Status: status.Starting}
	if _, _, err := s.Launch(sess, s.cfg.Tools["claude"], "cat", LaunchOptions{
		Env: map[string]string{hooks.EnvSessionID: "hijacked", "AGENT_MANAGER_PORT": "3100"},
	}); err != nil {
		t.Fatalf("launch: %v", err)
	}
	script, err := os.ReadFile(filepath.Join(os.TempDir(), "am-launch-"+sess.ID+".sh"))
	if err != nil {
		t.Fatalf("launch script: %v", err)
	}
	body := string(script)
	if strings.Contains(body, "hijacked") {
		t.Fatalf("a project overrode the session id:\n%s", body)
	}
	if !strings.Contains(body, "export "+hooks.EnvSessionID+"='"+sess.ID+"'") {
		t.Fatalf("session id missing from the pane:\n%s", body)
	}
	if !strings.Contains(body, "AGENT_MANAGER_PORT='3100'") {
		t.Fatalf("the project's own variables should still reach the pane:\n%s", body)
	}
}
