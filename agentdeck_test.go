package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentDeckWorkspaceLaunchAndReuse(t *testing.T) {
	t.Setenv("TRACKERATOR_AGENT_DECK_BIN", "/test/agent-deck")
	home := t.TempDir()
	s, err := openStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	id, err := s.add("Review release", nil)
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Fatalf("task ID = %d, want 1", id)
	}
	a, err := newWebApp(s, "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	a.runner = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return nil, nil
	}
	post := func(values url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/tasks/agent-deck", strings.NewReader(values.Encode()))
		req.Host = "127.0.0.1:8080"
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		a.handler().ServeHTTP(response, req)
		return response
	}
	values := url.Values{"csrf": {a.csrfToken}, "id": {"1"}, "tool": {"shell"}}
	response := post(values)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != agentDeckWebURL {
		t.Fatalf("launch: status %d, location %q, body %q", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	workspace := filepath.Join(home, ".trackerator", "workspaces", "task-1")
	if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
		t.Fatalf("workspace: %v, %v", info, err)
	}
	if len(calls) != 1 || strings.Join(calls[0], "|") != "/test/agent-deck|launch|"+workspace+"|-t|Trackerator #1: Review release|-g|trackerator|-c|shell|--hint|ticket=trackerator-1" {
		t.Fatalf("launch calls = %#v", calls)
	}
	response = post(values)
	if response.Code != http.StatusSeeOther || len(calls) != 1 {
		t.Fatalf("repeat launch: status %d, calls %#v", response.Code, calls)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:8080"
	page := httptest.NewRecorder()
	a.handler().ServeHTTP(page, req)
	if !strings.Contains(page.Body.String(), "Open Agent Deck") || !strings.Contains(page.Body.String(), "Trackerator #1: Review release") {
		t.Fatalf("session not shown: %q", page.Body.String())
	}
}

func TestAgentDeckClonesRepoAndRejectsInvalidInputs(t *testing.T) {
	home := t.TempDir()
	s, err := openStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if _, err := s.add("Code task", nil); err != nil {
		t.Fatal(err)
	}
	a, err := newWebApp(s, "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	a.runner = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if name == "git" {
			if err := os.MkdirAll(args[len(args)-1], 0700); err != nil {
				t.Fatal(err)
			}
		}
		return nil, nil
	}
	post := func(values url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/tasks/agent-deck", strings.NewReader(values.Encode()))
		req.Host = "127.0.0.1:8080"
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		a.handler().ServeHTTP(response, req)
		return response
	}
	values := url.Values{"csrf": {a.csrfToken}, "id": {"1"}, "tool": {"codex"}, "repo_url": {"ext::evil"}}
	if response := post(values); response.Code != http.StatusBadRequest || len(calls) != 0 {
		t.Fatalf("invalid Git URL: status %d, calls %#v", response.Code, calls)
	}
	values.Set("repo_url", "https://github.com/example/project.git")
	if response := post(values); response.Code != http.StatusSeeOther {
		t.Fatalf("clone and launch: status %d, body %q", response.Code, response.Body.String())
	}
	if len(calls) != 2 || calls[0][0] != "git" || calls[0][1] != "clone" || !strings.Contains(calls[0][len(calls[0])-1], ".repo-clone-") || calls[1][0] != "agent-deck" || calls[1][2] != filepath.Join(home, ".trackerator", "workspaces", "task-1", "repo") {
		t.Fatalf("clone/launch calls = %#v", calls)
	}
	session, err := s.agentDeckSession(1)
	if err != nil || session.Tool != "codex" || session.RepoURL != values.Get("repo_url") {
		t.Fatalf("saved session = %#v, error %v", session, err)
	}
}

func TestAgentDeckRequiresTaskAndCSRF(t *testing.T) {
	s, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	a, err := newWebApp(s, "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		values url.Values
		want   int
	}{
		{url.Values{"id": {"1"}, "tool": {"shell"}}, http.StatusForbidden},
		{url.Values{"csrf": {a.csrfToken}, "id": {"1"}, "tool": {"shell"}}, http.StatusBadRequest},
	} {
		req := httptest.NewRequest(http.MethodPost, "/tasks/agent-deck", strings.NewReader(test.values.Encode()))
		req.Host = "127.0.0.1:8080"
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		a.handler().ServeHTTP(response, req)
		if response.Code != test.want {
			t.Fatalf("status %d, want %d", response.Code, test.want)
		}
	}
}

func TestAgentDeckFailedCloneCanBeRetried(t *testing.T) {
	home := t.TempDir()
	s, err := openStore(home)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if _, err := s.add("Retry clone", nil); err != nil {
		t.Fatal(err)
	}
	a, err := newWebApp(s, "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	a.runner = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "git" {
			t.Fatalf("unexpected command %s", name)
		}
		if err := os.WriteFile(filepath.Join(args[len(args)-1], "partial"), []byte("partial"), 0600); err != nil {
			t.Fatal(err)
		}
		return nil, errors.New("clone failed")
	}
	values := url.Values{"csrf": {a.csrfToken}, "id": {"1"}, "tool": {"shell"}, "repo_url": {"https://github.com/example/repo.git"}}
	req := httptest.NewRequest(http.MethodPost, "/tasks/agent-deck", strings.NewReader(values.Encode()))
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	a.handler().ServeHTTP(response, req)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "Could not clone") {
		t.Fatalf("failed clone response: status %d, body %q", response.Code, response.Body.String())
	}
	workspace := filepath.Join(home, ".trackerator", "workspaces", "task-1")
	entries, err := os.ReadDir(workspace)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed clone left files: %v, %v", entries, err)
	}
	session, err := s.agentDeckSession(1)
	if err != nil || session.Title != "" {
		t.Fatalf("failed clone saved session: %#v, %v", session, err)
	}
}
