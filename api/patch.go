package api

import (
	"net/http"
	"strings"

	pmdb "aipmc/db"
	"aipmc/store"
	"aipmc/web"
)

func (s *Server) handlePatchRoutes(w http.ResponseWriter, method, path string, body map[string]any) bool {
	if method != "PATCH" {
		return false
	}
	switch {
	case strings.HasPrefix(path, "agents/"):
		id := strings.TrimPrefix(path, "agents/")
		a, err := store.UpdateAgentProfile(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, a)
	case strings.HasPrefix(path, "assignments/"):
		id := strings.TrimPrefix(path, "assignments/")
		a, err := store.UpdateAssignment(id, body)
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, a)
	default:
		entity, id := parseEntityID(path)
		if id == "" {
			return false
		}
		handlePatchEntity(w, entity, id, body)
	}
	return true
}

func (s *Server) handleDeleteRoutes(w http.ResponseWriter, method, path string) bool {
	if method != "DELETE" {
		return false
	}
	switch {
	case strings.HasPrefix(path, "threads/") && strings.Contains(path, "/items/"):
		pathTrimmed := strings.TrimPrefix(path, "threads/")
		parts := strings.SplitN(pathTrimmed, "/items/", 2)
		if len(parts) == 2 {
			threadID := parts[0]
			itemParts := strings.SplitN(parts[1], "/", 2)
			if len(itemParts) == 2 {
				store.RemoveFromThread(threadID, itemParts[0], itemParts[1])
				web.SendJSON(w, map[string]any{"ok": true})
				return true
			}
		}
		web.SendError(w, 400, "invalid thread item path")
	case strings.HasPrefix(path, "task-notes/"):
		web.SendJSON(w, map[string]any{"ok": true})
	case strings.HasPrefix(path, "links/"):
		id := strings.TrimPrefix(path, "links/")
		if err := store.DeleteLink(id); err != nil {
			web.SendError(w, 500, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"ok": true})
	case strings.HasPrefix(path, "tasks/"):
		id := strings.TrimPrefix(path, "tasks/")
		if err := store.DeleteTask(id); err != nil {
			web.SendError(w, 500, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"ok": true})
	case strings.HasPrefix(path, "plans/"):
		id := strings.TrimPrefix(path, "plans/")
		if err := store.DeletePlan(id); err != nil {
			web.SendError(w, 500, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"ok": true})
	case strings.HasPrefix(path, "bugs/"):
		id := strings.TrimPrefix(path, "bugs/")
		if err := store.DeleteBug(id); err != nil {
			web.SendError(w, 500, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"ok": true})
	default:
		return false
	}
	return true
}

func (s *Server) handleMeetingTyping(w http.ResponseWriter, method, path string, body map[string]any) bool {
	if method != "POST" || !strings.HasPrefix(path, "meetings/") || !strings.HasSuffix(path, "/typing") {
		return false
	}
	roomID := extractID(path, "meetings/", "/typing")
	typing := 0
	if v, ok := body["pm_typing"]; ok {
		if b, ok := v.(bool); ok && b {
			typing = 1
		}
		if f, ok := v.(float64); ok && f > 0 {
			typing = 1
		}
	}
	db, err := pmdb.Open()
	if err == nil {
		defer db.Close()
		db.Exec("UPDATE meeting_rooms SET pm_typing = ? WHERE id = ?", typing, roomID)
	}
	web.SendJSON(w, map[string]any{"ok": true, "pm_typing": typing})
	return true
}

func (s *Server) handleGetByID(w http.ResponseWriter, method, path string) bool {
	if method != "GET" {
		return false
	}
	entity, id := parseEntityID(path)
	if id == "" {
		return false
	}
	handleGetEntity(w, entity, id)
	return true
}
