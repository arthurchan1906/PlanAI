package ai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

type streamToolAcc struct {
	id, name string
	args     strings.Builder
}

// ChatStream calls /chat/completions with stream=true.
// onContent is invoked for each text delta; may be nil.
func (c *Client) ChatStream(messages []ChatMessage, tools []ToolDef, onContent func(string)) (*ChatResponse, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("AI not configured: set AI_ENDPOINT")
	}

	body := map[string]any{
		"model":    c.chatModel,
		"messages": messages,
		"stream":   true,
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}

	resp, err := c.streamPost(c.endpoint, "/chat/completions", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var content strings.Builder
	toolAcc := map[int]*streamToolAcc{}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil || len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			content.WriteString(delta.Content)
			if onContent != nil {
				onContent(delta.Content)
			}
		}
		for _, tc := range delta.ToolCalls {
			acc := toolAcc[tc.Index]
			if acc == nil {
				acc = &streamToolAcc{}
				toolAcc[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				acc.args.WriteString(tc.Function.Arguments)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}

	out := &ChatResponse{Content: content.String()}
	if len(toolAcc) > 0 {
		idxs := make([]int, 0, len(toolAcc))
		for i := range toolAcc {
			idxs = append(idxs, i)
		}
		sort.Ints(idxs)
		for _, i := range idxs {
			acc := toolAcc[i]
			args := map[string]any{}
			raw := acc.args.String()
			if raw != "" {
				if err := json.Unmarshal([]byte(raw), &args); err != nil {
					args = map[string]any{"_raw": raw}
				}
			}
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:   acc.id,
				Name: acc.name,
				Args: args,
			})
		}
	}
	return out, nil
}

func (c *Client) streamPost(baseURL, path string, body any) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	url := baseURL + path
	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http post %s: %w", url, err)
	}
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, url, string(data))
	}
	return resp, nil
}
