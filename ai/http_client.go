package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client implements Embedder, Summarizer, and Searcher via an
// OpenAI-compatible HTTP API (llama.cpp server, Ollama, OpenAI, etc.).
type Client struct {
	endpoint          string // chat endpoint
	embeddingEndpoint string // embedding endpoint (falls back to endpoint)
	model             string // embedding model name
	chatModel         string // summarization / chat model name
	apiKey            string
	http              *http.Client
}

// NewClient returns a Client ready to use.
// embeddingEndpoint can be empty to use the same endpoint as chat.
func NewClient(endpoint, embeddingEndpoint, model, chatModel, apiKey string) *Client {
	if embeddingEndpoint == "" {
		embeddingEndpoint = endpoint
	}
	return &Client{
		endpoint:          endpoint,
		embeddingEndpoint: embeddingEndpoint,
		model:             model,
		chatModel:         chatModel,
		apiKey:            apiKey,
		http:              &http.Client{Timeout: 60 * time.Second},
	}
}

// Enabled reports whether the client is configured with an endpoint.
func (c *Client) Enabled() bool { return c.endpoint != "" }

// Embed calls the /embeddings endpoint. Uses embeddingEndpoint.
func (c *Client) Embed(texts []string) ([][]float64, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("AI not configured: set AI_ENDPOINT")
	}
	body := map[string]any{"model": c.model, "input": texts}
	resp, err := c.post(c.embeddingEndpoint, "/embeddings", body)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []struct{ Embedding []float64 `json:"embedding"` } `json:"data"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("parse embeddings response: %w", err)
	}
	out := make([][]float64, len(result.Data))
	for i, d := range result.Data {
		out[i] = d.Embedding
	}
	return out, nil
}

// Summarize calls the /chat/completions endpoint.
func (c *Client) Summarize(text, instruction string) (string, error) {
	if !c.Enabled() { return "", fmt.Errorf("AI not configured") }
	messages := []map[string]string{
		{"role": "system", "content": instruction},
		{"role": "user", "content": text},
	}
	body := map[string]any{"model": c.chatModel, "messages": messages}
	resp, err := c.post(c.endpoint, "/chat/completions", body)
	if err != nil { return "", err }
	var result struct {
		Choices []struct{ Message struct{ Content string `json:"content"` } `json:"message"` } `json:"choices"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse chat response: %w", err)
	}
	if len(result.Choices) == 0 { return "", fmt.Errorf("no choices") }
	return result.Choices[0].Message.Content, nil
}

// RankSimilarity embeds query and candidates, then ranks by cosine similarity.
func (c *Client) RankSimilarity(query string, candidates []string) ([]RankedResult, error) {
	if !c.Enabled() { return nil, fmt.Errorf("AI not configured") }
	texts := make([]string, 1+len(candidates))
	texts[0] = query
	copy(texts[1:], candidates)
	embs, err := c.Embed(texts)
	if err != nil { return nil, err }
	if len(embs) != len(texts) {
		return nil, fmt.Errorf("embedding count mismatch: got %d, want %d", len(embs), len(texts))
	}
	queryEmb := embs[0]
	candEmbs := embs[1:]
	results := make([]RankedResult, len(candidates))
	for i, emb := range candEmbs {
		results[i] = RankedResult{Text: candidates[i], Score: CosineSimilarity(queryEmb, emb), Index: i}
	}
	return results, nil
}

// post sends a JSON POST request to the given URL and returns the raw body.
func (c *Client) post(baseURL, path string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil { return nil, fmt.Errorf("marshal request: %w", err) }
	url := baseURL + path
	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil { return nil, fmt.Errorf("create request: %w", err) }
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" { req.Header.Set("Authorization", "Bearer "+c.apiKey) }
	resp, err := c.http.Do(req)
	if err != nil { return nil, fmt.Errorf("http post %s: %w", url, err) }
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil { return nil, fmt.Errorf("read response: %w", err) }
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, url, string(data))
	}
	return data, nil
}
