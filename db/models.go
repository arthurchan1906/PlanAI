package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"aipmc/u"
)

// ── Model Registry (virtual model routing) ──────────────────────────────

// Provider defines a real LLM backend that the proxy can route to.
type Provider struct {
	Name         string `json:"name"`                    // e.g. "deepseek"
	OpenAIURL    string `json:"openai_url"`              // OpenAI-compatible base URL
	AnthropicURL string `json:"anthropic_url,omitempty"` // optional Anthropic-compatible base URL
	APIKeyEnv    string `json:"api_key_env,omitempty"`   // env var name for API key, e.g. "DEEPSEEK_API_KEY"
}

// VirtualModel maps a user-facing model name to a real backend + protocol-specific real model names.
type VirtualModel struct {
	ID          string   `json:"id"`                    // virtual name agent sees, e.g. "deepseek-v4-pro"
	Provider    string   `json:"provider"`              // → Provider.Name
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
// when the file doesn't exist — this is the backward-compat path.
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
// Priority: env var named by Provider.APIKeyEnv → global UPSTREAM_KEY.
func (r *ModelRegistry) ResolveAPIKey(providerName string) string {
	prov := r.FindProvider(providerName)
	if prov != nil && prov.APIKeyEnv != "" {
		if key := os.Getenv(prov.APIKeyEnv); key != "" {
			return key
		}
	}
	return os.Getenv("UPSTREAM_KEY")
}

// ModelForAgentProto picks the best real model name for a given agent's native protocol.
// Agent native protocol mapping:
//
//	claude → anthropic
//	codex  → responses (falls back to openai — no provider supports responses natively)
//	gemini → openai (fallback; gemini native not used by proxy)
//	other  → openai
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
