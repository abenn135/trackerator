package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	_ "modernc.org/sqlite"
)

type task struct {
	ID             int64
	ParentID       sql.NullInt64
	Title          string
	Status         string
	StartDate      sql.NullString
	CompletionDate sql.NullString
	Created        string
}

type store struct {
	db   *sql.DB
	home string
}

func openStore(home string) (*store, error) {
	dir := filepath.Join(home, ".trackerator")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	path := filepath.Join(dir, "trackerator.db")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("create database file: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close database file: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// SQLite pragmas apply per connection. One connection keeps foreign keys enabled.
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		`CREATE TABLE IF NOT EXISTS tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			parent_id INTEGER REFERENCES tasks(id),
			title TEXT NOT NULL CHECK (length(trim(title)) > 0),
			status TEXT NOT NULL DEFAULT 'todo' CHECK (status IN ('todo', 'started', 'blocked', 'done')),
			start_date TEXT,
			completion_date TEXT,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		"CREATE INDEX IF NOT EXISTS tasks_parent_id ON tasks(parent_id)",
		`CREATE TABLE IF NOT EXISTS task_urls (
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			url TEXT NOT NULL,
			PRIMARY KEY (task_id, url)
		)`,
		`CREATE TABLE IF NOT EXISTS task_agent_deck (
			task_id INTEGER PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
			session_title TEXT NOT NULL,
			workspace_path TEXT NOT NULL,
			tool TEXT NOT NULL,
			repo_url TEXT NOT NULL DEFAULT ''
		)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, fmt.Errorf("initialize database: %w", err)
		}
	}
	if err := migrateTaskColumns(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return &store{db: db, home: home}, nil
}

func migrateTaskColumns(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(tasks)")
	if err != nil {
		return err
	}
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, column := range []struct{ name, definition string }{
		{"status", "status TEXT NOT NULL DEFAULT 'todo' CHECK (status IN ('todo', 'started', 'blocked', 'done'))"},
		{"start_date", "start_date TEXT"},
		{"completion_date", "completion_date TEXT"},
	} {
		if columns[column.name] {
			continue
		}
		if _, err := db.Exec("ALTER TABLE tasks ADD COLUMN " + column.definition); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) close() error { return s.db.Close() }

func (s *store) add(title string, parentID *int64) (int64, error) {
	return s.addWithSchedule(title, parentID, "", "")
}

func (s *store) addWithSchedule(title string, parentID *int64, start, completion string) (int64, error) {
	startDate, err := parseScheduleDate(start)
	if err != nil {
		return 0, fmt.Errorf("start date: %w", err)
	}
	completionDate, err := parseScheduleDate(completion)
	if err != nil {
		return 0, fmt.Errorf("completion date: %w", err)
	}
	if startDate.Valid && completionDate.Valid && startDate.String > completionDate.String {
		return 0, fmt.Errorf("completion date must be on or after start date")
	}
	if parentID != nil {
		if _, err := s.get(*parentID); err != nil {
			return 0, err
		}
	}
	result, err := s.db.Exec("INSERT INTO tasks (parent_id, title, start_date, completion_date) VALUES (?, ?, ?, ?)", parentID, title, startDate, completionDate)
	if err != nil {
		return 0, fmt.Errorf("add task: %w", err)
	}
	return result.LastInsertId()
}

func (s *store) get(id int64) (task, error) {
	var t task
	err := s.db.QueryRow("SELECT id, parent_id, title, status, start_date, completion_date, created_at FROM tasks WHERE id = ?", id).
		Scan(&t.ID, &t.ParentID, &t.Title, &t.Status, &t.StartDate, &t.CompletionDate, &t.Created)
	if errors.Is(err, sql.ErrNoRows) {
		return task{}, fmt.Errorf("task %d not found", id)
	}
	if err != nil {
		return task{}, fmt.Errorf("get task: %w", err)
	}
	return t, nil
}

// A nil date leaves that field unchanged. An empty date clears it.
func (s *store) setSchedule(id int64, start, completion *string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin schedule update: %w", err)
	}
	defer tx.Rollback()
	var startDate, completionDate sql.NullString
	err = tx.QueryRow("SELECT start_date, completion_date FROM tasks WHERE id = ?", id).Scan(&startDate, &completionDate)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("task %d not found", id)
	}
	if err != nil {
		return fmt.Errorf("get task schedule: %w", err)
	}
	if start != nil {
		startDate, err = parseScheduleDate(*start)
		if err != nil {
			return fmt.Errorf("start date: %w", err)
		}
	}
	if completion != nil {
		completionDate, err = parseScheduleDate(*completion)
		if err != nil {
			return fmt.Errorf("completion date: %w", err)
		}
	}
	if startDate.Valid && completionDate.Valid && startDate.String > completionDate.String {
		return fmt.Errorf("completion date must be on or after start date")
	}
	_, err = tx.Exec("UPDATE tasks SET start_date = ?, completion_date = ? WHERE id = ?", startDate, completionDate, id)
	if err != nil {
		return fmt.Errorf("update task schedule: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit task schedule: %w", err)
	}
	return nil
}

