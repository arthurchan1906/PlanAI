package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"aipmc/u"
)

// ── Model Registry (virtual model routing) ──────────────────────────────────

// Provider defines a real LLM backend that the proxy can route to.
type Provider struct {
	Name         string `json:"name"`
	OpenAIURL    string `json:"openai_url"`
	AnthropicURL string `json:"anthropic_url,omitempty"`
	ResponsesURL string `json:"responses_url,omitempty"` // base URL，透传时拼 /responses
}

// ModelRoute describes how to reach a virtual model through a specific Provider.
// A VirtualModel can have multiple Routes, ordered by priority (index 0 first).
// Route selection at request time depends on which Provider the current
// credential profile has an API key for.
type ModelRoute struct {
	Provider       string `json:"provider"`
	ModelOpenAI    string `json:"model_openai,omitempty"`
	ModelAnthropic string `json:"model_anthropic,omitempty"`
	ModelResponses string `json:"model_responses,omitempty"` // 真实 Responses 模型名
}

// VirtualModel maps a user-facing model name to one or more provider routes.
type VirtualModel struct {
	ID          string       `json:"id"`                    // virtual name agent sees, e.g. "deepseek-v4-pro"
	DisplayName string       `json:"display_name,omitempty"` // human-readable label
	Routes      []ModelRoute `json:"routes"`                // priority-ordered provider routes
	Tags        []string     `json:"tags,omitempty"`        // e.g. ["reasoning", "fast"]
	VisibleTo   string       `json:"visible_to,omitempty"`  // "*" or "project-A,project-B"
	Priority    int          `json:"priority,omitempty"`    // sort order (lower = higher priority)

	// Legacy fields kept for migration only (json:"-" = not serialized to disk).
	// When loading old models.json, these are populated and then converted to Routes.
	Provider  string `json:"-"`
	Anthropic string `json:"-"`
	OpenAI    string `json:"-"`
}

// ── Legacy compatibility struct for reading old models.json ─────────────────

