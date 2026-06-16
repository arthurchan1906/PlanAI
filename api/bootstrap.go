package api

import (
	"net/http"

	"aipmc/web"
)

// handleBootstrap is deprecated; the web UI loads data via /pmai/web/* endpoints.
func (s *Server) handleBootstrap(w http.ResponseWriter) {
	web.SendJSON(w, map[string]any{
		"deprecated": true,
		"message":    "Use GET /pmai/web/{planning,commits,bugs,decisions,ideas,docs,threads,agents,audit,code,daily} instead",
	})
}