func (s *store) setStatus(id int64, status string) error {
	result, err := s.db.Exec("UPDATE tasks SET status = ? WHERE id = ?", status, id)
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("task %d not found", id)
	}
	return nil
}

func validateTaskURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return "", fmt.Errorf("invalid URL %q: expected an absolute http or https URL", raw)
	}
	return value, nil
}

func (s *store) addURL(id int64, raw string) error {
	value, err := validateTaskURL(raw)
	if err != nil {
		return err
	}
	if _, err := s.get(id); err != nil {
		return err
	}
	result, err := s.db.Exec("INSERT OR IGNORE INTO task_urls (task_id, url) VALUES (?, ?)", id, value)
	if err != nil {
		return fmt.Errorf("add task URL: %w", err)
	}
	if count, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("add task URL: %w", err)
	} else if count == 0 {
		return fmt.Errorf("URL already associated with task %d", id)
	}
	return nil
}

func (s *store) removeURL(id int64, raw string) error {
	value, err := validateTaskURL(raw)
	if err != nil {
		return err
	}
	if _, err := s.get(id); err != nil {
		return err
	}
	result, err := s.db.Exec("DELETE FROM task_urls WHERE task_id = ? AND url = ?", id, value)
	if err != nil {
		return fmt.Errorf("remove task URL: %w", err)
	}
	if count, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("remove task URL: %w", err)
	} else if count == 0 {
		return fmt.Errorf("URL not associated with task %d", id)
	}
	return nil
}

func (s *store) urls(id int64) ([]string, error) {
	if _, err := s.get(id); err != nil {
		return nil, err
	}
	rows, err := s.db.Query("SELECT url FROM task_urls WHERE task_id = ? ORDER BY rowid", id)
	if err != nil {
		return nil, fmt.Errorf("list task URLs: %w", err)
	}
	defer rows.Close()
	var links []string
	for rows.Next() {
		var link string
		if err := rows.Scan(&link); err != nil {
			return nil, fmt.Errorf("read task URL: %w", err)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list task URLs: %w", err)
	}
	return links, nil
}

func (s *store) allURLs() (map[int64][]string, error) {
	rows, err := s.db.Query("SELECT task_id, url FROM task_urls ORDER BY rowid")
	if err != nil {
		return nil, fmt.Errorf("list task URLs: %w", err)
	}
	defer rows.Close()
	links := make(map[int64][]string)
	for rows.Next() {
		var id int64
		var link string
		if err := rows.Scan(&id, &link); err != nil {
			return nil, fmt.Errorf("read task URL: %w", err)
		}
		links[id] = append(links[id], link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list task URLs: %w", err)
	}
	return links, nil
}

func (s *store) list(parentID *int64, query *string, includeDone bool) ([]task, error) {
	statement := "SELECT id, parent_id, title, status, start_date, completion_date, created_at FROM tasks"
	var conditions []string
	var args []any
	if parentID != nil {
		conditions = append(conditions, "parent_id = ?")
		args = append(args, *parentID)
	}
	if query != nil {
		conditions = append(conditions, "instr(lower(title), lower(?)) > 0")
		args = append(args, *query)
	}
	if !includeDone {
		conditions = append(conditions, "status != 'done'")
	}
	if len(conditions) > 0 {
		statement += " WHERE " + strings.Join(conditions, " AND ")
	}
	statement += " ORDER BY id"
	rows, err := s.db.Query(statement, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	var tasks []task
	for rows.Next() {
		var t task
		if err := rows.Scan(&t.ID, &t.ParentID, &t.Title, &t.Status, &t.StartDate, &t.CompletionDate, &t.Created); err != nil {
			return nil, fmt.Errorf("read task: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return tasks, nil
}
