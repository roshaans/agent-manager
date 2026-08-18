package sessioncmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/spawn"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// Session is a session this command created, as the caller needs to see it.
type Session struct {
	ID   string `json:"id" jsonschema:"new session's id"`
	Name string `json:"name" jsonschema:"name shown in Agent Manager; a placeholder until the new agent names itself"`
	Tool string `json:"tool" jsonschema:"agent CLI it launched"`
	// Group is the path holding it, empty for the root.
	Group     string `json:"group" jsonschema:"group path holding the session; empty is the root"`
	Directory string `json:"directory" jsonschema:"directory it works in: its own worktree when one was created for it"`
	Status    string `json:"status" jsonschema:"stored Agent Manager status; a new session starts as starting"`
	Branch    string `json:"branch,omitempty" jsonschema:"branch of the worktree created for it, when it has one"`
	// AutoRuns are the project scripts started beside the session for its new
	// worktree. Named to the caller because each holds a port and a process
	// the caller would otherwise rediscover by colliding with them.
	AutoRuns []AutoRun `json:"auto_runs,omitempty" jsonschema:"project run scripts started beside the session, each in its own terminal"`
	// Warnings is what the session survived rather than what stopped it: an
	// auto-run script that did not start leaves the agent it belongs to
	// running, and saying so beats a silence the caller reads as success.
	Warnings []string `json:"warnings,omitempty" jsonschema:"non-fatal problems; the session is running regardless"`
}

// AutoRun is one project script a create started, its own flat type because
// the MCP output schema cannot hold a Session inside a Session.
type AutoRun struct {
	ID        string `json:"id" jsonschema:"terminal session id, usable with the terminal tools"`
	Name      string `json:"name"`
	Directory string `json:"directory"`
}

type CreateSessionOptions struct {
	// Prompt is the session's first task. Required: a session created for an
	// agent with nothing to do is a row nobody asked for.
	Prompt string
	// Name empty lets the new session name itself once it knows what its work
	// is about, which is what the manager's own quick spawn does.
	Name string
	// Tool empty uses the manager's configured default CLI.
	Tool string
	// Group nil inherits the calling session's group; a pointer to an empty
	// string deliberately targets the root group.
	Group     *string
	Directory string
	// Worktree nil follows the target group's setting. An explicit true in a
	// directory that is not a repository is refused rather than quietly
	// downgraded: an agent that asked for isolation and did not get it would
	// work in the shared checkout believing otherwise.
	Worktree *bool
}

// Sessions creates agent sessions on behalf of a running one.
type Sessions struct {
	commands
}

func NewSessions(configDir string) *Sessions {
	return newSessions(configDir, tmux.New)
}

func newSessions(configDir string, newDriver func() (*tmux.Driver, error)) *Sessions {
	return &Sessions{commands{configDir: configDir, newDriver: newDriver}}
}

func (s *Sessions) Create(sessionID string, opts CreateSessionOptions) (Session, error) {
	if strings.TrimSpace(opts.Prompt) == "" {
		return Session{}, errors.New("prompt is empty; a new session needs a first task to work on")
	}
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.store.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return Session{}, err
	}
	// One read serves the target validation and the worktree-default walk, so
	// a group toggled mid-call cannot hand the two different answers.
	groups, err := runtime.store.Groups()
	if err != nil {
		return Session{}, err
	}
	group, dir, err := runtime.createTarget(caller, groups, opts.Group, opts.Directory)
	if err != nil {
		return Session{}, err
	}
	tool, err := runtime.resolveTool(opts.Tool)
	if err != nil {
		return Session{}, err
	}
	// A missing git binary only rules out worktrees; everything else about a
	// session works without it, so the failure surfaces at the request for one.
	gitDriver, _ := git.New()
	worktree, err := runtime.resolveWorktree(opts.Worktree, groups, group, dir, gitDriver)
	if err != nil {
		return Session{}, err
	}

	runtime.adoptPaneTheme()
	spawner := spawn.New(runtime.cfg, runtime.store, runtime.driver,
		hooks.NewManager(s.configDir), gitDriver, nil)
	result, err := spawner.Create(spawn.Options{
		Tool:      tool,
		Name:      opts.Name,
		Group:     group,
		Directory: dir,
		Prompt:    opts.Prompt,
		// The session that asked for this one, recorded the way a terminal
		// records the session it was opened for.
		ParentID: caller.ID,
		Worktree: worktree,
	})
	if err != nil {
		return Session{}, err
	}
	created := Session{
		ID:        result.Session.ID,
		Name:      result.Session.Name,
		Tool:      result.Session.Tool,
		Group:     result.Session.Group,
		Directory: result.Session.Cwd,
		Status:    result.Session.Status,
		Branch:    result.Session.WorktreeBranch,
		Warnings:  result.Warnings,
	}
	for _, run := range result.AutoRuns {
		created.AutoRuns = append(created.AutoRuns, AutoRun{ID: run.ID, Name: run.Name, Directory: run.Cwd})
	}
	return created, nil
}

// resolveTool picks the CLI a session launches with. A named one only has to
// be configured: hiding a CLI keeps it out of the manager's own pickers, which
// is not a reason to refuse a caller that asked for it by name. Only the
// default follows that setting, since a default is exactly a pick the user
// did not make here.
func (r *runtime) resolveTool(named string) (string, error) {
	if named = strings.TrimSpace(named); named != "" {
		return named, nil
	}
	hidden, err := r.setting(store.SettingHiddenTools)
	if err != nil {
		return "", err
	}
	chosen, err := r.setting(store.SettingDefaultTool)
	if err != nil {
		return "", err
	}
	tool := r.cfg.DefaultTool(chosen, store.ParseHiddenTools(hidden))
	if tool == "" {
		return "", fmt.Errorf("no agent CLI is enabled; turn one on in Agent Manager settings, or name one of: %s",
			strings.Join(r.cfg.AgentTools(), ", "))
	}
	return tool, nil
}

// resolveWorktree answers whether the new session gets its own worktree. An
// explicit request that cannot be met is an error; an inherited default that
// cannot be met just does not apply, the way the manager's own toggle greys
// itself out in a directory that is not a repository.
func (r *runtime) resolveWorktree(want *bool, groups []store.Group, group, dir string, gitDriver *git.Driver) (bool, error) {
	if gitDriver == nil {
		if want != nil && *want {
			return false, errors.New("worktree sessions need git installed")
		}
		return false, nil
	}
	_, repoErr := gitDriver.RepoRoot(dir)
	capable := repoErr == nil
	if want != nil {
		if *want && !capable {
			return false, fmt.Errorf("worktree sessions need a git repository: %s is not one", dir)
		}
		return *want, nil
	}
	if !capable {
		return false, nil
	}
	fallback, err := r.setting(store.SettingWorktreeDefault)
	if err != nil {
		return false, err
	}
	choices := make(map[string]string, len(groups))
	for _, candidate := range groups {
		choices[candidate.Name] = candidate.Worktree
	}
	return store.WorktreeDefault(choices, group, fallback == "on"), nil
}

func (r *runtime) setting(key string) (string, error) {
	value, err := r.store.Setting(key)
	if err != nil {
		return "", fmt.Errorf("reading the %s setting: %w", key, err)
	}
	return value, nil
}
