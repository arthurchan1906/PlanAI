package app

import (
	"os"
	"sync"

	"aipmc/ai"
	"aipmc/discussion"
	pmdb "aipmc/db"
	"aipmc/mcp"
	"aipmc/project"
	"aipmc/search"
	"aipmc/u"
)

// App holds runtime services shared by CLI, web API, and MCP.
type App struct {
	mu sync.RWMutex
	ai *ai.Client
}

// New creates an application instance.
func New() *App {
	return &App{}
}

// AI returns the current AI client in a thread-safe manner.
func (a *App) AI() *ai.Client {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.ai
}

// ReloadAI reads config/env and (re)initializes the AI client.
func (a *App) ReloadAI() {
	cfg := pmdb.LoadConfig()
	endpoint := cfg.AIEndpoint
	if endpoint == "" {
		endpoint = os.Getenv("AI_ENDPOINT")
	}
	model := cfg.AIModel
	if model == "" {
		model = os.Getenv("AI_MODEL")
	}
	chatModel := cfg.AIChatModel
	if chatModel == "" {
		chatModel = os.Getenv("AI_CHAT_MODEL")
	}
	if endpoint == "" {
		a.mu.Lock()
		a.ai = nil
		a.mu.Unlock()
		return
	}
	apiKey := cfg.AIApiKey
	if apiKey == "" {
		apiKey = os.Getenv("AI_API_KEY")
	}
	embEndpoint := cfg.AIEmbeddingEndpoint
	if embEndpoint == "" {
		embEndpoint = os.Getenv("AI_EMBEDDING_ENDPOINT")
	}
	a.mu.Lock()
	a.ai = ai.NewClient(endpoint, embEndpoint, model, chatModel, apiKey)
	a.mu.Unlock()
}

// SummarizerFor builds an AI summarizer from the given project's own
// .pmai/config.json, so the pipeline uses the model configured for that
// project rather than the serve instance's home project. Returns nil when the
// project has no AI endpoint configured (L2 summary is then skipped).
func (a *App) SummarizerFor(projectPath string) ai.Summarizer {
	cfg := pmdb.LoadConfigFor(projectPath)
	if cfg.AIEndpoint == "" {
		u.LogShared("PIPELINE", "summarizer project=%s none (no ai_endpoint)", projectPath)
		return nil
	}
	u.LogShared("PIPELINE", "summarizer project=%s endpoint=%s chat_model=%s", projectPath, cfg.AIEndpoint, cfg.AIChatModel)
	return ai.NewClient(cfg.AIEndpoint, cfg.AIEmbeddingEndpoint, cfg.AIModel, cfg.AIChatModel, cfg.AIApiKey)
}

// RunMCP starts the MCP stdio server with project services wired in.
func (a *App) RunMCP() error {
	return mcp.NewServer(a.AI(),
		search.ProjectContext,
		func(q string, l int) interface{} {
			hits := search.FTS5(q, l)
			if hits == nil {
				return nil
			}
			return search.HitsToMaps(hits)
		},
		func(q string) interface{} {
			return search.HitsToMaps(search.Linear(q))
		},
		func(q string, l int, hits interface{}) interface{} {
			if raw, ok := hits.([]map[string]interface{}); ok {
				reranked := search.RerankWithAI(a.AI(), q, l, search.HitsFromMaps(raw))
				return search.HitsToMaps(reranked)
			}
			return nil
		},
		func(query, source, sessionID, typeFilter, projectPath string, page, pageSize int) ([]map[string]any, int, error) {
			return discussion.Search(a.AI(), query, source, sessionID, typeFilter, projectPath, page, pageSize)
		},
	).Run()
}

func (a *App) SearchProjectContext(query string, limit int) map[string]any {
	return search.ProjectContext(query, limit)
}

func (a *App) SearchDiscussions(query, source, sessionID, typeFilter, projectPath string, page, pageSize int) ([]map[string]any, int, error) {
	return discussion.Search(a.AI(), query, source, sessionID, typeFilter, projectPath, page, pageSize)
}

func (a *App) EmbedDiscussions(batchSize int) (int, error) {
	return discussion.Embed(a.AI(), batchSize)
}

func (a *App) StatusSnapshot() map[string]any       { return project.StatusSnapshot() }
func (a *App) ContextPack() map[string]any          { return project.ContextPack() }
func (a *App) NextActionPacket() map[string]any     { return project.NextActionPacket() }
func (a *App) AgentStartPacket() map[string]any     { return project.AgentStartPacket(a.AI()) }
func (a *App) InboxSummary() map[string]any         { return project.InboxSummary() }
