package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ============================================================
// Feedback — remote feedback API (compatible with Python pmai)
// ============================================================

const defaultFeedbackBaseURL = "http://43.167.206.218:8080"
const feedbackTimeout = 8 * time.Second

func getFeedbackBaseURL() string {
	if url := os.Getenv("PMAI_FEEDBACK_BASE_URL"); url != "" {
		return url
	}
	return defaultFeedbackBaseURL
}

type feedbackPayload struct {
	Label   string `json:"label"`
	Content string `json:"content"`
}

// addFeedback sends feedback to the remote pmai feedback server.
func addFeedback(label, content string) (map[string]any, error) {
	baseURL := getFeedbackBaseURL()
	payload := feedbackPayload{Label: label, Content: content}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", baseURL+"/pmai/add", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: feedbackTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("feedback server unreachable: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("feedback server error %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if len(respBody) > 0 {
		json.Unmarshal(respBody, &result)
	}
	if result == nil {
		result = map[string]any{"ok": true}
	}
	result["label"] = label
	result["content"] = content
	return result, nil
}

// listFeedbacks fetches all feedback from the remote server.
func listFeedbacks(label string) ([]map[string]any, error) {
	baseURL := getFeedbackBaseURL()

	req, err := http.NewRequest("GET", baseURL+"/pmai/list", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: feedbackTimeout}
	resp, err := client.Do(req)
	if err != nil {
		// Server unreachable — return empty list instead of error
		return []map[string]any{}, nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return []map[string]any{}, nil
	}

	var items []map[string]any
	if err := json.Unmarshal(respBody, &items); err != nil {
		// Try single object wrapper
		var wrapper map[string]any
		if err2 := json.Unmarshal(respBody, &wrapper); err2 == nil {
			if arr, ok := wrapper["feedbacks"]; ok {
				if a, ok := arr.([]any); ok {
					for _, item := range a {
						if m, ok := item.(map[string]any); ok {
							items = append(items, m)
						}
					}
				}
			}
		}
	}

	if items == nil {
		items = []map[string]any{}
	}

	// Filter by label if specified
	if label != "" {
		var filtered []map[string]any
		for _, item := range items {
			if l, _ := item["label"].(string); l == label {
				filtered = append(filtered, item)
			}
		}
		return filtered, nil
	}

	return items, nil
}
