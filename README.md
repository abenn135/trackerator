# trackerator

This is a tool for tracking workstreams. A workstream could come from any source and be any size or priority; this tool is intended to help track artifacts, sub-tasks, blocked and unblocked work, etc., and provide helpful reminders for long-running work that may fall off radar.

Over time, the tool will incorporate agent tooling, for example to automatically check the status of code reviews, Jira tickets, deployments, etc.

## CLI

Build with `make` to create `bin/trackerator`, or run commands with `go run .`.
Run `make test` to check the CLI.

To use the local web interface, run `bin/trackerator serve` and open
`http://127.0.0.1:8080` in your browser. Use `serve -port 8081` to choose
another port. The web interface shares the CLI's SQLite database, so tasks
created in either place appear in both. It can create tasks and subtasks,
change their status, search titles, and show completed tasks.
Subtasks appear nested under their parent tasks, including in search results.
Each task can also have optional scheduled start and completion dates. The UI
highlights tasks ready to start and tasks due for completion at the top of the
page when their dates are today or earlier.

```text
trackerator add "Plan release"
trackerator subtask add 1 "Review changes"
trackerator start 1
trackerator block 2
trackerator complete 2
trackerator done 1
trackerator schedule 1 --start 2026-10-01 --complete 2026-10-05
trackerator schedule 1 --start none
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
dates use `YYYY-MM-DD`, and `none` clears a date. `list` calls out tasks still
in `todo` whose start date is today or earlier, and tasks not `done` whose
completion date is today or earlier. Dates are compared in the machine's local
timezone. Data is stored in `~/.trackerator/trackerator.db` and created on the
first task command.
Run `trackerator help` for the command overview, or `trackerator help COMMAND`
for details about a command (for example, `trackerator help subtask`).
