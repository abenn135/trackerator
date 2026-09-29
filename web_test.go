package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWebTaskWorkflow(t *testing.T) {
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	a, err := newWebApp(s, "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	handler := a.handler()
	request := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var body string
		if form != nil {
			body = form.Encode()
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1:8080"
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}

	page := request(http.MethodGet, "/", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `name="csrf" value="`+a.csrfToken+`"`) {
		t.Fatalf("initial page: status %d, body %q", page.Code, page.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "localhost:8080"
	localhostPage := httptest.NewRecorder()
	handler.ServeHTTP(localhostPage, req)
	if localhostPage.Code != http.StatusOK {
		t.Fatalf("localhost page: status %d", localhostPage.Code)
	}

	created := request(http.MethodPost, "/tasks", url.Values{"csrf": {a.csrfToken}, "title": {"Plan <release>"}})
	if created.Code != http.StatusSeeOther || created.Header().Get("Location") != "/#task-1" {
		t.Fatalf("create task: status %d, redirect %q", created.Code, created.Header().Get("Location"))
	}
	created = request(http.MethodPost, "/tasks", url.Values{"csrf": {a.csrfToken}, "title": {"Review changes"}, "parent_id": {"1"}})
	if created.Code != http.StatusSeeOther {
		t.Fatalf("create subtask: status %d, body %q", created.Code, created.Body.String())
	}
	page = request(http.MethodGet, "/", nil)
	if !strings.Contains(page.Body.String(), "Plan &lt;release&gt;") ||
		!strings.Contains(page.Body.String(), `class="children" aria-label="Subtasks of task 1"`) ||
		!strings.Contains(page.Body.String(), "Subtask of #1") {
		t.Fatalf("tasks not shown or escaped: %q", page.Body.String())
	}

	updated := request(http.MethodPost, "/tasks/status", url.Values{"csrf": {a.csrfToken}, "id": {"2"}, "status": {"done"}})
	if updated.Code != http.StatusSeeOther {
		t.Fatalf("complete subtask: status %d, body %q", updated.Code, updated.Body.String())
	}
	page = request(http.MethodGet, "/", nil)
	if strings.Contains(page.Body.String(), "Review changes") {
		t.Fatal("completed subtask shown in default list")
	}
	page = request(http.MethodGet, "/?all=1&q=review", nil)
	if !strings.Contains(page.Body.String(), "Review changes") || !strings.Contains(page.Body.String(), "status-done") {
		t.Fatal("completed subtask missing from search with show completed")
	}
	updated = request(http.MethodPost, "/tasks/status", url.Values{"csrf": {a.csrfToken}, "id": {"2"}, "status": {"started"}, "q": {"review"}, "all": {"1"}})
	if updated.Code != http.StatusSeeOther || updated.Header().Get("Location") != "/?all=1&q=review" {
		t.Fatalf("restart subtask: status %d, redirect %q", updated.Code, updated.Header().Get("Location"))
	}
	if got, err := s.get(2); err != nil || got.Status != "started" {
		t.Fatalf("subtask status = %q, error = %v", got.Status, err)
	}
}

func TestWebTreeKeepsSubtasksUnderFilteredAncestors(t *testing.T) {
	tasks := []task{
		{ID: 1, Title: "Finished parent", Status: "done"},
		{ID: 2, ParentID: sql.NullInt64{Int64: 1, Valid: true}, Title: "Active child", Status: "started"},
		{ID: 3, ParentID: sql.NullInt64{Int64: 2, Valid: true}, Title: "Review grandchild", Status: "todo"},
		{ID: 4, ParentID: sql.NullInt64{Int64: 1, Valid: true}, Title: "Finished sibling", Status: "done"},
	}

	roots, count := buildWebTree(tasks, "", false, "token", "2026-09-29")
	if count != 3 || len(roots) != 1 || !roots[0].ContextOnly || roots[0].Task.ID != 1 {
		t.Fatalf("default roots = %#v, count = %d", roots, count)
	}
	if len(roots[0].Children) != 1 || roots[0].Children[0].Task.ID != 2 || roots[0].Children[0].ContextOnly {
		t.Fatalf("active child not nested under completed parent: %#v", roots[0].Children)
	}
	if len(roots[0].Children[0].Children) != 1 || roots[0].Children[0].Children[0].Task.ID != 3 {
		t.Fatalf("grandchild not nested: %#v", roots[0].Children[0].Children)
	}

	roots, count = buildWebTree(tasks, "review", false, "token", "2026-09-29")
	if count != 3 || !roots[0].ContextOnly || !roots[0].Children[0].ContextOnly || roots[0].Children[0].Children[0].ContextOnly {
		t.Fatalf("search hierarchy = %#v, count = %d", roots, count)
	}
	roots, count = buildWebTree(tasks, "", true, "token", "2026-09-29")
	if count != 4 || len(roots[0].Children) != 2 || roots[0].ContextOnly {
		t.Fatalf("show completed hierarchy = %#v, count = %d", roots, count)
	}
}

func TestWebSchedulesSubtaskAndHighlightsDueDates(t *testing.T) {
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	parentID, err := s.add("Parent", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.add("Scheduled child", &parentID); err != nil {
		t.Fatal(err)
	}
	a, err := newWebApp(s, "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var body string
		if form != nil {
			body = form.Encode()
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Host = "127.0.0.1:8080"
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		response := httptest.NewRecorder()
		a.handler().ServeHTTP(response, req)
		return response
	}
	response := request(http.MethodPost, "/tasks/schedule", url.Values{
		"csrf": {a.csrfToken}, "id": {"2"},
		"start_date": {"2000-01-01"}, "completion_date": {"2000-01-02"},
	})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("schedule subtask: status %d, body %q", response.Code, response.Body.String())
	}
	page := request(http.MethodGet, "/", nil).Body.String()
	for _, want := range []string{
		"Due now", "Ready to start", "Completion due", `href="#task-2"`,
		"Start 2000-01-01", "Complete by 2000-01-02", `value="2000-01-01"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("scheduled page missing %q", want)
		}
	}
	response = request(http.MethodPost, "/tasks/status", url.Values{
		"csrf": {a.csrfToken}, "id": {"2"}, "status": {"started"},
	})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("start subtask: status %d", response.Code)
	}
	page = request(http.MethodGet, "/", nil).Body.String()
	if strings.Contains(page, "Ready to start") || !strings.Contains(page, "Completion due") {
		t.Fatalf("started subtask due indicators: %q", page)
	}
	response = request(http.MethodPost, "/tasks/schedule", url.Values{
		"csrf": {a.csrfToken}, "id": {"2"}, "start_date": {""}, "completion_date": {""},
	})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("clear schedule: status %d", response.Code)
	}
	page = request(http.MethodGet, "/", nil).Body.String()
	if strings.Contains(page, "Due now") || strings.Contains(page, "Complete by 2000-01-02") {
		t.Fatalf("cleared schedule still shown: %q", page)
	}
	response = request(http.MethodPost, "/tasks/schedule", url.Values{
		"csrf": {a.csrfToken}, "id": {"2"}, "start_date": {"2026-02-30"},
	})
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "valid date") {
		t.Fatalf("invalid date: status %d, body %q", response.Code, response.Body.String())
	}
	response = request(http.MethodPost, "/tasks", url.Values{
		"csrf": {a.csrfToken}, "title": {"Invalid new subtask"}, "parent_id": {"1"},
		"start_date": {"2026-02-30"},
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid date on create: status %d", response.Code)
	}
	tasks, err := s.list(nil, nil, true)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("invalid create changed task count: %d, error %v", len(tasks), err)
	}
	response = request(http.MethodPost, "/tasks", url.Values{
		"csrf": {a.csrfToken}, "title": {"New scheduled subtask"}, "parent_id": {"1"},
		"start_date": {"2000-02-01"}, "completion_date": {"2000-02-02"},
	})
	if response.Code != http.StatusSeeOther {
		t.Fatalf("create scheduled subtask: status %d", response.Code)
	}
	created, err := s.get(3)
	if err != nil || created.ParentID.Int64 != 1 || created.StartDate.String != "2000-02-01" || created.CompletionDate.String != "2000-02-02" {
		t.Fatalf("scheduled subtask = %#v, error %v", created, err)
	}
}

func TestWebRejectsUnauthorizedAndInvalidUpdates(t *testing.T) {
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	a, err := newWebApp(s, "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{"title": {"Unexpected"}}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(form))
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	a.handler().ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing form token: status %d", response.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "untrusted.example:8080"
	response = httptest.NewRecorder()
	a.handler().ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("untrusted host: status %d", response.Code)
	}

	form = url.Values{"csrf": {a.csrfToken}, "id": {"1"}, "status": {"invalid"}}.Encode()
	req = httptest.NewRequest(http.MethodPost, "/tasks/status", strings.NewReader(form))
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	a.handler().ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid status: status %d", response.Code)
	}
}
