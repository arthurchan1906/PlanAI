package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
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
// It iterates the model's Routes in priority order, picking the first route
// whose provider has an API key in the credential store.
//
// If the router is not active, returns nil (caller falls back).
// If the virtual model isn't found, returns nil.
// If no route has a credential-store key, falls back to the first route
// with any non-empty API key (from legacy env) or returns nil.
//
// Strategy extension point: future per-request routing strategies
// (e.g. "cheapest", "best-match") can be plugged in here before the
// priority-ordered loop.
func (r *ModelRouter) Resolve(virtualModel string, protocol string) *Route {
	if !r.IsActive() {
		return nil
	}
	reg := r.getRegistry()

	vm := reg.FindModel(virtualModel)
	if vm == nil {
		return nil
	}

	store := pmdb.GetCredentialStore()

	// Try each route in priority order: pick the first one whose provider
	// has an API key in the credential store.
	var firstRoute *pmdb.ModelRoute
	for i := range vm.Routes {
		rt := &vm.Routes[i]
		if firstRoute == nil {
			firstRoute = rt
		}
		// Check if the current credential profile has a key for this provider.
		if store != nil && store.Get(rt.Provider) != "" {
			return r.buildRoute(reg, rt, protocol, store.Get(rt.Provider))
		}
	}

	// No route has a key in the store. Fall back to the first route with
	// a legacy (env-based) key, or the very first route.
	if firstRoute != nil {
		apiKey := reg.ResolveAPIKey(firstRoute.Provider)
		if apiKey != "" || store == nil {
			return r.buildRoute(reg, firstRoute, protocol, apiKey)
		}
	}

	// Legacy fallback: model still has old Provider field set.
	if vm.Provider != "" {
		prov := reg.FindProvider(vm.Provider)
		if prov != nil {
			return r.buildLegacyRoute(reg, vm, prov, protocol)
		}
	}

	return nil
}

// buildRoute constructs a Route from a ModelRoute entry.
func (r *ModelRouter) buildRoute(reg *pmdb.ModelRegistry, rt *pmdb.ModelRoute, protocol, apiKey string) *Route {
	prov := reg.FindProvider(rt.Provider)
	if prov == nil {
		return nil
	}

	realModel := rt.ModelOpenAI
	baseURL := prov.OpenAIURL
	if protocol == "anthropic" {
		if rt.ModelAnthropic != "" {
			realModel = rt.ModelAnthropic
		}
		if prov.AnthropicURL != "" {
			baseURL = prov.AnthropicURL
		}
	}
	if realModel == "" {
		realModel = rt.ModelOpenAI // fallback
	}

	return &Route{
		Provider:  rt.Provider,
		RealModel: realModel,
		BaseURL:   baseURL,
		APIKey:    apiKey,
	}
}

// buildLegacyRoute handles the old single-provider format during migration.
func (r *ModelRouter) buildLegacyRoute(reg *pmdb.ModelRegistry, vm *pmdb.VirtualModel, prov *pmdb.Provider, protocol string) *Route {
	realModel := reg.ResolveModelForProtocol(vm.ID, protocol)
	baseURL := prov.OpenAIURL
	if protocol == "anthropic" && prov.AnthropicURL != "" {
		baseURL = prov.AnthropicURL
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

// maskKeyStr returns a masked key string for logging.
func maskKeyStr(key string) string {
	if key == "" {
		return "(empty)"
	}
	if len(key) <= 10 {
		return key[:2] + "***"
	}
	return key[:6] + "..." + key[len(key)-4:]
}

// ShouldPassthrough checks whether a virtual model supports Anthropic protocol
// natively (i.e., at least one route has a provider with anthropic_url and the
// route has a ModelAnthropic name). Used by the handler to decide whether to
// route /v1/messages through Anthropic passthrough or through the translation pipeline.
func (r *ModelRouter) ShouldPassthrough(virtualModel string) bool {
	if !r.IsActive() {
		return false
	}
	reg := r.getRegistry()
	vm := reg.FindModel(virtualModel)
	if vm == nil {
		return false
	}
	// Check routes first.
	for _, rt := range vm.Routes {
		prov := reg.FindProvider(rt.Provider)
		if prov != nil && prov.AnthropicURL != "" && rt.ModelAnthropic != "" {
			return true
		}
	}
	// Legacy fallback.
	if vm.Provider != "" {
		prov := reg.FindProvider(vm.Provider)
		return prov != nil && prov.AnthropicURL != "" && vm.Anthropic != ""
	}
	return false
}

// joinedProviders returns a comma-separated list of provider names from the model's routes.
func joinedProviders(vm *pmdb.VirtualModel) string {
	if len(vm.Routes) == 0 {
		if vm.Provider != "" {
			return vm.Provider
		}
		return "unknown"
	}
	names := make([]string, 0, len(vm.Routes))
	for _, rt := range vm.Routes {
		names = append(names, rt.Provider)
	}
	return strings.Join(names, ", ")
}

// ListOpenAIModels returns the virtual model list in OpenAI /v1/models format.
func (r *ModelRouter) ListOpenAIModels() map[string]any {
	reg := r.getRegistry()
	models := make([]map[string]any, 0, len(reg.Models))
	for _, vm := range reg.Models {
		ownedBy := joinedProviders(&vm)
		models = append(models, map[string]any{
			"id":       vm.ID,
			"object":   "model",
			"owned_by": ownedBy,
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

		providers := joinedProviders(&vm)

		models = append(models, map[string]any{
			"slug":                        slug,
			"display_name":                display,
			"description":                 fmt.Sprintf("Virtual model via %s", providers),
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