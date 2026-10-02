package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const agentDeckWebURL = "http://127.0.0.1:8420/"

type agentDeckSession struct {
	Title     string
	Workspace string
	Tool      string
	RepoURL   string
}

type externalRunner func(context.Context, string, ...string) ([]byte, error)

func runExternal(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (s *store) agentDeckSession(id int64) (agentDeckSession, error) {
	var session agentDeckSession
	err := s.db.QueryRow("SELECT session_title, workspace_path, tool, repo_url FROM task_agent_deck WHERE task_id = ?", id).
		Scan(&session.Title, &session.Workspace, &session.Tool, &session.RepoURL)
	if errors.Is(err, sql.ErrNoRows) {
		return session, nil
	}
	return session, err
}

func (s *store) allAgentDeckSessions() (map[int64]agentDeckSession, error) {
	rows, err := s.db.Query("SELECT task_id, session_title, workspace_path, tool, repo_url FROM task_agent_deck")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := make(map[int64]agentDeckSession)
	for rows.Next() {
		var id int64
		var session agentDeckSession
		if err := rows.Scan(&id, &session.Title, &session.Workspace, &session.Tool, &session.RepoURL); err != nil {
			return nil, err
		}
		sessions[id] = session
	}
	return sessions, rows.Err()
}

func (s *store) saveAgentDeckSession(id int64, session agentDeckSession) error {
	_, err := s.db.Exec(`INSERT INTO task_agent_deck (task_id, session_title, workspace_path, tool, repo_url)
		VALUES (?, ?, ?, ?, ?)`, id, session.Title, session.Workspace, session.Tool, session.RepoURL)
	return err
}

func attachAgentDeckSessions(roots []*webTask, sessions map[int64]agentDeckSession) {
	var walk func(*webTask)
	walk = func(node *webTask) {
		node.AgentDeck = sessions[node.Task.ID]
		for _, child := range node.Children {
			walk(child)
		}
	}
	for _, root := range roots {
		walk(root)
	}
}

var scpGitURL = regexp.MustCompile(`^git@[A-Za-z0-9][A-Za-z0-9.-]*:[A-Za-z0-9_./-]+$`)

func validGitURL(raw string) bool {
	if scpGitURL.MatchString(raw) {
		return !strings.Contains(raw, "..")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.Path == "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return u.User == nil
	}
	return u.Scheme == "ssh" && u.User != nil && u.User.Username() != ""
}

func (a *webApp) launchAgentDeck(w http.ResponseWriter, r *http.Request) {
	if !a.parsePost(w, r) {
		return
	}
	id, err := parseID(r.PostFormValue("id"))
	if err != nil {
		a.renderPostError(w, r, err.Error())
		return
	}
	t, err := a.store.get(id)
	if err != nil {
		a.renderPostError(w, r, err.Error())
		return
	}
	a.agentDeckMu.Lock()
	defer a.agentDeckMu.Unlock()
	if existing, err := a.store.agentDeckSession(id); err != nil {
		http.Error(w, "could not load Agent Deck session", http.StatusInternalServerError)
		return
	} else if existing.Title != "" {
		http.Redirect(w, r, agentDeckWebURL, http.StatusSeeOther)
		return
	}
	tool := r.PostFormValue("tool")
	if tool != "shell" && tool != "codex" && tool != "claude" {
		a.renderPostError(w, r, "Choose Shell, Codex, or Claude Code.")
		return
	}
	repoURL := strings.TrimSpace(r.PostFormValue("repo_url"))
	if repoURL != "" && !validGitURL(repoURL) {
		a.renderPostError(w, r, "Enter an HTTPS or SSH Git repository URL.")
		return
	}
	workspace := filepath.Join(a.store.home, ".trackerator", "workspaces", "task-"+strconv.FormatInt(id, 10))
	if err := os.MkdirAll(workspace, 0700); err != nil {
		a.renderPostError(w, r, "Could not create the task workspace.")
		return
	}
	sessionPath := workspace
	if repoURL != "" {
		sessionPath = filepath.Join(workspace, "repo")
		if _, err := os.Stat(sessionPath); errors.Is(err, os.ErrNotExist) {
			clonePath, err := os.MkdirTemp(workspace, ".repo-clone-")
			if err != nil {
				a.renderPostError(w, r, "Could not prepare the task repository directory.")
				return
			}
			defer os.RemoveAll(clonePath)
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
			defer cancel()
			if _, err := a.runner(ctx, "git", "clone", "--", repoURL, clonePath); err != nil {
				a.renderPostError(w, r, "Could not clone the repository. Check the URL and Git credentials.")
				return
			}
			if err := os.Rename(clonePath, sessionPath); err != nil {
				a.renderPostError(w, r, "Could not place the cloned repository in the task workspace.")
				return
			}
		} else if err != nil {
			a.renderPostError(w, r, "Could not inspect the task repository.")
			return
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			origin, err := a.runner(ctx, "git", "-C", sessionPath, "remote", "get-url", "origin")
			if err != nil || strings.TrimSpace(string(origin)) != repoURL {
				a.renderPostError(w, r, "This task workspace already contains a different repository.")
				return
			}
		}
	}
	title := fmt.Sprintf("Trackerator #%d: %s", id, t.Title)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	deckBin := os.Getenv("TRACKERATOR_AGENT_DECK_BIN")
	if deckBin == "" {
		deckBin = "agent-deck"
	}
	if _, err := a.runner(ctx, deckBin, "launch", sessionPath, "-t", title, "-g", "trackerator", "-c", tool, "--hint", fmt.Sprintf("ticket=trackerator-%d", id)); err != nil {
		a.renderPostError(w, r, "Could not launch Agent Deck. Install agent-deck and check its CLI setup, then try again.")
		return
	}
	if err := a.store.saveAgentDeckSession(id, agentDeckSession{Title: title, Workspace: sessionPath, Tool: tool, RepoURL: repoURL}); err != nil {
		http.Error(w, "Session launched, but Trackerator could not save its association", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, agentDeckWebURL, http.StatusSeeOther)
}
