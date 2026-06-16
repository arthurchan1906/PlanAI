package api

import (
	"fmt"
	"net/http"

	"aipmc/web"
)

// Server is the HTTP API handler for /pmai/* routes.
type Server struct {
	deps Deps
}

// New creates an API server with the given dependencies.
func New(deps Deps) *Server {
	return &Server{deps: deps}
}

// Handler returns the server as an http.Handler.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.ServeHTTP)
}

// ServeHTTP dispatches requests to domain-specific handlers.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := apiPath(r)

	defer func() {
		if rec := recover(); rec != nil {
			web.SendError(w, 500, fmt.Sprintf("%v", rec))
		}
	}()

	method := r.Method
	q := r.URL.Query()
	body := readBody(r)

	if s.handleChatRoutes(w, method, path, q, body) {
		return
	}
	if s.handleWebRoutes(w, method, path) {
		return
	}
	if s.handleQueryRoutes(w, method, path, q) {
		return
	}
	if s.handleMutateRoutes(w, method, path, q, body) {
		return
	}
	if s.handleListRoutes(w, method, path, q) {
		return
	}
	if s.handleCreateRoutes(w, method, path, body) {
		return
	}
	if s.handleNestedPostRoutes(w, method, path, body) {
		return
	}
	if s.handleDeleteRoutes(w, method, path) {
		return
	}
	if s.handlePatchRoutes(w, method, path, body) {
		return
	}
	if s.handleGetByID(w, method, path) {
		return
	}

	web.SendError(w, 404, fmt.Sprintf("not found: %s %s", method, path))
}
