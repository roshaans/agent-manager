package sessioncmd

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// commands is what every session-scoped command that talks to the manager's
// state needs: where the manager keeps it, and a way to reach its tmux server.
// Terminals and Sessions both embed it, so opening a terminal and creating a
// session resolve their caller, group and directory the same way.
type commands struct {
	configDir string
	newDriver func() (*tmux.Driver, error)
}

// runtime is one command's view of the manager: its config, its database and
// its tmux server, opened per call. These commands run in the agent's own
// process, which lives and dies with a single tool call, so nothing is held
// between them.
type runtime struct {
	cfg    config.Config
	store  *store.Store
	driver *tmux.Driver
}

func (c commands) open() (*runtime, error) {
	cfg, err := config.LoadDir(c.configDir)
	if err != nil {
		return nil, err
	}
	driver, err := c.newDriver()
	if err != nil {
		return nil, err
	}
	// This process never chose the pane theme the manager renders on, so it
	// takes the one already on the server. Without it a pane opened from here
	// starts its agent with no COLORFGBG and the agent picks its own colors
	// against a background it cannot see.
	driver.AdoptServerPaneTheme()
	st, err := store.Open(filepath.Join(c.configDir, "state.db"))
	if err != nil {
		return nil, err
	}
	return &runtime{cfg: cfg, store: st, driver: driver}, nil
}

// caller is the session the command was run from. Everything these commands
// do is relative to it, so a call that cannot name a live session is refused
// rather than guessed at.
func (r *runtime) caller(sessionID string) (store.Session, error) {
	if err := validSession(sessionID); err != nil {
		return store.Session{}, err
	}
	sess, err := r.store.Get(sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Session{}, fmt.Errorf("calling session %s no longer exists", sessionID)
	}
	return sess, err
}

// createTarget resolves where something the caller asked for should be made.
//
// The group is the caller's own unless one was named; a named group has to
// exist, because inventing it would hide a typo as a new empty group. The
// directory is whichever was given, else the chosen group's nearest inherited
// path, else wherever the calling agent currently is — which for a worktree
// session is that worktree, and is what makes the common call need no
// arguments at all.
func (r *runtime) createTarget(caller store.Session, wantGroup *string, directory string) (string, string, error) {
	group := caller.Group
	groups, err := r.store.Groups()
	if err != nil {
		return "", "", err
	}
	byName := make(map[string]store.Group, len(groups))
	archived := make(map[string]bool, len(groups))
	for _, candidate := range groups {
		byName[candidate.Name] = candidate
		archived[candidate.Name] = candidate.Archived
	}
	if wantGroup != nil {
		group = strings.TrimSpace(*wantGroup)
		if group != "" {
			if _, ok := byName[group]; !ok {
				return "", "", fmt.Errorf("group %q does not exist", group)
			}
		}
	}
	if group != "" && store.EffectivelyArchived(archived, group) {
		return "", "", fmt.Errorf("group %q is archived; restore it in Agent Manager first", group)
	}
	if strings.TrimSpace(directory) != "" {
		dir, err := resolveDirectory(directory)
		return group, dir, err
	}
	if wantGroup != nil {
		for current := group; current != ""; current = parentGroup(current) {
			if candidate := byName[current].Path; candidate != "" {
				if dir, err := resolveDirectory(candidate); err == nil {
					return group, dir, nil
				}
			}
		}
	}
	dir := caller.Cwd
	if current, err := r.driver.PaneCurrentPath(caller.ID); err == nil {
		dir = current
	}
	resolved, err := resolveDirectory(dir)
	if err != nil {
		return "", "", fmt.Errorf("no usable directory: %w", err)
	}
	return group, resolved, nil
}

func resolveDirectory(raw string) (string, error) {
	dir := strings.TrimSpace(raw)
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if dir == "~" {
			dir = home
		} else {
			dir = filepath.Join(home, strings.TrimPrefix(dir, "~/"))
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("directory %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	return abs, nil
}

func parentGroup(group string) string {
	if index := strings.LastIndex(group, "/"); index >= 0 {
		return group[:index]
	}
	return ""
}
