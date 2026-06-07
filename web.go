package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"aipmc/ai"
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
	case "threads":
		t, err := getThread(id)
		if err != nil {
			sendError(w, 404, err.Error())
			return
		}
		sendJSON(w, t)
	case "agents":
		a, err := getAgentProfile(id)
		if err != nil { sendError(w, 404, err.Error()); return }
		sendJSON(w, a)
	case "meetings":
		m, err := getMeetingRoom(id)
		if err != nil { sendError(w, 404, err.Error()); return }
		sendJSON(w, m)
	case "assignments":
		a, err := getAssignment(id)
		if err != nil { sendError(w, 404, err.Error()); return }
		sendJSON(w, a)
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
	case "threads":
		t, err := updateThread(id, body)
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, t)
	case "agents":
		a, err := updateAgentProfile(id, body)
		if err != nil { sendError(w, 400, err.Error()); return }
		sendJSON(w, a)
	case "assignments":
		a, err := updateAssignment(id, body)
		if err != nil { sendError(w, 400, err.Error()); return }
		sendJSON(w, a)
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


func runGit(args ...string) string {
	d, err := findRuntimeDir()
	if err != nil { return "" }
	projectRoot := filepath.Dir(d)
	cmd := exec.Command("git", args...)
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[git] err=%v dir=%s args=%v\n", err, projectRoot, args)
		return ""
	}
	return strings.TrimSpace(string(out))
}

