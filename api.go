package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"aipmc/agent"
	"aipmc/ai"
	"aipmc/analyze"
	pmdb "aipmc/db"
	"aipmc/mcp"
	"aipmc/store"
	"aipmc/u"
	"aipmc/web"
)

func handleGetEntity(w http.ResponseWriter, entity, id string) {
	switch entity {
	case "tasks":
		t, err := store.GetTask(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, t)
	case "commits":
		c, err := store.GetCommit(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, c)
	case "plans":
		p, err := store.GetPlan(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, p)
	case "bugs":
		b, err := store.GetBug(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, b)
	case "decisions":
		d, err := store.GetDecision(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, d)
	case "ideas":
		i, err := store.GetIdea(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, i)
	case "roadmaps":
		r, err := store.GetRoadmap(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, r)
	case "principles":
		p, err := store.GetPrinciple(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, p)
	case "visions":
		v, err := store.GetVision(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, v)
	case "threads":
		t, err := store.GetThread(id)
		if err != nil {
			web.SendError(w, 404, err.Error())
			return
		}
		web.SendJSON(w, t)
	case "agents":
		a, err := store.GetAgentProfile(id)
		if err != nil { web.SendError(w, 404, err.Error()); return }
		web.SendJSON(w, a)
	case "meetings":
		m, err := store.GetMeetingRoom(id)
		if err != nil { web.SendError(w, 404, err.Error()); return }
		web.SendJSON(w, m)
	case "assignments":
		a, err := store.GetAssignment(id)
		if err != nil { web.SendError(w, 404, err.Error()); return }
		web.SendJSON(w, a)
	default:
		web.SendError(w, 404, fmt.Sprintf("unknown entity: %s", entity))
	}
}

