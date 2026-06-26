package api

import (
	"net/http"

	pmdb "aipmc/db"
	"aipmc/u"
	"aipmc/web"
)

func (s *Server) handleGetConfig(w http.ResponseWriter) {
	cfg := pmdb.LoadConfig()
	gcfg := pmdb.LoadGlobalConfig()
	web.SendJSON(w, map[string]any{
		"ai_endpoint":           cfg.AIEndpoint,
		"ai_embedding_endpoint": cfg.AIEmbeddingEndpoint,
		"ai_model":              cfg.AIModel,
		"ai_chat_model":         cfg.AIChatModel,
		"ai_enabled":            cfg.AIEndpoint != "",
		"web_host":              cfg.WebHost,
		"web_port":              cfg.WebPort,
		"proxy_port":            gcfg.ProxyPort,
		"upstream_url":          gcfg.UpstreamURL,
		"proxy_model":           gcfg.ProxyModel,
		"proxy_log_dir":         gcfg.ProxyLogDir,
	})
}

func (s *Server) handlePostConfig(w http.ResponseWriter, body map[string]any) {
	cfg := pmdb.LoadConfig()
	gcfg := pmdb.LoadGlobalConfig()
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
	if v, ok := body["proxy_port"]; ok {
		if f, ok := v.(float64); ok {
			gcfg.ProxyPort = int(f)
		}
	}
	if v := u.Str(body["upstream_url"]); v != "" {
		gcfg.UpstreamURL = v
	}
	gcfg.ProxyModel = u.Str(body["proxy_model"])
	gcfg.ProxyLogDir = u.Str(body["proxy_log_dir"])
	if err := pmdb.SaveConfig(cfg); err != nil {
		web.SendError(w, 500, err.Error())
		return
	}
	if err := pmdb.SaveGlobalConfig(gcfg); err != nil {
		web.SendError(w, 500, err.Error())
		return
	}
	s.deps.App.ReloadAI()
	web.SendJSON(w, map[string]any{"ok": true, "ai_enabled": cfg.AIEndpoint != ""})
}
