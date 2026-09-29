package main

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIStoresAndQueriesTasks(t *testing.T) {
	home := t.TempDir()
	runCommand := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		if err := run(args, &out, func() (string, error) { return home, nil }); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	if got := runCommand("add", "Plan release"); got != "Created task 1: Plan release\n" {
		t.Fatalf("add output = %q", got)
	}
	if got := runCommand("subtask", "add", "1", "Review changes"); got != "Created subtask 2 under task 1: Review changes\n" {
		t.Fatalf("subtask output = %q", got)
	}
	if got := runCommand("list"); got != "1  [todo]  Plan release\n2  [todo]  (under 1)  Review changes\n" {
		t.Fatalf("list output = %q", got)
	}
	if got := runCommand("search", "REVIEW"); got != "2  [todo]  (under 1)  Review changes\n" {
		t.Fatalf("search output = %q", got)
	}
	if got := runCommand("show", "1"); !strings.Contains(got, "Status: todo\n") || !strings.Contains(got, "Subtasks:\n  2  [todo]  Review changes\n") {
		t.Fatalf("show output = %q", got)
	}
	if got := runCommand("show", "2"); !strings.Contains(got, "Parent: 1\n") {
		t.Fatalf("subtask output = %q", got)
	}

	path := filepath.Join(home, ".trackerator", "trackerator.db")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("database at %s: %v", path, err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("database permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestHelpTopics(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"help"}, "trackerator help [COMMAND]"},
		{[]string{"help", "add"}, "Create a top-level task"},
		{[]string{"help", "subtask"}, "PARENT_ID is the numeric ID"},
		{[]string{"help", "subtask", "add"}, "Usage: trackerator subtask add PARENT_ID TITLE"},
		{[]string{"help", "start"}, "status to started"},
		{[]string{"help", "block"}, "status to blocked"},
		{[]string{"help", "complete"}, "status to done"},
		{[]string{"help", "done"}, "Alias for \"complete\""},
		{[]string{"help", "list"}, "List all tasks in ID order"},
		{[]string{"help", "show"}, "including completed subtasks"},
		{[]string{"help", "search"}, "ignoring letter case"},
		{[]string{"help", "help"}, "Show the command overview"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			var out bytes.Buffer
			err := run(tt.args, &out, func() (string, error) {
				return "", errors.New("help should not access the home directory")
			})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tt.want) {
				t.Fatalf("help output %q does not contain %q", out.String(), tt.want)
			}
		})
	}

	var out bytes.Buffer
	err := run([]string{"help", "missing"}, &out, func() (string, error) { return "", nil })
	if err == nil || !strings.Contains(err.Error(), "unknown help topic") {
		t.Fatalf("unknown topic error = %v", err)
	}
}

func TestStatusesAndCompletedList(t *testing.T) {
	home := t.TempDir()
	runCommand := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		if err := run(args, &out, func() (string, error) { return home, nil }); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	runCommand("add", "Parent")
	runCommand("subtask", "add", "1", "Child")
	if got := runCommand("start", "1"); got != "Task 1 is now started.\n" {
		t.Fatalf("start output = %q", got)
	}
	if got := runCommand("block", "2"); got != "Task 2 is now blocked.\n" {
		t.Fatalf("block output = %q", got)
	}
	if got := runCommand("list"); got != "1  [started]  Parent\n2  [blocked]  (under 1)  Child\n" {
		t.Fatalf("list output = %q", got)
	}
	if got := runCommand("complete", "2"); got != "Task 2 is now done.\n" {
		t.Fatalf("complete output = %q", got)
	}
	if got := runCommand("list"); got != "1  [started]  Parent\n" {
		t.Fatalf("list after completing child = %q", got)
	}
	if got := runCommand("list", "-a"); got != "1  [started]  Parent\n2  [done]  (under 1)  Child\n" {
		t.Fatalf("list -a output = %q", got)
	}
	if got := runCommand("show", "1"); !strings.Contains(got, "2  [done]  Child") {
		t.Fatalf("show completed child = %q", got)
	}
	if got := runCommand("search", "Child"); !strings.Contains(got, "2  [done]") {
		t.Fatalf("search completed child = %q", got)
	}
	runCommand("complete", "1")
	if got := runCommand("list"); got != "No tasks found.\n" {
		t.Fatalf("list after completing all = %q", got)
	}
	runCommand("start", "2")
	if got := runCommand("list"); got != "2  [started]  (under 1)  Child\n" {
		t.Fatalf("list after restarting child = %q", got)
	}
	if got := runCommand("done", "2"); got != "Task 2 is now done.\n" {
		t.Fatalf("done alias output = %q", got)
	}
	if got := runCommand("list"); got != "No tasks found.\n" {
		t.Fatalf("list after done alias = %q", got)
	}
	if got := runCommand("list", "-a"); !strings.Contains(got, "2  [done]  (under 1)  Child") {
		t.Fatalf("list -a after done alias = %q", got)
	}

	var out bytes.Buffer
	err := run([]string{"block", "999"}, &out, func() (string, error) { return home, nil })
	if err == nil || !strings.Contains(err.Error(), "task 999 not found") {
		t.Fatalf("missing task error = %v", err)
	}
}

func TestExistingDatabaseGetsTodoStatus(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".trackerator")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "trackerator.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		parent_id INTEGER REFERENCES tasks(id),
		title TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO tasks (title) VALUES ('Existing task')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"list"}, &out, func() (string, error) { return home, nil }); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "1  [todo]  Existing task\n" {
		t.Fatalf("migrated task = %q", got)
	}
}

func TestSubtaskNeedsExistingParent(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	err := run([]string{"subtask", "add", "42", "Orphan"}, &out, func() (string, error) { return home, nil })
	if err == nil || !strings.Contains(err.Error(), "task 42 not found") {
		t.Fatalf("error = %v", err)
	}
	if err := run([]string{"show", "0"}, &out, func() (string, error) { return home, nil }); err == nil {
		t.Fatal("expected invalid ID error")
	}
}
