package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed web/*.html web/*.js
var webFiles embed.FS

type webApp struct {
	store       *store
	template    *template.Template
	csrfToken   string
	allowedHost string
}

type webPage struct {
	Roots          []*webTask
	Count          int
	DetailID       int64
	DueStarts      []*webTask
	DueCompletions []*webTask
	Query          string
	ShowAll        bool
	Token          string
	Error          string
}

type webTask struct {
	Task          task
	URLs          []string
	ViewID        int64
	HiddenPeers   int
	Children      []*webTask
	ContextOnly   bool
	StatusRank    int
	Expanded      bool
	StartDue      bool
	CompletionDue bool
	Query         string
	ShowAll       bool
	Token         string
}

func newWebApp(s *store, host string) (*webApp, error) {
	tmpl, err := template.ParseFS(webFiles, "web/*.html")
	if err != nil {
		return nil, fmt.Errorf("load web page: %w", err)
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, fmt.Errorf("create form token: %w", err)
	}
	return &webApp{
		store:       s,
		template:    tmpl,
		csrfToken:   hex.EncodeToString(token),
		allowedHost: host,
	}, nil
}

func (a *webApp) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.index)
	mux.HandleFunc("GET /tasks/{id}", a.detail)
	mux.HandleFunc("POST /tasks", a.createTask)
	mux.HandleFunc("POST /tasks/status", a.updateStatus)
	mux.HandleFunc("POST /tasks/schedule", a.updateSchedule)
	mux.HandleFunc("POST /tasks/url", a.updateURL)
	mux.HandleFunc("GET /assets/ui.js", a.uiScript)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.acceptsHost(r.Host) {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'")
		mux.ServeHTTP(w, r)
	})
}

func (a *webApp) uiScript(w http.ResponseWriter, r *http.Request) {
	script, err := webFiles.ReadFile("web/ui.js")
	if err != nil {
		http.Error(w, "could not load UI script", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Write(script)
}

func (a *webApp) acceptsHost(host string) bool {
	if host == a.allowedHost {
		return true
	}
	_, port, err := net.SplitHostPort(a.allowedHost)
	return err == nil && host == net.JoinHostPort("localhost", port)
}

func (a *webApp) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	a.render(w, r, http.StatusOK, "")
}

func (a *webApp) detail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.renderDetail(w, r, id, http.StatusOK, "")
}

func requestedOpenIDs(r *http.Request) map[int64]bool {
	if values, ok := r.URL.Query()["open"]; ok {
		return parseOpenIDs(values[0])
	}
	if values, ok := r.PostForm["open"]; ok {
		return parseOpenIDs(values[0])
	}
	return nil
}

func (a *webApp) render(w http.ResponseWriter, r *http.Request, status int, message string) {
	query := strings.TrimSpace(r.FormValue("q"))
	showAll := r.FormValue("all") == "1"
	openIDs := requestedOpenIDs(r)
	tasks, err := a.store.list(nil, nil, true)
	if err != nil {
		http.Error(w, "could not load tasks", http.StatusInternalServerError)
		return
	}
	today := localToday()
	roots, count := buildWebTree(tasks, query, showAll, a.csrfToken, today, openIDs)
	links, err := a.store.allURLs()
	if err != nil {
		http.Error(w, "could not load task URLs", http.StatusInternalServerError)
		return
	}
	attachWebURLs(roots, links)
	dueStarts, dueCompletions := collectWebDue(roots)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := a.template.ExecuteTemplate(w, "index.html", webPage{
		Roots: roots, Count: count, DueStarts: dueStarts, DueCompletions: dueCompletions,
		Query: query, ShowAll: showAll,
		Token: a.csrfToken, Error: message,
	}); err != nil {
		// The template is parsed at startup; an execution error means the response
		// may already be partially written, so logging is the useful fallback.
		log.Printf("trackerator: render web page: %v", err)
	}
}

func (a *webApp) renderDetail(w http.ResponseWriter, r *http.Request, id int64, status int, message string) {
	tasks, err := a.store.list(nil, nil, true)
	if err != nil {
		http.Error(w, "could not load tasks", http.StatusInternalServerError)
		return
	}
	openIDs := requestedOpenIDs(r)
	roots, _ := buildWebTree(tasks, "", true, a.csrfToken, localToday(), openIDs)
	root, count := buildWebDetail(roots, id, openIDs == nil)
	if root == nil {
		http.NotFound(w, r)
		return
	}
	links, err := a.store.allURLs()
	if err != nil {
		http.Error(w, "could not load task URLs", http.StatusInternalServerError)
		return
	}
	attachWebURLs([]*webTask{root}, links)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := a.template.ExecuteTemplate(w, "index.html", webPage{
		Roots: []*webTask{root}, Count: count, DetailID: id, Token: a.csrfToken, Error: message,
	}); err != nil {
		log.Printf("trackerator: render task detail: %v", err)
	}
}

func buildWebDetail(roots []*webTask, id int64, expandAll bool) (*webTask, int) {
	var countSubtree func(*webTask) int
	countSubtree = func(node *webTask) int {
		node.ViewID = id
		if expandAll {
			node.Expanded = true
		}
		count := 1
		for _, child := range node.Children {
			count += countSubtree(child)
		}
		return count
	}
	var find func(*webTask) (*webTask, int)
	find = func(node *webTask) (*webTask, int) {
		if node.Task.ID == id {
			return node, countSubtree(node)
		}
		for _, child := range node.Children {
			if selected, count := find(child); selected != nil {
				node.HiddenPeers = len(node.Children) - 1
				node.Children = []*webTask{selected}
				node.ContextOnly = true
				node.Expanded = true
				node.ViewID = id
				return node, count + 1
			}
		}
		return nil, 0
	}
	for _, root := range roots {
		if selected, count := find(root); selected != nil {
			return selected, count
		}
	}
	return nil, 0
}

func attachWebURLs(roots []*webTask, links map[int64][]string) {
	var attach func(*webTask)
	attach = func(node *webTask) {
		node.URLs = links[node.Task.ID]
		for _, child := range node.Children {
			attach(child)
		}
	}
	for _, root := range roots {
		attach(root)
	}
}

func buildWebTree(tasks []task, query string, showAll bool, token, today string, openIDs map[int64]bool) ([]*webTask, int) {
	nodes := make(map[int64]*webTask, len(tasks))
	for _, t := range tasks {
		nodes[t.ID] = &webTask{Task: t, Query: query, ShowAll: showAll, Token: token}
	}
	var roots []*webTask
	for _, t := range tasks {
		node := nodes[t.ID]
		if t.ParentID.Valid && nodes[t.ParentID.Int64] != nil {
			parent := nodes[t.ParentID.Int64]
			parent.Children = append(parent.Children, node)
		} else {
			roots = append(roots, node)
		}
	}

	search := strings.ToLower(query)
	var filter func(*webTask) (*webTask, int)
	filter = func(node *webTask) (*webTask, int) {
		visible := (showAll || node.Task.Status != "done") &&
			(search == "" || strings.Contains(strings.ToLower(node.Task.Title), search))
		filtered := &webTask{
			Task: node.Task, ContextOnly: !visible,
			Expanded: (openIDs == nil && query != "") || openIDs[node.Task.ID],
			StartDue: node.Task.startDue(today), CompletionDue: node.Task.completionDue(today),
			Query: query, ShowAll: showAll, Token: token,
		}
		count := 0
		for _, child := range node.Children {
			if child, childCount := filter(child); child != nil {
				filtered.Children = append(filtered.Children, child)
				count += childCount
			}
		}
		if !visible && len(filtered.Children) == 0 {
			return nil, 0
		}
		return filtered, count + 1
	}

	var displayed []*webTask
	count := 0
	for _, root := range roots {
		if node, nodeCount := filter(root); node != nil {
			displayed = append(displayed, node)
			count += nodeCount
		}
	}
	sortWebTasks(displayed)
	return displayed, count
}

func statusRank(status string) int {
	switch status {
	case "started":
		return 0
	case "todo":
		return 1
	case "blocked":
		return 2
	case "done":
		return 3
	default:
		return 4
	}
}

func ownStatusRank(node *webTask) int {
	if node.ContextOnly {
		return 4
	}
	return statusRank(node.Task.Status)
}

func sortWebTasks(nodes []*webTask) {
	for _, node := range nodes {
		node.StatusRank = ownStatusRank(node)
		if len(node.Children) > 0 {
			sortWebTasks(node.Children)
			if node.Children[0].StatusRank < node.StatusRank {
				node.StatusRank = node.Children[0].StatusRank
			}
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].StatusRank != nodes[j].StatusRank {
			return nodes[i].StatusRank < nodes[j].StatusRank
		}
		return ownStatusRank(nodes[i]) < ownStatusRank(nodes[j])
	})
}

func parseOpenIDs(raw string) map[int64]bool {
	ids := make(map[int64]bool)
	for _, part := range strings.Split(raw, ",") {
		if id, err := parseID(part); err == nil {
			ids[id] = true
		}
	}
	return ids
}

func collectWebDue(roots []*webTask) ([]*webTask, []*webTask) {
	var starts, completions []*webTask
	var walk func(*webTask)
	walk = func(node *webTask) {
		if !node.ContextOnly {
			if node.StartDue {
				starts = append(starts, node)
			}
			if node.CompletionDue {
				completions = append(completions, node)
			}
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	for _, root := range roots {
		walk(root)
	}
	return starts, completions
}

func (a *webApp) parsePost(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(a.csrfToken)) != 1 {
		http.Error(w, "invalid form token", http.StatusForbidden)
		return false
	}
	return true
}

func (a *webApp) createTask(w http.ResponseWriter, r *http.Request) {
	if !a.parsePost(w, r) {
		return
	}
	title := strings.TrimSpace(r.PostFormValue("title"))
	if title == "" {
		a.renderPostError(w, r, "Enter a task title.")
		return
	}
	var parentID *int64
	if raw := r.PostFormValue("parent_id"); raw != "" {
		id, err := parseID(raw)
		if err != nil {
			a.renderPostError(w, r, err.Error())
			return
		}
		parentID = &id
	}
	id, err := a.store.addWithSchedule(title, parentID, r.PostFormValue("start_date"), r.PostFormValue("completion_date"))
	if err != nil {
		a.renderPostError(w, r, err.Error())
		return
	}
	redirect := postReturnURL(r, "", r.PostFormValue("all") == "1") + "#task-" + strconv.FormatInt(id, 10)
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (a *webApp) updateStatus(w http.ResponseWriter, r *http.Request) {
	if !a.parsePost(w, r) {
		return
	}
	id, err := parseID(r.PostFormValue("id"))
	if err != nil {
		a.renderPostError(w, r, err.Error())
		return
	}
	status := r.PostFormValue("status")
	switch status {
	case "todo", "started", "blocked", "done":
	default:
		a.renderPostError(w, r, "Choose a valid status.")
		return
	}
	if err := a.store.setStatus(id, status); err != nil {
		a.renderPostError(w, r, err.Error())
		return
	}
	redirect := postReturnURL(r, r.PostFormValue("q"), r.PostFormValue("all") == "1") + "#task-" + strconv.FormatInt(id, 10)
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (a *webApp) updateSchedule(w http.ResponseWriter, r *http.Request) {
	if !a.parsePost(w, r) {
		return
	}
	id, err := parseID(r.PostFormValue("id"))
	if err != nil {
		a.renderPostError(w, r, err.Error())
		return
	}
	start := r.PostFormValue("start_date")
	completion := r.PostFormValue("completion_date")
	if err := a.store.setSchedule(id, &start, &completion); err != nil {
		a.renderPostError(w, r, err.Error())
		return
	}
	redirect := postReturnURL(r, r.PostFormValue("q"), r.PostFormValue("all") == "1") + "#task-" + strconv.FormatInt(id, 10)
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (a *webApp) updateURL(w http.ResponseWriter, r *http.Request) {
	if !a.parsePost(w, r) {
		return
	}
	id, err := parseID(r.PostFormValue("id"))
	if err != nil {
		a.renderPostError(w, r, err.Error())
		return
	}
	switch r.PostFormValue("action") {
	case "add":
		err = a.store.addURL(id, r.PostFormValue("url"))
	case "remove":
		err = a.store.removeURL(id, r.PostFormValue("url"))
	default:
		err = fmt.Errorf("choose a valid URL action")
	}
	if err != nil {
		a.renderPostError(w, r, err.Error())
		return
	}
	redirect := postReturnURL(r, r.PostFormValue("q"), r.PostFormValue("all") == "1") + "#task-" + strconv.FormatInt(id, 10)
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (a *webApp) renderPostError(w http.ResponseWriter, r *http.Request, message string) {
	if id, err := parseID(r.PostFormValue("view_id")); err == nil {
		a.renderDetail(w, r, id, http.StatusBadRequest, message)
		return
	}
	a.render(w, r, http.StatusBadRequest, message)
}

func postReturnURL(r *http.Request, query string, showAll bool) string {
	if id, err := parseID(r.PostFormValue("view_id")); err == nil {
		path := "/tasks/" + strconv.FormatInt(id, 10)
		if values, ok := r.PostForm["open"]; ok {
			return path + "?" + url.Values{"open": {values[0]}}.Encode()
		}
		return path
	}
	if values, ok := r.PostForm["open"]; ok {
		return listURL(query, showAll, values[0])
	}
	return listURL(query, showAll)
}

func listURL(query string, showAll bool, open ...string) string {
	values := url.Values{}
	if query != "" {
		values.Set("q", query)
	}
	if showAll {
		values.Set("all", "1")
	}
	if len(open) != 0 {
		values.Set("open", open[0])
	}
	if len(values) == 0 {
		return "/"
	}
	return "/?" + values.Encode()
}

func serveWeb(s *store, port int, out io.Writer) error {
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	a, err := newWebApp(s, address)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}
	fmt.Fprintf(out, "Trackerator web UI: http://%s\n", address)
	server := &http.Server{Handler: a.handler(), ReadHeaderTimeout: 5 * time.Second}
	return server.Serve(listener)
}
