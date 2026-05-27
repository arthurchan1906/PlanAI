package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
)

//go:embed frontend/dist
var uiFS embed.FS

func runWebServer() {
	cfg := loadConfig()
	addr := fmt.Sprintf("%s:%d", cfg.WebHost, cfg.WebPort)

	// Strip "frontend/dist/" prefix from embedded files
	staticFS, err := fs.Sub(uiFS, "frontend/dist")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load embedded UI: %v\n", err)
		os.Exit(1)
	}

	apiMux := http.NewServeMux()

	// API routes
	apiMux.HandleFunc("/pmai/", func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		handleAPI(w, r)
	})
	apiMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		sendJSON(w, map[string]any{"status": "ok"})
	})

	// Wrap: SPA fallback for non-API GET requests
	fileServer := http.FileServer(http.FS(staticFS))
	wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API requests
		if strings.HasPrefix(r.URL.Path, "/pmai/") || r.URL.Path == "/health" {
			setCORS(w)
			if r.Method == http.MethodOptions {
				w.WriteHeader(204)
				return
			}
			apiMux.ServeHTTP(w, r)
			return
		}

		// Only GET/HEAD for UI
		if r.Method != "GET" && r.Method != "HEAD" {
			http.NotFound(w, r)
			return
		}

		// Try to serve exact file
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Serve index.html for the root path
		if path == "index.html" {
			data, err := fs.ReadFile(staticFS, "index.html")
			if err == nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write(data)
				return
			}
		}

		// For other paths, check if it's a real file, otherwise SPA fallback
		f, err := staticFS.Open(path)
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for client-side routing
		data, err := fs.ReadFile(staticFS, "index.html")
		if err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
			return
		}

		http.NotFound(w, r)
	})

	fmt.Printf("AIPM web server listening on http://%s\n", addr)
	if err := http.ListenAndServe(addr, wrapper); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func sendJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func sendError(w http.ResponseWriter, status int, detail string) {
	w.WriteHeader(status)
	sendJSON(w, map[string]any{"detail": detail})
}

func handleGetEntity(w http.ResponseWriter, entity, id string) {
	switch entity {
	case "tasks":
		t, err := getTask(id)
		if err != nil {
			sendError(w, 404, err.Error())
			return
		}
		sendJSON(w, t)
	case "commits":
		c, err := getCommit(id)
		if err != nil {
			sendError(w, 404, err.Error())
			return
		}
		sendJSON(w, c)
	case "plans":
		p, err := getPlan(id)
		if err != nil {
			sendError(w, 404, err.Error())
			return
		}
		sendJSON(w, p)
	case "bugs":
		b, err := getBug(id)
		if err != nil {
			sendError(w, 404, err.Error())
			return
		}
		sendJSON(w, b)
	case "decisions":
		d, err := getDecision(id)
		if err != nil {
			sendError(w, 404, err.Error())
			return
		}
		sendJSON(w, d)
	case "ideas":
		i, err := getIdea(id)
		if err != nil {
			sendError(w, 404, err.Error())
			return
		}
		sendJSON(w, i)
	case "roadmaps":
		r, err := getRoadmap(id)
		if err != nil {
			sendError(w, 404, err.Error())
			return
		}
		sendJSON(w, r)
	case "principles":
		p, err := getPrinciple(id)
		if err != nil {
			sendError(w, 404, err.Error())
			return
		}
		sendJSON(w, p)
	case "visions":
		v, err := getVision(id)
		if err != nil {
			sendError(w, 404, err.Error())
			return
		}
		sendJSON(w, v)
	default:
		sendError(w, 404, fmt.Sprintf("unknown entity: %s", entity))
	}
}

func handlePatchEntity(w http.ResponseWriter, entity, id string, body map[string]any) {
	switch entity {
	case "tasks":
		task, err := updateTask(id, pstr(body, "status", ""), pstr(body, "note", ""), false, false)
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, task)
	case "commits":
		c, err := updateCommit(id, body)
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, c)
	case "plans":
		p, err := updatePlan(id, body)
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, p)
	case "bugs":
		b, err := updateBug(id, body)
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, b)
	case "decisions":
		d, err := updateDecisionStatus(id, pstr(body, "status", ""))
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, d)
	case "ideas":
		if _, hasNote := body["note"]; hasNote {
			idea, err := reviewIdea(id, str(body["status"]), str(body["note"]))
			if err != nil {
				sendError(w, 400, err.Error())
				return
			}
			sendJSON(w, idea)
		} else {
			idea, err := updateIdea(id, body)
			if err != nil {
				sendError(w, 400, err.Error())
				return
			}
			sendJSON(w, idea)
		}
	case "roadmaps":
		r, err := updateRoadmap(id, body)
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, r)
	case "principles":
		p, err := updatePrinciple(id, body)
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, p)
	case "visions":
		v, err := updateVision(id, body)
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, v)
	default:
		sendError(w, 404, fmt.Sprintf("unknown entity: %s", entity))
	}
}

