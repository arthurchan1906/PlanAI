// Package vision provides multimodal image analysis by routing to vision-capable
// LLM models via models.json configuration. It is designed as a stateless "eyes"
// service: agents capture screenshots themselves (adb / xcrun / screencapture)
// and call this package to describe what the image shows.
package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	pmdb "aipmc/db"
	"aipmc/session"
	"aipmc/store"

	"github.com/google/uuid"
)

// ── Public types ────────────────────────────────────────────────────────────

// VisionResult is the JSON-serializable response from RunVision.
type VisionResult struct {
	ID        string `json:"id"`
	OK        bool   `json:"ok"`
	Model     string `json:"model,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Iteration int    `json:"iteration,omitempty"`
	Text      string `json:"text,omitempty"`
	Resize    string `json:"resize,omitempty"`

	Error           string   `json:"error,omitempty"`
	Message         string   `json:"message,omitempty"`
	HTTPStatus      int      `json:"http_status,omitempty"`
	AvailableModels []string `json:"available_models,omitempty"`
}

// ── Public API ──────────────────────────────────────────────────────────────

// RunVision reads an image file, routes to a vision model, and returns the
// model's description. On success the result is also written to discussion_log.
func RunVision(imagePath, prompt string, iteration int, explicitModel string) *VisionResult {
	result := &VisionResult{
		ID:        uuid.New().String(),
		Iteration: iteration,
	}
	if iteration < 1 {
		result.Iteration = 1
	}

	// 1. Read + resize + base64 the image.
	imgData, origW, origH, err := loadAndResize(imagePath)
	if err != nil {
		result.OK = false
		result.Error = "image_read"
		result.Message = fmt.Sprintf("无法读取图片: %v", err)
		return result
	}
	b64 := base64.StdEncoding.EncodeToString(imgData)

	// 2. Resolve vision model.
	vr, err := resolveVisionModel(explicitModel)
	if err != nil {
		result.OK = false
		result.Error = "no_vision_model"
		result.Message = err.Error()
		avail := listVisionModels()
		if len(avail) > 0 {
			result.AvailableModels = avail
			result.Message += "。可用模型: " + strings.Join(avail, ", ")
		} else {
			result.Message += "。请先配置视觉模型: aipmc models add ..."
		}
		return result
	}

	// 3. Build prompt.
	assembledPrompt := buildVisionPrompt(prompt, result.Iteration)

	// 4. Call vision API (120 s timeout — local VL models need ~20 s for prompt processing).
	text, httpStatus, callErr := callVisionAPI(vr, assembledPrompt, b64)
	if callErr != nil {
		result.OK = false
		result.HTTPStatus = httpStatus
		switch {
		case strings.Contains(callErr.Error(), "timeout") ||
			strings.Contains(callErr.Error(), "deadline"):
			result.Error = "timeout"
			result.Message = "视觉模型加载超时（180s）。建议：1) 重试 2) 通过 --model 参数指定更快的本地模型"
			if av := listVisionModels(); len(av) > 0 {
				result.AvailableModels = av
			}
		case httpStatus >= 400 && httpStatus < 500:
			result.Error = "rejected"
			result.Message = "请求被拒绝（可能是安全过滤或格式不支持）。建议换个角度描述后重试。"
		case httpStatus >= 500:
			result.Error = "unavailable"
			result.Message = "视觉模型暂不可用（服务器错误）。建议稍后重试。"
		default:
			result.Error = "network"
			result.Message = fmt.Sprintf("网络错误: %v。建议检查网络后重试。", callErr)
		}
		return result
	}
	if strings.TrimSpace(text) == "" {
		result.OK = false
		result.Error = "empty_response"
		result.Message = "视觉模型返回为空，请重试。"
		return result
	}

	// 5. Success.
	newW, newH := origW, origH
	imgCfg, _, cfgErr := image.DecodeConfig(bytes.NewReader(imgData))
	if cfgErr == nil && imgCfg.Width > 0 {
		newW = imgCfg.Width
		newH = imgCfg.Height
	}
	if newW != origW || newH != origH {
		result.Resize = fmt.Sprintf("%dx%d → %dx%d", origW, origH, newW, newH)
	}

	result.OK = true
	result.Model = vr.DisplayName
	if result.Model == "" {
		result.Model = vr.RealModel
	}
	result.Provider = vr.Provider
	result.Text = text

	// 6. Write to discussion_log.
	logVisionDiscussion(imagePath, prompt, text, result)

	return result
}

// ── Vision route ────────────────────────────────────────────────────────────

type visionRoute struct {
	Provider    string
	RealModel   string
	BaseURL     string
	APIKey      string
	DisplayName string
}

func resolveVisionModel(explicit string) (*visionRoute, error) {
	reg := pmdb.LoadModelRegistry()

	if explicit != "" {
		vm := reg.FindModel(explicit)
		if vm == nil {
			return nil, fmt.Errorf("未知模型: %s", explicit)
		}
		r := pickBestRoute(reg, vm)
		if r == nil {
			return nil, fmt.Errorf("模型 %s 没有可用的 provider 路由", explicit)
		}
		return r, nil
	}

	// Sort models by priority (ascending) so higher-priority models come first.
	// Matches proxy/router.go ListCodexModels() behavior.
	sorted := make([]pmdb.VirtualModel, len(reg.Models))
	copy(sorted, reg.Models)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	var localVision, cloudVision *pmdb.VirtualModel
	for i := range sorted {
		vm := &sorted[i]
		if !hasTag(vm.Tags, "vision") {
			continue
		}
		if hasTag(vm.Tags, "local") && localVision == nil {
			localVision = vm
		}
		if cloudVision == nil {
			cloudVision = vm
		}
	}

	for _, vm := range []*pmdb.VirtualModel{localVision, cloudVision} {
		if vm == nil {
			continue
		}
		r := pickBestRoute(reg, vm)
		if r != nil {
			r.DisplayName = vm.DisplayName
			if r.DisplayName == "" {
				r.DisplayName = vm.ID
			}
			return r, nil
		}
	}

	return nil, fmt.Errorf("没有配置视觉模型")
}

func pickBestRoute(reg *pmdb.ModelRegistry, vm *pmdb.VirtualModel) *visionRoute {
	creds := pmdb.GetCredentialStore()
	var first *pmdb.ModelRoute
	for i := range vm.Routes {
		rt := &vm.Routes[i]
		if first == nil {
			first = rt
		}
		prov := reg.FindProvider(rt.Provider)
		if prov == nil {
			continue
		}
		if creds != nil && creds.Get(rt.Provider) != "" {
			return &visionRoute{
				Provider:  rt.Provider,
				RealModel: rt.ModelOpenAI,
				BaseURL:   strings.TrimRight(prov.OpenAIURL, "/") + "/chat/completions",
				APIKey:    creds.Get(rt.Provider),
			}
		}
	}
	if first != nil {
		prov := reg.FindProvider(first.Provider)
		if prov != nil {
			return &visionRoute{
				Provider:  first.Provider,
				RealModel: first.ModelOpenAI,
				BaseURL:   strings.TrimRight(prov.OpenAIURL, "/") + "/chat/completions",
				APIKey:    reg.ResolveAPIKey(first.Provider),
			}
		}
	}
	return nil
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

func listVisionModels() []string {
	reg := pmdb.LoadModelRegistry()
	var out []string
	for _, vm := range reg.Models {
		if hasTag(vm.Tags, "vision") {
			label := vm.ID
			if vm.DisplayName != "" {
				label = vm.DisplayName
			}
			out = append(out, label)
		}
	}
	return out
}

// ── Image processing ────────────────────────────────────────────────────────

// imageResizeThreshold is the maximum long-edge pixel count before scaling.
// OpenAI vision recommends ≤ 2048; Gemini tolerates more but the base64
// payload for 2048px images stays under ~4 MB which is the common limit.
const imageResizeThreshold = 2048

func loadAndResize(path string) (data []byte, origW, origH int, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("读取文件失败: %w", err)
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("无法解码图片 (支持 PNG/JPEG): %w", err)
	}
	origW, origH = cfg.Width, cfg.Height

	longEdge := origW
	if origH > longEdge {
		longEdge = origH
	}
	if longEdge <= imageResizeThreshold {
		data, err := reEncodePNG(raw)
		return data, origW, origH, err
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("解码失败: %w", err)
	}

	scale := float64(imageResizeThreshold) / float64(longEdge)
	newW := int(math.Round(float64(origW) * scale))
	newH := int(math.Round(float64(origH) * scale))
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	resized := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			srcX := int(math.Round(float64(x) / scale))
			srcY := int(math.Round(float64(y) / scale))
			resized.Set(x, y, img.At(srcX, srcY))
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, resized); err != nil {
		return nil, 0, 0, fmt.Errorf("编码 PNG 失败: %w", err)
	}
	return buf.Bytes(), origW, origH, nil
}

func reEncodePNG(raw []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ── Vision API call ─────────────────────────────────────────────────────────

func callVisionAPI(vr *visionRoute, prompt, b64Image string) (text string, httpStatus int, err error) {
	body := map[string]any{
		"model": vr.RealModel,
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": "你是 UI 视觉分析助手。只描述图片实际内容，不提供代码修改建议。如果不确定某个细节，请明确说明。",
			},
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": prompt},
					{"type": "image_url", "image_url": map[string]string{
						"url": "data:image/png;base64," + b64Image,
					}},
				},
			},
		},
		"max_tokens": 2048,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", vr.BaseURL, bytes.NewReader(payload))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if vr.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+vr.APIKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}

	if resp.StatusCode >= 400 {
		return "", resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", resp.StatusCode, fmt.Errorf("解析响应失败: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", resp.StatusCode, nil
	}
	return chatResp.Choices[0].Message.Content, resp.StatusCode, nil
}

// ── Prompt assembly ─────────────────────────────────────────────────────────

func buildVisionPrompt(userPrompt string, iteration int) string {
	var buf bytes.Buffer

	goals := getCachedSessionGoals()
	if len(goals) > 0 {
		buf.WriteString("[背景]\n最近 session:\n")
		for _, g := range goals {
			buf.WriteString("- " + g + "\n")
		}
		buf.WriteString("\n")
	}

	buf.WriteString(fmt.Sprintf("[第 %d 轮视觉检查]\n", iteration))

	prev := previousVisionSummary()
	if prev != "" {
		buf.WriteString("上一轮视觉分析: " + prev + "\n")
	}

	buf.WriteString("\n" + userPrompt)
	return buf.String()
}

var (
	cachedGoals     []string
	cachedGoalsTime time.Time
)

func getCachedSessionGoals() []string {
	if time.Since(cachedGoalsTime) < 60*time.Second && len(cachedGoals) > 0 {
		return cachedGoals
	}
	rows, err := store.ListSessionSummariesWithSummary("", 3)
	if err != nil || len(rows) == 0 {
		return nil
	}
	goals := make([]string, 0, len(rows))
	for _, r := range rows {
		var l2 session.SessionL2Summary
		if json.Unmarshal([]byte(r.Summary), &l2) == nil && l2.Goal != "" {
			sid := r.SessionID
			if len(sid) > 8 {
				sid = sid[:8]
			}
			goals = append(goals, fmt.Sprintf("[%s] %s", sid, l2.Goal))
		}
	}
	cachedGoals = goals
	cachedGoalsTime = time.Now()
	return goals
}

func previousVisionSummary() string {
	db, err := pmdb.Open()
	if err != nil {
		return ""
	}
	defer db.Close()

	var content string
	err = db.QueryRow(
		`SELECT content FROM discussion_log
		 WHERE source = 'aipmc-vision' AND role = 'assistant'
		 ORDER BY created_at DESC LIMIT 1`,
	).Scan(&content)
	if err != nil || content == "" {
		return ""
	}

	runes := []rune(content)
	if len(runes) > 200 {
		content = string(runes[:200])
	}
	return content
}

// ── Discussion log ──────────────────────────────────────────────────────────

func logVisionDiscussion(imagePath, prompt, text string, result *VisionResult) {
	meta, _ := json.Marshal(map[string]any{
		"id":        result.ID,
		"iteration": result.Iteration,
		"model":     result.Model,
		"provider":  result.Provider,
	})

	store.LogDiscussion("", "user", "aipmc-vision",
		fmt.Sprintf("(image: %s) %s", imagePath, prompt),
		string(meta),
	)

	store.LogDiscussion("", "assistant", "aipmc-vision",
		text,
		string(meta),
	)
}
