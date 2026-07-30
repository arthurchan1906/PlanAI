package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	pmdb "aipmc/db"
	"aipmc/store"
	"aipmc/u"
	"aipmc/web"
	"aipmc/webdata"
)

func (s *Server) handleWebRoutes(w http.ResponseWriter, method, path string, body []byte) bool {
	if !strings.HasPrefix(path, "web/") {
		return false
	}

	// POST routes for mutations
	if method == "POST" {
		switch path {
		case "web/guidelines":
			if err := writeGuidelines(body); err != nil {
				web.SendJSON(w, map[string]any{"ok": false, "error": err.Error()})
				return true
			}
			web.SendJSON(w, map[string]any{"ok": true})
			return true
		case "web/events/consume":
			if err := store.MarkEventsConsumed(); err != nil {
				web.SendJSON(w, map[string]any{"ok": false, "error": err.Error()})
				return true
			}
			// Return remaining unconsumed count
			events, _ := store.GetUnconsumedEvents()
			web.SendJSON(w, map[string]any{"ok": true, "remaining": len(events)})
			return true
		}
		return false
	}

	if method != "GET" {
		return false
	}
	switch path {
	case "web/planning":
		web.SendJSON(w, webdata.PlanningPayload())
	case "web/commits":
		web.SendJSON(w, webdata.CommitsPayload())
	case "web/bugs":
		web.SendJSON(w, webdata.BugsPayload())
	case "web/decisions":
		web.SendJSON(w, webdata.DecisionsPayload())
	case "web/ideas":
		web.SendJSON(w, webdata.IdeasPayload())
	case "web/docs":
		web.SendJSON(w, webdata.DocsPayload())
	case "web/threads":
		web.SendJSON(w, webdata.ThreadsPayload())
	case "web/agents":
		web.SendJSON(w, webdata.AgentsPayload())
	case "web/audit":
		web.SendJSON(w, webdata.AuditPayload())
	case "web/code":
		web.SendJSON(w, webdata.CodePayload())
	case "web/daily":
		web.SendJSON(w, webdata.DailyPayload())
	case "web/guidelines":
		web.SendJSON(w, readGuidelines())
		return true
	case "web/activity":
		web.SendJSON(w, webdata.ActivityPayload())
	case "web/events":
		events, _ := store.GetUnconsumedEvents()
		if events == nil {
			events = []map[string]any{}
		}
		// Filter to tentative_link only for activity view
		var tentative []map[string]any
		for _, e := range events {
			if u.Str(e["type"]) == "tentative_link" {
				tentative = append(tentative, e)
			}
		}
		if tentative == nil {
			tentative = []map[string]any{}
		}
		web.SendJSON(w, map[string]any{"events": tentative})
	case "web/bootstrap":
		s.handleBootstrap(w)
	default:
		return false
	}
	return true
}

// readGuidelines reads .pmai/guidelines.md and returns {content: "..."}.
func readGuidelines() map[string]any {
	dir, err := pmdb.RuntimeDir()
	if err != nil {
		return map[string]any{"content": ""}
	}
	data, err := os.ReadFile(filepath.Join(dir, "guidelines.md"))
	if err != nil {
		return map[string]any{"content": ""}
	}
	return map[string]any{"content": string(data)}
}

// writeGuidelines writes body["content"] to .pmai/guidelines.md.
func writeGuidelines(body []byte) error {
	var req struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return err
	}
	dir, err := pmdb.RuntimeDir()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "guidelines.md"), []byte(req.Content), 0644)
}
