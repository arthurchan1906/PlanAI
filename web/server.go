// Package web provides the HTTP server and API for AIPM.
package web

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
)

// Server serves the AIPM web UI and API.
type Server struct {
	staticFS   fs.FS
	apiHandler http.Handler
	host       string
	port       int
}

// NewServer creates a Server. staticFS provides the React frontend files.
// apiHandler handles /pmai/ API requests.
func NewServer(staticFS fs.FS, apiHandler http.Handler, host string, port int) *Server {
	return &Server{
		staticFS:   staticFS,
		apiHandler: apiHandler,
		host:       host,
		port:       port,
	}
}

// Listen starts the HTTP server and blocks until it exits.
func (s *Server) Listen() error {
	apiMux := http.NewServeMux()

	apiMux.HandleFunc("/pmai/", func(w http.ResponseWriter, r *http.Request) {
		CORS(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		s.apiHandler.ServeHTTP(w, r)
	})
	apiMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		SendJSON(w, map[string]any{"status": "ok"})
	})

	fileServer := http.FileServer(http.FS(s.staticFS))
	wrapper := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/pmai/") || r.URL.Path == "/health" {
			CORS(w)
			if r.Method == http.MethodOptions {
				w.WriteHeader(204)
				return
			}
			apiMux.ServeHTTP(w, r)
			return
		}

		if r.Method != "GET" && r.Method != "HEAD" {
			http.NotFound(w, r)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if path == "index.html" {
			data, err := fs.ReadFile(s.staticFS, "index.html")
			if err == nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write(data)
				return
			}
		}

		f, err := s.staticFS.Open(path)
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback
		data, err := fs.ReadFile(s.staticFS, "index.html")
		if err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
			return
		}

		http.NotFound(w, r)
	})

	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	fmt.Printf("AIPM web server listening on http://%s\n", addr)
	if err := http.ListenAndServe(addr, wrapper); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
	return nil
}

// ── HTTP helpers ──────────────────────────────────────────────────────

// CORS sets CORS headers on the response.
func CORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

// SendJSON writes v as a JSON response.
func SendJSON(w http.ResponseWriter, v any) {
	CORS(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// SendError writes a JSON error response.
func SendError(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": detail})
}
