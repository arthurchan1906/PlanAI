package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"aipmc/u"
)

// 鈹€鈹€ Model Registry (virtual model routing) 鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€

// Provider defines a real LLM backend that the proxy can route to.
type Provider struct {
	Name         string `json:"name"`                    // e.g. "deepseek"
	OpenAIURL    string `json:"openai_url"`              // OpenAI-compatible base URL
	AnthropicURL string `json:"anthropic_url,omitempty"` // optional Anthropic-compatible base URL
}

// VirtualModel maps a user-facing model name to a real backend + protocol-specific real model names.
type VirtualModel struct {
	ID          string   `json:"id"`                    // virtual name agent sees, e.g. "deepseek-v4-pro"
	Provider    string   `json:"provider"`              // 鈫?Provider.Name
	DisplayName string   `json:"display_name,omitempty"` // human-readable label
	Anthropic   string   `json:"anthropic,omitempty"`   // real model name for Anthropic protocol
	OpenAI      string   `json:"openai,omitempty"`      // real model name for OpenAI protocol
	Tags        []string `json:"tags,omitempty"`        // e.g. ["reasoning", "fast"]
	VisibleTo   string   `json:"visible_to,omitempty"`  // "*" or "project-A,project-B"
	Priority    int      `json:"priority,omitempty"`    // sort order (lower = higher priority)
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
// when the file doesn't exist 鈥?this is the backward-compat path.
func LoadModelRegistry() *ModelRegistry {
	data, err := os.ReadFile(modelsPath())
	if err != nil {
		return &ModelRegistry{}
	}
	var reg ModelRegistry
	if json.Unmarshal(data, &reg) != nil {
		return &ModelRegistry{}
	}
	return &reg
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

// ResolveModelForProtocol returns the real model name for a given virtual model
// and target protocol. Falls back to the virtual model ID if no protocol-specific
// mapping exists.
func (r *ModelRegistry) ResolveModelForProtocol(virtualModelID, protocol string) string {
	vm := r.FindModel(virtualModelID)
	if vm == nil {
		return virtualModelID
	}
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
	// Fallback: use virtual model ID as-is
	return virtualModelID
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
//
//	claude 鈫?anthropic
//	codex  鈫?responses (falls back to openai 鈥?no provider supports responses natively)
//	gemini 鈫?openai (fallback; gemini native not used by proxy)
//	other  鈫?openai
func (r *ModelRegistry) ModelForAgentProto(virtualModelID, agentType string) string {
	vm := r.FindModel(virtualModelID)
	if vm == nil {
		return virtualModelID
	}
	// Anthropic-native agents (Claude Code): prefer Anthropic protocol name
	if agentType == "claude" || agentType == "claude-code" {
		if vm.Anthropic != "" {
			return vm.Anthropic
		}
	}
	// All other agents (Codex, Gemini, OpenCode, Cursor): use OpenAI protocol name
	if vm.OpenAI != "" {
		return vm.OpenAI
	}
	return virtualModelID
}

// ═══ Current model selection (per-agent) ════════════════════════════════════

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

// CurrentModelProvider returns the provider name for the current model of an agent.
func CurrentModelProvider(agent string) string {
	cm := LoadCurrentModel(agent)
	if cm == "" {
		return ""
	}
	reg := LoadModelRegistry()
	if vm := reg.FindModel(cm); vm != nil {
		return vm.Provider
	}
	return ""
}

// 鈹€鈹€ Mutation helpers 鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€鈹€

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
// (duplicate IDs, references to unknown providers, etc.).
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
		if r.FindProvider(m.Provider) == nil {
			return fmt.Errorf("model %s references unknown provider: %s", m.ID, m.Provider)
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

