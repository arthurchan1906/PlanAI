package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"

	"aipmc/agent"
	"aipmc/web"
)

func (s *Server) handleChatRoutes(w http.ResponseWriter, method, path string, q url.Values, body map[string]any) bool {
	switch {
	case method == "GET" && path == "chat/sessions":
		s.handleChatListSessions(w)
	case method == "GET" && path == "chat/session":
		s.handleChatGetSession(w, q.Get("id"))
	case method == "POST" && path == "chat/send/stream":
		s.handleChatSendStream(w, body)
	case method == "POST" && path == "chat/send":
		s.handleChatSend(w, body)
	default:
		return false
	}
	return true
}

func (s *Server) handleChatListSessions(w http.ResponseWriter) {
	sessions, err := agent.ListSessions(agent.ProjectWorkDir())
	if err != nil {
		web.SendError(w, 500, err.Error())
		return
	}
	out := make([]map[string]any, len(sessions))
	for i, sess := range sessions {
		out[i] = map[string]any{"id": sess.ID, "updated_at": sess.UpdatedAt, "events": sess.Events}
	}
	web.SendJSON(w, map[string]any{"sessions": out})
}

func (s *Server) handleChatGetSession(w http.ResponseWriter, sid string) {
	if sid == "" {
		web.SendError(w, 400, "缺少 id 参数")
		return
	}
	if !agent.IsValidSessionID(sid) {
		web.SendError(w, 400, "非法会话 id")
		return
	}
	sessPath := filepath.Join(agent.SessionDir(agent.ProjectWorkDir()), sid+".json")
	sess, err := agent.LoadSession(sessPath)
	if err != nil {
		web.SendError(w, 404, "会话不存在: "+sid)
		return
	}
	web.SendJSON(w, map[string]any{
		"id":         sess.ID,
		"events":     sess.Events,
		"traces":     sess.Traces,
		"created_at": sess.CreatedAt,
		"updated_at": sess.UpdatedAt,
	})
}

func (s *Server) handleChatSend(w http.ResponseWriter, body map[string]any) {
	if s.deps.App.AI() == nil || !s.deps.App.AI().Enabled() {
		web.SendError(w, 503, "AI 未配置。请设置 AI_ENDPOINT 环境变量。")
		return
	}
	msg, _ := body["message"].(string)
	sid, _ := body["session_id"].(string)
	if msg == "" {
		web.SendError(w, 400, "缺少 message 参数")
		return
	}
	svc := agent.NewChatService(s.deps.App.AI(), agent.ProjectWorkDir(), "aipmc-web")
	result, err := svc.Send(sid, msg)
	if err != nil {
		web.SendError(w, 500, fmt.Sprintf("Agent 错误: %v", err))
		return
	}
	web.SendJSON(w, map[string]any{
		"session_id": result.SessionID,
		"response":   result.Response,
		"events":     result.Events,
		"traces":     result.Traces,
	})
}

func (s *Server) handleChatSendStream(w http.ResponseWriter, body map[string]any) {
	if s.deps.App.AI() == nil || !s.deps.App.AI().Enabled() {
		web.SendError(w, 503, "AI 未配置。请设置 AI_ENDPOINT 环境变量。")
		return
	}
	msg, _ := body["message"].(string)
	sid, _ := body["session_id"].(string)
	if msg == "" {
		web.SendError(w, 400, "缺少 message 参数")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		web.SendError(w, 500, "streaming not supported")
		return
	}

	web.CORS(w)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	writeSSE := func(event string, data map[string]any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		flusher.Flush()
	}

	svc := agent.NewChatService(s.deps.App.AI(), agent.ProjectWorkDir(), "aipmc-web")
	result, err := svc.SendStream(sid, msg, writeSSE)
	if err != nil {
		writeSSE("error", map[string]any{"message": err.Error()})
		return
	}
	writeSSE("done", map[string]any{
		"session_id": result.SessionID,
		"response":   result.Response,
		"events":     result.Events,
		"traces":     result.Traces,
	})
}
