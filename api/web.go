package api

import (
	"net/http"
	"strings"

	"aipmc/web"
	"aipmc/webdata"
)

func (s *Server) handleWebRoutes(w http.ResponseWriter, method, path string) bool {
	if method != "GET" || !strings.HasPrefix(path, "web/") {
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
	case "web/bootstrap":
		s.handleBootstrap(w)
	default:
		return false
	}
	return true
}