func parseEntityID(path string) (entity, id string) {
	parts := strings.SplitN(path, "/", 2)
	entity = parts[0]
	if len(parts) > 1 {
		id = parts[1]
	}
	return
}

func extractID(path, prefix, suffix string) string {
	s := strings.TrimPrefix(path, prefix)
	s = strings.TrimSuffix(s, suffix)
	return s
}

func pstr(m map[string]any, key, def string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

// ---- API handler ----

func handleAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/pmai")
	path = strings.TrimPrefix(path, "/")

	defer func() {
		if rec := recover(); rec != nil {
			sendError(w, 500, fmt.Sprintf("%v", rec))
		}
	}()

	readBody := func() map[string]any {
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		if len(b) > 0 {
			json.Unmarshal(b, &body)
		}
		if body == nil {
			body = map[string]any{}
		}
		return body
	}

	method := r.Method
	q := r.URL.Query()

	switch {
	case method == "GET" && path == "tasks":
		tasks, _ := listTasks(q.Get("status"), q.Get("plan_id"))
		sendJSON(w, map[string]any{"tasks": tasks})
	case method == "GET" && path == "commits":
		commits, _ := listCommits(q.Get("status"), q.Get("task_id"), q.Get("decision_id"), "", 0)
		sendJSON(w, map[string]any{"commits": commits})
	case method == "GET" && path == "plans":
		plans, _ := listPlans(q.Get("roadmap_id"), q.Get("status"))
		sendJSON(w, map[string]any{"plans": plans})
	case method == "GET" && path == "bugs":
		bugs, _ := listBugs(q.Get("status"), q.Get("severity"), q.Get("commit_id"), 0)
		sendJSON(w, map[string]any{"bugs": bugs})
	case method == "GET" && path == "decisions":
		decs, _ := listDecisions()
		sendJSON(w, map[string]any{"decisions": decs})
	case method == "GET" && path == "ideas":
		ideas, _ := listIdeas(q.Get("status"))
		sendJSON(w, map[string]any{"ideas": ideas})
	case method == "GET" && path == "roadmaps":
		rds, _ := listRoadmaps(q.Get("vision_id"))
		sendJSON(w, map[string]any{"roadmaps": rds})
	case method == "GET" && path == "principles":
		prs, _ := listPrinciples(q.Get("status"), q.Get("kind"))
		sendJSON(w, map[string]any{"principles": prs})
	case method == "GET" && path == "docs":
		docs, _ := listDocRecords(q.Get("status"), q.Get("layer"))
		sendJSON(w, map[string]any{"docs": docs})
	case method == "GET" && path == "visions":
		visions, _ := listVisions()
		sendJSON(w, map[string]any{"visions": visions})
	case method == "GET" && path == "links":
		links, _ := listLinks(q.Get("source_id"), q.Get("target_id"), q.Get("relation"))
		sendJSON(w, map[string]any{"links": links})
	case method == "GET" && path == "search":
		sendJSON(w, searchProjectContext(q.Get("q"), 8))
	case method == "GET" && path == "dashboard":
		sendJSON(w, getStatusSnapshot())
	case method == "GET" && path == "context":
		sendJSON(w, buildContextPack())
	case method == "GET" && path == "next":
		sendJSON(w, buildNextActionPacket())
	case method == "GET" && path == "inbox":
		sendJSON(w, getInboxSummary())
	case method == "GET" && path == "canon":
		c, _ := getCanon()
		sendJSON(w, c)
	case method == "GET" && path == "daily":
		d, _ := getDailyNote(q.Get("date"))
		sendJSON(w, d)
	case method == "GET" && path == "daily/history":
		d, _ := listDailyNotes()
		sendJSON(w, map[string]any{"daily_notes": d})
	case method == "POST" && path == "tasks":
		body := readBody()
		task, err := createTask(str(body["title"]), pstr(body, "priority", "P1"), pstr(body, "status", "todo"), pstr(body, "phase", "general"), str(body["plan_id"]), nil)
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, task)
	case method == "POST" && path == "commits":
		body := readBody()
		c, err := createCommit(str(body["title"]), pstr(body, "summary", ""), pstr(body, "evidence_summary", ""), pstr(body, "review_notes", ""), pstr(body, "branch", ""), pstr(body, "commit_hash", ""), str(body["task_id"]), pstr(body, "decision_id", ""), pstr(body, "status", "draft"), pstr(body, "test_status", "not_run"), pstr(body, "review_status", "pending"), nil)
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, c)
	case method == "POST" && path == "plans":
		body := readBody()
		plan, err := createPlan(str(body["title"]), pstr(body, "goal", ""), str(body["roadmap_id"]), pstr(body, "vision_id", ""), pstr(body, "priority", "P1"), pstr(body, "status", "draft"), nil, nil, nil, nil)
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, plan)
	case method == "POST" && path == "bugs":
		body := readBody()
		bug, err := createBug(str(body["title"]), pstr(body, "description", ""), pstr(body, "severity", "minor"), pstr(body, "status", "open"), pstr(body, "commit_id", ""), pstr(body, "error", ""), pstr(body, "files", ""), pstr(body, "root_cause", ""), pstr(body, "fix", ""), pstr(body, "tags", ""))
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, bug)
	case method == "POST" && path == "decisions":
		body := readBody()
		d, err := createDecision(str(body["title"]), str(body["background"]), str(body["decision"]), pstr(body, "status", "proposed"))
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, d)
	case method == "POST" && path == "ideas":
		body := readBody()
		idea, err := createIdea(str(body["title"]), str(body["summary"]), pstr(body, "impact", ""), pstr(body, "source", "manual"), false, pstr(body, "current_summary", ""), pstr(body, "main_question", ""), pstr(body, "recommended_next_action", "continue_discussion"))
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, idea)
	case method == "POST" && path == "roadmaps":
		body := readBody()
		r, err := createRoadmap(str(body["title"]), pstr(body, "target_date", ""), pstr(body, "vision_id", ""), pstr(body, "status", "planned"), pstr(body, "priority", "P1"))
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, r)
	case method == "POST" && path == "principles":
		body := readBody()
		p, err := createPrinciple(str(body["title"]), pstr(body, "summary", ""), pstr(body, "kind", "governance"), pstr(body, "status", "active"))
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, p)
	case method == "POST" && path == "visions":
		body := readBody()
		v, err := createVision(str(body["title"]), pstr(body, "summary", ""), pstr(body, "status", "active"), pstr(body, "horizon", "long_term"))
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, v)
	case method == "POST" && path == "links":
		body := readBody()
		link, err := createLink(str(body["source_type"]), str(body["source_id"]), str(body["relation"]), str(body["target_type"]), str(body["target_id"]), pstr(body, "note", ""))
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, link)
	case method == "POST" && strings.HasPrefix(path, "tasks/") && strings.HasSuffix(path, "/notes"):
		id := extractID(path, "tasks/", "/notes")
		body := readBody()
		result, err := appendTaskNote(id, str(body["content"]))
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, result)
	case method == "POST" && strings.HasPrefix(path, "ideas/") && strings.HasSuffix(path, "/comments"):
		id := extractID(path, "ideas/", "/comments")
		body := readBody()
		comment, err := createIdeaComment(id, str(body["content"]), pstr(body, "kind", "comment"), pstr(body, "author_type", "ai"), pstr(body, "author_name", "aipmc"))
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, comment)
	case method == "POST" && strings.HasPrefix(path, "ideas/") && strings.HasSuffix(path, "/convert"):
		id := extractID(path, "ideas/", "/convert")
		body := readBody()
		if str(body["to"]) == "task" {
			result, err := convertIdeaToTask(id, str(body["plan_id"]))
			if err != nil {
				sendError(w, 400, err.Error())
				return
			}
			sendJSON(w, result)
		} else {
			result, err := convertIdeaToDecision(id)
			if err != nil {
				sendError(w, 400, err.Error())
				return
			}
			sendJSON(w, result)
		}
	case method == "GET":
		entity, id := parseEntityID(path)
		handleGetEntity(w, entity, id)
	case method == "PATCH":
		entity, id := parseEntityID(path)
		handlePatchEntity(w, entity, id, readBody())
	case method == "DELETE" && strings.HasPrefix(path, "links/"):
		id := strings.TrimPrefix(path, "links/")
		err := deleteLink(id)
		if err != nil {
			sendError(w, 500, err.Error())
			return
		}
		sendJSON(w, map[string]any{"ok": true})
	default:
		sendError(w, 404, fmt.Sprintf("not found: %s %s", method, path))
	}
}
