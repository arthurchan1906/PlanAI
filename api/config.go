package api

import (
	"net/http"

	pmdb "aipmc/db"
	"aipmc/u"
	"aipmc/web"
)

func (s *Server) handleGetConfig(w http.ResponseWriter) {
	cfg := pmdb.LoadConfig()
	web.SendJSON(w, map[string]any{
		"ai_endpoint":           cfg.AIEndpoint,
		"ai_embedding_endpoint": cfg.AIEmbeddingEndpoint,
		"ai_model":              cfg.AIModel,
		"ai_chat_model":         cfg.AIChatModel,
		"ai_enabled":            cfg.AIEndpoint != "",
		"web_host":              cfg.WebHost,
		"web_port":              cfg.WebPort,
	})
}

func (s *Server) handlePostConfig(w http.ResponseWriter, body map[string]any) {
	cfg := pmdb.LoadConfig()
	if v := u.Str(body["ai_endpoint"]); v != "" {
		cfg.AIEndpoint = v
	}
	if v := u.Str(body["ai_embedding_endpoint"]); v != "" {
		cfg.AIEmbeddingEndpoint = v
	}
	if v := u.Str(body["ai_model"]); v != "" {
		cfg.AIModel = v
	}
	if v := u.Str(body["ai_chat_model"]); v != "" {
		cfg.AIChatModel = v
	}
	if v := u.Str(body["web_host"]); v != "" {
		cfg.WebHost = v
	}
	if v, ok := body["web_port"]; ok {
		if f, ok := v.(float64); ok {
			cfg.WebPort = int(f)
		}
	}
	if err := pmdb.SaveConfig(cfg); err != nil {
		web.SendError(w, 500, err.Error())
		return
	}
	s.deps.App.ReloadAI()
	web.SendJSON(w, map[string]any{"ok": true, "ai_enabled": cfg.AIEndpoint != ""})
}
