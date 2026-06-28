package api

import (
	"fmt"
	"io"
	"net/http"
	"net/url"

	pmdb "aipmc/db"
	"aipmc/mcp"
	"aipmc/store"
	"aipmc/u"
	"aipmc/web"
)

func (s *Server) handleQueryRoutes(w http.ResponseWriter, method, path string, q url.Values) bool {
	if method != "GET" {
		return false
	}
	app := s.deps.App
	switch path {
	case "search":
		web.SendJSON(w, app.SearchProjectContext(q.Get("q"), 8))
	case "smart-search":
		web.SendJSON(w, app.SearchProjectContext(q.Get("q"), 8))
	case "dashboard":
		web.SendJSON(w, app.StatusSnapshot())
	case "context":
		web.SendJSON(w, app.ContextPack())
	case "next":
		web.SendJSON(w, app.NextActionPacket())
	case "inbox":
		web.SendJSON(w, app.InboxSummary())
	case "events":
		events, _ := store.ListEvents(q.Get("filter"))
		web.SendJSON(w, map[string]any{"events": events})
	case "feedbacks":
		fbs, _ := mcp.ListFeedbacks(q.Get("label"))
		web.SendJSON(w, map[string]any{"feedbacks": fbs})
	case "canon":
		c, _ := store.GetCanon()
		web.SendJSON(w, c)
	case "daily":
		d, _ := store.GetDailyNote(q.Get("date"))
		web.SendJSON(w, d)
	case "daily/history":
		d, _ := store.ListDailyNotes()
		web.SendJSON(w, map[string]any{"daily_notes": d})
	case "docs/content":
		c, err := store.ReadDocContent(q.Get("path"))
		if err != nil {
			web.SendError(w, 404, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"content": c, "path": q.Get("path")})
	case "discussions":
		page := 1
		if p := q.Get("page"); p != "" {
			fmt.Sscanf(p, "%d", &page)
		}
		results, total, _ := app.SearchDiscussions(q.Get("q"), q.Get("source"), q.Get("type"), q.Get("project_path"), page, 20)
		web.SendJSON(w, map[string]any{"discussions": results, "total": total, "page": page})
	case "discussions/sources":
		sources, _ := store.ListDiscussionSources()
		web.SendJSON(w, map[string]any{"sources": sources})
	case "config":
		s.handleGetConfig(w)
	case "proxy-status":
		proxyForward(w, "status")
	case "proxy-traffic":
		proxyForward(w, "traffic")
	case "agent/sessions":
		s.handleAgentSessions(w)
	default:
		return false
	}
	return true
}

func (s *Server) handleMutateRoutes(w http.ResponseWriter, method, path string, q url.Values, body map[string]any) bool {
	app := s.deps.App
	switch {
	case method == "POST" && path == "feedbacks":
		fb, err := mcp.AddFeedback(u.Str(body["label"]), u.Str(body["content"]))
		if err != nil {
			web.SendJSON(w, map[string]any{"status": "stored_locally", "detail": err.Error()})
			return true
		}
		web.SendJSON(w, fb)
	case method == "POST" && path == "events":
		evt, _ := store.CreateEvent(u.Str(body["type"]), u.Str(body["entity_type"]), u.Str(body["entity_id"]), u.Str(body["summary"]))
		web.SendJSON(w, evt)
	case method == "POST" && path == "daily":
		d, _ := store.AppendDailyNote(q.Get("date"), map[string][]string{})
		web.SendJSON(w, d)
	case method == "PUT" && path == "daily":
		d, _ := store.ReplaceDailyNote(q.Get("date"), map[string][]string{})
		web.SendJSON(w, d)
	case method == "POST" && path == "discussions":
		d, err := store.LogDiscussion(u.Str(body["session_id"]), u.Str(body["role"]), u.Str(body["source"]), u.Str(body["content"]), "")
		if err != nil {
			web.SendError(w, 400, err.Error())
			return true
		}
		web.SendJSON(w, d)
	case method == "POST" && path == "discussions/embed":
		count, err := app.EmbedDiscussions(100)
		if err != nil {
			web.SendError(w, 500, err.Error())
			return true
		}
		web.SendJSON(w, map[string]any{"ok": true, "embedded": count})
	case method == "POST" && path == "ai-test":
		s.handleAITest(w)
	case method == "POST" && path == "agent/launch":
		s.handleAgentLaunch(w, body)
	case method == "POST" && path == "proxy/stop":
		s.handleProxyStop(w, body)
	case method == "POST" && path == "proxy/restart":
		s.handleProxyRestart(w, body)
	case method == "POST" && path == "config":
		s.handlePostConfig(w, body)
	case method == "DELETE" && path == "proxy-traffic":
		proxyForwardDelete(w, "traffic")
	default:
		return false
	}
	return true
}

func (s *Server) handleAITest(w http.ResponseWriter) {
	client := s.deps.App.AI
	if client == nil || !client.Enabled() {
		web.SendJSON(w, map[string]any{"ok": false, "error": "AI 未配置"})
		return
	}
	_, err := client.Embed([]string{"test"})
	if err != nil {
		web.SendJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	web.SendJSON(w, map[string]any{"ok": true, "message": "AI 连接正常"})
}

func proxyForward(w http.ResponseWriter, endpoint string) {
	gcfg := pmdb.LoadGlobalConfig()
	url := fmt.Sprintf("http://127.0.0.1:%d/__proxy/%s", gcfg.ProxyPort, endpoint)
	resp, err := http.Get(url)
	if err != nil {
		web.SendJSON(w, map[string]any{"running": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func proxyForwardDelete(w http.ResponseWriter, endpoint string) {
	gcfg := pmdb.LoadGlobalConfig()
	url := fmt.Sprintf("http://127.0.0.1:%d/__proxy/%s", gcfg.ProxyPort, endpoint)
	req, _ := http.NewRequest("DELETE", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		web.SendJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

