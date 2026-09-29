package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const dateLayout = "2006-01-02"

func parseScheduleDate(value string) (sql.NullString, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return sql.NullString{}, nil
	}
	parsed, err := time.Parse(dateLayout, value)
	if err != nil || parsed.Format(dateLayout) != value {
		return sql.NullString{}, fmt.Errorf("expected a valid date in YYYY-MM-DD format")
	}
	return sql.NullString{String: value, Valid: true}, nil
}

func localToday() string {
	return time.Now().In(time.Local).Format(dateLayout)
}

func (t task) startDue(today string) bool {
	return t.Status == "todo" && t.StartDate.Valid && t.StartDate.String <= today
}

func (t task) completionDue(today string) bool {
	return t.Status != "done" && t.CompletionDate.Valid && t.CompletionDate.String <= today
}
