# trackerator

This is a tool for tracking workstreams. A workstream could come from any source and be any size or priority; this tool is intended to help track artifacts, sub-tasks, blocked and unblocked work, etc., and provide helpful reminders for long-running work that may fall off radar.

Over time, the tool will incorporate agent tooling, for example to automatically check the status of code reviews, Jira tickets, deployments, etc.

## CLI

Build with `make` to create `bin/trackerator`, or run commands with `go run .`.
Run `make test` to check the CLI. Run `make serve` to start Agent Deck's
local web UI (if it is not already running) and serve Trackerator at
`http://127.0.0.1:8080`. Ctrl-C stops Trackerator and also stops Agent Deck
if this invocation started it. If Agent Deck is installed outside your PATH
and `~/.local/bin`, pass `AGENT_DECK_BIN=/path/to/agent-deck` to `make serve`.
Use `make serve PORT=8081` if port 8080 is already in use.

To use the local web interface, run `bin/trackerator serve` and open
`http://127.0.0.1:8080` in your browser. Use `serve -port 8081` to choose
another port. The web interface shares the CLI's SQLite database, so tasks
created in either place appear in both. It can create tasks and subtasks,
change their status, search titles, show completed tasks, and attach multiple
URLs to each task or subtask. Use Add URL to attach a link, or Remove beside a
link to detach it.
Web task lists prioritize started tasks, then todo, blocked, and completed
tasks, while keeping subtasks nested beneath their parents.
Click a task number for a stable `/tasks/ID` link. That page shows the task
and its full subtask tree; a subtask's page shows its parent chain without
unrelated peers, with links to parent pages when other subtasks are hidden.
Use the Subtasks section to expand a task's children; the Add subtask and
Schedule dates buttons reveal their forms separately. Expanded Subtasks sections
are reflected in the URL, so reloading or bookmarking the page preserves them.
Creation timestamps are displayed in the browser's local timezone (with UTC
shown as a fallback when JavaScript is unavailable).
Each task can also have optional scheduled start and completion dates. The UI
highlights tasks ready to start and tasks due for completion at the top of the
page when their dates are today or earlier.

To work on a task in [Agent Deck](https://github.com/asheshgoplani/agent-deck),
install its CLI and use `make serve`, or start `agent-deck web` separately
(default: `http://127.0.0.1:8420/`).
On a task, click **Agent Deck**, choose Shell, Codex, or Claude Code, and
optionally enter an HTTPS or SSH Git repository URL. Trackerator creates a
workspace at `~/.trackerator/workspaces/task-ID/`; a supplied repository is
cloned into its `repo/` directory. Without a URL, the shell or agent opens in
the task workspace so you can clone repositories there yourself. Trackerator
launches one named Agent Deck session per task and remembers the association;
subsequent clicks open Agent Deck without creating another session. Agent Deck
currently opens at its session list rather than a specific session, so select
the session named `Trackerator #ID: TITLE`. Keep `agent-deck web` running to
use the browser handoff. Trackerator does not manage Agent Deck's web process
or remove task workspaces when tasks are completed.

```text
trackerator add "Plan release"
trackerator subtask add 1 "Review changes"
trackerator start 1
trackerator block 2
trackerator complete 2
trackerator done 1
trackerator schedule 1 --start 2026-10-01 --complete 2026-10-05
trackerator schedule 1 --start none
trackerator url add 1 https://example.com/review/42
trackerator url add 1 https://example.com/notes
trackerator url list 1
trackerator url remove 1 https://example.com/notes
trackerator list
trackerator list -a
trackerator show 1
trackerator search "review"
trackerator help add
```

Tasks receive numeric IDs automatically and start with status `todo`. Use
`start`, `block`, or `complete` with a task ID to set its status to `started`,
`blocked`, or `done`. `done` is an alias for `complete`. Each task and subtask
has its own status. `list` shows unfinished tasks and their parent IDs;
`list -a` includes completed tasks.
`show` displays one task and its immediate subtasks, including completed ones.
Search matches text in task titles without regard to letter case, including
completed tasks. `schedule` sets or clears either date on a task or subtask;
dates use `YYYY-MM-DD`, and `none` clears a date. `url add`, `url list`, and
`url remove` manage HTTP(S) links for a task; `show` also displays its links.
`list` calls out tasks still in `todo` whose start date is today or earlier,
and tasks not `done` whose completion date is today or earlier. Dates are
compared in the machine's local
timezone. Data is stored in `~/.trackerator/trackerator.db` and created on the
first task command.
Run `trackerator help` for the command overview, or `trackerator help COMMAND`
for details about a command (for example, `trackerator help subtask`).
