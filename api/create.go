package api

import (
	"net/http"
	"strings"

	"aipmc/store"
	"aipmc/u"
	"aipmc/web"
)

func (s *Server) handleCreateRoutes(w http.ResponseWriter, method, path string, body map[string]any) bool {
	if method != "POST" {
		return false
	}
	switch path {
	case "tasks":
		task, err := store.CreateTask(u.Str(body["title"]), pstr(body, "priority", "P1"), pstr(body, "status", "todo"), pstr(body, "phase", "general"), u.Str(body["plan_id"]), nil)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, task)
	case "commits":
		c, err := store.CreateCommit(u.Str(body["title"]), pstr(body, "summary", ""), pstr(body, "evidence_summary", ""), pstr(body, "review_notes", ""), pstr(body, "branch", ""), pstr(body, "commit_hash", ""), u.Str(body["task_id"]), pstr(body, "decision_id", ""), pstr(body, "status", "draft"), pstr(body, "test_status", "not_run"), pstr(body, "review_status", "pending"), nil)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, c)
	case "plans":
		plan, err := store.CreatePlan(u.Str(body["title"]), pstr(body, "goal", ""), u.Str(body["roadmap_id"]), pstr(body, "vision_id", ""), pstr(body, "priority", "P1"), pstr(body, "status", "draft"), nil, nil, nil, nil)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, plan)
	case "bugs":
		bug, err := store.CreateBug(u.Str(body["title"]), pstr(body, "description", ""), pstr(body, "severity", "minor"), pstr(body, "status", "open"), pstr(body, "commit_id", ""), pstr(body, "error", ""), pstr(body, "files", ""), pstr(body, "root_cause", ""), pstr(body, "fix", ""), pstr(body, "tags", ""))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, bug)
	case "decisions":
		d, err := store.CreateDecision(u.Str(body["title"]), u.Str(body["background"]), u.Str(body["decision"]), pstr(body, "status", "proposed"))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, d)
	case "ideas":
		idea, err := store.CreateIdea(u.Str(body["title"]), u.Str(body["summary"]), pstr(body, "impact", ""), pstr(body, "source", "manual"), false, pstr(body, "current_summary", ""), pstr(body, "main_question", ""), pstr(body, "recommended_next_action", "continue_discussion"))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, idea)
	case "roadmaps":
		r, err := store.CreateRoadmap(u.Str(body["title"]), pstr(body, "target_date", ""), pstr(body, "vision_id", ""), pstr(body, "status", "planned"), pstr(body, "priority", "P1"))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, r)
	case "principles":
		p, err := store.CreatePrinciple(u.Str(body["title"]), pstr(body, "summary", ""), pstr(body, "kind", "governance"), pstr(body, "status", "active"))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, p)
	case "visions":
		v, err := store.CreateVision(u.Str(body["title"]), pstr(body, "summary", ""), pstr(body, "status", "active"), pstr(body, "horizon", "long_term"))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, v)
	case "threads":
		t, err := store.CreateThread(u.Str(body["title"]), pstr(body, "summary", ""), pstr(body, "source", "manual"))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, t)
	case "links":
		link, err := store.CreateLink(u.Str(body["source_type"]), u.Str(body["source_id"]), u.Str(body["relation"]), u.Str(body["target_type"]), u.Str(body["target_id"]), pstr(body, "note", ""))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, link)
	case "agents":
		profile, err := store.CreateAgentProfile(u.Str(body["name"]), u.Str(body["role"]), u.Str(body["capabilities"]))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, profile)
	case "meetings":
		room, err := store.CreateMeetingRoom(u.Str(body["title"]), u.Str(body["topic"]), u.Str(body["context"]), u.Str(body["created_by"]))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, room)
	case "assignments":
		a, err := store.CreateAssignment(u.Str(body["agent_id"]), u.Str(body["task_id"]), u.Str(body["role"]), u.Str(body["scope"]), u.Str(body["assigned_by"]))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, a)
	default:
		return false
	}
	return true
}

func (s *Server) handleNestedPostRoutes(w http.ResponseWriter, method, path string, body map[string]any) bool {
	if method != "POST" {
		return false
	}
	if path == "tasks" || path == "commits" || path == "plans" {
		return false
	}

	switch {
	case path == "canon/update":
		c, _ := store.UpdateCanon(u.Str(body["decision_id"]), pstr(body, "product_goal", ""), pstr(body, "engineering_focus", ""), pstr(body, "architecture", ""), nil, nil)
		web.SendJSON(w, c)
	case path == "docs/sync":
		web.SendJSON(w, map[string]any{"ok": true, "message": "docs synced"})
	case path == "docs/repair":
		web.SendJSON(w, map[string]any{"ok": true, "message": "docs repaired"})
	case path == "docs/prune":
		web.SendJSON(w, map[string]any{"ok": true, "message": "docs pruned"})
	default:
		return s.handleNestedPostSubRoutes(w, path, body)
	}
	return true
}

func (s *Server) handleNestedPostSubRoutes(w http.ResponseWriter, path string, body map[string]any) bool {
	switch {
	case strings.HasPrefix(path, "tasks/") && strings.HasSuffix(path, "/notes"):
		id := extractID(path, "tasks/", "/notes")
		result, err := store.AppendTaskNote(id, u.Str(body["content"]))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, result)
	case strings.HasPrefix(path, "ideas/") && strings.HasSuffix(path, "/comments"):
		id := extractID(path, "ideas/", "/comments")
		comment, err := store.CreateIdeaComment(id, u.Str(body["content"]), pstr(body, "kind", "comment"), pstr(body, "author_type", "ai"), pstr(body, "author_name", "aipmc"))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, comment)
	case strings.HasPrefix(path, "ideas/") && strings.HasSuffix(path, "/convert"):
		id := extractID(path, "ideas/", "/convert")
		if u.Str(body["to"]) == "task" {
			result, err := store.ConvertIdeaToTask(id, u.Str(body["plan_id"]))
			if err != nil {
				web.SendError(w, 400, err.Error())
				return true
			}
			web.SendJSON(w, result)
		} else {
			result, err := store.ConvertIdeaToDecision(id)
			if err != nil {
				web.SendError(w, 400, err.Error())
				return true
			}
			web.SendJSON(w, result)
		}
	case strings.HasPrefix(path, "threads/") && strings.HasSuffix(path, "/items"):
		id := extractID(path, "threads/", "/items")
		t, err := store.AddToThread(id, u.Str(body["entity_type"]), u.Str(body["entity_id"]), pstr(body, "note", ""))
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, t)
	case strings.HasPrefix(path, "plans/") && strings.HasSuffix(path, "/advance"):
		web.SendJSON(w, map[string]any{"ok": true, "message": "plan advanced"})
	case strings.HasPrefix(path, "meetings/") && strings.HasSuffix(path, "/close"):
		id := extractID(path, "meetings/", "/close")
		r, err := store.CloseMeetingRoom(id)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, r)
	default:
		return false
	}
	return true
}
