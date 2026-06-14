package app

import (
	"os"

	"aipmc/ai"
	"aipmc/discussion"
	pmdb "aipmc/db"
	"aipmc/mcp"
	"aipmc/project"
	"aipmc/search"
)

// App holds runtime services shared by CLI, web API, and MCP.
type App struct {
	AI *ai.Client
}

// New creates an application instance.
func New() *App {
	return &App{}
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
		a.AI = nil
		return
	}
	apiKey := os.Getenv("AI_API_KEY")
	embEndpoint := cfg.AIEmbeddingEndpoint
	if embEndpoint == "" {
		embEndpoint = os.Getenv("AI_EMBEDDING_ENDPOINT")
	}
	a.AI = ai.NewClient(endpoint, embEndpoint, model, chatModel, apiKey)
}

// RunMCP starts the MCP stdio server with project services wired in.
func (a *App) RunMCP() error {
	return mcp.NewServer(a.AI,
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
				reranked := search.RerankWithAI(a.AI, q, l, search.HitsFromMaps(raw))
				return search.HitsToMaps(reranked)
			}
			return nil
		},
		func(query, source, typeFilter, projectPath string, page, pageSize int) ([]map[string]any, int, error) {
			return discussion.Search(a.AI, query, source, typeFilter, projectPath, page, pageSize)
		},
	).Run()
}

func (a *App) SearchProjectContext(query string, limit int) map[string]any {
	return search.ProjectContext(query, limit)
}

func (a *App) SearchDiscussions(query, source, typeFilter, projectPath string, page, pageSize int) ([]map[string]any, int, error) {
	return discussion.Search(a.AI, query, source, typeFilter, projectPath, page, pageSize)
}

func (a *App) EmbedDiscussions(batchSize int) (int, error) {
	return discussion.Embed(a.AI, batchSize)
}

func (a *App) StatusSnapshot() map[string]any       { return project.StatusSnapshot() }
func (a *App) ContextPack() map[string]any          { return project.ContextPack() }
func (a *App) NextActionPacket() map[string]any     { return project.NextActionPacket() }
func (a *App) AgentStartPacket() map[string]any     { return project.AgentStartPacket(a.AI) }
func (a *App) InboxSummary() map[string]any         { return project.InboxSummary() }
