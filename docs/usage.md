# Usage

```bash
agent-manager
```

Sessions run inside tmux (`am_*` namespace), so they survive the manager quitting. Inside a session, **Ctrl+Q** detaches back to the manager when your terminal and tmux leave it available; **Ctrl+\\** is an alternate under the same rule. **Ctrl+R** opens the session's diff review and **F3** opens its directory in your editor. In a full-screen attach, the session footer also shows an inner tmux prefix followed by `d` when configured. When nested inside another tmux, send the inner prefix shown in the footer, then press `d`. If both tmux servers use the same prefix, invoke the outer tmux's `send-prefix` binding; if the outer tmux otherwise captures the inner prefix, configure it to forward that key. `agent-manager --version` prints the version.

Agent sessions live on a private tmux server named `agentmgr`, so they never mix with the tmux you run yourself and a `kill-server` on your own socket leaves them alone. To reach one from a plain shell, name that server: `tmux -L agentmgr ls`, then `tmux -L agentmgr attach -t am_<id>`.

## Keys

| Key | Action |
|-----|--------|
| `n` | New session (name, tool, directory, worktree toggle, optional starting prompt, group picker) |
| `T` | New terminal tab: a plain shell in the selected group, with no agent in it |
| `c` | A fork without the context: a fresh conversation on the checkout under the cursor, same CLI and branch ([Several chats on one checkout](#several-chats-on-one-checkout)) |
| `shift+←` / `shift+→` | Next / previous chat of that checkout, from the list or from inside a pane |
| `1`…`9` | In the list: jump to that chat by the number the rail prints beside it |
| `tab` / `shift+tab` | In the list: next / previous chat |
| `alt+1`…`alt+9` | Focused: jump straight to a chat, on terminals that send Option as Meta |
| `p` | Run a project script from `.agent-manager/settings.toml` in the row's directory, or offer to create the file when the project has none ([Project settings](project-settings.md)) |
| `T` | New terminal tab: a shell under the selected agent, or in the selected group |
| `o` | Open the selected row's directory in your editor |
| `O` | Show what this worktree is running: its server in a browser, or a TUI run session's pane |
| `P` | Open the session's pull request; offer to open one when it has none, and pick when it has several ([Pull requests](#pull-requests)) |
| `G` | Open [lazygit](#lazygit) on the row's repository, full screen; quit it to come back to the list |
| `Y` | Copy the selected row's directory to the clipboard: a session's checkout — its worktree, when it has one — or a group's default path |
| `i` | The rules the selected session runs under: the `CLAUDE.md` / `AGENTS.md` / `GEMINI.md` its tool reads, read in place or opened in your editor ([Agent rules](#agent-rules)) |
| `f` | Fork the selected conversation into a named session in the same group. A fork of a worktree session gets a worktree of its own, branched from where its source is, so two agents never write to one checkout; `alt+w` shares the source's directory instead |
| `g` | New group (name, parent, default path) |
| `enter` | Focus session in place (keys go to the agent, list stays) / fold group |
| `A` | Attach session full screen (Settings can swap it with `enter`) |
| `ctrl+q` / `ctrl+\` | Inside a session: back to the manager when the terminal and tmux leave the key available |
| `esc` `esc` | Focused: back to the manager, leaving a working agent working. Neither Escape reaches it; a single `esc` still does, half a second later, so it keeps clearing the prompt and interrupting a turn |
| tmux prefix, then `d` | Inside a full-screen attach: back to the manager when the prefix reaches the inner tmux |
| `F3` | Inside a session: open its directory in your editor |
| `→` | Step into the row: focus the session, or open the group. In beta; Settings (`s`) can turn the pair off |
| `←` | Step out: close the group, or — focused, with the caret at the start of the agent's prompt — back to the manager. This needs the tool's prompt marker (its `activity_cutoff`) on the caret's row, so a CLI without one keeps `←` entirely; anywhere else in the prompt it moves the caret as usual |
| `K` / `J` (or `shift+↑` / `shift+↓`) | Reorder session or group among its visible siblings |
| `m` | Move a session to a group, or a terminal into a session |
| `r` | Rename session / edit tool; edit group name and default path |
| `x` | Kill the selected session, or every live session under a group: frees the RAM their agents hold, and the rows stay for `v` |
| `X` | Kill every live session in view |
| `v` | Revive a dead session, or every dead session under a group |
| `V` | Revive every dead session in view |
| `R` | Restart the selected session on an empty context: same name, group, directory and tool |
| `a` / `u` | Archive / restore a session or group. Archive kills the process and keeps the last preview; restore resumes it |
| `d` | Delete session and the terminals opened for it, or a group + its entire subtree |
| `space` | Quick prompt: answer the selected session, or spawn an agent in the selected group |
| `ctrl+r` | Review the selected session's changes: full-screen whole-file diffs, with `c` to comment a line and `C` to send the comments to the agent |
| `F` | Fold / unfold every group |
| `s` | Settings (quick-spawn tool, theme, theme follows OS, list density, review layout, after quick send, session keys, ←→ step in/out, worktree sessions, notifications, report a bug, suggest a change) |
| `\|` | Resize the split: `←→` nudge the divider, `enter` commits, `esc` cancels |
| `t` | Toggle archived view |
| `w` | Filter to sessions that need attention (`waiting`, `finished`, `errored`); press again to show all |
| `M` | Messages (updates, tips; `x` dismisses one for good). The welcome message points at Settings for a bug or an idea. |
| `e` | Hide / show empty groups |
| `/` | Search |
| `?` | The key map: every binding, grouped by what it acts on. It scrolls (`↑↓`/`jk`, `pgup`/`pgdn`, `g`/`G`) and `/` searches it down to one line. |
| `q` | Quit (sessions keep running) |

Navigation is keyboard-driven. The manager claims mouse reporting so the wheel stays inside the app and cannot scroll the TUI out of view: a notch scrolls the diff, and in a focused session it walks that pane's scrollback, where click-drag also selects pane text and copies it. In a focused agent that tracks the mouse, a click passes straight through to its own clickable UI while a drag still selects and copies; hold `alt` to pass a whole drag through instead, for the agent's own text selection or sliders. In the list the wheel does nothing, since moving the selection with it retargets every key that follows. Fuller mouse support, on by default with a settings toggle, is tracked in [#110](https://github.com/YoanWai/agent-manager/issues/110).

## Quick prompt

Press `space` to dock a prompt bar at the bottom of the sidebar. The target follows the cursor while the bar is open (`↑↓` still navigate):

- On a **session** row, `enter` sends the typed text straight into the session's pane, so the agent gets it as a user message without you attaching. The bar clears and stays open, ready for the next answer; Settings (`s`) can make it close instead.
- On a **group** row, `enter` spawns a new agent in that group and submits the prompt at startup, using the group's default path. The spawn tool starts at the Settings default and `tab` cycles it (claude ↔ opencode ↔ any configured tool); the footer shows the current pick. `alt+w` toggles whether the new agent spawns into its own git worktree, starting from the Settings default; the footer shows `worktree: on` or `worktree: off`, or `worktree: unavailable (not a git repo)` when the target directory cannot hold one. Answering an existing session ignores the toggle, since there is no new session to place in a worktree. The agent starts working on the prompt immediately.

`ctrl+v` pastes an image from the system clipboard as an `[Image #1]` chip at the caret. The image is saved under `agent-manager-pastes` in your temp directory, and on send each chip is swapped back for its path, so the paths reach the agent in the order and the places you pasted them. `backspace` next to a chip removes the whole chip, and an edit that swallows one releases its image. A clipboard holding text rather than an image pastes as text. Pasted images older than seven days are cleared at startup and once a day while the manager runs, so an agent can still open one from an earlier session while temp stays tidy.

`esc` closes the bar.

The new-session form's optional `prompt` field launches an agent the same way. It takes `ctrl+v` and its chips too, since a first task is often the screenshot that explains it: paste the design to match or the crash to read, and the agent opens the file on its first turn. Leaving the form without creating the session releases the images it was holding, the way closing the bar does. Tools whose CLI takes the prompt behind a flag declare it with `prompt_flag`, while a persistent CLI with no startup-prompt argument uses `prompt_mode = "send"` (see [Configuration](configuration.md)).

![answering a working Claude Code session from the prompt bar, without attaching](demo-space.gif)

## Terminal tabs

`T` opens a shell tab in the group under the cursor: a session like any other — same list, same row keys, same `enter`, `x`, `v` and `R` — with your shell in the pane instead of an agent. It lands in the selected session's directory, wherever that session has moved to, or in the group's default path when a group row is selected, so the shell that runs the tests sits next to the agent that wrote them. Opened on a session it takes that session's name — `terminal-review-done` rather than `terminal-0ab5` — and records which session it was opened for, so a shell stays attributable to its worktree even after you `cd` out of it. Its status rests at idle throughout, a build included: turn tracking belongs to agents, and a shell has no turns.

The shell is the `[tools.terminal]` block in [config.toml](configuration.md). It ships with no command, which leaves the pane on `$SHELL`; set one to open a different shell. What marks it as a shell is `shell = true`, not its name, so a `[tools.terminal]` block you wrote yourself stays the agent CLI you meant it to be.

### Where the shells sit

Shells gather under a **Terminals** rule pinned to the foot of the list, holding every shell from every group. The tree scrolls above it; the block keeps at most half the list and scrolls inside itself once you have more shells than that. A pinned row stands away from the tree, so it names the session it was opened for — the group would be shared by every worktree under it and tell them apart from nothing. A shell with no session to name falls back to the directory it was launched in.

Settings (`s`) has a `terminal rows` row that switches this from `pinned` to `nested`, which hangs each shell off the session it was opened for, one level under it in the tree and marked with `❯` where an agent carries its status dot. The worktree a terminal belongs to is then the row above it rather than something to work out from a name, and the row leaves its own column empty because that row already says it. A second `T` on a shell joins it as a sibling instead of nesting deeper. A shell that recorded no session — one opened on a group row, or one whose session has since been archived — looks for the oldest agent launched in its own directory, so a terminal opened on a worktree's group row still finds the agent living there. Only a shell that finds neither, in its own group, sits in that group beside the agents.

A group's dots and counts describe its agents either way, so the shell you left running a build never shows up as work in progress.
`T` opens a shell tab: a session like any other (same list, same row keys, same `enter`, `x`, `v` and `R`) with your shell in the pane instead of an agent. On an agent, the new shell nests under that session, in that agent's group and directory. On a group, it lands in the group as an un-nested sibling, in the group's default path. On a nested shell it joins the same parent; on an un-nested shell it stays un-nested in that shell's group. Either way it opens in that shell's own directory, so a shell you have `cd`'d somewhere hands the next one the same place. A nested shell is named after the session it hangs under, `terminal-review-done` rather than `terminal-0ab5`, and the next one under that session counts up to `terminal-review-done-2`; a shell with no session over it keeps the generated name, and `r` renames any of them. Its status rests at idle throughout: turn tracking belongs to agents, and a shell has no turns.

The shell is the `[tools.terminal]` block in [config.toml](configuration.md). It ships with no command, which leaves the pane on `$SHELL`; set one to open a different shell. What marks it as a shell is `shell = true`, not its name, so a `[tools.terminal]` block you wrote yourself stays the agent CLI you meant it to be.

Shells live in the tree with the agents they belong to, marked with `❯` where an agent carries its status dot. `m` on a terminal moves it onto an agent (nests under that session) or onto a group (un-nests into that group). A group's dots and counts describe its agents, so only agent work shows as in progress.

Deleting a session (`d`) takes the terminals opened for it, the way deleting a group takes its subtree. The dialog names them before anything happens, because nothing else can speak for them: a shell rests at idle whatever it is running, so neither its row nor the dialog can tell one serving a port from one sitting at a prompt. `x` and `a` leave them alone — both come back, so there is nothing to clean up after. A shell that has since been `cd`'d elsewhere still goes: it was opened for that session, which is what the link records.

**The keys that write into a pane refuse a shell.** `space` and the review screen's `C` both paste their text and press Enter, so on a shell a sentence meant for an agent would run as a command. Both say the row is a shell and send nothing; enter the session (`↵`) to type there, where what you type is plainly a command. `f` says the same, since a shell has no conversation to fork.

A shell left on its empty command carries no session id, so `agent-manager rename` run inside one cannot find its session. Rename it from the list with `r`. Give the block a command and the pane gets an id like any other session.

## Opening the editor

`o` opens the row under the cursor in your editor: a session's live working directory (wherever its shell or agent has moved to, not only where it started), the directory it was created in when the live one cannot be read, or a group's default path. It works on a [terminal tab](#terminal-tabs) too — the shell you ran the build in is usually sitting in the directory you want open.

Agent Manager takes the first of these it finds: `editor` in [config.toml](configuration.md), `$AGENT_MANAGER_EDITOR`, a GUI editor on `PATH` (`code`, `cursor`, `windsurf`, `zed`, `subl`, `idea`), then `$VISUAL` or `$EDITOR`, and last a terminal editor on `PATH` (`nvim`, `vim`, `hx`, `emacs`, `nano`, `vi`). The environment comes after the GUI editors because it usually names the editor you set for git commit messages rather than the one a project should open in.

The line is run directly, never through a shell, so nothing in it is expanded and an `.envrc` that sets `EDITOR` cannot smuggle a command in behind it. Arguments are allowed, and quotes group one that carries a space: `editor = "code -n"`, `editor = "open -a 'Visual Studio Code'"`. Because there is no shell, a `$VISUAL` or `$EDITOR` that names a shell function or an alias is nothing this process can run, so it is passed over for the terminal editor on `PATH` instead of failing on it. A configured `editor` is always taken at its word: point it at a script if you want a wrapper.

Inside a session, attached or focused, `F3` opens that session's directory the same way. It costs an attach its client, so the manager steps back into the session once a windowed editor is running, or once one that draws in the terminal exits. An editor that fails to start keeps the manager on screen, where you can read why.

Like `ctrl+q` and `ctrl+r`, the manager keeps `F3` for itself inside a session, so a program running in there stops seeing it. Every `ctrl` combination reaches the program instead, `ctrl+o` included: Claude Code shows more lines with it, Gemini CLI toggles copy mode, and in a [terminal tab](#terminal-tabs) `nano` writes the file out.

A known windowed editor (the six above, plus `open` and `xdg-open`) starts detached and the manager stays on screen, with the status line naming what opened. Everything else takes the terminal over the way an attach does and hands it back on exit — that way round because a terminal editor started detached would have nowhere to draw, while a windowed one launched this way only costs a repaint.

## lazygit

`G` hands the terminal to [lazygit](https://github.com/jesseduffield/lazygit) on the repository under the cursor, the way an attach hands it over, and takes it back when lazygit quits. A session opens on its live working directory, so one spawned into a [worktree](#worktree-sessions) opens on its own branch rather than on the repository it branched from; a group row opens on its default path.

It is the other half of `ctrl+r`. The [review](#diff-review) reads what an agent changed and sends comments back to it; lazygit is where you stage, commit, switch branches, stash and read the log. Neither replaces the other, so they keep separate keys.

lazygit has to be on `PATH` — nothing is configurable here, and the status line says so when it is missing. A row whose directory is not inside a git repository says that instead of handing the screen to a program that would exit on its own error.

## Pull requests

A session with an open pull request wears its number: `#328` beside the name, on a chip tinted by that pull request's checks — green when they pass, amber while they run, red when one fails, and a neutral tint when it has none. A draft sits on the plain chip and is marked `✎`, and `⚠` marks one git cannot merge as it stands, and suffixed `+1` when there is more than one. `P` opens it in your browser. A session with several opens a picker, where `↵` opens the one under the cursor and `r` opens the repository instead.

`P` on a session with **no** pull request offers to open one: `↵` pushes the branch and creates it, titled from its commits, in the repository the branch was pushed to. `r` opens the repository page, which is what `P` used to do here on its own. Creating it this way is the only moment the link between a session and its pull request is a fact rather than a reading — everything below is working out after the event what this knows at it.

### Which pull request belongs to a session

Four sources, each adding what the ones before it missed:

1. **Created.** The manager opened the pull request itself. A fact, not a guess.
2. **Commit.** Any pull request containing the session's current commit. A commit either is in a pull request or is not, which is what a branch name cannot tell you: a name is a label two forks can both be using, and a rename detaches it from the pull request it belonged to.
3. **Branch.** Any open pull request whose head is the branch the session's checkout sits on. A label, and only as good as labels are.
4. **Printed.** An address the session put on screen, recorded so it survives scrolling away. An agent almost always prints the URL when it opens one, so this catches a pull request made outside the manager — including one on a branch nobody checked out. It comes last because a session asked to *look at* somebody else's pull request prints that address too, and a reading must never displace a commit that says otherwise. Only addresses on the session's own repository or its parent count at all.

A session that opened a pull request somewhere else has not stopped working on the branch it is sitting on, so it keeps both; the source that knows most leads, and is what `P` opens.

The created and printed links are written to the session and outlive the manager run, because what a session printed scrolls out of its pane long before the work it names is finished with. The titles and states are not stored — those are re-read every pass, so a badge is never a stale claim. A pull request that has been merged stops wearing a badge, since the badge is for work in flight, but `P` still opens it: it is still what that session produced.

On a wide enough pane the detail head adds what the pull request does to the tree — `+588 −99 in 14 files` — and the picker shows it beside each entry, which is what tells a one-line fix from a rewrite. The checks, the mergeability and the size all ride the same listing as the numbers, so none of them costs an extra request. The numbers come from [`gh`](https://cli.github.com), re-read once a minute. Each repository is listed once per pass no matter how many sessions or worktrees sit in it, and sessions sharing a checkout share the one commit lookup, so a dozen agents on one repo cost one pass and not a dozen. Without `gh` — or signed out of it, or on a host it does not know — no badges appear and `P` opens the repository page. Nothing is cached to disk except the link itself.

A checkout whose `origin` is a fork is read twice per pass — once naming that fork, once letting `gh` resolve the repository itself, which always answers with the parent. Both are needed, because a fork holds pull requests either way round: opened against the parent, a pull request lives upstream, and opened against the fork it lives on the fork. A checkout that is nobody's fork answers both the same way and the repeats are dropped.

## Ahead and behind

A session whose checkout has drifted from its remote branch says so beside its name: `❨↑2❩` for commits it has not pushed, `❨↓3❩` for commits the remote has that it does not. Behind is coloured to be noticed and ahead is not, because unpushed work is what a session in progress looks like, while a checkout that has fallen behind is usually a conflict nobody has run into yet.

A branch with no upstream shows nothing at all. "In step" and "nothing to compare against" are different answers, and a `↑0 ↓0` would report the second as the first.

The brackets are what tell this apart from the chips beside it: the tool, the branch and the pull request are all things that exist somewhere, where this is a comparison between two of them and belongs to neither.

This rides the same once-a-minute pass as the [pull request](#pull-requests) badge. Ahead comes out of git alone, so it works with no network and with no `gh` installed. Behind is only as fresh as the last fetch, so the pass fetches — once per repository rather than once per session, since several worktrees of one repository all have the same answer. The fetch moves remote-tracking refs and nothing else: no local branch, no index, nothing in the working tree, and `FETCH_HEAD` is deliberately left alone, since that is a file an agent may be reading for its own purposes.

## Agent rules

Every CLI reads an instruction file before its first turn — `CLAUDE.md` for Claude Code, `AGENTS.md` for Codex, OpenCode, Grok, Pi and Hermes, `GEMINI.md` for Gemini CLI — and which one it reads is a property of the tool, not of the directory. `i` on a session shows the files *that* session is actually governed by, so you can read what an agent was told before you ask why it did something.

```
▤ Rules · claude
❯ CLAUDE.md            project · 1.4KB · 3d ago
  CLAUDE.local.md      project · not created
  ~/.claude/CLAUDE.md  global · 6.2KB · 2h ago
```

- `↑↓` picks a file, `↵` reads it in place — scroll with `↑↓`/`jk`, `pgup`/`pgdn`, `g`/`G` — and `esc` goes back to the list rather than out of the surface, because reading one rules file and then its neighbour is the normal way round.
- `e` opens the file in [your editor](#opening-the-editor).
- A file that has not been written yet is listed too, marked `not created`: "this project tells the agent nothing" is an answer, and `↵` on that row creates it and opens it. It is created **empty** — anything Agent Manager put in it would become a rule the agent then obeys.

Project files are looked for in the session's live directory and every directory up to the repository root, nearest first, which is the chain the agents themselves read: a session started in `packages/api` shows that package's rules and the repository's. The walk stops at the repository, for the same reason [project settings](project-settings.md) discovery does. When nothing along the chain exists, the repository root is offered as where the file would go.

A group row, or a [terminal tab](#terminal-tabs), names no tool to resolve against, so it lists every instruction file that actually exists there for any configured CLI instead of offering to create one.

Which files each tool reads is [configurable](configuration.md#rules-files) per tool block, so a CLI that reads somewhere else is one line away from being listed correctly.

## Worktree sessions

A session can spawn into its own git worktree instead of the shared working directory: the `n` form has a `worktree` field between `dir` and `prompt` (`◂ on ▸` / `◂ off ▸`, toggled with `←→`), and the quick prompt's `alt+w` does the same for a group spawn. Settings (`s`) has a "worktree sessions" row that sets the default both start from.

The worktree lives at `<repo>-worktrees/<name>` next to the repo, on a new branch `am/<name>`. Its starting point is the remote's default branch (`origin/HEAD`) when that resolves, falling back to a local `main` or `master`, and finally to `HEAD`. A worktree that fails to create blocks the spawn with an error instead of falling back to a shared directory.

A directory that is not a git repo cannot hold a worktree, so the field and the quick prompt's footer read `unavailable (not a git repo)` in place of on/off, the toggle says why when pressed, and the session spawns in that directory as a plain session. This is what a group path sitting above several repos does: the umbrella itself is not a repo, so its sessions launch in it directly.

Renaming a session (`r`, or the agent's own `agent-manager rename`) carries its worktree along: the directory becomes `<repo>-worktrees/<new name>` and the branch becomes `am/<new name>`, so an agent that names itself after the work it picked up is reviewed and merged under that name. A destination directory or branch that already exists reports that and keeps the session where it is, so the name, the directory and the branch always agree. A worktree you have renamed or removed by hand is left alone and the session still takes the new name.

Deleting (`d`) a session that holds a worktree removes the worktree and its branch when it is clean: no uncommitted changes and no commits ahead of its base. A dirty worktree is left in place and its path shown, so nothing is lost. Note that "clean" is judged by `git status`, so gitignored files inside the worktree (a `.env`, local config, build output) are removed along with the directory. Killing, archiving, and reviving a session never touch its worktree.

## Killing and reviving sessions

`x` ends a session that is holding RAM you want back, and on a group row it ends every live session under it; `X` ends every live session in view. Each asks to confirm first, and what it ends is the tmux session, not the record: the row stays in the tree, marked `dead`, with its name, group, and conversation id intact.

![ending every session under a group for the RAM, then reviving the whole subtree on its own conversations](demo-revive.gif)

`v` relaunches a dead session under its old id, keeping its name, group, and history. When the manager holds that session's own conversation id, revive resumes **that exact conversation** through the tool's `resume_by_id_command`: `claude --resume {id}`, `codex resume {id}`, `opencode --session {id}`, `grok --resume {id}`, `gemini --resume {id}`, `pi --session {id}`, `hermes --cli --resume {id}`.

The id arrives one of two ways: tools with a `session_id_flag` launch under an id the manager mints, and tools that mint their own are read back by a `session_store` capturer (`codex`, `opencode`, `gemini`, `hermes`). Without an id, revive falls back to `revive_command` (`claude --continue`), which resumes the working directory's most recent conversation, and the manager says so in the status line, since sessions sharing a directory would otherwise land on the wrong one. On a group row `v` revives every dead session under it, and `V` revives every dead session in view; both revive what they can and name the first failure rather than stopping.

## Restarting a session on an empty context

`R` keeps the row and drops the context: same name, group, tool, and working directory, a managed worktree included, launched on a conversation the agent has never seen. It is what you want when a session has piled up context you are done with, where reviving it would spend the budget re-reading history or land straight in a compact.

It asks to confirm first, and it works on a live session too: the running agent ends, then the fresh one launches. The conversation it was on is retired rather than resumed: the manager mints a new id for tools that take one (`session_id_flag`) and captures the new one for tools that mint their own (`session_store`). The retired conversation is left on disk untouched, and the row stops pointing at it, so a later `v` resumes the conversation the restart started rather than the context it dropped. The row changes hands only once the new agent is up, so a launch that cannot start (a tool gone from `PATH`, a directory that moved) leaves the session on the conversation it had, still there for `v`.

## Forking sessions

1. Select a session and press `f`.
2. Enter a name.
3. Press `enter`.

The fork uses the source session's tool, group, working directory, and conversation history.

Claude Code and Codex support forks by default. A custom tool needs a `fork_command` in its configuration. The source session must have a conversation to fork: one that has never taken a turn is refused where you asked for it, rather than launching a pane that reports the missing conversation itself. A tool handed its id at spawn (`session_id_flag`) carries one from its first frame while the conversation behind it does not exist yet, so the check reads the tool's `session_store` to tell the two apart.

A fork shares its source session's managed worktree. Agent Manager keeps the worktree until you delete the last session that uses it. `c` is the same shape without the conversation — see [Several chats on one checkout](#several-chats-on-one-checkout).

## Several chats on one checkout

`c` is `f` without the context. A fork continues a conversation in a new session on the same checkout; a chat opens a new session on the same checkout with no conversation behind it. Both inherit the CLI, the directory, the group and the worktree of the session they were opened from, and neither cuts a second worktree — so nothing a first session does to a fresh checkout happens again: the project's setup script does not re-run, and auto-run scripts are not started a second time. The checkout is already dressed; what is missing is a conversation.

That is the whole of it. `c` works from a terminal tab too, which knows the checkout it was opened in.

Both record where they came from, in the same link a terminal has always recorded, so the rail draws them as one family: the first conversation on a checkout keeps its row, the ones opened beside it sit one level under it, and each carries the number you jump to it by. A session with no siblings is drawn exactly as before — no number, no block, nothing given up to a feature it is not using. Deleting one of them promotes the eldest of the rest into its place, so what is left of a family still hangs together.

| Where you are | Keys |
|-----|--------|
| The list | `1`…`9` jump to a chat by its number; `shift+←` / `shift+→` and `tab` / `shift+tab` step; `↑↓` walk them like any other rows |
| Focused in a pane | `shift+←` / `shift+→` step through them without giving up the pane, so switching costs one key rather than leaving focus and coming back. `alt+1`…`alt+9` jump straight to one, on terminals that send Option as Meta |

Above the pane, a checkout with more than one chat draws a strip naming them all with their states and the keys that reach them.

The binding is narrower than it sounds. It cannot use Meta: on macOS, Option is a compose key by default, so `alt+.` arrives as `≥` with no modifier on it and `alt+1` as `¡` — a terminal that has not been told otherwise never sends Meta at all. It cannot be a bare character, because in a pane every character belongs to the agent. And it cannot be `tab` or `shift+tab`, which are the agent's own — completion, and Claude Code's permission-mode cycling. Shift+arrow is left: a CSI sequence (`ESC [ 1;2C`) that carries no Meta, that every terminal sends, and that no agent CLI wants.

Renaming a session whose checkout has anything else running in it renames the row and leaves the directory and branch where they are, saying so once. A worktree is named for the session it was created for, and a chat, a fork or a terminal opened beside that one is neither what it is named after nor something to move out from under.

## Self-naming sessions

Sessions spawned without a custom name (every quick spawn, and the form with the name left blank) get a placeholder like `claude-a1b2`, and their first prompt opens by asking the agent to run `agent-manager rename "<name>"` once with a short name for the broad feature of the session (not a single subtask). The directive also tells the agent not to rename again unless you ask. When the first prompt cannot carry the directive (a `/slash` command, or no prompt at all), the manager sends it as its own message once the tool's input box appears in the pane. The subcommand drops the name into a per-session file; the manager picks it up on the next poll and updates the sidebar row and the tmux status bar. This works with any tool, since it only needs the agent to read its prompt and run one shell command.

Sessions you name yourself keep that name: the first prompt only notes that `agent-manager rename` is available later if you ask, and does not instruct the agent to rename now. You can still ask an agent to rename its session later, or run `agent-manager rename` yourself from a shell inside the session.

## Declaring the repo under review

A session's working directory is often an umbrella folder holding many repos, so review can only guess which one the agent means. An agent that knows which repo it is working in can say so by running `agent-manager review-repo <path>` from a shell inside its session. The subcommand checks that the path is (or sits inside) a git repo, resolves it to the repo root, and drops it into a per-session file; the manager picks it up on the next poll and review opens on that repo the next time you open it. A path that is not inside a git repo is rejected, so a declaration is always a fact rather than a guess.

An agent can also declare what its branch diffs against by running `agent-manager review-base <ref>` from inside its worktree: the ref is validated in that repo, stored per session and repo, and the "vs target" scope uses it from then on. `agent-manager review-base --clear` returns to automatic detection. A stored ref that stops resolving surfaces as an error in review, and `B` opens a target picker (the repo's branches plus an `auto` entry) to set or clear it by hand.

Agents usually work in git worktrees, one branch per worktree, and those worktrees can live anywhere on disk. A declared path that is a worktree root is accepted wherever it lives, so one `review-repo` call names both the repo and the branch under review. Review resolves its target in a fixed order: a repo you picked by hand with `r` or `b` wins for as long as the manager is running, then the agent's declared repo, then the ranking (dirty working trees first, then most recent commit). When the picked or declared path stops being a git repo, review says so in the status line and `r` is there to pick the right one.

## MCP: how agents discover these commands

Every session of an MCP-capable tool carries the agent-manager MCP server on spawn and revive, so its agent sees the session and terminal operations as native tools with descriptions telling it when to call each: no prompt injection, no per-project setup. Pi is the one tool without an MCP client, so its sessions rely on the subcommands alone. The server lives in the same binary (`agent-manager mcp`, stdio) and identifies the calling session through its environment.

| Tool | Action |
|------|--------|
| `rename` | Rename the calling session |
| `review_repo` | Declare the repo or worktree under review |
| `review_base` | Declare or clear the review base ref |
| `review_mode` | Select the diff scope review opens with |
| `create_session` | Create another agent session, with a first task and optionally its own worktree |
| `list_terminals` | List active managed terminals and their current directories |
| `create_terminal` | Open a terminal under the calling session, or beside it when that session is itself a terminal, unless `nest` is false |
| `send_terminal` | Submit a command or send exact keys to a running terminal |
| `read_terminal` | Read the plain-text content currently visible in a terminal |
| `close_terminal` | Close a finished terminal nested under the caller: kill the pane and delete the row |

`create_terminal` nests under the calling session unless `nest` is false, and a call from a terminal opens the new shell beside it: under the same agent, or un-nested in the same group when that terminal is itself un-nested. It defaults to the calling agent's group and live pane directory. A group other than the caller's needs `nest: false`, since a nested terminal lives in its parent's group; that group then supplies its nearest inherited default path, and an explicit directory wins over both. `close_terminal` kills the pane and removes the row once the job is finished, and it reaches only the terminals nested under the calling session: a shell someone else opened, or one deliberately left un-nested, is the user's to close. `send_terminal` accepts exactly one of a command, which is pasted and submitted with Enter, or a sequence of tmux key names such as `C-c`, `Up`, and `Enter`. `read_terminal` returns the current screen rather than unlimited scrollback.

The server's MCP initialization instructions teach agents to open a terminal for human-visible work such as SSH, when the user should be able to watch it, attach, or take over. They list and reuse a relevant running terminal first; `create_terminal` nests under the caller unless `nest` is false; they send the command and read its screen while the job runs; and they call `close_terminal` when that job ends, unless the terminal is being left for the user. One-shot local commands stay in the agent's normal tools. The same guidance is repeated in the individual tool descriptions for clients that expose tools but not server instructions.

`create_session` starts another agent beside the calling one. It needs a first task; everything else defaults the way `create_terminal` does — the caller's group and current directory, and the CLI chosen in Settings. Called from inside a worktree the manager created, the new session joins that worktree the way a fork does — same tree, same branch, counted by the same last-one-out cleanup — differing from a fork only in starting with a fresh conversation. `worktree` instead gives it its own git worktree, branch and `$AGENT_MANAGER_PORT` block, so two agents can work the same repository without touching each other's tree; elsewhere the target group's spawn-in-worktree setting decides, and an explicit request in a directory that is not a repository is refused rather than quietly downgraded. Without a `name` the new session is asked to name itself from its work, exactly as a quick-prompt spawn is. A session created this way records the agent that asked for it.

The new session starts on its prompt immediately and works on its own, so the prompt has to carry everything it needs to begin — the tool description and the server instructions both say so, since a client may expose one, the other, or both. Nothing bounds how many sessions get created or how deep the chain goes: a session created this way carries the same tools, so it can create sessions too. Because the manager only sees the new row on its next poll, a session created this way shows its generated name for a moment before the name its agent picks arrives. For a tool with `prompt_mode = "send"` the first task is typed into the pane by the running manager rather than carried on the command line, so it reaches the new session once the manager is open.

Sending a command to a terminal executes it on the user's machine. Agents should treat `send_terminal` with the same care as typing into an attached shell: inspect the target returned by `list_terminals`, avoid destructive commands unless the user asked for them, and read the result before continuing.

Registration is per tool. Claude gets a generated `--mcp-config` file. Codex gets `-c mcp_servers...` overrides. OpenCode gets an `OPENCODE_CONFIG` merge file. Grok and Gemini each get a one-time `mcp add --scope user` entry on their first launch. Hermes gets its own one-time `mcp add` flow, which needs the MCP SDK its installer treats as optional: a Hermes still missing it refuses the spawn with a dialog pointing at `hermes setup`, so a Hermes session always carries these tools.

Pi does not include an MCP client. The `rename`, `review-repo`, and `review-base` commands still work inside Pi sessions.

A custom tool opts in with `mcp = "<style>"` in its config section. Set `mcp = "none"` to disable registration.

## Diff review

Press `ctrl+r` on a session to open a full-screen review of its repo: changed files with +/− counts on the left, the whole file on the right with syntax highlighting and changed lines tinted, so every edit reads in full context. The diff refreshes as the agent keeps editing.

| Key | Action |
|-----|--------|
| `↑↓` / `jk`, `ctrl+d` / `ctrl+u` | Scroll the file |
| `g` / `G` | Jump to top / bottom |
| `J` / `K` (or `tab` / `shift+tab`) | Previous / next file |
| `n` / `N` | Jump between changes |
| `u` | Toggle unified and side-by-side |
| `s` | Cycle the scope: uncommitted, vs target, last commit, staged |
| `r` | Pick the repo when the session's directory holds several (type to filter) |
| `b` | Pick the branch from the repo's worktrees |
| `B` | Pick the target (merge-into branch) the "vs target" scope compares against |
| `space` | Mark a file reviewed |
| `f` | Show code files only, hiding images, compiled assets and lock files from the list; press again to show them |
| `c` / `d` | Write / drop a line comment |
| `C` | Send every comment to the agent as one review prompt (`enter` or `y` confirms) |
| `esc` / `q` | Close the review |

Each changeable value in the header wears its own key, so the scope, layout, repo, and target pills read as `s`, `u`, `r`, `B` legends at a glance.

![review, side by side, with the changed lines tinted in full file context](screenshot-review.png)

Comments stay on the review screen until you send them: `C` flattens every one of them into a single prompt, asks you to confirm, and delivers it into the agent's pane, so the agent starts addressing your notes while you watch the diff update.

![review mode: scrolling a changed file, switching to unified, jumping to the next file, then a line comment sent back to the agent](demo-diff.gif)

## Groups

![folding the tree, creating a nested group, reordering, and archiving one](demo-groups.gif)

Groups are paths (`backend/api/auth`) forming a tree of unlimited depth. Sessions can live at any node, including the root. Create subgroups inline with `g`, reorder both groups and sessions with `K` / `J` (or `shift+↑↓`; the order persists), fold a subtree with `enter` on its row, fold or unfold the whole tree with `F`, hide or restore empty groups visually with `e`, and edit a group's name and default path with `r`. On a session, `r` renames it and `tab` cycles the tool (status rules and revive follow the new tool; useful when you quit one agent in the pane and start another).

## Status

Each session's tmux pane is polled (default every 2s) to derive a status:

| Status | Meaning |
|--------|---------|
| `working` | The agent is busy on a turn |
| `waiting` | Blocked on you: a dialog, a permission ask, or a plain-text question |
| `finished` | Turn ended — an alert that clears to `idle` once you enter the session |
| `errored` | The tool reported an error |
| `idle` | Nothing running |
| `dead` | The tmux session is gone |

`w` narrows the list to sessions that need attention (`waiting`, `finished`, `errored`). Press again to show every status. An `ATTENTION` badge sits over the list with the key that clears it, and the session counts follow the filter; folds open so matches are not hidden. The archived view (`t`) and hidden empty groups (`e`) label themselves the same way. The badges take whatever room the rail has: padded away from the entries on a tall terminal, tight against them on a short one, and yielding to the entries once the list is down to its last rows.

![the session tree, with a waiting agent's permission prompt in the preview](screenshot-sessions.png)

Each row carries its status and tool inline, and a folded group keeps a count per status so a collapsed subtree still tells you whether anything needs you. Selecting a session shows the tail of its pane on the right, which is how a `waiting` agent's actual question reaches you without attaching. A session with no window left, archived or killed, shows the snapshot taken when it still had one.

Detection matches per-tool regex rules against the visible pane, analyzes the newest turn to tell `finished` from `waiting`, and treats streaming output (content changing between polls) as `working`. A turn that ends without any turn-summary line still resolves: when a `working` pane goes quiet, the turn counts as `finished`, or `waiting` when it ends on a question. Work that outlives the turn which started it (background agents and shells) is matched by `busy_line`, so a turn-end summary keeps reading as `working` while that work runs. A usage or rate-limit banner (`limit_line`) is `errored`. Polling keeps running while you are inside a session, so statuses stay live. The selected session's pane tail renders in the preview panel, and moving the cursor fetches the preview immediately.

For Claude Code, status comes first-hand from [hook events](https://docs.anthropic.com/en/docs/claude-code/hooks) instead of pane guessing: sessions launch with a generated `--settings` file whose hooks write the lifecycle state (`working`, `waiting`, `finished`, `idle`) to a per-session status file that the poller reads first. A `StopFailure` of `rate_limit` writes `errored`. Pane rules still refine it — hooks cannot see a plain-text question, an Esc interrupt, or an error line, so a matching pane verdict upgrades the hook status — and they take over fully as fallback when the hook file is missing or stale. Enabled per tool with `status_source = "claude-hooks"`.

## Notifications

When a session's status flips to `waiting` or `errored`, the manager fires one notification with the session name, tool, and state — once per transition, never per poll — so you can look away from the list without missing an agent that needs you. This is tool-neutral: every configured CLI reaches the same notification path after the manager classifies its status. The macOS desktop path gives waiting, finished, and errored the Funk, Hero, and Basso sounds respectively. Linux sends matching standard sound, icon, category, and urgency hints through `notify-send`; the desktop notification server uses the capabilities it supports and safely ignores the rest. Inside Ghostty or cmux the state travels as an OSC 777 escape to the drawing terminal, which owns its presentation and sound and attributes it to the right window and workspace. Because that escape rides the terminal connection, it also reaches you when the manager runs on a remote host over SSH. Other terminals use the OS desktop path, while a headless box or WSL installation without one falls back to the terminal bell. Settings (`s`) has a `notifications` row that silences them (on by default) and a `notify on finish` row that adds `finished` transitions (off by default).

## Stats

The header shows a fleet summary: per-status session counts, plus `agents total usage: cpu N% · ram M% · X GB` for every live agent's full process tree (shell, agent, and children). CPU is that tree's CPU time over the last poll as a share of total machine capacity (same 0–100% unit as the computer gauge). RAM is resident set as a share of installed memory, with absolute size beside it. The selected session's detail line uses the same scale for that session alone.

The Computer block in the sessions panel shows machine gauges:

- **CPU**: whole-machine utilization (0-100%)
- **Memory**: used/total. On macOS this matches Activity Monitor's Memory Used (resident RAM minus free, speculative, and reclaimable file cache). On Linux it is `Total - MemAvailable`, so file cache is not counted as used.
- **Swap**: used/total of the current swap allocation (`used/total * 100`). On macOS the swap file grows under pressure, so the denominator is the live size from `vm.swapusage`, not a fixed partition.
- **Disk**: fill of the root filesystem (used / (used + available)), with free space from the kernel's available figure
- **Network**: up/down rates on real NICs only (loopback, utun, bridges, and similar virtual interfaces are excluded)
- **Temperature**: `cpu`, `gpu` and `soc` readings in °C, each the hottest sensor in its category, sampled every 5s. Apple Silicon draws no CPU/GPU line, so its dies report as one `soc` figure. A reading appears when the machine exposes that sensor.

## Themes

`s` opens Settings, where `↑↓` move between fields and `←→` change the focused one.

![settings, with the theme picker and its palette swatches](screenshot-settings.png)

Fifteen palettes ship. Nine dark: `classic`, `solarized dark`, `catppuccin mocha`, `tokyo night`, `gruvbox dark`, `nord`, `dracula`, `rosé pine`, and `monochrome`. Six light: `solarized light`, `catppuccin latte`, `tokyo night day`, `gruvbox light`, `rosé pine dawn`, and `paper`. The swatch strip beside the name previews the palette, and the theme applies as you step through it, so the picker is a live preview of the whole UI. The manager also matches the terminal's own background to the palette, so the window has no seam against it, and restores the terminal's background on exit. Your pick is saved with the rest of the state and restored on the next run.

**theme follows OS** resolves the palette at startup from the environment's light/dark preference: the OS setting on macOS and Linux desktops, and the terminal's own background elsewhere, including over SSH. A theme already on the detected side stays; only a mismatch switches, to `classic` or `solarized light`. Your manual pick is kept separately, so turning the toggle off returns to it, and stepping the theme picker by hand turns the toggle off. Agent panes render on the theme's own backdrop, and the pane declares that background to the agent inside it, so an agent that auto-detects its palette resolves to the same side the manager is drawing. A session already running keeps the palette it resolved at launch until it is restarted.
