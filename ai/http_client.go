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
//
// When endpoint is empty, all methods return errors — callers should
// check Enabled() or handle errors by falling back to non-AI behavior.
type Client struct {
	endpoint  string // e.g. "http://localhost:8080/v1"
	model     string // embedding model name
	chatModel string // summarization / chat model name
	apiKey    string
	http      *http.Client
}

// NewClient returns a Client ready to use. Pass an empty endpoint to
// disable AI (all methods will return errors).
func NewClient(endpoint, model, chatModel, apiKey string) *Client {
	return &Client{
		endpoint:  endpoint,
		model:     model,
		chatModel: chatModel,
		apiKey:    apiKey,
		http:      &http.Client{Timeout: 60 * time.Second},
	}
}

// Enabled reports whether the client is configured with an endpoint.
func (c *Client) Enabled() bool { return c.endpoint != "" }

// Embed calls the /embeddings endpoint. Returns one []float64 per input text.
func (c *Client) Embed(texts []string) ([][]float64, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("AI not configured: set AI_ENDPOINT")
	}

	body := map[string]any{
		"model": c.model,
		"input": texts,
	}
	resp, err := c.post("/embeddings", body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
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

// Summarize calls the /chat/completions endpoint with a summarization prompt.
func (c *Client) Summarize(text, instruction string) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("AI not configured: set AI_ENDPOINT")
	}

	messages := []map[string]string{
		{"role": "system", "content": instruction},
		{"role": "user", "content": text},
	}

	body := map[string]any{
		"model":    c.chatModel,
		"messages": messages,
	}
	resp, err := c.post("/chat/completions", body)
	if err != nil {
		return "", err
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("parse chat response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in chat response")
	}
	return result.Choices[0].Message.Content, nil
}

// RankSimilarity embeds query and candidates, then ranks by cosine similarity.
func (c *Client) RankSimilarity(query string, candidates []string) ([]RankedResult, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("AI not configured: set AI_ENDPOINT")
	}

	texts := make([]string, 1+len(candidates))
	texts[0] = query
	copy(texts[1:], candidates)

	embs, err := c.Embed(texts)
	if err != nil {
		return nil, err
	}
	if len(embs) != len(texts) {
		return nil, fmt.Errorf("embedding count mismatch: got %d, want %d", len(embs), len(texts))
	}

	queryEmb := embs[0]
	candEmbs := embs[1:]

	results := make([]RankedResult, len(candidates))
	for i, emb := range candEmbs {
		results[i] = RankedResult{
			Text:  candidates[i],
			Score: CosineSimilarity(queryEmb, emb),
			Index: i,
		}
	}

	// Sort descending by score (handled by caller or a utility wrapper)
	return results, nil
}

// post sends a JSON POST request to the given path and returns the raw body.
func (c *Client) post(path string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.endpoint + path
	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post %s: %w", url, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, url, string(data))
	}

	return data, nil
}
