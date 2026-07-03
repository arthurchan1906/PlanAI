package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"

	pmdb "aipmc/db"
)

// Route is the resolved destination for a virtual model request.
type Route struct {
	Provider  string // Provider.Name
	RealModel string // real model name for the target protocol
	BaseURL   string // provider's base URL for the target protocol
	APIKey    string // resolved API key
}

// ModelRouter resolves virtual model names to provider destinations.
type ModelRouter struct {
	registry *pmdb.ModelRegistry
	mu       sync.RWMutex
}

// NewModelRouter loads the model registry from disk and returns a router.
// Returns an inactive router when no models.json exists.
// Logs a warning if models.json has structural problems.
func NewModelRouter() *ModelRouter {
	reg := pmdb.LoadModelRegistry()
	if reg.IsActive() {
		if err := reg.Validate(); err != nil {
			log.Printf("[PROXY] models.json validation warning: %v", err)
		}
	}
	return &ModelRouter{registry: reg}
}

// Reload re-reads models.json and swaps the registry atomically.
func (r *ModelRouter) Reload() error {
	reg := pmdb.LoadModelRegistry()
	if err := reg.Validate(); err != nil {
		return fmt.Errorf("invalid models.json: %w", err)
	}
	r.mu.Lock()
	r.registry = reg
	r.mu.Unlock()
	return nil
}

// getRegistry returns the current registry snapshot.
func (r *ModelRouter) getRegistry() *pmdb.ModelRegistry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.registry
}

// IsActive returns true when virtual model routing is configured.
func (r *ModelRouter) IsActive() bool {
	if r == nil {
		return false
	}
	return r.getRegistry().IsActive()
}

// Resolve maps a virtual model + target protocol to a Route.
// The protocol parameter should be the real protocol being used to call the
// upstream: "openai" for OpenAI Chat Completions, "anthropic" for Anthropic
// Messages passthrough.
//
// If the router is not active, returns an empty Route (caller falls back).
// If the virtual model isn't found, the real model is set to the virtual
// model itself and the global upstream is used as fallback.
func (r *ModelRouter) Resolve(virtualModel string, protocol string) *Route {
	if !r.IsActive() {
		return nil
	}
	reg := r.getRegistry()

	vm := reg.FindModel(virtualModel)
	if vm == nil {
		// Unknown virtual model: return nil so caller uses global fallback
		return nil
	}

	prov := reg.FindProvider(vm.Provider)
	if prov == nil {
		return nil
	}

	realModel := reg.ResolveModelForProtocol(virtualModel, protocol)

	var baseURL string
	switch protocol {
	case "anthropic":
		baseURL = prov.AnthropicURL
	case "openai":
		baseURL = prov.OpenAIURL
	default:
		baseURL = prov.OpenAIURL
	}

	if baseURL == "" {
		return nil
	}

	return &Route{
		Provider:  vm.Provider,
		RealModel: realModel,
		BaseURL:   baseURL,
		APIKey:    reg.ResolveAPIKey(vm.Provider),
	}
}

// ShouldPassthrough checks whether a virtual model supports Anthropic protocol
// natively (i.e., its provider has an anthropic_url and the model has an
// Anthropic protocol name). Used by the handler to decide whether to route
// /v1/messages through Anthropic passthrough or through the translation pipeline.
func (r *ModelRouter) ShouldPassthrough(virtualModel string) bool {
	if !r.IsActive() {
		return false
	}
	reg := r.getRegistry()
	vm := reg.FindModel(virtualModel)
	if vm == nil {
		return false
	}
	prov := reg.FindProvider(vm.Provider)
	if prov == nil {
		return false
	}
	return prov.AnthropicURL != "" && vm.Anthropic != ""
}

// ListOpenAIModels returns the virtual model list in OpenAI /v1/models format.
func (r *ModelRouter) ListOpenAIModels() map[string]any {
	reg := r.getRegistry()
	models := make([]map[string]any, 0, len(reg.Models))
	for _, vm := range reg.Models {
		models = append(models, map[string]any{
			"id":       vm.ID,
			"object":   "model",
			"owned_by": vm.Provider,
		})
	}
	return map[string]any{
		"object": "list",
		"data":   models,
	}
}

// ListCodexModels returns the virtual model list in Codex CLI's extended format.
func (r *ModelRouter) ListCodexModels() []map[string]any {
	reg := r.getRegistry()
	models := make([]map[string]any, 0, len(reg.Models))

	// Sort by priority
	sorted := make([]pmdb.VirtualModel, len(reg.Models))
	copy(sorted, reg.Models)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	for _, vm := range sorted {
		maxTokens := 131072
		slug := vm.ID
		display := vm.DisplayName
		if display == "" {
			display = slug
		}
		shellType := "local"

		var reasonLevels []map[string]string
		if vm.Tags != nil {
			for _, t := range vm.Tags {
				if t == "reasoning" {
					reasonLevels = []map[string]string{
						{"effort": "low", "description": "Low effort"},
						{"effort": "medium", "description": "Medium effort"},
						{"effort": "high", "description": "High effort"},
					}
					break
				}
			}
		}

		var defReasonLevel *string
		if len(reasonLevels) > 0 {
			s := "medium"
			defReasonLevel = &s
		}

		models = append(models, map[string]any{
			"slug":                        slug,
			"display_name":                display,
			"description":                 fmt.Sprintf("Virtual model via %s", vm.Provider),
			"shell_type":                  shellType,
			"visibility":                  "list",
			"supported_in_api":            true,
			"priority":                    vm.Priority,
			"base_instructions":           "",
			"supports_reasoning_summaries": len(reasonLevels) > 0,
			"default_reasoning_summary":   "none",
			"default_reasoning_level":     defReasonLevel,
			"supported_reasoning_levels":  reasonLevels,
			"support_verbosity":           false,
			"truncation_policy": map[string]any{
				"mode":  "tokens",
				"limit": maxTokens,
			},
			"supports_parallel_tool_calls":  true,
			"supports_search_tool":          false,
			"web_search_tool_type":          "text",
			"experimental_supported_tools":  []string{},
		})
	}
	return models
}

// peekModel reads the model field from a JSON request body without fully
// parsing the entire payload. Returns the model name and the body bytes.
func peekModel(body []byte) string {
	var peek struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &peek) == nil {
		return peek.Model
	}
	return ""
}

// replaceModelInBody replaces the "model" field in a JSON body with a new value.
// Uses json.Unmarshal → modify → json.Marshal for correctness with nested keys.
// When the model field is not found, the body is returned unchanged.
func replaceModelInBody(bodyJSON []byte, newModel string) []byte {
	if newModel == "" {
		return bodyJSON
	}
	// Try to unmarshal, replace, and re-marshal for correctness
	var raw map[string]any
	if json.Unmarshal(bodyJSON, &raw) != nil {
		return bodyJSON
	}
	raw["model"] = newModel
	out, err := json.Marshal(raw)
	if err != nil {
		return bodyJSON
	}
	return out
}
