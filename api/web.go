package api

import (
	"net/http"
	"strings"

	"aipmc/store"
	"aipmc/u"
	"aipmc/web"
	"aipmc/webdata"
)

func (s *Server) handleWebRoutes(w http.ResponseWriter, method, path string) bool {
	if !strings.HasPrefix(path, "web/") {
		return false
	}

	// POST routes for mutations
	if method == "POST" {
		switch path {
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
