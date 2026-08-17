package webdata

import (
	"os"
	"path/filepath"
	"strings"

	pmdb "aipmc/db"
	"aipmc/store"
	"aipmc/u"
)

// Bundle holds raw store data for one HTTP request; load methods are lazy.
type Bundle struct {
	tasks     []store.Task
	commits   []map[string]any
	bugs      []map[string]any
	ideas     []map[string]any
	docs      []map[string]any
	decisions []map[string]any
	visions   []map[string]any
	roadmaps  []map[string]any
	plans     []map[string]any
	principles []map[string]any
	canon     map[string]any
	daily     map[string]any
	taskNotes []map[string]any

	taskTitles     map[string]string
	decisionTitles map[string]string
	commitTitles   map[string]string
	commitsByTask  map[string][]map[string]any

	flags struct {
		tasks, commits, bugs, ideas, docs, decisions, visions, roadmaps, plans, principles, canon, daily, taskNotes bool
	}
}

func NewBundle() *Bundle {
	return &Bundle{
		taskTitles:     map[string]string{},
		decisionTitles: map[string]string{},
		commitTitles:   map[string]string{},
		commitsByTask:  map[string][]map[string]any{},
	}
}

func (b *Bundle) loadTasks() {
	if b.flags.tasks {
		return
	}
	b.tasks, _ = store.ListTasks("", "")
	for _, t := range b.tasks {
		b.taskTitles[t.ID] = t.Title
	}
	b.flags.tasks = true
}

func (b *Bundle) loadCommits() {
	if b.flags.commits {
		return
	}
	b.loadTasks()
	b.commits, _ = store.ListCommits("", "", "", "", 0)
	for _, c := range b.commits {
		b.commitTitles[u.Str(c["id"])] = u.Str(c["title"])
		if tid := u.Str(c["task_id"]); tid != "" {
			b.commitsByTask[tid] = append(b.commitsByTask[tid], c)
		}
	}
	b.flags.commits = true
}

func (b *Bundle) loadDecisions() {
	if b.flags.decisions {
		return
	}
	b.decisions, _ = store.ListDecisions()
	for _, d := range b.decisions {
		b.decisionTitles[u.Str(d["id"])] = u.Str(d["title"])
	}
	b.flags.decisions = true
}

func (b *Bundle) loadBugs() {
	if b.flags.bugs {
		return
	}
	b.bugs, _ = store.ListBugs("", "", "", 0, 0)
	b.flags.bugs = true
}

func (b *Bundle) loadIdeas() {
	if b.flags.ideas {
		return
	}
	b.ideas, _ = store.ListIdeas("")
	b.flags.ideas = true
}

func (b *Bundle) loadDocs() {
	if b.flags.docs {
		return
	}
	b.docs, _ = store.ListDocRecords("", "")
	b.flags.docs = true
}

func (b *Bundle) loadVisions() {
	if b.flags.visions {
		return
	}
	b.visions, _ = store.ListVisions()
	b.flags.visions = true
}

func (b *Bundle) loadRoadmaps() {
	if b.flags.roadmaps {
		return
	}
	b.roadmaps, _ = store.ListRoadmaps("")
	b.flags.roadmaps = true
}

func (b *Bundle) loadPlans() {
	if b.flags.plans {
		return
	}
	b.plans, _ = store.ListPlans("", "")
	b.flags.plans = true
}

func (b *Bundle) loadPrinciples() {
	if b.flags.principles {
		return
	}
	b.principles, _ = store.ListPrinciples("", "")
	b.flags.principles = true
}

func (b *Bundle) loadCanon() {
	if b.flags.canon {
		return
	}
	b.canon, _ = store.GetCanon()
	b.flags.canon = true
}

func (b *Bundle) loadDaily() {
	if b.flags.daily {
		return
	}
	b.daily, _ = store.GetDailyNote("")
	b.flags.daily = true
}

func (b *Bundle) loadTaskNotes() {
	if b.flags.taskNotes {
		return
	}
	b.loadTasks()
	for _, t := range b.tasks {
		n, _ := store.ListTaskNotes(t.ID, 999)
		b.taskNotes = append(b.taskNotes, n...)
	}
	b.flags.taskNotes = true
}