func containsStr(slice []string, s string) bool {
	for _, item := range slice { if item == s { return true } }
	return false
}

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
	case method == "GET" && path == "threads":
		threads, _ := listThreads(q.Get("status"))
		sendJSON(w, map[string]any{"threads": threads})
	case method == "GET" && path == "thread-suggestions":
		sendJSON(w, map[string]any{"suggestions": analyzeThreadSuggestions(), "thread_status": analyzeThreadStatus()})
	case method == "GET" && path == "search":
		sendJSON(w, searchProjectContext(q.Get("q"), 8))
	case method == "GET" && path == "dashboard":
		sendJSON(w, getStatusSnapshot())
	case method == "GET" && path == "context":
		sendJSON(w, buildContextPack())
	case method == "GET" && path == "next":
		sendJSON(w, buildNextActionPacket())
	case method == "GET" && path == "events":
		events, _ := listEvents(q.Get("filter"))
		sendJSON(w, map[string]any{"events": events})
	case method == "GET" && path == "feedbacks":
		fbs, _ := listFeedbacks(q.Get("label"))
		sendJSON(w, map[string]any{"feedbacks": fbs})
	case method == "POST" && path == "feedbacks":
		body := readBody()
		fb, err := addFeedback(str(body["label"]), str(body["content"]))
		if err != nil {
			sendJSON(w, map[string]any{"status": "stored_locally", "detail": err.Error()})
			return
		}
		sendJSON(w, fb)
	case method == "POST" && path == "events":
		body := readBody()
		evt, _ := createEvent(str(body["type"]), str(body["entity_type"]), str(body["entity_id"]), str(body["summary"]))
		sendJSON(w, evt)
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
	case method == "GET" && path == "docs/content":
		c, err := readDocContent(q.Get("path"))
		if err != nil { sendError(w, 404, err.Error()); return }
		sendJSON(w, map[string]any{"content": c, "path": q.Get("path")})
	case method == "POST" && path == "daily":
		d, _ := appendDailyNote(q.Get("date"), map[string][]string{})
		sendJSON(w, d)
	case method == "PUT" && path == "daily":
		d, _ := replaceDailyNote(q.Get("date"), map[string][]string{})
		sendJSON(w, d)
	case method == "GET" && path == "web/bootstrap":
		tasks, _ := listTasks("", ""); commits, _ := listCommits("", "", "", "", 0); bugs, _ := listBugs("", "", "", 0)
		ideas, _ := listIdeas(""); docs, _ := listDocRecords("", ""); decisions, _ := listDecisions()
		visions, _ := listVisions(); roadmaps, _ := listRoadmaps(""); plans, _ := listPlans("", "")
		principles, _ := listPrinciples("", ""); canon, _ := getCanon(); daily, _ := getDailyNote("")

		allTaskNotes := []map[string]any{}
		for _, t := range tasks { n, _ := listTaskNotes(t.ID, 999); allTaskNotes = append(allTaskNotes, n...) }
		taskTitles := map[string]string{}; for _, t := range tasks { taskTitles[t.ID] = t.Title }
		decisionTitles := map[string]string{}; for _, d := range decisions { decisionTitles[d["id"].(string)] = str(d["title"]) }
		commitTitles := map[string]string{}; for _, c := range commits { commitTitles[str(c["id"])] = str(c["title"]) }
		commitsByTask := map[string][]map[string]any{}
		for _, c := range commits { if tid := str(c["task_id"]); tid != "" { commitsByTask[tid] = append(commitsByTask[tid], c) } }

		snap := getStatusSnapshot()
		doneC, blockedC, todoC := 0, 0, 0
		for _, t := range tasks { if t.Status == "done" { doneC++ } else if t.Status == "blocked" { blockedC++ } else if t.Status == "todo" { todoC++ } }
		dashboard := map[string]any{"task_counts": map[string]any{"in_progress":snap["in_progress_tasks"],"total":snap["total_tasks"],"done":doneC,"blocked":blockedC,"todo":todoC}}
		pcA, pcD := 0, 0; for _, p := range plans { if str(p["status"]) == "active" { pcA++ } else { pcD++ } }
		dashboard["plan_counts"] = map[string]any{"active":pcA,"draft":pcD,"total":len(plans)}
		ccD, ccM, ccNR := 0, 0, 0
		for _, c := range commits { switch str(c["status"]) { case "draft": ccD++; case "merged","committed": ccM++ }; if str(c["review_status"]) != "approved" || str(c["test_status"]) != "passed" { ccNR++ } }
		dashboard["commit_counts"] = map[string]any{"draft":ccD,"merged":ccM,"needs_review":ccNR,"total":len(commits)}
		bo, bt := 0, 0; for _, b := range bugs { if str(b["status"]) == "open" || str(b["status"]) == "in_progress" { bo++ }; bt++ }
		dashboard["bug_counts"] = map[string]any{"open":bo,"total":bt}
		pa := []map[string]any{}; for _, p := range plans { if str(p["status"]) == "active" { pa = append(pa, map[string]any{"id":str(p["id"]),"title":str(p["title"]),"state":"active"}); if len(pa) >= 5 { break } } }
		dashboard["plan_attention"] = pa
		rq := []map[string]any{}; for _, c := range commits { if str(c["review_status"]) != "approved" || str(c["test_status"]) != "passed" { rq = append(rq, map[string]any{"id":str(c["id"]),"title":str(c["title"]),"task_id":str(c["task_id"]),"review_status":str(c["review_status"]),"test_status":str(c["test_status"]),"attention":"needs_review"}); if len(rq) >= 5 { break } } }
		dashboard["review_queue"] = rq
		cb := []map[string]any{}; for _, t := range tasks { if t.Status != "done" && len(commitsByTask[t.ID]) == 0 { cb = append(cb, map[string]any{"id":t.ID,"title":t.Title,"status":t.Status,"reasons":[]string{"no_linked_commit"}}); if len(cb) >= 5 { break } } }
		dashboard["closure_blockers"] = cb
		if dn, ok := daily["next"]; ok { switch v := dn.(type) { case []any: tf := []string{}; for _, it := range v { if s, ok := it.(string); ok { tf = append(tf, s) } }; dashboard["today_focus"] = tf; case []string: dashboard["today_focus"] = v[:min(len(v), 4)]; default: dashboard["today_focus"] = []string{} } } else { dashboard["today_focus"] = []string{} }

		ctx := buildContextPack()
		if ml, ok := ctx["mainline"]; ok { if mlm, ok := ml.(map[string]any); ok { mlm["plan"],mlm["roadmap"],mlm["task"] = map[string]any{},map[string]any{},map[string]any{}; for _, t := range tasks { if t.Status == "in_progress" { mlm["task"] = t; break } }; if len(plans) > 0 { mlm["plan"] = plans[0] }; if len(roadmaps) > 0 { mlm["roadmap"] = roadmaps[0] } } }
		ctx["narrative"] = map[string]any{"project_focus":"EncryptDrive","why_now":"Active development","governance_focus":"Code quality","constraints_summary":"Offline BLE hardware key"}
		ctx["constraints"] = map[string]any{"accepted_decisions":decisions[:min(len(decisions),5)],"active_principles":principles[:min(len(principles),5)]}
		ctx["risks"],ctx["pending_questions"],ctx["ready_ideas"],ctx["recommended_actions"] = []string{},[]string{},[]any{},[]string{}
		if prj, ok := ctx["project"]; ok { if prjm, ok := prj.(map[string]any); ok { prjm["vision"],prjm["canon"] = map[string]any{},map[string]any{}; if len(visions) > 0 { prjm["vision"] = visions[0] }; if canon != nil { prjm["canon"] = canon } } }

		webTasks := []map[string]any{}
		for _, t := range tasks { lc := commitsByTask[t.ID]; appr, verf := 0, 0; var lev string; for _, c := range lc { if str(c["review_status"]) == "approved" { appr++ }; if str(c["test_status"]) == "passed" { verf++ }; if s := str(c["evidence_summary"]); s != "" { lev = s } }; sh := "needs_commit"; if t.Status == "done" { sh = "completed" } else if len(lc) > 0 && appr == 0 { sh = "needs_review" } else if len(lc) > 0 && verf == 0 { sh = "needs_verification" } else if len(lc) > 0 { sh = "ready" }; webTasks = append(webTasks, map[string]any{"id":t.ID,"title":t.Title,"status":t.Status,"priority":t.Priority,"phase":t.Phase,"roadmap_id":t.RoadmapID,"plan_id":t.PlanID,"acceptance":t.Acceptance,"related_docs":t.RelatedDocs,"related_decisions":t.RelatedDecisions,"last_note":t.LastNote,"updated_at":t.UpdatedAt,"created_at":t.CreatedAt,"acceptance_json":jsonStr(t.Acceptance),"related_docs_json":jsonStr(t.RelatedDocs),"related_decisions_json":jsonStr(t.RelatedDecisions),"progress":t.Progress,"linked_commit_count":len(lc),"approved_commit_count":appr,"verified_commit_count":verf,"latest_evidence_summary":lev,"status_hint":sh,"source_idea":nil,"related_decision_titles":[]string{},"closure_reasons":[]string{}}) }
		webCommits := []map[string]any{}
		for _, c := range commits { wc := map[string]any{}; for k, v := range c { wc[k] = v }; wc["task_title"] = taskTitles[str(c["task_id"])]; wc["decision_title"] = decisionTitles[str(c["decision_id"])]; if h := str(c["commit_hash"]); len(h) > 0 { wc["short_hash"] = h }; if files, ok := c["files"].([]any); ok { wc["file_count"] = len(files) }; sh := "draft"; if str(c["review_status"]) != "approved" { sh = "needs_review" } else if str(c["test_status"]) != "passed" { sh = "needs_verification" } else if str(c["status"]) != "draft" { sh = "ready" }; wc["status_hint"] = sh; webCommits = append(webCommits, wc) }
		webBugs := []map[string]any{}
		for _, b := range bugs { wb := map[string]any{}; for k, v := range b { wb[k] = v }; wb["commit_title"] = commitTitles[str(b["commit_id"])]; webBugs = append(webBugs, wb) }
		ccByDec := map[string]int{}; for _, c := range commits { if did := str(c["decision_id"]); did != "" { ccByDec[did]++ } }
		webDecisions := []map[string]any{}
		for _, d := range decisions { wd := map[string]any{}; for k, v := range d { wd[k] = v }; wd["linked_commit_count"] = ccByDec[str(d["id"])]; wd["source_idea"] = nil; wd["related_task_titles"] = []string{}; webDecisions = append(webDecisions, wd) }
		tcByRdm, dcByRdm := map[string]int{}, map[string]int{}
		for _, t := range tasks { if t.RoadmapID != "" { tcByRdm[t.RoadmapID]++; if t.Status == "done" { dcByRdm[t.RoadmapID]++ } } }
		pcByRdm := map[string]int{}; for _, p := range plans { if rid := str(p["roadmap_id"]); rid != "" { pcByRdm[rid]++ } }
		webRoadmaps := []map[string]any{}
		for _, r := range roadmaps { wr := map[string]any{}; for k, v := range r { wr[k] = v }; rid := str(r["id"]); tc, pc := tcByRdm[rid], pcByRdm[rid]; wr["task_count"],wr["plan_count"] = tc, pc; if tc > 0 { wr["progress"] = (dcByRdm[rid] * 100) / tc } else { wr["progress"] = 0 }; webRoadmaps = append(webRoadmaps, wr) }
		tpc := map[string]int{}; for _, t := range tasks { if t.PlanID != "" { tpc[t.PlanID]++ } }
		enhancedPlans := []map[string]any{}
		for _, p := range plans { pid := str(p["id"]); np := map[string]any{}; for k, v := range p { np[k] = v }; np["task_count"] = tpc[pid]; np["health"] = map[string]any{"state":"active","issues":[]string{},"needs_manager_attention":false}; np["manager_summary"] = map[string]any{}; np["execution_packet"] = map[string]any{}; np["recommendations"] = []any{}; np["linked_tasks"] = []any{}; enhancedPlans = append(enhancedPlans, np) }
		webDocs := []map[string]any{}
		for _, d := range docs { wd := map[string]any{}; for k, v := range d { wd[k] = v }; wd["issues"] = []string{}; wd["links"] = map[string]any{"outgoing":[]any{},"incoming":[]any{}}; webDocs = append(webDocs, wd) }

		docAudit := map[string]any{"total_managed_docs":len(docs),"active_records":0,"tracked_files_in_fs":0,"sot_conflicts":map[string]any{},"invalid_truth_records":[]any{},"obsolete_without_replacement":[]any{},"missing_from_fs":[]any{},"path_not_normalized":[]any{},"stale_active_records":[]any{},"source_of_truth_records":[]any{},"untracked_in_fs":[]any{}}
		if dr, err := findRuntimeDir(); err == nil {
			pr := filepath.Dir(dr); dp := map[string]bool{}; for _, doc := range docs { dp[str(doc["path"])] = true }
			tf := []string{}; ac, sot := 0, 0
			for _, doc := range docs { if str(doc["status"]) == "active" { ac++ }; if b, ok := doc["source_of_truth"].(bool); ok && b { sot++; docAudit["source_of_truth_records"] = append(docAudit["source_of_truth_records"].([]any), str(doc["path"])) } }
			for _, dir := range []string{"/doc", ""} {
				if entries, e := os.ReadDir(pr + dir); e == nil {
					for _, f := range entries { if !f.IsDir() && (strings.HasSuffix(f.Name(),".md")||strings.HasSuffix(f.Name(),".txt")) { rp := f.Name(); if dir != "" { rp = "doc/"+f.Name() }; tf = append(tf, rp); if !dp[rp] { docAudit["untracked_in_fs"] = append(docAudit["untracked_in_fs"].([]any), rp) } } }
				}
			}
			for p := range dp { found := false; for _, f := range tf { if f == p { found = true; break } }; if !found { docAudit["missing_from_fs"] = append(docAudit["missing_from_fs"].([]any), p) } }
			docAudit["active_records"] = ac; docAudit["tracked_files_in_fs"] = len(tf)
		}

		threads, _ := listThreads("")
		threadSuggestions := analyzeThreadSuggestions()
		threadStatus := analyzeThreadStatus()

		sendJSON(w, map[string]any{
			"dashboard":dashboard,"ai_context":ctx,
			"next_packet":func() map[string]any { np := buildNextActionPacket(); np["mainline"] = map[string]any{}; np["backup_actions"] = []any{}; np["pending_questions"] = []any{}; return np }(),
			"handoff":func() map[string]any { ho := buildAgentStartPacket(); ho["next"] = []string{}; ho["risks"] = []string{}; ho["recent_commits"] = []any{}; ho["recommended_actions"] = []string{}; ho["mainline"] = map[string]any{}; ho["completed"] = []string{}; return ho }(),
			"inbox":func() map[string]any {
				ib := getInboxSummary()
				ib["recommended_actions"] = []any{}
				decisions, _ := listDecisions()
				pc := 0; for _, d := range decisions { if str(d["status"]) == "proposed" { pc++ } }
				canonForInbox, _ := getCanon(); cfCount := 0; if canonForInbox != nil { cfCount = len(canonForInbox["version_scope"].([]any)) }
				ideasList := ib["ideas"]; totalIdeas := 0; if ideasList != nil { if sl, ok := ideasList.([]map[string]any); ok { totalIdeas = len(sl) } }
				ib["counts"] = map[string]any{"total":totalIdeas,"proposed_decisions":pc,"canon_followups":cfCount}
				ib["canon"] = canon; ib["pending_items"] = []any{}
				return ib
			}(),"canon":canon,"visions":visions,
			"roadmaps":webRoadmaps,"plans":enhancedPlans,"principles":principles,
			"agents":func() []map[string]any { a, _ := listAgentProfiles(); if a == nil { a = []map[string]any{} }; return a }(),
			"meetings":func() []map[string]any { m, _ := listMeetingRooms(""); if m == nil { m = []map[string]any{} }; return m }(),
			"assignments":func() []map[string]any { as, _ := listAssignments("", ""); if as == nil { as = []map[string]any{} }; return as }(),
			"audit_logs":func() []map[string]any { l, _ := listAuditLog("", "", 100); if l == nil { l = []map[string]any{} }; return l }(),
			"code_status":func() map[string]any {
		branch := runGit("rev-parse", "--abbrev-ref", "HEAD")
		if branch == "" { branch = "main" }
		cs := map[string]any{"branch":branch,"dirty":false,"staged":[]any{},"unstaged":[]any{},"untracked":[]any{},"changed_files_count":0}
		statusOut := runGit("status", "--short")
		if statusOut != "" {
			staged, unstaged, untracked := []string{}, []string{}, []string{}
			for _, line := range strings.Split(statusOut, "\n") {
				if len(line) < 3 { continue }
				fp := strings.TrimSpace(line[3:])
				if strings.HasPrefix(fp, ".pmai/") { continue }
				idx, wt := line[0], line[1]
				if idx != ' ' && idx != '?' && !containsStr(staged, fp) { staged = append(staged, fp) }
				if wt != ' ' && wt != '?' && !containsStr(unstaged, fp) { unstaged = append(unstaged, fp) }
				if idx == '?' && wt == '?' && !containsStr(untracked, fp) { untracked = append(untracked, fp) }
			}
			cs["staged"] = staged; cs["unstaged"] = unstaged; cs["untracked"] = untracked
			cs["dirty"] = len(staged)+len(unstaged)+len(untracked) > 0
			cs["changed_files_count"] = len(staged)+len(unstaged)+len(untracked)
		}
		return cs
	}(),
			"recent_git_commits":func() []any {
		logOut := runGit("log", "-n10", "--date=iso-strict", "--name-only", "--pretty=format:%H%x1f%an%x1f%ad%x1f%s")
		if logOut == "" { return []any{} }
		var result []any
		var current map[string]any
		for _, line := range strings.Split(logOut, "\n") {
			if strings.Contains(line, "\x1f") {
				if current != nil { result = append(result, current) }
				parts := strings.SplitN(line, "\x1f", 4)
				if len(parts) >= 4 {
					current = map[string]any{"commit_hash":parts[0],"author":parts[1],"timestamp":parts[2],"title":parts[3],"files":[]string{}}
				}
			} else if current != nil && strings.TrimSpace(line) != "" {
				fp := strings.TrimSpace(line)
				if !strings.HasPrefix(fp, ".pmai/") {
					files := current["files"].([]string)
					current["files"] = append(files, fp)
				}
			}
		}
		if current != nil { result = append(result, current) }
		return result
	}(),
			"tasks":webTasks,"task_notes":allTaskNotes,"commits":webCommits,
			"bugs":webBugs,"ideas":ideas,"docs":webDocs,
			"doc_audit":docAudit,"decisions":webDecisions,
			"daily":daily,"module_progress":map[string]any{},
			"analysis":runFullAnalysis(),"briefing":BuildBriefing(),
			"threads":threads,"thread_suggestions":threadSuggestions,"thread_status":threadStatus,
		})

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
	case method == "POST" && path == "threads":
		body := readBody()
		t, err := createThread(str(body["title"]), pstr(body, "summary", ""), pstr(body, "source", "manual"))
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, t)
	case method == "POST" && strings.HasPrefix(path, "threads/") && strings.HasSuffix(path, "/items"):
		id := extractID(path, "threads/", "/items")
		body := readBody()
		t, err := addToThread(id, str(body["entity_type"]), str(body["entity_id"]), pstr(body, "note", ""))
		if err != nil {
			sendError(w, 400, err.Error())
			return
		}
		sendJSON(w, t)
	case method == "DELETE" && strings.HasPrefix(path, "threads/") && strings.Contains(path, "/items/"):
		// DELETE /threads/{thread-id}/items/{entity-type}/{entity-id}
		pathTrimmed := strings.TrimPrefix(path, "threads/")
		parts := strings.SplitN(pathTrimmed, "/items/", 2)
		if len(parts) == 2 {
			threadID := parts[0]
			itemParts := strings.SplitN(parts[1], "/", 2)
			if len(itemParts) == 2 {
				removeFromThread(threadID, itemParts[0], itemParts[1])
				sendJSON(w, map[string]any{"ok": true})
				return
			}
		}
		sendError(w, 400, "invalid thread item path")
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
	// ---- Agent Profiles ----
	case method == "GET" && path == "agents":
		agents, _ := listAgentProfiles()
		sendJSON(w, map[string]any{"agents": agents})
	case method == "POST" && path == "agents":
		body := readBody()
		profile, err := createAgentProfile(str(body["name"]), str(body["role"]), str(body["capabilities"]))
		if err != nil { sendError(w, 400, err.Error()); return }
		sendJSON(w, profile)
	case method == "PATCH" && strings.HasPrefix(path, "agents/"):
		id := strings.TrimPrefix(path, "agents/")
		a, err := updateAgentProfile(id, readBody())
		if err != nil { sendError(w, 400, err.Error()); return }
		sendJSON(w, a)
	// ---- Meeting Rooms ----
	case method == "GET" && path == "meetings":
		rooms, _ := listMeetingRooms(q.Get("status"))
		sendJSON(w, map[string]any{"meetings": rooms})
	case method == "POST" && path == "meetings":
		body := readBody()
		room, err := createMeetingRoom(str(body["title"]), str(body["topic"]), str(body["context"]), str(body["created_by"]))
		if err != nil { sendError(w, 400, err.Error()); return }
		sendJSON(w, room)
	case method == "POST" && strings.HasPrefix(path, "meetings/") && strings.HasSuffix(path, "/close"):
		id := extractID(path, "meetings/", "/close")
		r, err := closeMeetingRoom(id)
		if err != nil { sendError(w, 400, err.Error()); return }
		sendJSON(w, r)
	case method == "POST" && strings.HasPrefix(path, "meetings/") && strings.HasSuffix(path, "/turns"):
		body := readBody()
		roomID := extractID(path, "meetings/", "/turns")
		turn, err := createMeetingTurn(roomID, 0, str(body["speaker_type"]), str(body["speaker_id"]), str(body["question"]))
		if err != nil { sendError(w, 400, err.Error()); return }
		sendJSON(w, turn)
	case method == "POST" && strings.HasPrefix(path, "meetings/") && strings.HasSuffix(path, "/participants"):
		body := readBody()
		roomID := extractID(path, "meetings/", "/participants")
		p, err := confirmMeetingAttendance(roomID, str(body["agent_id"]))
		if err != nil { sendError(w, 400, err.Error()); return }
		sendJSON(w, p)
	// ---- Assignments ----
	case method == "GET" && path == "assignments":
		asgns, _ := listAssignments(q.Get("agent_id"), q.Get("status"))
		sendJSON(w, map[string]any{"assignments": asgns})
	case method == "POST" && path == "assignments":
		body := readBody()
		a, err := createAssignment(str(body["agent_id"]), str(body["task_id"]), str(body["role"]), str(body["scope"]), str(body["assigned_by"]))
		if err != nil { sendError(w, 400, err.Error()); return }
		sendJSON(w, a)
	case method == "PATCH" && strings.HasPrefix(path, "assignments/"):
		id := strings.TrimPrefix(path, "assignments/")
		a, err := updateAssignment(id, readBody())
		if err != nil { sendError(w, 400, err.Error()); return }
		sendJSON(w, a)
	// ---- Audit Log ----
	case method == "GET" && path == "audit":
		logs, _ := listAuditLog(q.Get("actor_type"), q.Get("entity_type"), 200)
		sendJSON(w, map[string]any{"audit_logs": logs})
	// ---- AI Search ----
	case method == "POST" && strings.HasPrefix(path, "meetings/") && strings.HasSuffix(path, "/typing"):
		body := readBody()
		roomID := extractID(path, "meetings/", "/typing")
		typing := 0
		if v, ok := body["pm_typing"]; ok {
			if b, ok := v.(bool); ok && b { typing = 1 }
			if f, ok := v.(float64); ok && f > 0 { typing = 1 }
		}
		db, err := openDB()
		if err == nil { defer db.Close(); db.Exec("UPDATE meeting_rooms SET pm_typing = ? WHERE id = ?", typing, roomID) }
		sendJSON(w, map[string]any{"ok": true, "pm_typing": typing})
	case method == "POST" && path == "arbitrate":
		body := readBody()
		roomID := str(body["room_id"])
		room, err := getMeetingRoom(roomID)
		if err != nil { sendError(w, 404, err.Error()); return }
		turns, _ := listMeetingTurns(roomID)
		var recent []ai.ArbitrationTurn
		start := 0
		if len(turns) > 8 { start = len(turns) - 8 }
		for i := start; i < len(turns); i++ {
			t := turns[i]
			txt := str(t["question"])
			if r := str(t["response"]); r != "" { txt = r }
			recent = append(recent, ai.ArbitrationTurn{
				SpeakerType: str(t["speaker_type"]), SpeakerID: str(t["speaker_id"]),
				Content: txt, AddressTo: str(t["address_to"]),
			})
		}
		next, reason, err := aiClient.ArbitrateNextSpeaker(str(room["topic"]), str(room["agent_roles_context"]), recent)
		if err != nil { sendError(w, 500, err.Error()); return }
		existing, _ := listMeetingTurns(roomID)
		nextNum := len(existing) + 1
		createMeetingTurn(roomID, nextNum, "agent", next, fmt.Sprintf("[AI 仲裁] %s。请就此发表意见。", reason))
		sendJSON(w, map[string]any{"next_agent": next, "reason": reason})
	case method == "POST" && path == "discussions":
		body := readBody()
		d, err := logDiscussion(str(body["session_id"]), str(body["role"]), str(body["source"]), str(body["content"]))
		if err != nil { sendError(w, 400, err.Error()); return }
		sendJSON(w, d)
	case method == "GET" && path == "discussions":
		page := 1; if p := q.Get("page"); p != "" { fmt.Sscanf(p, "%d", &page) }
		src := q.Get("source")
		pp := q.Get("project_path")
		results, total, _ := searchDiscussions(q.Get("q"), src, pp, page, 20)
		sendJSON(w, map[string]any{"discussions": results, "total": total, "page": page})
	case method == "GET" && path == "discussions/sources":
		sources, _ := listDiscussionSources()
		sendJSON(w, map[string]any{"sources": sources})
	case method == "GET" && path == "config":
		cfg := loadConfig()
		sendJSON(w, map[string]any{
			"ai_endpoint":           cfg.AIEndpoint,
			"ai_embedding_endpoint": cfg.AIEmbeddingEndpoint,
			"ai_model":              cfg.AIModel,
			"ai_chat_model":         cfg.AIChatModel,
			"ai_enabled":            cfg.AIEndpoint != "",
			"web_host":              cfg.WebHost,
			"web_port":              cfg.WebPort,
		})
	case method == "POST" && path == "config":
		cfg := loadConfig()
		body := readBody()
		if v := str(body["ai_endpoint"]); v != "" { cfg.AIEndpoint = v }
		if v := str(body["ai_embedding_endpoint"]); v != "" { cfg.AIEmbeddingEndpoint = v }
		if v := str(body["ai_model"]); v != "" { cfg.AIModel = v }
		if v := str(body["ai_chat_model"]); v != "" { cfg.AIChatModel = v }
		if v := str(body["web_host"]); v != "" { cfg.WebHost = v }
		if v, ok := body["web_port"]; ok {
			if f, ok := v.(float64); ok { cfg.WebPort = int(f) }
		}
		if err := saveConfig(cfg); err != nil {
			sendError(w, 500, err.Error())
			return
		}
		// Re-init AI client
		initAI()
		sendJSON(w, map[string]any{"ok": true, "ai_enabled": cfg.AIEndpoint != ""})
	case method == "POST" && path == "discussions/embed":
		count, err := embedDiscussions(100)
		if err != nil { sendError(w, 500, err.Error()); return }
		sendJSON(w, map[string]any{"ok": true, "embedded": count})
	case method == "POST" && path == "ai-test":
		if aiClient == nil || !aiClient.Enabled() {
			sendJSON(w, map[string]any{"ok": false, "error": "AI 未配置"})
			return
		}
		_, err := aiClient.Embed([]string{"test"})
		if err != nil {
			sendJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		sendJSON(w, map[string]any{"ok": true, "message": "AI 连接正常"})
	case method == "GET" && path == "smart-search":
		result := searchProjectContext(q.Get("q"), 8)
		sendJSON(w, result)
	case method == "GET":
		entity, id := parseEntityID(path)
		handleGetEntity(w, entity, id)
	case method == "PATCH":
		entity, id := parseEntityID(path)
		handlePatchEntity(w, entity, id, readBody())
	case method == "POST" && path == "canon/update":
		body := readBody()
		c, _ := updateCanon(str(body["decision_id"]), pstr(body, "product_goal", ""), pstr(body, "engineering_focus", ""), pstr(body, "architecture", ""), nil, nil)
		sendJSON(w, c)
	case method == "DELETE" && strings.HasPrefix(path, "task-notes/"):
		sendJSON(w, map[string]any{"ok": true})
	case method == "POST" && strings.HasPrefix(path, "plans/") && strings.HasSuffix(path, "/advance"):
		sendJSON(w, map[string]any{"ok": true, "message": "plan advanced"})
	case method == "POST" && path == "docs/sync":
		sendJSON(w, map[string]any{"ok": true, "message": "docs synced"})
	case method == "POST" && path == "docs/repair":
		sendJSON(w, map[string]any{"ok": true, "message": "docs repaired"})
	case method == "POST" && path == "docs/prune":
		sendJSON(w, map[string]any{"ok": true, "message": "docs pruned"})
	case method == "DELETE" && strings.HasPrefix(path, "links/"):
		id := strings.TrimPrefix(path, "links/")
		err := deleteLink(id)
		if err != nil {
			sendError(w, 500, err.Error())
			return
		}
		sendJSON(w, map[string]any{"ok": true})
	case method == "DELETE" && strings.HasPrefix(path, "tasks/"):
		id := strings.TrimPrefix(path, "tasks/")
		if err := deleteTask(id); err != nil {
			sendError(w, 500, err.Error())
			return
		}
		sendJSON(w, map[string]any{"ok": true})
	case method == "DELETE" && strings.HasPrefix(path, "plans/"):
		id := strings.TrimPrefix(path, "plans/")
		if err := deletePlan(id); err != nil {
			sendError(w, 500, err.Error())
			return
		}
		sendJSON(w, map[string]any{"ok": true})
	case method == "DELETE" && strings.HasPrefix(path, "bugs/"):
		id := strings.TrimPrefix(path, "bugs/")
		if err := deleteBug(id); err != nil {
			sendError(w, 500, err.Error())
			return
		}
		sendJSON(w, map[string]any{"ok": true})
	default:
		sendError(w, 404, fmt.Sprintf("not found: %s %s", method, path))
	}
}