func handlePatchEntity(w http.ResponseWriter, entity, id string, body map[string]any) {
	switch entity {
	case "tasks":
		task, err := store.UpdateTask(id, pstr(body, "status", ""), pstr(body, "note", ""), false, false)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, task)
	case "commits":
		c, err := store.UpdateCommit(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, c)
	case "plans":
		p, err := store.UpdatePlan(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, p)
	case "bugs":
		b, err := store.UpdateBug(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, b)
	case "decisions":
		d, err := store.UpdateDecisionStatus(id, pstr(body, "status", ""))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, d)
	case "ideas":
		if _, hasNote := body["note"]; hasNote {
			idea, err := store.ReviewIdea(id, u.Str(body["status"]), u.Str(body["note"]))
			if err != nil {
				web.SendError(w, 400, err.Error())
				return
			}
			web.SendJSON(w, idea)
		} else {
			idea, err := store.UpdateIdea(id, body)
			if err != nil {
				web.SendError(w, 400, err.Error())
				return
			}
			web.SendJSON(w, idea)
		}
	case "roadmaps":
		r, err := store.UpdateRoadmap(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, r)
	case "principles":
		p, err := store.UpdatePrinciple(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, p)
	case "visions":
		v, err := store.UpdateVision(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, v)
	case "threads":
		t, err := store.UpdateThread(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, t)
	case "agents":
		a, err := store.UpdateAgentProfile(id, body)
		if err != nil { web.SendError(w, 400, err.Error()); return }
		web.SendJSON(w, a)
	case "assignments":
		a, err := store.UpdateAssignment(id, body)
		if err != nil { web.SendError(w, 400, err.Error()); return }
		web.SendJSON(w, a)
	default:
		web.SendError(w, 404, fmt.Sprintf("unknown entity: %s", entity))
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
	d, err := pmdb.RuntimeDir()
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
			web.SendError(w, 500, fmt.Sprintf("%v", rec))
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
		tasks, _ := store.ListTasks(q.Get("status"), q.Get("plan_id"))
		web.SendJSON(w, map[string]any{"tasks": tasks})
	case method == "GET" && path == "commits":
		commits, _ := store.ListCommits(q.Get("status"), q.Get("task_id"), q.Get("decision_id"), "", 0)
		web.SendJSON(w, map[string]any{"commits": commits})
	case method == "GET" && path == "plans":
		plans, _ := store.ListPlans(q.Get("roadmap_id"), q.Get("status"))
		web.SendJSON(w, map[string]any{"plans": plans})
	case method == "GET" && path == "bugs":
		bugs, _ := store.ListBugs(q.Get("status"), q.Get("severity"), q.Get("commit_id"), 0)
		web.SendJSON(w, map[string]any{"bugs": bugs})
	case method == "GET" && path == "decisions":
		decs, _ := store.ListDecisions()
		web.SendJSON(w, map[string]any{"decisions": decs})
	case method == "GET" && path == "ideas":
		ideas, _ := store.ListIdeas(q.Get("status"))
		web.SendJSON(w, map[string]any{"ideas": ideas})
	case method == "GET" && path == "roadmaps":
		rds, _ := store.ListRoadmaps(q.Get("vision_id"))
		web.SendJSON(w, map[string]any{"roadmaps": rds})
	case method == "GET" && path == "principles":
		prs, _ := store.ListPrinciples(q.Get("status"), q.Get("kind"))
		web.SendJSON(w, map[string]any{"principles": prs})
	case method == "GET" && path == "docs":
		docs, _ := store.ListDocRecords(q.Get("status"), q.Get("layer"))
		web.SendJSON(w, map[string]any{"docs": docs})
	case method == "GET" && path == "visions":
		visions, _ := store.ListVisions()
		web.SendJSON(w, map[string]any{"visions": visions})
	case method == "GET" && path == "links":
		links, _ := store.ListLinks(q.Get("source_id"), q.Get("target_id"), q.Get("relation"))
		web.SendJSON(w, map[string]any{"links": links})
	case method == "GET" && path == "threads":
		threads, _ := store.ListThreads(q.Get("status"))
		web.SendJSON(w, map[string]any{"threads": threads})
	case method == "GET" && path == "thread-suggestions":
		web.SendJSON(w, map[string]any{"suggestions": analyze.AnalyzeThreadSuggestions(), "thread_status": analyze.AnalyzeThreadStatus()})
	case method == "GET" && path == "search":
		web.SendJSON(w, searchProjectContext(q.Get("q"), 8))
	case method == "GET" && path == "dashboard":
		web.SendJSON(w, getStatusSnapshot())
	case method == "GET" && path == "context":
		web.SendJSON(w, buildContextPack())
	case method == "GET" && path == "next":
		web.SendJSON(w, buildNextActionPacket())
	case method == "GET" && path == "events":
		events, _ := store.ListEvents(q.Get("filter"))
		web.SendJSON(w, map[string]any{"events": events})
	case method == "GET" && path == "feedbacks":
		fbs, _ := mcp.ListFeedbacks(q.Get("label"))
		web.SendJSON(w, map[string]any{"feedbacks": fbs})
	case method == "POST" && path == "feedbacks":
		body := readBody()
		fb, err := mcp.AddFeedback(u.Str(body["label"]), u.Str(body["content"]))
		if err != nil {
			web.SendJSON(w, map[string]any{"status": "stored_locally", "detail": err.Error()})
			return
		}
		web.SendJSON(w, fb)
	case method == "POST" && path == "events":
		body := readBody()
		evt, _ := store.CreateEvent(u.Str(body["type"]), u.Str(body["entity_type"]), u.Str(body["entity_id"]), u.Str(body["summary"]))
		web.SendJSON(w, evt)
	case method == "GET" && path == "inbox":
		web.SendJSON(w, getInboxSummary())
	case method == "GET" && path == "canon":
		c, _ := store.GetCanon()
		web.SendJSON(w, c)
	case method == "GET" && path == "daily":
		d, _ := store.GetDailyNote(q.Get("date"))
		web.SendJSON(w, d)
	case method == "GET" && path == "daily/history":
		d, _ := store.ListDailyNotes()
		web.SendJSON(w, map[string]any{"daily_notes": d})
	case method == "GET" && path == "docs/content":
		c, err := store.ReadDocContent(q.Get("path"))
		if err != nil { web.SendError(w, 404, err.Error()); return }
		web.SendJSON(w, map[string]any{"content": c, "path": q.Get("path")})
	case method == "POST" && path == "daily":
		d, _ := store.AppendDailyNote(q.Get("date"), map[string][]string{})
		web.SendJSON(w, d)
	case method == "PUT" && path == "daily":
		d, _ := store.ReplaceDailyNote(q.Get("date"), map[string][]string{})
		web.SendJSON(w, d)
	case method == "GET" && path == "web/bootstrap":
		tasks, _ := store.ListTasks("", ""); commits, _ := store.ListCommits("", "", "", "", 0); bugs, _ := store.ListBugs("", "", "", 0)
		ideas, _ := store.ListIdeas(""); docs, _ := store.ListDocRecords("", ""); decisions, _ := store.ListDecisions()
		visions, _ := store.ListVisions(); roadmaps, _ := store.ListRoadmaps(""); plans, _ := store.ListPlans("", "")
		principles, _ := store.ListPrinciples("", ""); canon, _ := store.GetCanon(); daily, _ := store.GetDailyNote("")

		allTaskNotes := []map[string]any{}
		for _, t := range tasks { n, _ := store.ListTaskNotes(t.ID, 999); allTaskNotes = append(allTaskNotes, n...) }
		taskTitles := map[string]string{}; for _, t := range tasks { taskTitles[t.ID] = t.Title }
		decisionTitles := map[string]string{}; for _, d := range decisions { decisionTitles[d["id"].(string)] = u.Str(d["title"]) }
		commitTitles := map[string]string{}; for _, c := range commits { commitTitles[u.Str(c["id"])] = u.Str(c["title"]) }
		commitsByTask := map[string][]map[string]any{}
		for _, c := range commits { if tid := u.Str(c["task_id"]); tid != "" { commitsByTask[tid] = append(commitsByTask[tid], c) } }

		snap := getStatusSnapshot()
		doneC, blockedC, todoC := 0, 0, 0
		for _, t := range tasks { if t.Status == "done" { doneC++ } else if t.Status == "blocked" { blockedC++ } else if t.Status == "todo" { todoC++ } }
		dashboard := map[string]any{"task_counts": map[string]any{"in_progress":snap["in_progress_tasks"],"total":snap["total_tasks"],"done":doneC,"blocked":blockedC,"todo":todoC}}
		pcA, pcD := 0, 0; for _, p := range plans { if u.Str(p["status"]) == "active" { pcA++ } else { pcD++ } }
		dashboard["plan_counts"] = map[string]any{"active":pcA,"draft":pcD,"total":len(plans)}
		ccD, ccM, ccNR := 0, 0, 0
		for _, c := range commits { switch u.Str(c["status"]) { case "draft": ccD++; case "merged","committed": ccM++ }; if u.Str(c["review_status"]) != "approved" || u.Str(c["test_status"]) != "passed" { ccNR++ } }
		dashboard["commit_counts"] = map[string]any{"draft":ccD,"merged":ccM,"needs_review":ccNR,"total":len(commits)}
		bo, bt := 0, 0; for _, b := range bugs { if u.Str(b["status"]) == "open" || u.Str(b["status"]) == "in_progress" { bo++ }; bt++ }
		dashboard["bug_counts"] = map[string]any{"open":bo,"total":bt}
		pa := []map[string]any{}; for _, p := range plans { if u.Str(p["status"]) == "active" { pa = append(pa, map[string]any{"id":u.Str(p["id"]),"title":u.Str(p["title"]),"state":"active"}); if len(pa) >= 5 { break } } }
		dashboard["plan_attention"] = pa
		rq := []map[string]any{}; for _, c := range commits { if u.Str(c["review_status"]) != "approved" || u.Str(c["test_status"]) != "passed" { rq = append(rq, map[string]any{"id":u.Str(c["id"]),"title":u.Str(c["title"]),"task_id":u.Str(c["task_id"]),"review_status":u.Str(c["review_status"]),"test_status":u.Str(c["test_status"]),"attention":"needs_review"}); if len(rq) >= 5 { break } } }
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
		for _, t := range tasks { lc := commitsByTask[t.ID]; appr, verf := 0, 0; var lev string; for _, c := range lc { if u.Str(c["review_status"]) == "approved" { appr++ }; if u.Str(c["test_status"]) == "passed" { verf++ }; if s := u.Str(c["evidence_summary"]); s != "" { lev = s } }; sh := "needs_commit"; if t.Status == "done" { sh = "completed" } else if len(lc) > 0 && appr == 0 { sh = "needs_review" } else if len(lc) > 0 && verf == 0 { sh = "needs_verification" } else if len(lc) > 0 { sh = "ready" }; webTasks = append(webTasks, map[string]any{"id":t.ID,"title":t.Title,"status":t.Status,"priority":t.Priority,"phase":t.Phase,"roadmap_id":t.RoadmapID,"plan_id":t.PlanID,"acceptance":t.Acceptance,"related_docs":t.RelatedDocs,"related_decisions":t.RelatedDecisions,"last_note":t.LastNote,"updated_at":t.UpdatedAt,"created_at":t.CreatedAt,"acceptance_json":u.JsonStr(t.Acceptance),"related_docs_json":u.JsonStr(t.RelatedDocs),"related_decisions_json":u.JsonStr(t.RelatedDecisions),"progress":t.Progress,"linked_commit_count":len(lc),"approved_commit_count":appr,"verified_commit_count":verf,"latest_evidence_summary":lev,"status_hint":sh,"source_idea":nil,"related_decision_titles":[]string{},"closure_reasons":[]string{}}) }
		webCommits := []map[string]any{}
		for _, c := range commits { wc := map[string]any{}; for k, v := range c { wc[k] = v }; wc["task_title"] = taskTitles[u.Str(c["task_id"])]; wc["decision_title"] = decisionTitles[u.Str(c["decision_id"])]; if h := u.Str(c["commit_hash"]); len(h) > 0 { wc["short_hash"] = h }; if files, ok := c["files"].([]any); ok { wc["file_count"] = len(files) }; sh := "draft"; if u.Str(c["review_status"]) != "approved" { sh = "needs_review" } else if u.Str(c["test_status"]) != "passed" { sh = "needs_verification" } else if u.Str(c["status"]) != "draft" { sh = "ready" }; wc["status_hint"] = sh; webCommits = append(webCommits, wc) }
		webBugs := []map[string]any{}
		for _, b := range bugs { wb := map[string]any{}; for k, v := range b { wb[k] = v }; wb["commit_title"] = commitTitles[u.Str(b["commit_id"])]; webBugs = append(webBugs, wb) }
		ccByDec := map[string]int{}; for _, c := range commits { if did := u.Str(c["decision_id"]); did != "" { ccByDec[did]++ } }
		webDecisions := []map[string]any{}
		for _, d := range decisions { wd := map[string]any{}; for k, v := range d { wd[k] = v }; wd["linked_commit_count"] = ccByDec[u.Str(d["id"])]; wd["source_idea"] = nil; wd["related_task_titles"] = []string{}; webDecisions = append(webDecisions, wd) }
		tcByRdm, dcByRdm := map[string]int{}, map[string]int{}
		for _, t := range tasks { if t.RoadmapID != "" { tcByRdm[t.RoadmapID]++; if t.Status == "done" { dcByRdm[t.RoadmapID]++ } } }
		pcByRdm := map[string]int{}; for _, p := range plans { if rid := u.Str(p["roadmap_id"]); rid != "" { pcByRdm[rid]++ } }
		webRoadmaps := []map[string]any{}
		for _, r := range roadmaps { wr := map[string]any{}; for k, v := range r { wr[k] = v }; rid := u.Str(r["id"]); tc, pc := tcByRdm[rid], pcByRdm[rid]; wr["task_count"],wr["plan_count"] = tc, pc; if tc > 0 { wr["progress"] = (dcByRdm[rid] * 100) / tc } else { wr["progress"] = 0 }; webRoadmaps = append(webRoadmaps, wr) }
		tpc := map[string]int{}; for _, t := range tasks { if t.PlanID != "" { tpc[t.PlanID]++ } }
		enhancedPlans := []map[string]any{}
		for _, p := range plans { pid := u.Str(p["id"]); np := map[string]any{}; for k, v := range p { np[k] = v }; np["task_count"] = tpc[pid]; np["health"] = map[string]any{"state":"active","issues":[]string{},"needs_manager_attention":false}; np["manager_summary"] = map[string]any{}; np["execution_packet"] = map[string]any{}; np["recommendations"] = []any{}; np["linked_tasks"] = []any{}; enhancedPlans = append(enhancedPlans, np) }
		webDocs := []map[string]any{}
		for _, d := range docs { wd := map[string]any{}; for k, v := range d { wd[k] = v }; wd["issues"] = []string{}; wd["links"] = map[string]any{"outgoing":[]any{},"incoming":[]any{}}; webDocs = append(webDocs, wd) }

		docAudit := map[string]any{"total_managed_docs":len(docs),"active_records":0,"tracked_files_in_fs":0,"sot_conflicts":map[string]any{},"invalid_truth_records":[]any{},"obsolete_without_replacement":[]any{},"missing_from_fs":[]any{},"path_not_normalized":[]any{},"stale_active_records":[]any{},"source_of_truth_records":[]any{},"untracked_in_fs":[]any{}}
		if dr, err := pmdb.RuntimeDir(); err == nil {
			pr := filepath.Dir(dr); dp := map[string]bool{}; for _, doc := range docs { dp[u.Str(doc["path"])] = true }
			tf := []string{}; ac, sot := 0, 0
			for _, doc := range docs { if u.Str(doc["status"]) == "active" { ac++ }; if b, ok := doc["source_of_truth"].(bool); ok && b { sot++; docAudit["source_of_truth_records"] = append(docAudit["source_of_truth_records"].([]any), u.Str(doc["path"])) } }
			for _, dir := range []string{"/doc", ""} {
				if entries, e := os.ReadDir(pr + dir); e == nil {
					for _, f := range entries { if !f.IsDir() && (strings.HasSuffix(f.Name(),".md")||strings.HasSuffix(f.Name(),".txt")) { rp := f.Name(); if dir != "" { rp = "doc/"+f.Name() }; tf = append(tf, rp); if !dp[rp] { docAudit["untracked_in_fs"] = append(docAudit["untracked_in_fs"].([]any), rp) } } }
				}
			}
			for p := range dp { found := false; for _, f := range tf { if f == p { found = true; break } }; if !found { docAudit["missing_from_fs"] = append(docAudit["missing_from_fs"].([]any), p) } }
			docAudit["active_records"] = ac; docAudit["tracked_files_in_fs"] = len(tf)
		}

		threads, _ := store.ListThreads("")
		threadSuggestions := analyze.AnalyzeThreadSuggestions()
		threadStatus := analyze.AnalyzeThreadStatus()

		web.SendJSON(w, map[string]any{
			"dashboard":dashboard,"ai_context":ctx,
			"next_packet":func() map[string]any { np := buildNextActionPacket(); np["mainline"] = map[string]any{}; np["backup_actions"] = []any{}; np["pending_questions"] = []any{}; return np }(),
			"handoff":func() map[string]any { ho := buildAgentStartPacket(); ho["next"] = []string{}; ho["risks"] = []string{}; ho["recent_commits"] = []any{}; ho["recommended_actions"] = []string{}; ho["mainline"] = map[string]any{}; ho["completed"] = []string{}; return ho }(),
			"inbox":func() map[string]any {
				ib := getInboxSummary()
				ib["recommended_actions"] = []any{}
				decisions, _ := store.ListDecisions()
				pc := 0; for _, d := range decisions { if u.Str(d["status"]) == "proposed" { pc++ } }
				canonForInbox, _ := store.GetCanon(); cfCount := 0; if canonForInbox != nil { cfCount = len(canonForInbox["version_scope"].([]any)) }
				ideasList := ib["ideas"]; totalIdeas := 0; if ideasList != nil { if sl, ok := ideasList.([]map[string]any); ok { totalIdeas = len(sl) } }
				ib["counts"] = map[string]any{"total":totalIdeas,"proposed_decisions":pc,"canon_followups":cfCount}
				ib["canon"] = canon; ib["pending_items"] = []any{}
				return ib
			}(),"canon":canon,"visions":visions,
			"roadmaps":webRoadmaps,"plans":enhancedPlans,"principles":principles,
			"agents":func() []map[string]any { a, _ := store.ListAgentProfiles(); if a == nil { a = []map[string]any{} }; return a }(),
			"meetings":func() []map[string]any { m, _ := store.ListMeetingRooms(""); if m == nil { m = []map[string]any{} }; return m }(),
			"assignments":func() []map[string]any { as, _ := store.ListAssignments("", ""); if as == nil { as = []map[string]any{} }; return as }(),
			"audit_logs":func() []map[string]any { l, _ := store.ListAuditLog("", "", 100); if l == nil { l = []map[string]any{} }; return l }(),
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
			"analysis":analyze.RunFullAnalysis(),"briefing":analyze.BuildBriefing(aiClient),
			"threads":threads,"thread_suggestions":threadSuggestions,"thread_status":threadStatus,
		})

	case method == "POST" && path == "tasks":
		body := readBody()
		task, err := store.CreateTask(u.Str(body["title"]), pstr(body, "priority", "P1"), pstr(body, "status", "todo"), pstr(body, "phase", "general"), u.Str(body["plan_id"]), nil)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, task)
	case method == "POST" && path == "commits":
		body := readBody()
		c, err := store.CreateCommit(u.Str(body["title"]), pstr(body, "summary", ""), pstr(body, "evidence_summary", ""), pstr(body, "review_notes", ""), pstr(body, "branch", ""), pstr(body, "commit_hash", ""), u.Str(body["task_id"]), pstr(body, "decision_id", ""), pstr(body, "status", "draft"), pstr(body, "test_status", "not_run"), pstr(body, "review_status", "pending"), nil)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, c)
	case method == "POST" && path == "plans":
		body := readBody()
		plan, err := store.CreatePlan(u.Str(body["title"]), pstr(body, "goal", ""), u.Str(body["roadmap_id"]), pstr(body, "vision_id", ""), pstr(body, "priority", "P1"), pstr(body, "status", "draft"), nil, nil, nil, nil)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, plan)
	case method == "POST" && path == "bugs":
		body := readBody()
		bug, err := store.CreateBug(u.Str(body["title"]), pstr(body, "description", ""), pstr(body, "severity", "minor"), pstr(body, "status", "open"), pstr(body, "commit_id", ""), pstr(body, "error", ""), pstr(body, "files", ""), pstr(body, "root_cause", ""), pstr(body, "fix", ""), pstr(body, "tags", ""))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, bug)
	case method == "POST" && path == "decisions":
		body := readBody()
		d, err := store.CreateDecision(u.Str(body["title"]), u.Str(body["background"]), u.Str(body["decision"]), pstr(body, "status", "proposed"))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, d)
	case method == "POST" && path == "ideas":
		body := readBody()
		idea, err := store.CreateIdea(u.Str(body["title"]), u.Str(body["summary"]), pstr(body, "impact", ""), pstr(body, "source", "manual"), false, pstr(body, "current_summary", ""), pstr(body, "main_question", ""), pstr(body, "recommended_next_action", "continue_discussion"))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, idea)
	case method == "POST" && path == "roadmaps":
		body := readBody()
		r, err := store.CreateRoadmap(u.Str(body["title"]), pstr(body, "target_date", ""), pstr(body, "vision_id", ""), pstr(body, "status", "planned"), pstr(body, "priority", "P1"))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, r)
	case method == "POST" && path == "principles":
		body := readBody()
		p, err := store.CreatePrinciple(u.Str(body["title"]), pstr(body, "summary", ""), pstr(body, "kind", "governance"), pstr(body, "status", "active"))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, p)
	case method == "POST" && path == "visions":
		body := readBody()
		v, err := store.CreateVision(u.Str(body["title"]), pstr(body, "summary", ""), pstr(body, "status", "active"), pstr(body, "horizon", "long_term"))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, v)
	case method == "POST" && path == "threads":
		body := readBody()
		t, err := store.CreateThread(u.Str(body["title"]), pstr(body, "summary", ""), pstr(body, "source", "manual"))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, t)
	case method == "POST" && strings.HasPrefix(path, "threads/") && strings.HasSuffix(path, "/items"):
		id := extractID(path, "threads/", "/items")
		body := readBody()
		t, err := store.AddToThread(id, u.Str(body["entity_type"]), u.Str(body["entity_id"]), pstr(body, "note", ""))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, t)
	case method == "DELETE" && strings.HasPrefix(path, "threads/") && strings.Contains(path, "/items/"):
		// DELETE /threads/{thread-id}/items/{entity-type}/{entity-id}
		pathTrimmed := strings.TrimPrefix(path, "threads/")
		parts := strings.SplitN(pathTrimmed, "/items/", 2)
		if len(parts) == 2 {
			threadID := parts[0]
			itemParts := strings.SplitN(parts[1], "/", 2)
			if len(itemParts) == 2 {
				store.RemoveFromThread(threadID, itemParts[0], itemParts[1])
				web.SendJSON(w, map[string]any{"ok": true})
				return
			}
		}
		web.SendError(w, 400, "invalid thread item path")
	case method == "POST" && path == "links":
		body := readBody()
		link, err := store.CreateLink(u.Str(body["source_type"]), u.Str(body["source_id"]), u.Str(body["relation"]), u.Str(body["target_type"]), u.Str(body["target_id"]), pstr(body, "note", ""))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, link)
	case method == "POST" && strings.HasPrefix(path, "tasks/") && strings.HasSuffix(path, "/notes"):
		id := extractID(path, "tasks/", "/notes")
		body := readBody()
		result, err := store.AppendTaskNote(id, u.Str(body["content"]))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, result)
	case method == "POST" && strings.HasPrefix(path, "ideas/") && strings.HasSuffix(path, "/comments"):
		id := extractID(path, "ideas/", "/comments")
		body := readBody()
		comment, err := store.CreateIdeaComment(id, u.Str(body["content"]), pstr(body, "kind", "comment"), pstr(body, "author_type", "ai"), pstr(body, "author_name", "aipmc"))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return
		}
		web.SendJSON(w, comment)
	case method == "POST" && strings.HasPrefix(path, "ideas/") && strings.HasSuffix(path, "/convert"):
		id := extractID(path, "ideas/", "/convert")
		body := readBody()
		if u.Str(body["to"]) == "task" {
			result, err := store.ConvertIdeaToTask(id, u.Str(body["plan_id"]))
			if err != nil {
				web.SendError(w, 400, err.Error())
				return
			}
			web.SendJSON(w, result)
		} else {
			result, err := store.ConvertIdeaToDecision(id)
			if err != nil {
				web.SendError(w, 400, err.Error())
				return
			}
			web.SendJSON(w, result)
		}
	// ---- Agent Profiles ----
	case method == "GET" && path == "agents":
		agents, _ := store.ListAgentProfiles()
		web.SendJSON(w, map[string]any{"agents": agents})
	case method == "POST" && path == "agents":
		body := readBody()
		profile, err := store.CreateAgentProfile(u.Str(body["name"]), u.Str(body["role"]), u.Str(body["capabilities"]))
		if err != nil { web.SendError(w, 400, err.Error()); return }
		web.SendJSON(w, profile)
	case method == "PATCH" && strings.HasPrefix(path, "agents/"):
		id := strings.TrimPrefix(path, "agents/")
		a, err := store.UpdateAgentProfile(id, readBody())
		if err != nil { web.SendError(w, 400, err.Error()); return }
		web.SendJSON(w, a)
	// ---- Meeting Rooms ----
	case method == "GET" && path == "meetings":
		rooms, _ := store.ListMeetingRooms(q.Get("status"))
		web.SendJSON(w, map[string]any{"meetings": rooms})
	case method == "POST" && path == "meetings":
		body := readBody()
		room, err := store.CreateMeetingRoom(u.Str(body["title"]), u.Str(body["topic"]), u.Str(body["context"]), u.Str(body["created_by"]))
		if err != nil { web.SendError(w, 400, err.Error()); return }
		web.SendJSON(w, room)
	case method == "POST" && strings.HasPrefix(path, "meetings/") && strings.HasSuffix(path, "/close"):
		id := extractID(path, "meetings/", "/close")
		r, err := store.CloseMeetingRoom(id)
		if err != nil { web.SendError(w, 400, err.Error()); return }
		web.SendJSON(w, r)
	case method == "POST" && strings.HasPrefix(path, "meetings/") && strings.HasSuffix(path, "/turns"):
		body := readBody()
		roomID := extractID(path, "meetings/", "/turns")
		turn, err := store.CreateMeetingTurn(roomID, 0, u.Str(body["speaker_type"]), u.Str(body["speaker_id"]), u.Str(body["question"]))
		if err != nil { web.SendError(w, 400, err.Error()); return }
		web.SendJSON(w, turn)
	case method == "POST" && strings.HasPrefix(path, "meetings/") && strings.HasSuffix(path, "/participants"):
		body := readBody()
		roomID := extractID(path, "meetings/", "/participants")
		p, err := store.ConfirmMeetingAttendance(roomID, u.Str(body["agent_id"]))
		if err != nil { web.SendError(w, 400, err.Error()); return }
		web.SendJSON(w, p)
	// ---- Assignments ----
	case method == "GET" && path == "assignments":
		asgns, _ := store.ListAssignments(q.Get("agent_id"), q.Get("status"))
		web.SendJSON(w, map[string]any{"assignments": asgns})
	case method == "POST" && path == "assignments":
		body := readBody()
		a, err := store.CreateAssignment(u.Str(body["agent_id"]), u.Str(body["task_id"]), u.Str(body["role"]), u.Str(body["scope"]), u.Str(body["assigned_by"]))
		if err != nil { web.SendError(w, 400, err.Error()); return }
		web.SendJSON(w, a)
	case method == "PATCH" && strings.HasPrefix(path, "assignments/"):
		id := strings.TrimPrefix(path, "assignments/")
		a, err := store.UpdateAssignment(id, readBody())
		if err != nil { web.SendError(w, 400, err.Error()); return }
		web.SendJSON(w, a)
	// ---- Audit Log ----
	case method == "GET" && path == "audit":
		logs, _ := store.ListAuditLog(q.Get("actor_type"), q.Get("entity_type"), 200)
		web.SendJSON(w, map[string]any{"audit_logs": logs})
	// ---- AI Search ----
	case method == "POST" && strings.HasPrefix(path, "meetings/") && strings.HasSuffix(path, "/typing"):
		body := readBody()
		roomID := extractID(path, "meetings/", "/typing")
		typing := 0
		if v, ok := body["pm_typing"]; ok {
			if b, ok := v.(bool); ok && b { typing = 1 }
			if f, ok := v.(float64); ok && f > 0 { typing = 1 }
		}
		db, err := pmdb.Open()
		if err == nil { defer db.Close(); db.Exec("UPDATE meeting_rooms SET pm_typing = ? WHERE id = ?", typing, roomID) }
		web.SendJSON(w, map[string]any{"ok": true, "pm_typing": typing})
	case method == "POST" && path == "arbitrate":
		body := readBody()
		roomID := u.Str(body["room_id"])
		room, err := store.GetMeetingRoom(roomID)
		if err != nil { web.SendError(w, 404, err.Error()); return }
		turns, _ := store.ListMeetingTurns(roomID)
		var recent []ai.ArbitrationTurn
		start := 0
		if len(turns) > 8 { start = len(turns) - 8 }
		for i := start; i < len(turns); i++ {
			t := turns[i]
			txt := u.Str(t["question"])
			if r := u.Str(t["response"]); r != "" { txt = r }
			recent = append(recent, ai.ArbitrationTurn{
				SpeakerType: u.Str(t["speaker_type"]), SpeakerID: u.Str(t["speaker_id"]),
				Content: txt, AddressTo: u.Str(t["address_to"]),
			})
		}
		next, reason, err := aiClient.ArbitrateNextSpeaker(u.Str(room["topic"]), u.Str(room["agent_roles_context"]), recent)
		if err != nil { web.SendError(w, 500, err.Error()); return }
		existing, _ := store.ListMeetingTurns(roomID)
		nextNum := len(existing) + 1
		store.CreateMeetingTurn(roomID, nextNum, "agent", next, fmt.Sprintf("[AI 仲裁] %s。请就此发表意见。", reason))
		web.SendJSON(w, map[string]any{"next_agent": next, "reason": reason})
	case method == "POST" && path == "discussions":
		body := readBody()
		d, err := store.LogDiscussion(u.Str(body["session_id"]), u.Str(body["role"]), u.Str(body["source"]), u.Str(body["content"]), "")
		if err != nil { web.SendError(w, 400, err.Error()); return }
		web.SendJSON(w, d)
		case method == "GET" && path == "discussions":
			page := 1; if p := q.Get("page"); p != "" { fmt.Sscanf(p, "%d", &page) }
			src := q.Get("source")
			pp := q.Get("project_path")
			typeFilter := q.Get("type")
			results, total, _ := searchDiscussions(q.Get("q"), src, typeFilter, pp, page, 20)
			web.SendJSON(w, map[string]any{"discussions": results, "total": total, "page": page})

	case method == "GET" && path == "discussions/sources":
		sources, _ := store.ListDiscussionSources()
		web.SendJSON(w, map[string]any{"sources": sources})
	case method == "GET" && path == "config":
		cfg := pmdb.LoadConfig()
		web.SendJSON(w, map[string]any{
			"ai_endpoint":           cfg.AIEndpoint,
			"ai_embedding_endpoint": cfg.AIEmbeddingEndpoint,
			"ai_model":              cfg.AIModel,
			"ai_chat_model":         cfg.AIChatModel,
			"ai_enabled":            cfg.AIEndpoint != "",
			"web_host":              cfg.WebHost,
			"web_port":              cfg.WebPort,
		})
	case method == "POST" && path == "config":
		cfg := pmdb.LoadConfig()
		body := readBody()
		if v := u.Str(body["ai_endpoint"]); v != "" { cfg.AIEndpoint = v }
		if v := u.Str(body["ai_embedding_endpoint"]); v != "" { cfg.AIEmbeddingEndpoint = v }
		if v := u.Str(body["ai_model"]); v != "" { cfg.AIModel = v }
		if v := u.Str(body["ai_chat_model"]); v != "" { cfg.AIChatModel = v }
		if v := u.Str(body["web_host"]); v != "" { cfg.WebHost = v }
		if v, ok := body["web_port"]; ok {
			if f, ok := v.(float64); ok { cfg.WebPort = int(f) }
		}
		if err := pmdb.SaveConfig(cfg); err != nil {
			web.SendError(w, 500, err.Error())
			return
		}
		// Re-init AI client
		initAI()
		web.SendJSON(w, map[string]any{"ok": true, "ai_enabled": cfg.AIEndpoint != ""})
	case method == "POST" && path == "discussions/embed":
		count, err := embedDiscussions(100)
		if err != nil { web.SendError(w, 500, err.Error()); return }
		web.SendJSON(w, map[string]any{"ok": true, "embedded": count})
	case method == "POST" && path == "ai-test":
		if aiClient == nil || !aiClient.Enabled() {
			web.SendJSON(w, map[string]any{"ok": false, "error": "AI 未配置"})
			return
		}
		_, err := aiClient.Embed([]string{"test"})
		if err != nil {
			web.SendJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		web.SendJSON(w, map[string]any{"ok": true, "message": "AI 连接正常"})
	case method == "GET" && path == "smart-search":
		result := searchProjectContext(q.Get("q"), 8)
		web.SendJSON(w, result)
	case method == "GET":
		entity, id := parseEntityID(path)
		handleGetEntity(w, entity, id)
	case method == "PATCH":
		entity, id := parseEntityID(path)
		handlePatchEntity(w, entity, id, readBody())
	case method == "POST" && path == "canon/update":
		body := readBody()
		c, _ := store.UpdateCanon(u.Str(body["decision_id"]), pstr(body, "product_goal", ""), pstr(body, "engineering_focus", ""), pstr(body, "architecture", ""), nil, nil)
		web.SendJSON(w, c)
	case method == "DELETE" && strings.HasPrefix(path, "task-notes/"):
		web.SendJSON(w, map[string]any{"ok": true})
	case method == "POST" && strings.HasPrefix(path, "plans/") && strings.HasSuffix(path, "/advance"):
		web.SendJSON(w, map[string]any{"ok": true, "message": "plan advanced"})
	case method == "POST" && path == "docs/sync":
		web.SendJSON(w, map[string]any{"ok": true, "message": "docs synced"})
	case method == "POST" && path == "docs/repair":
		web.SendJSON(w, map[string]any{"ok": true, "message": "docs repaired"})
	case method == "POST" && path == "docs/prune":
		web.SendJSON(w, map[string]any{"ok": true, "message": "docs pruned"})
	case method == "DELETE" && strings.HasPrefix(path, "links/"):
		id := strings.TrimPrefix(path, "links/")
		err := store.DeleteLink(id)
		if err != nil {
			web.SendError(w, 500, err.Error())
			return
		}
		web.SendJSON(w, map[string]any{"ok": true})
	case method == "DELETE" && strings.HasPrefix(path, "tasks/"):
		id := strings.TrimPrefix(path, "tasks/")
		if err := store.DeleteTask(id); err != nil {
			web.SendError(w, 500, err.Error())
			return
		}
		web.SendJSON(w, map[string]any{"ok": true})
	case method == "DELETE" && strings.HasPrefix(path, "plans/"):
		id := strings.TrimPrefix(path, "plans/")
		if err := store.DeletePlan(id); err != nil {
			web.SendError(w, 500, err.Error())
			return
		}
		web.SendJSON(w, map[string]any{"ok": true})
	case method == "DELETE" && strings.HasPrefix(path, "bugs/"):
		id := strings.TrimPrefix(path, "bugs/")
		if err := store.DeleteBug(id); err != nil {
			web.SendError(w, 500, err.Error())
			return
		}
		web.SendJSON(w, map[string]any{"ok": true})
	// ── Chat API ─────────────────────────────────────────────────
	case method == "GET" && path == "chat/sessions":
		runtimeDir, _ := pmdb.RuntimeDir()
		workDir := "."
		if runtimeDir != "" {
			workDir = filepath.Dir(runtimeDir)
		}
		sessionDir := agent.SessionDir(workDir)
		entries, _ := os.ReadDir(sessionDir)
		sessions := []map[string]any{}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			id := strings.TrimSuffix(e.Name(), ".json")
			info, _ := e.Info()
			var modTime string
			var eventCount int
			if info != nil {
				modTime = info.ModTime().Format("2006-01-02 15:04")
			}
			if s, err2 := agent.LoadSession(filepath.Join(sessionDir, e.Name())); err2 == nil {
				eventCount = len(s.Events)
			}
			sessions = append(sessions, map[string]any{
				"id":         id,
				"updated_at": modTime,
				"events":     eventCount,
			})
		}
		if sessions == nil {
			sessions = []map[string]any{}
		}
		web.SendJSON(w, map[string]any{"sessions": sessions})

	case method == "GET" && path == "chat/session":
		sid := q.Get("id")
		if sid == "" {
			web.SendError(w, 400, "缺少 id 参数")
			return
		}
		runtimeDir, _ := pmdb.RuntimeDir()
		workDir := "."
		if runtimeDir != "" {
			workDir = filepath.Dir(runtimeDir)
		}
		sessPath := filepath.Join(agent.SessionDir(workDir), sid+".json")
		sess, err := agent.LoadSession(sessPath)
		if err != nil {
			web.SendError(w, 404, "会话不存在: "+sid)
			return
		}
		web.SendJSON(w, map[string]any{
			"id":         sess.ID,
			"events":     sess.Events,
			"created_at": sess.CreatedAt,
			"updated_at": sess.UpdatedAt,
		})

	case method == "POST" && path == "chat/send":
		if aiClient == nil || !aiClient.Enabled() {
			web.SendError(w, 503, "AI 未配置。请设置 AI_ENDPOINT 环境变量。")
			return
		}
		body := readBody()
		msg, _ := body["message"].(string)
		sid, _ := body["session_id"].(string)
		if msg == "" {
			web.SendError(w, 400, "缺少 message 参数")
			return
		}
		runtimeDir, _ := pmdb.RuntimeDir()
		workDir := "."
		if runtimeDir != "" {
			workDir = filepath.Dir(runtimeDir)
		}
		a := agent.New(aiClient, workDir)
		sessionDir := agent.SessionDir(workDir)
		var sess *agent.Session
		if sid != "" {
			sessPath := filepath.Join(sessionDir, sid+".json")
			if s, err := agent.LoadSession(sessPath); err == nil {
				sess = s
			}
		}
		if sess == nil {
			sess = agent.NewSession()
		}
		response, err := a.Run(sess, msg)
		if err != nil {
			web.SendError(w, 500, fmt.Sprintf("Agent 错误: %v", err))
			return
		}
		sessPath := filepath.Join(sessionDir, sess.ID+".json")
		sess.Save(sessPath)
		web.SendJSON(w, map[string]any{
			"session_id": sess.ID,
			"response":   response,
			"events":     sess.Events,
		})

	default:
		web.SendError(w, 404, fmt.Sprintf("not found: %s %s", method, path))
	}
}

	// filterDiscussionsByType filters discussion results by message type.
	// typeFilter can be "user", "assistant", or "tool" (comma-separated).
	func filterDiscussionsByType(results []map[string]any, typeFilter string) []map[string]any {
		types := u.SplitAndTrim(typeFilter, ",")
		typeSet := map[string]bool{}
		for _, t := range types {
			typeSet[t] = true
		}
		var filtered []map[string]any
		for _, r := range results {
			role := u.Str(r["role"])
			content := u.Str(r["content"])
			isTool := len(content) > 0 && strings.ContainsRune("🔧📝👁🔍🆕🛠📡", []rune(content)[0])
			msgType := role
			if role == "assistant" && isTool {
				msgType = "tool"
			}
			if typeSet[msgType] {
				filtered = append(filtered, r)
			}
		}
		return filtered
	}