func (b *Bundle) enhancedTasks() []map[string]any {
	b.loadCommits()
	out := []map[string]any{}
	for _, t := range b.tasks {
		lc := b.commitsByTask[t.ID]
		appr, verf := 0, 0
		var lev string
		for _, c := range lc {
			if u.Str(c["review_status"]) == "approved" {
				appr++
			}
			if u.Str(c["test_status"]) == "passed" {
				verf++
			}
			if s := u.Str(c["evidence_summary"]); s != "" {
				lev = s
			}
		}
		sh := "needs_commit"
		if t.Status == "done" {
			sh = "completed"
		} else if len(lc) > 0 && appr == 0 {
			sh = "needs_review"
		} else if len(lc) > 0 && verf == 0 {
			sh = "needs_verification"
		} else if len(lc) > 0 {
			sh = "ready"
		}
		out = append(out, map[string]any{
			"id": t.ID, "title": t.Title, "status": t.Status, "priority": t.Priority, "phase": t.Phase,
			"roadmap_id": t.RoadmapID, "plan_id": t.PlanID, "acceptance": t.Acceptance,
			"related_docs": t.RelatedDocs, "related_decisions": t.RelatedDecisions,
			"last_note": t.LastNote, "updated_at": t.UpdatedAt, "created_at": t.CreatedAt,
			"acceptance_json": u.JsonStr(t.Acceptance), "related_docs_json": u.JsonStr(t.RelatedDocs),
			"related_decisions_json": u.JsonStr(t.RelatedDecisions), "progress": t.Progress,
			"linked_commit_count": len(lc), "approved_commit_count": appr, "verified_commit_count": verf,
			"latest_evidence_summary": lev, "status_hint": sh,
			"source_idea": nil, "related_decision_titles": []string{}, "closure_reasons": []string{},
		})
	}
	return out
}

func (b *Bundle) enhancedCommits() []map[string]any {
	b.loadCommits()
	b.loadDecisions()
	out := []map[string]any{}
	for _, c := range b.commits {
		wc := map[string]any{}
		for k, v := range c {
			wc[k] = v
		}
		wc["task_title"] = b.taskTitles[u.Str(c["task_id"])]
		wc["decision_title"] = b.decisionTitles[u.Str(c["decision_id"])]
		if h := u.Str(c["commit_hash"]); len(h) > 0 {
			wc["short_hash"] = h
		}
		if files, ok := c["files"].([]any); ok {
			wc["file_count"] = len(files)
		}
		sh := "draft"
		if u.Str(c["review_status"]) != "approved" {
			sh = "needs_review"
		} else if u.Str(c["test_status"]) != "passed" {
			sh = "needs_verification"
		} else if u.Str(c["status"]) != "draft" {
			sh = "ready"
		}
		wc["status_hint"] = sh
		out = append(out, wc)
	}
	return out
}

func (b *Bundle) enhancedBugs() []map[string]any {
	b.loadBugs()
	b.loadCommits()
	out := []map[string]any{}
	for _, bug := range b.bugs {
		wb := map[string]any{}
		for k, v := range bug {
			wb[k] = v
		}
		wb["commit_title"] = b.commitTitles[u.Str(bug["commit_id"])]
		out = append(out, wb)
	}
	return out
}

func (b *Bundle) enhancedDecisions() []map[string]any {
	b.loadDecisions()
	b.loadCommits()
	ccByDec := map[string]int{}
	for _, c := range b.commits {
		if did := u.Str(c["decision_id"]); did != "" {
			ccByDec[did]++
		}
	}
	out := []map[string]any{}
	for _, d := range b.decisions {
		wd := map[string]any{}
		for k, v := range d {
			wd[k] = v
		}
		wd["linked_commit_count"] = ccByDec[u.Str(d["id"])]
		wd["source_idea"] = nil
		wd["related_task_titles"] = []string{}
		out = append(out, wd)
	}
	return out
}