// legacyVirtualModel is the old format with flat Provider/Anthropic/OpenAI fields.
type legacyVirtualModel struct {
	ID          string   `json:"id"`
	Provider    string   `json:"provider"`
	DisplayName string   `json:"display_name,omitempty"`
	Anthropic   string   `json:"anthropic,omitempty"`
	OpenAI      string   `json:"openai,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	VisibleTo   string   `json:"visible_to,omitempty"`
	Priority    int      `json:"priority,omitempty"`
}

// legacyModelRegistry is the old flat-format models.json.
type legacyModelRegistry struct {
	Version   int                  `json:"version"`
	Providers []Provider           `json:"providers"`
	Models    []legacyVirtualModel `json:"models"`
}

// ModelRegistry is the full contents of models.json.
type ModelRegistry struct {
	Version   int            `json:"version"`
	Providers []Provider     `json:"providers"`
	Models    []VirtualModel `json:"models"`
}

// modelsPath returns ~/.aipmc/models.json.
func modelsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aipmc", "models.json")
}

// LoadModelRegistry reads models.json. Returns an empty registry (not error)
// when the file doesn't exist — this is the backward-compat path.
// Automatically migrates old flat-format (Provider/Anthropic/OpenAI on VirtualModel)
// to the new Routes-based format.
func LoadModelRegistry() *ModelRegistry {
	data, err := os.ReadFile(modelsPath())
	if err != nil {
		return &ModelRegistry{}
	}

	// Try new format first.
	var reg ModelRegistry
	if json.Unmarshal(data, &reg) == nil && len(reg.Models) > 0 {
		// Check if any model still has legacy format (Routes empty, old Provider set in json:"-").
		// Re-read as legacy to detect.
		var legacy legacyModelRegistry
		if json.Unmarshal(data, &legacy) == nil {
			needsMigration := false
			for i := range reg.Models {
				if len(reg.Models[i].Routes) == 0 && legacyModelsFindProvider(&legacy, reg.Models[i].ID) != "" {
					needsMigration = true
					break
				}
			}
			if needsMigration {
				migrateLegacyToRoutes(&reg, &legacy)
			}
		}
		return &reg
	}

	// Try legacy format.
	var legacy legacyModelRegistry
	if json.Unmarshal(data, &legacy) != nil {
		return &ModelRegistry{}
	}
	reg = migrateLegacyToRoutes(&ModelRegistry{Version: legacy.Version, Providers: legacy.Providers}, &legacy)
	return &reg
}

func legacyModelsFindProvider(legacy *legacyModelRegistry, id string) string {
	for _, m := range legacy.Models {
		if m.ID == id {
			return m.Provider
		}
	}
	return ""
}

// migrateLegacyToRoutes converts old flat-format models to Routes-based format.
func migrateLegacyToRoutes(reg *ModelRegistry, legacy *legacyModelRegistry) ModelRegistry {
	reg.Version = legacy.Version
	if reg.Providers == nil {
		reg.Providers = legacy.Providers
	}
	reg.Models = make([]VirtualModel, 0, len(legacy.Models))
	for _, lm := range legacy.Models {
		vm := VirtualModel{
			ID:          lm.ID,
			DisplayName: lm.DisplayName,
			Tags:        lm.Tags,
			VisibleTo:   lm.VisibleTo,
			Priority:    lm.Priority,
			Routes: []ModelRoute{{
				Provider:       lm.Provider,
				ModelOpenAI:    lm.OpenAI,
				ModelAnthropic: lm.Anthropic,
			}},
			// Keep legacy fields for in-memory fallback.
			Provider:  lm.Provider,
			Anthropic: lm.Anthropic,
			OpenAI:    lm.OpenAI,
		}
		reg.Models = append(reg.Models, vm)
	}
	return *reg
}

// SaveModelRegistry writes models.json.
func SaveModelRegistry(reg *ModelRegistry) error {
	path := modelsPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	return os.WriteFile(path, u.MustMarshal(reg), 0644)
}

// FindModel looks up a virtual model by ID.
func (r *ModelRegistry) FindModel(id string) *VirtualModel {
	for i := range r.Models {
		if r.Models[i].ID == id {
			return &r.Models[i]
		}
	}
	return nil
}

// FindProvider looks up a provider by name.
func (r *ModelRegistry) FindProvider(name string) *Provider {
	for i := range r.Providers {
		if r.Providers[i].Name == name {
			return &r.Providers[i]
		}
	}
	return nil
}

// IsActive returns true when virtual model routing is configured.
func (r *ModelRegistry) IsActive() bool {
	return r != nil && len(r.Models) > 0
}

// ── Route-aware helpers ──────────────────────────────────────────────────────

// ResolveModelForProtocol returns the real model name for a given virtual model
// and target protocol. It uses the Routes array: for each route, checks if it
// has a protocol-specific name.
func (r *ModelRegistry) ResolveModelForProtocol(virtualModelID, protocol string) string {
	vm := r.FindModel(virtualModelID)
	if vm == nil {
		return virtualModelID
	}
	// Check Routes first (new format).
	for _, rt := range vm.Routes {
		switch protocol {
		case "anthropic":
			if rt.ModelAnthropic != "" {
				return rt.ModelAnthropic
			}
		case "openai":
			if rt.ModelOpenAI != "" {
				return rt.ModelOpenAI
			}
		}
	}
	// Fallback: check legacy fields.
	switch protocol {
	case "anthropic":
		if vm.Anthropic != "" {
			return vm.Anthropic
		}
	case "openai":
		if vm.OpenAI != "" {
			return vm.OpenAI
		}
	}
	return virtualModelID
}

// realModelForRoute returns the protocol-specific real model name for a route.
func realModelForRoute(rt *ModelRoute, protocol string) string {
	switch protocol {
	case "anthropic":
		if rt.ModelAnthropic != "" {
			return rt.ModelAnthropic
		}
	case "openai":
		if rt.ModelOpenAI != "" {
			return rt.ModelOpenAI
		}
	case "responses":
		if rt.ModelResponses != "" {
			return rt.ModelResponses
		}
	}
	return ""
}

// ResolveAPIKey returns the effective API key for a provider.
// Priority: encrypted credential store.
func (r *ModelRegistry) ResolveAPIKey(providerName string) string {
	if store := GetCredentialStore(); store != nil {
		return store.Get(providerName)
	}
	return ""
}

// ModelForAgentProto picks the best real model name for a given agent's native protocol.
// Agent native protocol mapping:
//   claude  → anthropic
//   codex   → responses (falls back to openai — no provider supports responses natively)
//   gemini  → openai (fallback; gemini native not used by proxy)
//   other   → openai
func (r *ModelRegistry) ModelForAgentProto(virtualModelID, agentType string) string {
	vm := r.FindModel(virtualModelID)
	if vm == nil {
		return virtualModelID
	}
	targetProto := "openai"
	if agentType == "claude" || agentType == "claude-code" {
		targetProto = "anthropic"
	} else if agentType == "codex" {
		targetProto = "responses"
	}
	// Walk Routes: prefer the route that has a protocol-specific name.
	for _, rt := range vm.Routes {
		if name := realModelForRoute(&rt, targetProto); name != "" {
			return name
		}
	}
	// 2. codex 若 responses 未配置，回退 openai 名
	if targetProto == "responses" {
		for _, rt := range vm.Routes {
			if rt.ModelOpenAI != "" {
				return rt.ModelOpenAI
			}
		}
	}
	// Fallback to legacy fields.
	if targetProto == "anthropic" && vm.Anthropic != "" {
		return vm.Anthropic
	}
	if vm.OpenAI != "" {
		return vm.OpenAI
	}
	return virtualModelID
}

// ListModelProviders returns all provider names across all routes for a model.
func (r *ModelRegistry) ListModelProviders(virtualModelID string) []string {
	vm := r.FindModel(virtualModelID)
	if vm == nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for _, rt := range vm.Routes {
		if !seen[rt.Provider] {
			seen[rt.Provider] = true
			names = append(names, rt.Provider)
		}
	}
	// Legacy fallback.
	if len(names) == 0 && vm.Provider != "" && !seen[vm.Provider] {
		names = append(names, vm.Provider)
	}
	return names
}

// ── Current model selection (per-agent overrides) ──────────────────────────

// currentModelPath returns ~/.aipmc/current_model.
func currentModelPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aipmc", "current_model")
}

// loadCurrentModelMap reads the full per-agent model map from disk.
func loadCurrentModelMap() map[string]string {
	data, err := os.ReadFile(currentModelPath())
	if err != nil {
		return map[string]string{}
	}
	var m map[string]string
	if json.Unmarshal(data, &m) != nil {
		return map[string]string{}
	}
	return m
}

// saveCurrentModelMap writes the per-agent model map to disk (removing empty entries).
func saveCurrentModelMap(m map[string]string) error {
	path := currentModelPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	cleaned := map[string]string{}
	for k, v := range m {
		if v != "" {
			cleaned[k] = v
		}
	}
	if len(cleaned) == 0 {
		os.Remove(path)
		return nil
	}
	out, err := json.Marshal(cleaned)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

// LoadCurrentModel reads the per-agent override for a given agent.
// Returns "" when no override is set (Auto mode / passthrough).
// Valid agents: "claude", "codex", "opencode", "gemini", "cursor"
func LoadCurrentModel(agent string) string {
	return loadCurrentModelMap()[agent]
}

// SaveCurrentModel persists a per-agent model override.
// When model is "" (Auto mode), the agent's entry is removed from the map.
// When model is non-empty, it is validated against the ModelRegistry before writing.
func SaveCurrentModel(agent, model string) error {
	m := loadCurrentModelMap()
	if model == "" {
		delete(m, agent)
		return saveCurrentModelMap(m)
	}
	reg := LoadModelRegistry()
	if reg.FindModel(model) == nil {
		return fmt.Errorf("unknown model: %s", model)
	}
	m[agent] = model
	return saveCurrentModelMap(m)
}

// LoadAllCurrentModels returns all per-agent overrides as a map.
func LoadAllCurrentModels() map[string]string {
	return loadCurrentModelMap()
}

// ── Mutation helpers ─────────────────────────────────────────────────────────

// AddProvider appends a provider, or replaces an existing one with the same name.
func (r *ModelRegistry) AddProvider(p Provider) {
	for i := range r.Providers {
		if r.Providers[i].Name == p.Name {
			r.Providers[i] = p
			return
		}
	}
	r.Providers = append(r.Providers, p)
}

// RemoveProvider deletes a provider by name. Returns false if not found.
func (r *ModelRegistry) RemoveProvider(name string) bool {
	for i := range r.Providers {
		if r.Providers[i].Name == name {
			r.Providers = append(r.Providers[:i], r.Providers[i+1:]...)
			return true
		}
	}
	return false
}

// AddModel appends a virtual model, or replaces an existing one with the same ID.
func (r *ModelRegistry) AddModel(m VirtualModel) {
	for i := range r.Models {
		if r.Models[i].ID == m.ID {
			r.Models[i] = m
			return
		}
	}
	r.Models = append(r.Models, m)
}

// RemoveModel deletes a virtual model by ID. Returns false if not found.
func (r *ModelRegistry) RemoveModel(id string) bool {
	for i := range r.Models {
		if r.Models[i].ID == id {
			r.Models = append(r.Models[:i], r.Models[i+1:]...)
			return true
		}
	}
	return false
}

// Validate returns an error if the registry has structural problems
// (duplicate IDs, references to unknown providers, missing routes, etc.).
func (r *ModelRegistry) Validate() error {
	seenModels := map[string]bool{}
	for _, m := range r.Models {
		if m.ID == "" {
			return fmt.Errorf("model has empty id")
		}
		if seenModels[m.ID] {
			return fmt.Errorf("duplicate model id: %s", m.ID)
		}
		seenModels[m.ID] = true

		// Check routes.
		if len(m.Routes) == 0 {
			// Allow legacy-only (Provider field set, no routes yet — migration pending).
			if m.Provider == "" {
				return fmt.Errorf("model %s has no routes", m.ID)
			}
		}
		seenProviders := map[string]bool{}
		for _, rt := range m.Routes {
			if rt.Provider == "" {
				return fmt.Errorf("model %s has route with empty provider", m.ID)
			}
			if seenProviders[rt.Provider] {
				return fmt.Errorf("model %s has duplicate route provider: %s", m.ID, rt.Provider)
			}
			seenProviders[rt.Provider] = true
			if r.FindProvider(rt.Provider) == nil {
				return fmt.Errorf("model %s route references unknown provider: %s", m.ID, rt.Provider)
			}
		}
	}
	seenProviders := map[string]bool{}
	for _, p := range r.Providers {
		if p.Name == "" {
			return fmt.Errorf("provider has empty name")
		}
		if seenProviders[p.Name] {
			return fmt.Errorf("duplicate provider name: %s", p.Name)
		}
		seenProviders[p.Name] = true
		if p.OpenAIURL == "" {
			return fmt.Errorf("provider %s has empty openai_url", p.Name)
		}
	}
	return nil
}