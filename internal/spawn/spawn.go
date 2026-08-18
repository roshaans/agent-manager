// Package spawn creates the sessions Agent Manager manages: the worktree, the
// project's setup and auto-run scripts, the hook and MCP wiring, the tmux
// session and the store row, as one operation with one rollback.
//
// It knows nothing about a calling session or a TUI, so the manager's own New
// Session form and the commands an agent runs create sessions the same way,
// and a fix to the launch path lands on every caller at once.
package spawn

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/YoanWai/agent-manager/internal/project"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/google/uuid"
)

// Spawner holds what creating a session needs. git may be nil: a machine
// without it runs everything except worktree sessions.
type Spawner struct {
	cfg   config.Config
	store *store.Store
	tmux  *tmux.Driver
	hooks *hooks.Manager
	git   *git.Driver
	// paneSize is the size a new session's pane boots at. It is asked for at
	// the moment tmux is told rather than held, because the manager's preview
	// panel changes size with the terminal and with its own chrome — and
	// because a spawn that is going to be refused should not be measuring a
	// panel it will never fill.
	paneSize func() (width, height int)
}

// New builds a spawner. paneSize may be nil, which leaves tmux's 80x24
// default: that is what a session created outside the TUI gets, until the
// manager's next resize pass sizes it to the preview.
func New(cfg config.Config, st *store.Store, driver *tmux.Driver,
	hookManager *hooks.Manager, gitDriver *git.Driver, paneSize func() (int, int)) *Spawner {
	return &Spawner{
		cfg: cfg, store: st, tmux: driver, hooks: hookManager,
		git: gitDriver, paneSize: paneSize,
	}
}

func (s *Spawner) pane() (int, int) {
	if s.paneSize == nil {
		return 0, 0
	}
	return s.paneSize()
}

// Options describe an agent session to create. Everything here is already
// resolved: Directory exists, Group is a real group, Tool names a configured
// agent CLI. Resolving those is the caller's job, because a default means
// different things to the form (the row under the cursor) and to an agent
// (the session it is calling from).
type Options struct {
	Tool string
	// Name empty generates one, and the session is asked to rename itself
	// once it knows what it is working on.
	Name      string
	Group     string
	Directory string
	Prompt    string
	// ParentID links a session to whoever asked for it, the way a terminal
	// records the session it was opened for.
	ParentID string
	Worktree bool
}

// Result is the created session and what the caller still has to decide about
// it. AutoNamed marks a session this package named, which the TUI holds a
// placeholder for until the agent's own name lands. AutoRuns are the project
// script sessions started beside the agent: rows the caller has to list now,
// since the store already holds them and a list that waits for the next poll
// paints a worktree without the servers it just started.
type Result struct {
	Session   store.Session
	AutoNamed bool
	AutoRuns  []store.Session
	Warnings  []string
}

// LaunchOptions are the extras a session launches with, for the callers that
// build their own store.Session — a shell, a fork, a project run script.
type LaunchOptions struct {
	// RollbackWorktree removes the worktree this session was created in when
	// the launch fails partway. A fresh worktree is clean by construction, so
	// the removal fires.
	RollbackWorktree bool
	// Env is exported into the pane on top of what buildLaunch sets, for the
	// project variables a run or setup script is given. Keys already set by
	// buildLaunch are left alone, so a project cannot shadow the hook and MCP
	// wiring a session needs to report its status.
	Env map[string]string
	// Setup is the project's setup script, run in the pane before the agent.
	// It is applied here rather than by the caller because buildLaunch appends
	// to the command it is given — MCP config, a --settings path — and those
	// flags belong to the agent, inside the wrapper's success branch, not
	// dangling after its fi.
	Setup string
	// SetupMarker is the file the setup wrapper touches on success, so an
	// auto-run script in another pane can wait for it.
	SetupMarker string
}