func (b *Bundle) enhancedRoadmaps() []map[string]any {
	b.loadRoadmaps()
	b.loadTasks()
	b.loadPlans()
	tcByRdm, dcByRdm := map[string]int{}, map[string]int{}
	for _, t := range b.tasks {
		if t.RoadmapID != "" {
			tcByRdm[t.RoadmapID]++
			if t.Status == "done" {
				dcByRdm[t.RoadmapID]++
			}
		}
	}
	pcByRdm := map[string]int{}
	for _, p := range b.plans {
		if rid := u.Str(p["roadmap_id"]); rid != "" {
			pcByRdm[rid]++
		}
	}
	out := []map[string]any{}
	for _, r := range b.roadmaps {
		wr := map[string]any{}
		for k, v := range r {
			wr[k] = v
		}
		rid := u.Str(r["id"])
		tc, pc := tcByRdm[rid], pcByRdm[rid]
		wr["task_count"], wr["plan_count"] = tc, pc
		if tc > 0 {
			wr["progress"] = (dcByRdm[rid] * 100) / tc
		} else {
			wr["progress"] = 0
		}
		out = append(out, wr)
	}
	return out
}

func (b *Bundle) enhancedPlans() []map[string]any {
	b.loadPlans()
	b.loadTasks()
	tpc := map[string]int{}
	for _, t := range b.tasks {
		if t.PlanID != "" {
			tpc[t.PlanID]++
		}
	}
	out := []map[string]any{}
	for _, p := range b.plans {
		pid := u.Str(p["id"])
		np := map[string]any{}
		for k, v := range p {
			np[k] = v
		}
		np["task_count"] = tpc[pid]
		np["health"] = map[string]any{"state": "active", "issues": []string{}, "needs_manager_attention": false}
		np["manager_summary"] = map[string]any{}
		np["execution_packet"] = map[string]any{}
		np["recommendations"] = []any{}
		np["linked_tasks"] = []any{}
		out = append(out, np)
	}
	return out
}

func (b *Bundle) enhancedDocs() []map[string]any {
	b.loadDocs()
	out := []map[string]any{}
	for _, d := range b.docs {
		wd := map[string]any{}
		for k, v := range d {
			wd[k] = v
		}
		wd["issues"] = []string{}
		wd["links"] = map[string]any{"outgoing": []any{}, "incoming": []any{}}
		out = append(out, wd)
	}
	return out
}

func (b *Bundle) docAudit() map[string]any {
	b.loadDocs()
	docAudit := map[string]any{
		"total_managed_docs": len(b.docs), "active_records": 0, "tracked_files_in_fs": 0,
		"sot_conflicts": map[string]any{}, "invalid_truth_records": []any{},
		"obsolete_without_replacement": []any{}, "missing_from_fs": []any{},
		"path_not_normalized": []any{}, "stale_active_records": []any{},
		"source_of_truth_records": []any{}, "untracked_in_fs": []any{},
	}
	if dr, err := pmdb.RuntimeDir(); err == nil {
		pr := filepath.Dir(dr)
		dp := map[string]bool{}
		for _, doc := range b.docs {
			dp[u.Str(doc["path"])] = true
		}
		tf := []string{}
		ac := 0
		for _, doc := range b.docs {
			if u.Str(doc["status"]) == "active" {
				ac++
			}
			if ok, _ := doc["source_of_truth"].(bool); ok {
				docAudit["source_of_truth_records"] = append(docAudit["source_of_truth_records"].([]any), u.Str(doc["path"]))
			}
		}
		for _, dir := range []string{"/doc", ""} {
			if entries, e := os.ReadDir(pr + dir); e == nil {
				for _, f := range entries {
					if !f.IsDir() && (strings.HasSuffix(f.Name(), ".md") || strings.HasSuffix(f.Name(), ".txt")) {
						rp := f.Name()
						if dir != "" {
							rp = "doc/" + f.Name()
						}
						tf = append(tf, rp)
						if !dp[rp] {
							docAudit["untracked_in_fs"] = append(docAudit["untracked_in_fs"].([]any), rp)
						}
					}
				}
			}
		}
		for p := range dp {
			found := false
			for _, f := range tf {
				if f == p {
					found = true
					break
				}
			}
			if !found {
				docAudit["missing_from_fs"] = append(docAudit["missing_from_fs"].([]any), p)
			}
		}
		docAudit["active_records"] = ac
		docAudit["tracked_files_in_fs"] = len(tf)
	}
	return docAudit
}
