package api

import (
	"fmt"
	"net/http"
	"net/url"

	"aipmc/ai"
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
	case method == "POST" && path == "config":
		s.handlePostConfig(w, body)
	case method == "POST" && path == "arbitrate":
		s.handleArbitrate(w, body)
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

func (s *Server) handleArbitrate(w http.ResponseWriter, body map[string]any) {
	client := s.deps.App.AI
	if client == nil {
		web.SendError(w, 503, "AI 未配置")
		return
	}
	roomID := u.Str(body["room_id"])
	room, err := store.GetMeetingRoom(roomID)
	if err != nil {
		web.SendError(w, 404, err.Error())
		return
	}
	turns, _ := store.ListMeetingTurns(roomID)
	var recent []ai.ArbitrationTurn
	start := 0
	if len(turns) > 8 {
		start = len(turns) - 8
	}
	for i := start; i < len(turns); i++ {
		t := turns[i]
		txt := u.Str(t["question"])
		if r := u.Str(t["response"]); r != "" {
			txt = r
		}
		recent = append(recent, ai.ArbitrationTurn{
			SpeakerType: u.Str(t["speaker_type"]),
			SpeakerID:   u.Str(t["speaker_id"]),
			Content:     txt,
			AddressTo:   u.Str(t["address_to"]),
		})
	}
	next, reason, err := client.ArbitrateNextSpeaker(u.Str(room["topic"]), u.Str(room["agent_roles_context"]), recent)
	if err != nil {
		web.SendError(w, 500, err.Error())
		return
	}
	existing, _ := store.ListMeetingTurns(roomID)
	nextNum := len(existing) + 1
	store.CreateMeetingTurn(roomID, nextNum, "agent", next, fmt.Sprintf("[AI 仲裁] %s。请就此发表意见。", reason))
	web.SendJSON(w, map[string]any{"next_agent": next, "reason": reason})
}
