package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const usage = `Usage:
  trackerator add TITLE
  trackerator subtask add PARENT_ID TITLE
  trackerator start ID
  trackerator block ID
  trackerator complete ID
  trackerator done ID
  trackerator schedule ID [--start DATE|none] [--complete DATE|none]
  trackerator list [-a]
  trackerator show ID
  trackerator search QUERY
  trackerator serve [-port PORT]
  trackerator help [COMMAND]

Titles and search queries with spaces should be quoted.
Run "trackerator help COMMAND" for command details.`

var commandHelp = map[string]string{
	"add": `Usage: trackerator add TITLE

Create a top-level task and print its assigned numeric ID.

Example: trackerator add "Plan release"`,
	"subtask": `Usage: trackerator subtask add PARENT_ID TITLE

Create a task under an existing task. PARENT_ID is the numeric ID of the
parent task; subtasks can have subtasks of their own.

Example: trackerator subtask add 1 "Review changes"`,
	"start": `Usage: trackerator start ID

Set a task or subtask's status to started.

Example: trackerator start 1`,
	"block": `Usage: trackerator block ID

Set a task or subtask's status to blocked.

Example: trackerator block 1`,
	"complete": `Usage: trackerator complete ID

Set a task or subtask's status to done. Done tasks are hidden from list
unless you use list -a. "done" is an alias for "complete".

Example: trackerator complete 1`,
	"done": `Usage: trackerator done ID

Alias for "complete". Set a task or subtask's status to done.

Example: trackerator done 1`,
	"list": `Usage: trackerator list [-a]

List all tasks in ID order, including subtasks. Each subtask shows its
parent task's ID. Done tasks are hidden by default; -a includes them.
Tasks due to start or complete today or earlier are called out first.`,
	"schedule": `Usage: trackerator schedule ID [--start DATE|none] [--complete DATE|none]

Set scheduled start and/or completion dates for a task or subtask. Dates use
YYYY-MM-DD in your local timezone. Use none to clear a date; omitted dates
stay unchanged. Provide at least one option. The completion date cannot be
before the start date.

Example: trackerator schedule 1 --start 2026-10-01 --complete 2026-10-05`,
	"show": `Usage: trackerator show ID

Show a task's title, status, scheduled dates, creation time, parent (if any),
and immediate subtasks, including completed subtasks.

Example: trackerator show 1`,
	"search": `Usage: trackerator search QUERY

Find tasks whose titles contain QUERY, ignoring letter case. Results include
subtasks and show their parent task IDs.

Example: trackerator search "review"`,
	"serve": `Usage: trackerator serve [-port PORT]

Open the local web interface at http://127.0.0.1:8080. Use -port to choose
another local port. The web interface uses the same task database as the CLI.

Example: trackerator serve -port 8081`,
	"help": `Usage: trackerator help [COMMAND]

Show the command overview, or detailed help for add, subtask, list, show,
search, start, block, complete, done, schedule, serve, or help.

Example: trackerator help add`,
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.UserHomeDir); err != nil {
		fmt.Fprintln(os.Stderr, "trackerator:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer, userHome func() (string, error)) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	if args[0] == "help" {
		return printHelp(out, args[1:])
	}
	if args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(out, usage)
		return nil
	}

	var command, title string
	var id int64
	var includeDone bool
	var scheduleStart, scheduleCompletion *string
	port := 8080
	switch args[0] {
	case "add", "search":
		if len(args) < 2 {
			return errors.New(usage)
		}
		command = args[0]
		title = strings.TrimSpace(strings.Join(args[1:], " "))
		if title == "" {
			return errors.New("title or query cannot be empty")
		}
	case "subtask":
		if len(args) < 4 || args[1] != "add" {
			return errors.New(usage)
		}
		command = "subtask"
		var err error
		id, err = parseID(args[2])
		if err != nil {
			return err
		}
		title = strings.TrimSpace(strings.Join(args[3:], " "))
		if title == "" {
			return errors.New("title cannot be empty")
		}
	case "list":
		if len(args) == 2 && args[1] == "-a" {
			includeDone = true
		} else if len(args) != 1 {
			return errors.New(usage)
		}
		command = "list"
	case "schedule":
		if len(args) < 4 || len(args)%2 != 0 {
			return errors.New(usage)
		}
		command = "schedule"
		var err error
		id, err = parseID(args[1])
		if err != nil {
			return err
		}
		for i := 2; i < len(args); i += 2 {
			value := args[i+1]
			if value == "none" {
				value = ""
			}
			if _, err := parseScheduleDate(value); err != nil {
				return fmt.Errorf("%s: %w", args[i], err)
			}
			switch args[i] {
			case "--start":
				if scheduleStart != nil {
					return errors.New("--start specified more than once")
				}
				scheduleStart = &value
			case "--complete":
				if scheduleCompletion != nil {
					return errors.New("--complete specified more than once")
				}
				scheduleCompletion = &value
			default:
				return fmt.Errorf("unknown schedule option %q", args[i])
			}
		}
	case "show", "start", "block", "complete", "done":
		if len(args) != 2 {
			return errors.New(usage)
		}
		command = args[0]
		if command == "done" {
			command = "complete"
		}
		var err error
		id, err = parseID(args[1])
		if err != nil {
			return err
		}
	case "serve":
		if len(args) == 3 && args[1] == "-port" {
			var err error
			port, err = strconv.Atoi(args[2])
			if err != nil || port < 1 || port > 65535 {
				return fmt.Errorf("invalid port %q: expected 1 through 65535", args[2])
			}
		} else if len(args) != 1 {
			return errors.New(usage)
		}
		command = "serve"
	default:
		return errors.New(usage)
	}

	home, err := userHome()
	if err != nil {
		return fmt.Errorf("find home directory: %w", err)
	}
	s, err := openStore(home)
	if err != nil {
		return err
	}
	defer s.close()

	switch command {
	case "serve":
		return serveWeb(s, port, out)
	case "add":
		newID, err := s.add(title, nil)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Created task %d: %s\n", newID, title)
	case "subtask":
		newID, err := s.add(title, &id)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Created subtask %d under task %d: %s\n", newID, id, title)
	case "schedule":
		if err := s.setSchedule(id, scheduleStart, scheduleCompletion); err != nil {
			return err
		}
		fmt.Fprintf(out, "Updated schedule for task %d.\n", id)
	case "list":
		tasks, err := s.list(nil, nil, includeDone)
		if err != nil {
			return err
		}
		printTasks(out, tasks)
	case "search":
		tasks, err := s.list(nil, &title, true)
		if err != nil {
			return err
		}
		printTasks(out, tasks)
	case "show":
		t, err := s.get(id)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Task %d: %s\n", t.ID, t.Title)
		fmt.Fprintf(out, "Status: %s\n", t.Status)
		if t.StartDate.Valid {
			fmt.Fprintf(out, "Scheduled start: %s\n", t.StartDate.String)
		}
		if t.CompletionDate.Valid {
			fmt.Fprintf(out, "Scheduled completion: %s\n", t.CompletionDate.String)
		}
		if t.ParentID.Valid {
			fmt.Fprintf(out, "Parent: %d\n", t.ParentID.Int64)
		}
		fmt.Fprintf(out, "Created: %s UTC\n", t.Created)
		children, err := s.list(&id, nil, true)
		if err != nil {
			return err
		}
		if len(children) > 0 {
			fmt.Fprintln(out, "Subtasks:")
			for _, child := range children {
				fmt.Fprintf(out, "  %d  [%s]  %s\n", child.ID, child.Status, child.Title)
			}
		}
	case "start", "block", "complete":
		status := map[string]string{"start": "started", "block": "blocked", "complete": "done"}[command]
		if err := s.setStatus(id, status); err != nil {
			return err
		}
		fmt.Fprintf(out, "Task %d is now %s.\n", id, status)
	}
	return nil
}

