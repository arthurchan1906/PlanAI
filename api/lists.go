package api

import (
	"net/http"
	"net/url"

	"aipmc/analyze"
	"aipmc/store"
	"aipmc/web"
)

func (s *Server) handleListRoutes(w http.ResponseWriter, method, path string, q url.Values) bool {
	if method != "GET" {
		return false
	}
	switch path {
	case "tasks":
		tasks, _ := store.ListTasks(q.Get("status"), q.Get("plan_id"))
		web.SendJSON(w, map[string]any{"tasks": tasks})
	case "commits":
		commits, _ := store.ListCommits(q.Get("status"), q.Get("task_id"), q.Get("decision_id"), "", 0)
		web.SendJSON(w, map[string]any{"commits": commits})
	case "plans":
		plans, _ := store.ListPlans(q.Get("roadmap_id"), q.Get("status"))
		web.SendJSON(w, map[string]any{"plans": plans})
	case "bugs":
		bugs, _ := store.ListBugs(q.Get("status"), q.Get("severity"), q.Get("commit_id"), 0, 0)
		web.SendJSON(w, map[string]any{"bugs": bugs})
	case "decisions":
		decs, _ := store.ListDecisions()
		web.SendJSON(w, map[string]any{"decisions": decs})
	case "ideas":
		ideas, _ := store.ListIdeas(q.Get("status"))
		web.SendJSON(w, map[string]any{"ideas": ideas})
	case "roadmaps":
		rds, _ := store.ListRoadmaps(q.Get("vision_id"))
		web.SendJSON(w, map[string]any{"roadmaps": rds})
	case "principles":
		prs, _ := store.ListPrinciples(q.Get("status"), q.Get("kind"))
		web.SendJSON(w, map[string]any{"principles": prs})
	case "docs":
		docs, _ := store.ListDocRecords(q.Get("status"), q.Get("layer"))
		web.SendJSON(w, map[string]any{"docs": docs})
	case "visions":
		visions, _ := store.ListVisions()
		web.SendJSON(w, map[string]any{"visions": visions})
	case "links":
		links, _ := store.ListLinks(q.Get("source_id"), q.Get("target_id"), q.Get("relation"))
		web.SendJSON(w, map[string]any{"links": links})
	case "threads":
		threads, _ := store.ListThreads(q.Get("status"))
		web.SendJSON(w, map[string]any{"threads": threads})
	case "thread-suggestions":
		web.SendJSON(w, map[string]any{
			"suggestions":   analyze.AnalyzeThreadSuggestions(),
			"thread_status": analyze.AnalyzeThreadStatus(),
		})
	case "agents":
		agents, _ := store.ListAgentProfiles()
		web.SendJSON(w, map[string]any{"agents": agents})
	case "audit":
		logs, _ := store.ListAuditLog(q.Get("actor_type"), q.Get("entity_type"), 200)
		web.SendJSON(w, map[string]any{"audit_logs": logs})
	default:
		return false
	}
	return true
}