// Create makes an agent session: the whole path, from the worktree to the
// auto-run scripts started beside it.
func (s *Spawner) Create(opts Options) (Result, error) {
	tool, configured := s.cfg.Tools[opts.Tool]
	if !configured {
		return Result{}, fmt.Errorf("tool %q is not configured (have: %s)",
			opts.Tool, strings.Join(s.cfg.AgentTools(), ", "))
	}
	if tool.Shell {
		return Result{}, fmt.Errorf("%s opens a shell, not an agent", opts.Tool)
	}
	if !isDir(opts.Directory) {
		return Result{}, errors.New("working directory does not exist: " + opts.Directory)
	}
	// Checked here rather than at each caller, because every caller's prompt
	// reaches a command line: a leading dash is read as a flag by the tool.
	if strings.HasPrefix(opts.Prompt, "-") {
		return Result{}, errors.New(`prompt cannot start with "-": the tool would read it as a flag`)
	}

	name, autoNamed := strings.TrimSpace(opts.Name), false
	if name == "" {
		name, autoNamed = opts.Tool+"-"+NewID()[:4], true
	}

	id := NewID()
	dir := opts.Directory
	worktreeRepo, worktreeBranch := "", ""
	if opts.Worktree {
		if s.git == nil {
			return Result{}, errors.New("worktree sessions need git installed")
		}
		root, err := s.git.RepoRoot(dir)
		if err != nil {
			return Result{}, err
		}
		path, branch, err := s.git.AddWorktree(root, name)
		if err != nil {
			return Result{}, err
		}
		dir = path
		worktreeRepo, worktreeBranch = root, branch
	}

	deferDirective := autoNamed && !directiveEmbeddable(opts.Prompt)
	prompt := launchPrompt(opts.Prompt, autoNamed)
	base := withPrompt(tool, tool.Command, prompt)

	// A worktree is a fresh checkout: whatever the project needs that git does
	// not track — installed dependencies, an .env, generated files — is not
	// there yet, and an agent started into that tree spends its first turn on
	// errors that are not the code's. The setup script runs in the pane ahead
	// of the agent so its output is visible and a failure holds the pane.
	launchEnv, setup, marker := map[string]string(nil), "", ""
	var autoRuns project.Settings
	if opts.Worktree {
		settings, err := project.LoadWorktree(dir, worktreeRepo)
		if err != nil {
			s.discardWorktree(worktreeRepo, dir, worktreeBranch)
			return Result{}, err
		}
		setup = settings.Setup
		if setup != "" {
			marker = project.SetupMarker(dir)
		}
		autoRuns = settings
		launchEnv = settings.Env(project.PortKey(dir))
	}

	var pendingInputs []string
	if tool.PromptMode == "send" && prompt != "" {
		pendingInputs = append(pendingInputs, prompt)
	}
	if deferDirective {
		pendingInputs = append(pendingInputs, deferredRenameDirective)
	}

	// Tools that accept a chosen session id launch with one, so a later revive
	// resumes this exact conversation rather than the directory's most recent
	// one. Tools without the flag mint their own id, captured after launch by
	// the poller.
	agentSessionID := ""
	if tool.SessionIDFlag != "" {
		agentSessionID = uuid.NewString()
		base += " " + tool.SessionIDFlag + " " + agentSessionID
	}

	launched, launchWarnings, err := s.Launch(store.Session{
		ID:    id,
		Name:  name,
		Tool:  opts.Tool,
		Cwd:   dir,
		Group: opts.Group,
		// Starting until the agent first draws to its pane, so the row shows a
		// launch state immediately; the poller flips it to the real status.
		Status:         status.Starting,
		AgentSessionID: agentSessionID,
		WorktreeRepo:   worktreeRepo,
		WorktreeBranch: worktreeBranch,
		ParentID:       opts.ParentID,
		PendingInputs:  pendingInputs,
	}, tool, base, LaunchOptions{
		RollbackWorktree: worktreeRepo != "",
		Env:              launchEnv,
		Setup:            setup,
		SetupMarker:      marker,
	})
	if err != nil {
		return Result{}, err
	}

	// Auto-run scripts start after the session exists, so a failure to spawn
	// the agent is not compounded by servers left running for a worktree that
	// never got one. Each waits on the setup marker in its own pane.
	autoRunSessions, autoRunWarnings := s.startAutoRuns(autoRuns, dir, opts.Group)

	return Result{
		Session:   launched,
		AutoNamed: autoNamed,
		AutoRuns:  autoRunSessions,
		Warnings:  append(launchWarnings, autoRunWarnings...),
	}, nil
}

// Launch creates one session's tmux pane and its store row. It is the seam
// every session goes through, agent or shell, so the hook wiring, the MCP
// registration and the worktree rollback have exactly one implementation.
//
// Once the error is nil the session exists and is running, and nothing in
// warnings unmakes it: a status-bar label that could not be written is not a
// reason for the caller to act as though the spawn was refused.
func (s *Spawner) Launch(sess store.Session, tool config.Tool, baseCommand string, opts LaunchOptions) (store.Session, []string, error) {
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now()
	}
	if sess.LastStatusAt.IsZero() {
		sess.LastStatusAt = sess.CreatedAt
	}
	discardWorktree := func() {
		if opts.RollbackWorktree {
			s.discardWorktree(sess.WorktreeRepo, sess.Cwd, sess.WorktreeBranch)
		}
	}
	command, env, err := s.BuildLaunch(sess.Tool, tool, baseCommand, sess.ID)
	if err != nil {
		discardWorktree()
		return store.Session{}, nil, err
	}
	for key, value := range opts.Env {
		if _, taken := env[key]; !taken {
			env[key] = value
		}
	}
	command = project.SetupCommand(opts.Setup, command, opts.SetupMarker)
	width, height := s.pane()
	if err := s.tmux.Create(sess.ID, sess.Cwd, command, env, width, height); err != nil {
		discardWorktree()
		return store.Session{}, nil, err
	}
	if err := s.store.CreateSession(sess); err != nil {
		_ = s.tmux.Kill(sess.ID)
		_ = s.hooks.Remove(sess.ID)
		discardWorktree()
		return store.Session{}, nil, err
	}
	// A label is what the tmux status bar reads, so failing to write one is
	// worth saying and not worth unmaking a running agent over.
	var warnings []string
	if err := s.tmux.SetLabel(sess.ID, SessionLabel(sess.Group, sess.Name)); err != nil {
		warnings = append(warnings, err.Error())
	}
	return sess, warnings, nil
}

