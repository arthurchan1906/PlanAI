package api

import (
	"encoding/json"
	"fmt"
	"log"
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
		"api_keys":              credentialKeyList(),
		"current_profile":       pmdb.CurrentProfile(),
		"all_profiles":           pmdb.ListProfiles(),
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
				if len(provs) == 0 {
					web.SendError(w, 400, "providers cannot be an empty array — send null or omit the field to keep existing providers")
					return
				}
				reg.Providers = make([]pmdb.Provider, 0, len(provs))
				for _, p := range provs {
					if pm, ok := p.(map[string]any); ok {
						reg.Providers = append(reg.Providers, pmdb.Provider{
							Name:         u.Str(pm["name"]),
							OpenAIURL:    u.Str(pm["openai_url"]),
							AnthropicURL: u.Str(pm["anthropic_url"]),
						})
					}
				}
			} else {
				web.SendError(w, 400, "providers must be a JSON array")
				return
			}
		}
		if hasModels {
			if mods, ok := body["models"].([]any); ok {
				if len(mods) == 0 {
					web.SendError(w, 400, "models cannot be an empty array — send null or omit the field to keep existing models")
					return
				}
				reg.Models = make([]pmdb.VirtualModel, 0, len(mods))
				for _, m := range mods {
					if mm, ok := m.(map[string]any); ok {
						vm := pmdb.VirtualModel{
							ID:          u.Str(mm["id"]),
							DisplayName: u.Str(mm["display_name"]),
							VisibleTo:   u.Str(mm["visible_to"]),
						}
						// Parse routes (new format).
						if routes, ok := mm["routes"].([]any); ok {
							for _, r := range routes {
								if rm, ok := r.(map[string]any); ok {
									vm.Routes = append(vm.Routes, pmdb.ModelRoute{
										Provider:       u.Str(rm["provider"]),
										ModelOpenAI:    u.Str(rm["model_openai"]),
										ModelAnthropic: u.Str(rm["model_anthropic"]),
									})
								}
							}
						}
						// Backward compat: if no routes but provider is set, create single route.
						if len(vm.Routes) == 0 {
							provider := u.Str(mm["provider"])
							if provider != "" {
								vm.Routes = []pmdb.ModelRoute{{
									Provider:       provider,
									ModelAnthropic: u.Str(mm["anthropic"]),
									ModelOpenAI:    u.Str(mm["openai"]),
								}}
							}
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
			} else {
				web.SendError(w, 400, "models must be a JSON array")
				return
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

// ── Credentials API ────────────────────────────────────────────────────

// credentialKeyList returns the list of keys from the in-memory store (masked).
func credentialKeyList() map[string]string {
	store := pmdb.GetCredentialStore()
	if store == nil {
		return nil
	}
	out := map[string]string{}
	for _, name := range store.List() {
		key := store.Get(name)
		if len(key) > 10 {
			out[name] = key[:6] + "..." + key[len(key)-4:]
		} else {
			out[name] = key
		}
	}
	return out
}

// handleCredentials handles POST /pmai/credentials.
// Actions: unlock, lock, init, set, delete, passwd, list-profiles, create-profile, delete-profile
func (s *Server) handleCredentials(w http.ResponseWriter, body map[string]any) {
	action, _ := body["action"].(string)
	profile, _ := body["profile"].(string)
	if profile == "" { profile = "default" }

	switch action {
	case "unlock":
		password, _ := body["password"].(string)
		if password == "" { web.SendError(w, 400, "password required"); return }
		store, err := pmdb.LoadCredentialsProfile([]byte(password), profile)
		if err != nil || store == nil { web.SendError(w, 401, "wrong password"); return }
		store.UnlockSession([]byte(password))
		pmdb.SetCredentialStore(store)
		web.SendJSON(w, map[string]any{"ok": true, "unlocked": len(store.Keys), "timeout": 1800, "profile": profile})

	case "lock":
		pmdb.Lock()
		web.SendJSON(w, map[string]any{"ok": true})

	case "init":
		password, _ := body["password"].(string)
		if password == "" { web.SendError(w, 400, "password required"); return }
		if pmdb.CredentialsExistForProfile(profile) { web.SendError(w, 409, "credentials already exist for profile "+profile); return }
		store := &pmdb.CredentialStore{Keys: map[string]string{}, Profile: profile}
		if err := pmdb.SaveCredentialsToProfile(store, []byte(password), profile); err != nil { web.SendError(w, 500, err.Error()); return }
		web.SendJSON(w, map[string]any{"ok": true, "profile": profile})

	case "set":
		provider, _ := body["provider"].(string)
		key, _ := body["key"].(string)
		if provider == "" || key == "" { web.SendError(w, 400, "provider and key required"); return }
		// Always require password for key modification
		password, _ := body["password"].(string)
		if password == "" { web.SendError(w, 400, "password required to modify keys"); return }
		store, err := pmdb.LoadCredentialsProfile([]byte(password), profile)
		if err != nil || store == nil { web.SendError(w, 401, "wrong password"); return }
		store.Set(provider, key)
		store.Profile = profile
		pmdb.SaveCredentialsToProfile(store, []byte(password), profile)
		if old := pmdb.GetCredentialStore(); old != nil && old != store { old.Lock() }
		pmdb.SetCredentialStore(store)
		log.Printf("[CRED] set key provider=%q keyPrefix=%s profile=%s", provider, key[:min(8, len(key))], profile)
		web.SendJSON(w, map[string]any{"ok": true})

	case "delete":
		provider, _ := body["provider"].(string)
		if provider == "" { web.SendError(w, 400, "provider required"); return }
		// Always require password for key modification
		password, _ := body["password"].(string)
		if password == "" { web.SendError(w, 400, "password required to modify keys"); return }
		store, err := pmdb.LoadCredentialsProfile([]byte(password), profile)
		if err != nil || store == nil { web.SendError(w, 401, "wrong password"); return }
		store.Remove(provider)
		store.Profile = profile
		pmdb.SaveCredentialsToProfile(store, []byte(password), profile)
		if old := pmdb.GetCredentialStore(); old != nil && old != store { old.Lock() }
		pmdb.SetCredentialStore(store)
		web.SendJSON(w, map[string]any{"ok": true})

	case "passwd":
		oldPass, _ := body["old_password"].(string)
		newPass, _ := body["new_password"].(string)
		if oldPass == "" || newPass == "" { web.SendError(w, 400, "old_password and new_password required"); return }
		if err := pmdb.ChangePasswordForProfile([]byte(oldPass), []byte(newPass), profile); err != nil { web.SendError(w, 401, err.Error()); return }
		web.SendJSON(w, map[string]any{"ok": true})

	case "list-profiles":
		web.SendJSON(w, map[string]any{
			"profiles": pmdb.ListProfiles(),
		})

	case "create-profile":
		name, _ := body["profile"].(string)
		password, _ := body["password"].(string)
		if name == "" || password == "" { web.SendError(w, 400, "profile name and password required"); return }
		if err := pmdb.CreateProfile(name, password); err != nil { web.SendError(w, 409, err.Error()); return }
		web.SendJSON(w, map[string]any{"ok": true, "profile": name})

	case "delete-profile":
		name, _ := body["profile"].(string)
		if name == "" { web.SendError(w, 400, "profile name required"); return }
		if err := pmdb.DeleteProfile(name); err != nil { web.SendError(w, 400, err.Error()); return }
		web.SendJSON(w, map[string]any{"ok": true})

	default:
		web.SendError(w, 400, "unknown action: "+action)
	}
}