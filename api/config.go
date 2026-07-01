package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	pmdb "aipmc/db"
	"aipmc/u"
	"aipmc/web"
)

func (s *Server) handleGetConfig(w http.ResponseWriter) {
	cfg := pmdb.LoadConfig()
	gcfg := pmdb.LoadGlobalConfig()
	reg := pmdb.LoadModelRegistry()
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
		"anthropic_url":         gcfg.AnthropicURL,
		"extra_env":             gcfg.ExtraEnv,
		"default_model":         gcfg.DefaultModel,
		"claude":                gcfg.Claude,
		"codex":                 gcfg.Codex,
		"gemini":                gcfg.Gemini,
		"opencode":              gcfg.OpenCode,
		"providers":             reg.Providers,
		"models":                reg.Models,
		"project_model":         cfg.Model,
		"agent_overrides":       cfg.AgentOverrides,
	})
}

// mapProfile round-trips a map[string]any through JSON to a typed struct.
func mapProfile[T any](m map[string]any) T {
	data, _ := json.Marshal(m)
	var p T
	json.Unmarshal(data, &p)
	return p
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
	gcfg.AnthropicURL = u.Str(body["anthropic_url"])

	// Parse per-agent profiles via json round-trip
	if v, ok := body["claude"].(map[string]any); ok {
		gcfg.Claude = mapProfile[pmdb.ClaudeProfile](v)
	}
	if v, ok := body["codex"].(map[string]any); ok {
		gcfg.Codex = mapProfile[pmdb.CodexProfile](v)
	}
	if v, ok := body["gemini"].(map[string]any); ok {
		gcfg.Gemini = mapProfile[pmdb.GeminiProfile](v)
	}
	if v, ok := body["opencode"].(map[string]any); ok {
		gcfg.OpenCode = mapProfile[pmdb.OpenCodeProfile](v)
	}

	if v, ok := body["extra_env"]; ok {
		if m, ok := v.(map[string]any); ok {
			gcfg.ExtraEnv = make(map[string]string)
			for k, val := range m {
				if s, ok := val.(string); ok {
					gcfg.ExtraEnv[k] = s
				}
			}
		}
	} else {
		gcfg.ExtraEnv = nil
	}
	// Handle new fields: default_model, project_model, agent_overrides
	if v := u.Str(body["project_model"]); v != "" {
		cfg.Model = v
	}
	if v := u.Str(body["default_model"]); v != "" {
		gcfg.DefaultModel = v
	}
	if v, ok := body["agent_overrides"]; ok {
		if raw, ok := v.(map[string]any); ok {
			cfg.AgentOverrides = map[string]pmdb.AgentOverride{}
			for k, val := range raw {
				if sub, ok := val.(map[string]any); ok {
					cfg.AgentOverrides[k] = pmdb.AgentOverride{
						Model:           u.Str(sub["model"]),
						EffortLevel:     u.Str(sub["effort_level"]),
						SubAgentModel:   u.Str(sub["sub_agent_model"]),
						OpusModel:       u.Str(sub["opus_model"]),
						SonnetModel:     u.Str(sub["sonnet_model"]),
						HaikuModel:      u.Str(sub["haiku_model"]),
						SmallFastModel:  u.Str(sub["small_fast_model"]),
						ReasoningEffort: u.Str(sub["reasoning_effort"]),
					}
				}
			}
		}
	}
	// Handle models.json: load once, modify if needed, save once.
	hasProviders := body["providers"] != nil
	hasModels := body["models"] != nil
	if hasProviders || hasModels {
		reg := pmdb.LoadModelRegistry()
		if hasProviders {
			if provs, ok := body["providers"].([]any); ok {
				reg.Providers = make([]pmdb.Provider, 0, len(provs))
				for _, p := range provs {
					if pm, ok := p.(map[string]any); ok {
						reg.Providers = append(reg.Providers, pmdb.Provider{
							Name:         u.Str(pm["name"]),
							OpenAIURL:    u.Str(pm["openai_url"]),
							AnthropicURL: u.Str(pm["anthropic_url"]),
							APIKeyEnv:    u.Str(pm["api_key_env"]),
						})
					}
				}
			}
		}
		if hasModels {
			if mods, ok := body["models"].([]any); ok {
				reg.Models = make([]pmdb.VirtualModel, 0, len(mods))
				for _, m := range mods {
					if mm, ok := m.(map[string]any); ok {
						vm := pmdb.VirtualModel{
							ID:          u.Str(mm["id"]),
							Provider:    u.Str(mm["provider"]),
							DisplayName: u.Str(mm["display_name"]),
							Anthropic:   u.Str(mm["anthropic"]),
							OpenAI:      u.Str(mm["openai"]),
							VisibleTo:   u.Str(mm["visible_to"]),
						}
						if tags, ok := mm["tags"].([]any); ok {
							for _, t := range tags {
								if s, ok := t.(string); ok {
									vm.Tags = append(vm.Tags, s)
								}
							}
						}
						if p, ok := mm["priority"].(float64); ok {
							vm.Priority = int(p)
						}
						reg.Models = append(reg.Models, vm)
					}
				}
			}
		}
		reg.Version = 1
		pmdb.SaveModelRegistry(reg)
	}


	if err := pmdb.SaveConfig(cfg); err != nil {
		web.SendError(w, 500, err.Error())
		return
	}
	if err := pmdb.SaveGlobalConfig(gcfg); err != nil {
		web.SendError(w, 500, err.Error())
		return
	}
	// Sync opencode.json provider models with the saved model list
	if cwd, err := os.Getwd(); err == nil {
		pmdb.SyncOpencodeModels(cwd, gcfg.OpenCode.Models)
	}
	s.deps.App.ReloadAI()

	// Update projects.json with the new web_port so serve can pick it up
	if cwd, err := os.Getwd(); err == nil {
		pmdb.SaveProject(pmdb.ProjectEntry{
			Path:      cwd,
			Name:      filepath.Base(cwd),
			WebPort:   cfg.WebPort,
			ProxyPort: gcfg.ProxyPort,
		})
	}

	// Reload proxy config if proxy is running (best-effort, ignore errors)
	go func() {
		http.Post(
			fmt.Sprintf("http://127.0.0.1:%d/__proxy/reload", gcfg.ProxyPort),
			"application/json", nil,
		)
	}()

	web.SendJSON(w, map[string]any{"ok": true, "ai_enabled": cfg.AIEndpoint != ""})
}