// BuildLaunch resolves the shell command and environment a session launches
// with. Every session carries its id so the rename subcommand can find it;
// tools backed by hooks additionally get the generated settings file and their
// status-file path, plus a clean slate from any earlier files under the same id.
//
// Exported for relaunch, which puts a dead session's existing row back on a
// running pane: it makes no row and no worktree, so it wants this half of the
// launch and none of the rest, but it must wire the pane up identically or a
// revived session stops reporting its status.
func (s *Spawner) BuildLaunch(toolName string, tool config.Tool, baseCommand, id string) (string, map[string]string, error) {
	if err := s.hooks.RemoveName(id); err != nil {
		return "", nil, err
	}
	env := map[string]string{hooks.EnvSessionID: id}
	command, err := mcpreg.Apply(mcpreg.Style(toolName, tool.MCP), mcpExecutable(), s.hooks.Dir(), baseCommand, env)
	if err != nil {
		return "", nil, err
	}
	if tool.StatusSource != hooks.StatusSourceClaude {
		return command, env, nil
	}
	settingsPath, err := s.hooks.EnsureSettings()
	if err != nil {
		return "", nil, err
	}
	if err := s.hooks.Remove(id); err != nil {
		return "", nil, err
	}
	env[hooks.EnvStatusFile] = s.hooks.StatusFile(id)
	return command + " --settings " + tmux.ShellQuote(settingsPath), env, nil
}

// autoRunWait caps how long an auto-run script waits on the setup marker. A
// setup that installs a large dependency tree is slow but finite; one that is
// never going to finish should not leave a pane spinning for the rest of the
// session.
const autoRunWait = 15 * time.Minute

// startAutoRuns launches the scripts a project marked auto, for a worktree
// that has just been created. Failures are reported and the rest still start:
// one server refusing to come up is not a reason to withhold the others, and
// none of them is a reason to withhold the agent they were started beside.
func (s *Spawner) startAutoRuns(settings project.Settings, dir, group string) ([]store.Session, []string) {
	names := settings.AutoRuns()
	if len(names) == 0 {
		return nil, nil
	}
	toolName, tool, ok := s.cfg.ShellTool()
	if !ok {
		return nil, []string{`no shell configured: add a tool block with shell = true to config.toml`}
	}
	marker := ""
	if strings.TrimSpace(settings.Setup) != "" {
		marker = project.SetupMarker(dir)
	}
	var started []store.Session
	var warnings []string
	for _, name := range names {
		command := project.WaitForSetup(marker, settings.Run[name].Command, autoRunWait)
		sess := store.Session{
			ID:     NewID(),
			Name:   name + "-" + filepath.Base(dir),
			Tool:   toolName,
			Cwd:    dir,
			Group:  group,
			Status: status.Starting,
		}
		run, runWarnings, err := s.Launch(sess, tool, command, LaunchOptions{
			Env: settings.Env(project.PortKey(dir)),
		})
		if err != nil {
			warnings = append(warnings, "auto-run "+name+": "+err.Error())
			continue
		}
		started = append(started, run)
		warnings = append(warnings, runWarnings...)
	}
	return started, warnings
}

// discardWorktree rolls back a worktree created for a spawn that failed
// partway; a fresh worktree is clean by construction, so the removal fires.
func (s *Spawner) discardWorktree(repo, path, branch string) {
	if repo == "" || s.git == nil {
		return
	}
	_, _ = s.git.RemoveWorktreeIfClean(repo, path, branch)
}

func withPrompt(tool config.Tool, command, prompt string) string {
	if prompt == "" || tool.PromptMode == "send" {
		return command
	}
	if tool.PromptFlag != "" {
		return command + " " + tool.PromptFlag + " " + tmux.ShellQuote(prompt)
	}
	return command + " " + tmux.ShellQuote(prompt)
}

// mcpExecutable names the binary generated MCP configs point at: the running
// manager itself, falling back to the PATH-resolved name.
func mcpExecutable() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "agent-manager"
}

// SessionLabel renders a session's identity for the tmux status bar.
func SessionLabel(group, name string) string {
	if group == "" {
		return name
	}
	return group + " · " + name
}

// NewID mints a manager-side session id: short enough to read off a row,
// hex so the session-scoped commands can validate one they are handed.
func NewID() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405")))
	}
	return hex.EncodeToString(buf)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
