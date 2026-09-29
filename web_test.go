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
		!strings.Contains(page.Body.String(), `class="subtasks" aria-label="Subtasks of task 1" data-task-id="1"`) ||
		!strings.Contains(page.Body.String(), `data-reveal-target="add-subtask-1"`) ||
		!strings.Contains(page.Body.String(), `id="add-subtask-1" hidden`) ||
		!strings.Contains(page.Body.String(), `data-reveal-target="schedule-1"`) ||
		!strings.Contains(page.Body.String(), `id="schedule-1" hidden`) ||
		!strings.Contains(page.Body.String(), "Subtask of #1") {
		t.Fatalf("tasks not shown or escaped: %q", page.Body.String())
	}
	if strings.Contains(page.Body.String(), `data-task-id="1" open`) {
		t.Fatal("subtasks should be collapsed on the default list")
	}
	page = request(http.MethodGet, "/?open=1", nil)
	if !strings.Contains(page.Body.String(), `data-task-id="1" open`) {
		t.Fatal("URL-selected subtasks should be expanded")
	}
	script := request(http.MethodGet, "/assets/ui.js", nil)
	if script.Code != http.StatusOK || !strings.Contains(script.Header().Get("Content-Type"), "javascript") ||
		!strings.Contains(script.Body.String(), "openParentSubtasks") ||
		!strings.Contains(script.Body.String(), `searchParams.set("open", openSubtaskIDs())`) {
		t.Fatalf("UI script: status %d, content type %q", script.Code, script.Header().Get("Content-Type"))
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
	if !strings.Contains(page.Body.String(), `data-task-id="1" open`) {
		t.Fatal("search results should expand matching subtask branches")
	}
	page = request(http.MethodGet, "/?all=1&q=review&open=", nil)
	if strings.Contains(page.Body.String(), `data-task-id="1" open`) {
		t.Fatal("explicitly collapsed subtasks should stay collapsed on search")
	}
	updated = request(http.MethodPost, "/tasks/status", url.Values{"csrf": {a.csrfToken}, "id": {"2"}, "status": {"started"}, "q": {"review"}, "all": {"1"}, "open": {"1"}})
	if updated.Code != http.StatusSeeOther || updated.Header().Get("Location") != "/?all=1&open=1&q=review#task-2" {
		t.Fatalf("restart subtask: status %d, redirect %q", updated.Code, updated.Header().Get("Location"))
	}
	created = request(http.MethodPost, "/tasks", url.Values{"csrf": {a.csrfToken}, "title": {"Another task"}, "open": {""}})
	if created.Code != http.StatusSeeOther || created.Header().Get("Location") != "/?open=#task-3" {
		t.Fatalf("create with collapsed state: status %d, redirect %q", created.Code, created.Header().Get("Location"))
	}
	if got, err := s.get(2); err != nil || got.Status != "started" {
		t.Fatalf("subtask status = %q, error = %v", got.Status, err)
	}
}

