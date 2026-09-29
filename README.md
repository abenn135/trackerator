# trackerator

This is a tool for tracking workstreams. A workstream could come from any source and be any size or priority; this tool is intended to help track artifacts, sub-tasks, blocked and unblocked work, etc., and provide helpful reminders for long-running work that may fall off radar.

Over time, the tool will incorporate agent tooling, for example to automatically check the status of code reviews, Jira tickets, deployments, etc.

## CLI

Build with `go build -o trackerator .`, or run commands with `go run .`.

```text
trackerator add "Plan release"
trackerator subtask add 1 "Review changes"
trackerator start 1
trackerator block 2
trackerator complete 2
trackerator done 1
trackerator list
trackerator list -a
trackerator show 1
trackerator search "review"
trackerator help add
```

Tasks receive numeric IDs automatically and start with status `todo`. Use
`start`, `block`, or `complete` with a task ID to set its status to `started`,
`blocked`, or `done`. `done` is an alias for `complete`. Each task and subtask
has its own status. `list` shows
unfinished tasks and their parent IDs; `list -a` includes completed tasks.
`show` displays one task and its immediate subtasks, including completed ones.
Search matches text in task titles without regard to letter case, including
completed tasks. Data is stored in
`~/.trackerator/trackerator.db` and created on the first task command.
Run `trackerator help` for the command overview, or `trackerator help COMMAND`
for details about a command (for example, `trackerator help subtask`).
