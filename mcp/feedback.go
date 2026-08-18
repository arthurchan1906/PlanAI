package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
func AddFeedback(label, content string) (map[string]any, error) {
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
func ListFeedbacks(label string) ([]map[string]any, error) {
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

// ── Feedback triage state (B9/B13: 例行 triage + 30 天复测窗口) ──
// 本地记录已处理的 feedback id 与处理时间，供 daily_review 展示未处理列表
// 与「修复后 30 天窗口复测同类反馈」检查。文件 ~/.aipmc/feedback_triage.json。
// 注意：远程 feedback 服务器是临时数据源（项目成熟后取消）。本机制只依赖
// 本地 triage 文件，不依赖远程服务器；服务器取消后 daily_review 的 triage
// 区块自动隐藏（ListFeedbacks 不可达时返回空列表），后续可将数据源替换为
// 本地反馈渠道，或整体移除 triage 区块与 aipm_feedback_triage 工具。
type feedbackTriageState struct {
	Processed map[string]string `json:"processed"` // feedback id → 处理时间 ISO
}

const feedbackTriageFile = "feedback_triage.json"

func feedbackTriagePath() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = "."
	}
	return filepath.Join(home, ".aipmc", feedbackTriageFile)
}

func loadFeedbackTriage() feedbackTriageState {
	st := feedbackTriageState{Processed: map[string]string{}}
	data, err := os.ReadFile(feedbackTriagePath())
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, &st)
	if st.Processed == nil {
		st.Processed = map[string]string{}
	}
	return st
}

func saveFeedbackTriage(st feedbackTriageState) error {
	dir := filepath.Dir(feedbackTriagePath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(feedbackTriagePath(), data, 0o644)
}

// MarkFeedbackProcessed records a feedback id as triaged (now), replacing any
// previous timestamp — a re-triaged item resets its 30-day recheck window.
func MarkFeedbackProcessed(id string) error {
	st := loadFeedbackTriage()
	st.Processed[id] = time.Now().Format(time.RFC3339)
	return saveFeedbackTriage(st)
}

// FeedbackTriageSnapshot returns unprocessed feedback (not yet triaged) and
// the count of recently-processed items still inside the 30-day recheck
// window (B13: 修复后 30 天复测同类反馈是否消失).
func FeedbackTriageSnapshot(feedbacks []map[string]any, recheckDays int) (unprocessed []map[string]any, inWindow int) {
	if recheckDays <= 0 {
		recheckDays = 30
	}
	st := loadFeedbackTriage()
	cutoff := time.Now().AddDate(0, 0, -recheckDays)
	for _, f := range feedbacks {
		id, _ := f["id"].(float64)
		idStr := fmt.Sprintf("%.0f", id)
		if ts, ok := st.Processed[idStr]; ok {
			if t, err := time.Parse(time.RFC3339, ts); err == nil && t.After(cutoff) {
				inWindow++
			}
			continue
		}
		unprocessed = append(unprocessed, f)
	}
	return unprocessed, inWindow
}
