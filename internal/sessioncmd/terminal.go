package sessioncmd

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/spawn"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/x/ansi"
)

type Terminal struct {
	ID        string `json:"id" jsonschema:"managed terminal session id"`
	Name      string `json:"name" jsonschema:"terminal name shown in Agent Manager"`
	Group     string `json:"group" jsonschema:"group path holding the terminal; empty is the root"`
	Directory string `json:"directory" jsonschema:"terminal's current working directory, or its launch directory when stopped"`
	Status    string `json:"status" jsonschema:"stored Agent Manager status"`
	Running   bool   `json:"running" jsonschema:"whether the terminal currently has a live tmux pane"`
}

type TerminalScreen struct {
	Terminal Terminal `json:"terminal"`
	Output   string   `json:"output" jsonschema:"plain text currently visible in the terminal pane"`
}

type CreateTerminalOptions struct {
	// Nil inherits the calling session's group; a pointer to an empty string
	// deliberately targets the root group.
	Group     *string
	Directory string
}

type Terminals struct {
	commands
}

func NewTerminals(configDir string) *Terminals {
	return newTerminals(configDir, tmux.New)
}

func newTerminals(configDir string, newDriver func() (*tmux.Driver, error)) *Terminals {
	return &Terminals{commands{configDir: configDir, newDriver: newDriver}}
}

func (r *runtime) terminal(id string) (store.Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return store.Session{}, errors.New("terminal_id is empty; call list_terminals to get one")
	}
	sess, err := r.store.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Session{}, fmt.Errorf("terminal %s does not exist; call list_terminals for current ids", id)
	}
	if err != nil {
		return store.Session{}, err
	}
	if !r.cfg.Tools[sess.Tool].Shell {
		return store.Session{}, fmt.Errorf("session %s is an agent, not a terminal", id)
	}
	if sess.Archived {
		return store.Session{}, fmt.Errorf("terminal %s is archived; restore it in Agent Manager first", id)
	}
	return sess, nil
}

func (r *runtime) info(sess store.Session, running bool) Terminal {
	dir := sess.Cwd
	if running {
		if current, err := r.driver.PaneCurrentPath(sess.ID); err == nil {
			dir = current
		}
	}
	return Terminal{
		ID:        sess.ID,
		Name:      sess.Name,
		Group:     sess.Group,
		Directory: dir,
		Status:    sess.Status,
		Running:   running,
	}
}

func (t *Terminals) List(sessionID string) ([]Terminal, error) {
	runtime, err := t.open()
	if err != nil {
		return nil, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return nil, err
	}
	sessions, err := runtime.store.ListSessions(false)
	if err != nil {
		return nil, err
	}
	panes, err := runtime.driver.Panes()
	if err != nil {
		return nil, err
	}
	terminals := make([]Terminal, 0)
	for _, sess := range sessions {
		if !runtime.cfg.Tools[sess.Tool].Shell {
			continue
		}
		_, running := panes[sess.ID]
		terminals = append(terminals, runtime.info(sess, running))
	}
	return terminals, nil
}

func (t *Terminals) Create(sessionID string, opts CreateTerminalOptions) (Terminal, error) {
	runtime, err := t.open()
	if err != nil {
		return Terminal{}, err
	}
	defer runtime.store.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return Terminal{}, err
	}
	toolName, tool, ok := runtime.cfg.ShellTool()
	if !ok {
		return Terminal{}, errors.New("no shell configured; add a tool block with shell = true to config.toml")
	}
	groups, err := runtime.store.Groups()
	if err != nil {
		return Terminal{}, err
	}
	group, dir, err := runtime.createTarget(caller, groups, opts.Group, opts.Directory)
	if err != nil {
		return Terminal{}, err
	}
	runtime.driver.AdoptServerPaneTheme()
	id := spawn.NewID()
	sess := store.Session{
		ID:     id,
		Name:   toolName + "-" + id[:4],
		Tool:   toolName,
		Cwd:    dir,
		Group:  group,
		Status: status.Starting,
		// The caller is the session this terminal was opened for, which is
		// what nests it under that agent in the list instead of leaving it
		// among a group's shells with no way back to who asked for it.
		ParentID: caller.ID,
	}
	if err := runtime.driver.Create(sess.ID, sess.Cwd, tool.Command, nil, 0, 0); err != nil {
		return Terminal{}, err
	}
	if err := runtime.store.CreateSession(sess); err != nil {
		_ = runtime.driver.Kill(sess.ID)
		return Terminal{}, err
	}
	_ = runtime.driver.SetLabel(sess.ID, spawn.SessionLabel(sess.Group, sess.Name))
	return runtime.info(sess, true), nil
}

func (t *Terminals) Send(sessionID, terminalID, command string, keys []string) error {
	hasCommand := strings.TrimSpace(command) != ""
	hasKeys := len(keys) > 0
	if hasCommand == hasKeys {
		return errors.New("provide exactly one of command or keys")
	}
	for _, key := range keys {
		if key == "" {
			return errors.New("keys cannot contain an empty value")
		}
	}
	runtime, err := t.open()
	if err != nil {
		return err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return err
	}
	terminal, err := runtime.terminal(terminalID)
	if err != nil {
		return err
	}
	if !runtime.driver.Exists(terminal.ID) {
		return fmt.Errorf("terminal %s is not running; revive it in Agent Manager first", terminal.ID)
	}
	if hasCommand {
		return runtime.driver.SendText(terminal.ID, command)
	}
	return runtime.driver.SendKeys(terminal.ID, keys...)
}

func (t *Terminals) Read(sessionID, terminalID string) (TerminalScreen, error) {
	runtime, err := t.open()
	if err != nil {
		return TerminalScreen{}, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return TerminalScreen{}, err
	}
	terminal, err := runtime.terminal(terminalID)
	if err != nil {
		return TerminalScreen{}, err
	}
	if !runtime.driver.Exists(terminal.ID) {
		return TerminalScreen{}, fmt.Errorf("terminal %s is not running; revive it in Agent Manager first", terminal.ID)
	}
	output, err := runtime.driver.CapturePane(terminal.ID)
	if err != nil {
		return TerminalScreen{}, err
	}
	return TerminalScreen{
		Terminal: runtime.info(terminal, true),
		Output:   strings.TrimRight(ansi.Strip(output), "\r\n"),
	}, nil
}