func TestWebTaskURLs(t *testing.T) {
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	parent, err := s.add("Parent", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.add("Child", &parent); err != nil {
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
	link := "https://example.com/review?a=1&b=2"
	added := request(http.MethodPost, "/tasks/url", url.Values{
		"csrf": {a.csrfToken}, "id": {"2"}, "action": {"add"}, "url": {link}, "open": {"1"},
	})
	if added.Code != http.StatusSeeOther || added.Header().Get("Location") != "/?open=1#task-2" {
		t.Fatalf("add URL: status %d, redirect %q", added.Code, added.Header().Get("Location"))
	}
	page := request(http.MethodGet, "/?open=1", nil).Body.String()
	for _, want := range []string{
		`data-reveal-target="add-url-2"`, `id="add-url-2" hidden`,
		`href="https://example.com/review?a=1&amp;b=2"`,
		`name="url" value="https://example.com/review?a=1&amp;b=2"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("task URL page missing %q", want)
		}
	}
	invalid := request(http.MethodPost, "/tasks/url", url.Values{
		"csrf": {a.csrfToken}, "id": {"2"}, "action": {"add"}, "url": {"javascript:alert(1)"},
	})
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), "invalid URL") {
		t.Fatalf("invalid URL: status %d, body %q", invalid.Code, invalid.Body.String())
	}
	removed := request(http.MethodPost, "/tasks/url", url.Values{
		"csrf": {a.csrfToken}, "id": {"2"}, "action": {"remove"}, "url": {link}, "open": {"1"},
	})
	if removed.Code != http.StatusSeeOther {
		t.Fatalf("remove URL: status %d, body %q", removed.Code, removed.Body.String())
	}
	if page := request(http.MethodGet, "/?open=1", nil).Body.String(); strings.Contains(page, `href="https://example.com/review`) {
		t.Fatal("removed URL still shown")
	}
}

func TestStableTaskPagesShowAncestryWithoutPeers(t *testing.T) {
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	root, err := s.add("Root", nil)
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.add("Chosen child", &root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.add("Root peer", &root); err != nil {
		t.Fatal(err)
	}
	grandchild, err := s.add("Chosen grandchild", &child)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.setStatus(grandchild, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.add("Child peer", &child); err != nil {
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
	index := request(http.MethodGet, "/", nil).Body.String()
	if !strings.Contains(index, `class="task-id" href="/tasks/1"`) || !strings.Contains(index, `class="task-id" href="/tasks/2"`) {
		t.Fatal("task IDs should link to stable task pages")
	}
	parentPage := request(http.MethodGet, "/tasks/1", nil)
	if parentPage.Code != http.StatusOK || !strings.Contains(parentPage.Body.String(), "Root peer") || !strings.Contains(parentPage.Body.String(), "Chosen grandchild") || !strings.Contains(parentPage.Body.String(), `data-task-id="1" open`) || !strings.Contains(parentPage.Body.String(), `data-task-id="2" open`) || !strings.Contains(parentPage.Body.String(), "status-done") {
		t.Fatalf("parent detail: status %d, body %q", parentPage.Code, parentPage.Body.String())
	}
	childPage := request(http.MethodGet, "/tasks/2", nil)
	if childPage.Code != http.StatusOK || !strings.Contains(childPage.Body.String(), "Chosen grandchild") || !strings.Contains(childPage.Body.String(), "Child peer") || strings.Contains(childPage.Body.String(), "Root peer") || !strings.Contains(childPage.Body.String(), `1 other subtask not shown`) || !strings.Contains(childPage.Body.String(), `href="/tasks/1">View all subtasks of #1`) {
		t.Fatalf("child detail: status %d, body %q", childPage.Code, childPage.Body.String())
	}
	grandchildPage := request(http.MethodGet, "/tasks/4", nil)
	if grandchildPage.Code != http.StatusOK || !strings.Contains(grandchildPage.Body.String(), "Root") || !strings.Contains(grandchildPage.Body.String(), "Chosen child") || strings.Contains(grandchildPage.Body.String(), "Root peer") || strings.Contains(grandchildPage.Body.String(), "Child peer") || !strings.Contains(grandchildPage.Body.String(), "status-done") || !strings.Contains(grandchildPage.Body.String(), `href="/tasks/2">View all subtasks of #2`) {
		t.Fatalf("grandchild detail: status %d, body %q", grandchildPage.Code, grandchildPage.Body.String())
	}
	if response := request(http.MethodGet, "/tasks/999", nil); response.Code != http.StatusNotFound {
		t.Fatalf("missing task detail: status %d", response.Code)
	}
	if response := request(http.MethodPost, "/tasks/status", url.Values{
		"csrf": {a.csrfToken}, "id": {"4"}, "status": {"started"}, "view_id": {"2"}, "open": {"1,2"},
	}); response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/tasks/2?open=1%2C2#task-4" {
		t.Fatalf("detail status redirect: status %d, location %q", response.Code, response.Header().Get("Location"))
	}
}

func TestWebTreeKeepsSubtasksUnderFilteredAncestors(t *testing.T) {
	tasks := []task{
		{ID: 1, Title: "Finished parent", Status: "done"},
		{ID: 2, ParentID: sql.NullInt64{Int64: 1, Valid: true}, Title: "Active child", Status: "started"},
		{ID: 3, ParentID: sql.NullInt64{Int64: 2, Valid: true}, Title: "Review grandchild", Status: "todo"},
		{ID: 4, ParentID: sql.NullInt64{Int64: 1, Valid: true}, Title: "Finished sibling", Status: "done"},
	}

	roots, count := buildWebTree(tasks, "", false, "token", "2026-09-29", nil)
	if count != 3 || len(roots) != 1 || !roots[0].ContextOnly || roots[0].Task.ID != 1 {
		t.Fatalf("default roots = %#v, count = %d", roots, count)
	}
	if len(roots[0].Children) != 1 || roots[0].Children[0].Task.ID != 2 || roots[0].Children[0].ContextOnly {
		t.Fatalf("active child not nested under completed parent: %#v", roots[0].Children)
	}
	if len(roots[0].Children[0].Children) != 1 || roots[0].Children[0].Children[0].Task.ID != 3 {
		t.Fatalf("grandchild not nested: %#v", roots[0].Children[0].Children)
	}

	roots, count = buildWebTree(tasks, "review", false, "token", "2026-09-29", nil)
	if count != 3 || !roots[0].ContextOnly || !roots[0].Children[0].ContextOnly || roots[0].Children[0].Children[0].ContextOnly {
		t.Fatalf("search hierarchy = %#v, count = %d", roots, count)
	}
	if !roots[0].Expanded || !roots[0].Children[0].Expanded {
		t.Fatal("search should expand every ancestor of a matching subtask")
	}
	roots, _ = buildWebTree(tasks, "review", false, "token", "2026-09-29", parseOpenIDs("2,garbage,-1"))
	if roots[0].Expanded || !roots[0].Children[0].Expanded {
		t.Fatal("explicit URL state should expand only valid selected sections")
	}
	roots, count = buildWebTree(tasks, "", true, "token", "2026-09-29", nil)
	if count != 4 || len(roots[0].Children) != 2 || roots[0].ContextOnly {
		t.Fatalf("show completed hierarchy = %#v, count = %d", roots, count)
	}
}

func TestWebListsSortStatusesWithoutFlatteningSubtasks(t *testing.T) {
	tasks := []task{
		{ID: 1, Title: "Work blocked", Status: "blocked"},
		{ID: 2, Title: "Work todo", Status: "todo"},
		{ID: 3, Title: "Work started", Status: "started"},
		{ID: 4, Title: "Work done", Status: "done"},
		{ID: 5, Title: "Parent", Status: "todo"},
		{ID: 6, ParentID: sql.NullInt64{Int64: 5, Valid: true}, Title: "Work nested blocked", Status: "blocked"},
		{ID: 7, ParentID: sql.NullInt64{Int64: 5, Valid: true}, Title: "Work nested started", Status: "started"},
		{ID: 8, ParentID: sql.NullInt64{Int64: 5, Valid: true}, Title: "Work nested todo", Status: "todo"},
	}
	roots, count := buildWebTree(tasks, "work", true, "token", "2026-09-29", nil)
	if count != 8 {
		t.Fatalf("search count = %d, want 8", count)
	}
	for i, want := range []int64{3, 5, 2, 1, 4} {
		if roots[i].Task.ID != want {
			t.Fatalf("search root %d = #%d, want #%d", i, roots[i].Task.ID, want)
		}
	}
	if !roots[1].ContextOnly {
		t.Fatal("nested matches should retain their parent as context")
	}
	for i, want := range []int64{7, 8, 6} {
		if roots[1].Children[i].Task.ID != want {
			t.Fatalf("search child %d = #%d, want #%d", i, roots[1].Children[i].Task.ID, want)
		}
	}
	roots, _ = buildWebTree(tasks, "work", false, "token", "2026-09-29", nil)
	if len(roots) != 4 || roots[0].Task.ID != 3 || roots[3].Task.ID != 1 {
		t.Fatalf("unfinished search order = %#v", roots)
	}
	roots, _ = buildWebTree(tasks, "", true, "token", "2026-09-29", nil)
	for i, want := range []int64{3, 5, 2, 1, 4} {
		if roots[i].Task.ID != want {
			t.Fatalf("unfiltered root %d = #%d, want status order #%d", i, roots[i].Task.ID, want)
		}
	}
}

func TestWebHomeShowsStartedBeforeBlocked(t *testing.T) {
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	blocked, err := s.add("Blocked parent", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.setStatus(blocked, "blocked"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.add("Todo subtask", &blocked); err != nil {
		t.Fatal(err)
	}
	started, err := s.add("Started task", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.setStatus(started, "started"); err != nil {
		t.Fatal(err)
	}
	a, err := newWebApp(s, "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	a.handler().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("home page status = %d", response.Code)
	}
	page := response.Body.String()
	startedAt := strings.Index(page, `id="task-3"`)
	blockedAt := strings.Index(page, `id="task-1"`)
	if startedAt < 0 || blockedAt < 0 || startedAt > blockedAt {
		t.Fatalf("started task should precede blocked branch: started at %d, blocked at %d", startedAt, blockedAt)
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
