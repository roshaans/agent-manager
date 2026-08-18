package ui

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/spawn"
	"github.com/YoanWai/agent-manager/internal/store"
)

// The two functions below are all the manager adds to internal/spawn: the
// list the new row joins and the status bar a warning lands on. The pane size
// the preview will capture at goes in as a callback at construction, since it
// changes with the terminal. Everything else about creating a session is the
// same work whoever asked for it, so it lives in that package instead.

// spawnSession creates an agent session for the New Session form and quick
// spawn, and holds the placeholder for one that was told to name itself.
func (m *Model) spawnSession(opts spawn.Options) error {
	result, err := m.spawner.Create(opts)
	if err != nil {
		return err
	}
	m.sessions = append(append(m.sessions, result.Session), result.AutoRuns...)
	m.rebuildRows()
	// The directive went out with the launch, so the row waits for the name
	// the agent picks instead of showing the one generated for it.
	if result.AutoNamed {
		if m.awaitedRenames == nil {
			m.awaitedRenames = map[string]string{}
		}
		m.awaitedRenames[result.Session.ID] = result.Session.Name
	}
	m.noteWarnings(result.Warnings)
	return nil
}

// launchNewSession is the same for the sessions the manager builds itself:
// a shell, a fork, a project's run script.
func (m *Model) launchNewSession(sess store.Session, tool config.Tool, baseCommand string, opts spawn.LaunchOptions) error {
	launched, warnings, err := m.spawner.Launch(sess, tool, baseCommand, opts)
	if err != nil {
		return err
	}
	m.sessions = append(m.sessions, launched)
	m.rebuildRows()
	m.noteWarnings(warnings)
	return nil
}

// noteWarnings puts what went wrong beside a session that was still created
// on the status bar. The bar is one line, so the first warning speaks for the
// rest — counted, not dropped, so two problems never read as one.
func (m *Model) noteWarnings(warnings []string) {
	if len(warnings) == 0 {
		return
	}
	text := warnings[0]
	if len(warnings) > 1 {
		text += fmt.Sprintf(" (+%d more)", len(warnings)-1)
	}
	m.errBar.text = text
}
