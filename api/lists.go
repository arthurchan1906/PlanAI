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
		tasks, err := store.ListTasks(q.Get("status"), q.Get("plan_id"))
		if err != nil {
			web.SendError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"tasks": tasks})
	case "commits":
		commits, err := store.ListCommits(q.Get("status"), q.Get("task_id"), q.Get("decision_id"), "", 0)
		if err != nil {
			web.SendError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"commits": commits})
	case "plans":
		plans, err := store.ListPlans(q.Get("roadmap_id"), q.Get("status"))
		if err != nil {
			web.SendError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"plans": plans})
	case "bugs":
		bugs, err := store.ListBugs(q.Get("status"), q.Get("severity"), q.Get("commit_id"), 0, 0)
		if err != nil {
			web.SendError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"bugs": bugs})
	case "decisions":
		decs, err := store.ListDecisions()
		if err != nil {
			web.SendError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"decisions": decs})
	case "ideas":
		ideas, err := store.ListIdeas(q.Get("status"))
		if err != nil {
			web.SendError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"ideas": ideas})
	case "roadmaps":
		rds, err := store.ListRoadmaps(q.Get("vision_id"))
		if err != nil {
			web.SendError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"roadmaps": rds})
	case "principles":
		prs, err := store.ListPrinciples(q.Get("status"), q.Get("kind"))
		if err != nil {
			web.SendError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"principles": prs})
	case "docs":
		docs, err := store.ListDocRecords(q.Get("status"), q.Get("layer"))
		if err != nil {
			web.SendError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"docs": docs})
	case "visions":
		visions, err := store.ListVisions()
		if err != nil {
			web.SendError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"visions": visions})
	case "links":
		links, err := store.ListLinks(q.Get("source_id"), q.Get("target_id"), q.Get("relation"))
		if err != nil {
			web.SendError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"links": links})
	case "threads":
		threads, err := store.ListThreads(q.Get("status"))
		if err != nil {
			web.SendError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"threads": threads})
	case "thread-suggestions":
		web.SendJSON(w, map[string]any{
			"suggestions":   analyze.AnalyzeThreadSuggestions(),
			"thread_status": analyze.AnalyzeThreadStatus(),
		})
	case "agents":
		agents, err := store.ListAgentProfiles()
		if err != nil {
			web.SendError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"agents": agents})
	case "audit":
		logs, err := store.ListAuditLog(q.Get("actor_type"), q.Get("entity_type"), 200)
		if err != nil {
			web.SendError(w, http.StatusInternalServerError, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"audit_logs": logs})
	default:
		return false
	}
	return true
}