func printHelp(out io.Writer, topics []string) error {
	if len(topics) == 0 {
		fmt.Fprintln(out, usage)
		return nil
	}
	topic := strings.Join(topics, " ")
	if topic == "subtask add" {
		topic = "subtask"
	}
	help, ok := commandHelp[topic]
	if !ok {
		return fmt.Errorf("unknown help topic %q; run 'trackerator help' for available commands", strings.Join(topics, " "))
	}
	fmt.Fprintln(out, help)
	return nil
}

func parseID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid task ID %q: expected a positive integer", value)
	}
	return id, nil
}

func printTasks(out io.Writer, tasks []task) {
	printTasksAt(out, tasks, localToday())
}

func printTasksAt(out io.Writer, tasks []task, today string) {
	if len(tasks) == 0 {
		fmt.Fprintln(out, "No tasks found.")
		return
	}
	var dueLines []string
	for _, t := range tasks {
		if t.startDue(today) {
			dueLines = append(dueLines, fmt.Sprintf("  Start due: #%d %s (%s)", t.ID, t.Title, t.StartDate.String))
		}
		if t.completionDue(today) {
			dueLines = append(dueLines, fmt.Sprintf("  Completion due: #%d %s (%s)", t.ID, t.Title, t.CompletionDate.String))
		}
	}
	if len(dueLines) > 0 {
		fmt.Fprintln(out, "Due now:")
		for _, line := range dueLines {
			fmt.Fprintln(out, line)
		}
		fmt.Fprintln(out)
	}
	for _, t := range tasks {
		var details []string
		if t.StartDate.Valid {
			label := "start"
			if t.startDue(today) {
				label = "START DUE"
			}
			details = append(details, fmt.Sprintf("[%s: %s]", label, t.StartDate.String))
		}
		if t.CompletionDate.Valid {
			label := "complete"
			if t.completionDue(today) {
				label = "COMPLETE DUE"
			}
			details = append(details, fmt.Sprintf("[%s: %s]", label, t.CompletionDate.String))
		}
		suffix := ""
		if len(details) > 0 {
			suffix = "  " + strings.Join(details, " ")
		}
		if t.ParentID.Valid {
			fmt.Fprintf(out, "%d  [%s]  (under %d)  %s%s\n", t.ID, t.Status, t.ParentID.Int64, t.Title, suffix)
		} else {
			fmt.Fprintf(out, "%d  [%s]  %s%s\n", t.ID, t.Status, t.Title, suffix)
		}
	}
}
