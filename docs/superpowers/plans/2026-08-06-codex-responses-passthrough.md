# Codex 原生 Responses API 透传实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为模型注册表新增第三个协议（`responses_url` / `model_responses`），让 Codex 在配置了响应式协议时走原生 Responses 透传，未配置时回落现有翻译管道；Web 面板 Edit 直接更新模型不重建。

**Architecture:** 镜像 Claude 的 `handleAnthropicPassthrough` 透传模式。数据层加字段 → 路由层加 `ShouldPassthroughResponses` 判断 → 新 handler 透传（只改 model + 鉴权，字节级 SSE）→ 管理入口（Web 表单/API/CLI）识别新字段。

**Tech Stack:** Go 1.21+（现有项目）、React 18 + antd（前端）、`~/.aipmc/models.json`（配置存储）

## Global Constraints

- 上游 `ResponsesURL` 存 **base URL**，透传时拼 `/responses` 端点（对齐 DeepSeek `base_url` + Codex 拼 `/responses`）
- 透传 handler 的 `model` 替换用 `replaceModelInBody`；鉴权用 `copyUpstreamHeaders` 的 key 覆写语义（同 `handleAnthropicPassthrough`）
- 所有 `ResponsesURL` / `ModelResponses` 字段都是 `omitempty`，旧 `models.json` 无字段时读取为空串 → 正确回落翻译
- 新增 handler 命名为 `handleResponsesPassthrough`，agent 记为 `"codex"`
- 透传上游失败：直接 `http.Error` 透出错误，**不回退翻译**
- 前端新字段名：provider `responses_url`、route `model_responses`

---

### Task 1: 数据模型加第三协议字段

**Files:**
- Modify: `db/models.go:15-29`（`Provider` / `ModelRoute` struct）
- Modify: `db/models.go:229-241`（`realModelForRoute`）
- Modify: `db/models.go:258-281`（`ModelForAgentProto`）
- Test: `db/models_test.go`（新建）

**Interfaces:**
- Produces: `Provider.ResponsesURL string`、`ModelRoute.ModelResponses string`
- Produces: `realModelForRoute(rt *ModelRoute, protocol string)` 支持 `"responses"` 返回 `rt.ModelResponses`
- Produces: `ModelForAgentProto(virtualModelID, agentType)` 对 codex 优先返回 `model_responses`，找不到回退 openai

- [ ] **Step 1: 写失败测试**

```go
package db

import "testing"

func TestRealModelForRouteResponses(t *testing.T) {
	rt := &ModelRoute{Provider: "deepseek", ModelOpenAI: "deepseek-chat", ModelAnthropic: "deepseek-claude", ModelResponses: "deepseek-v4-flash"}
	if got := realModelForRoute(rt, "responses"); got != "deepseek-v4-flash" {
		t.Fatalf("responses protocol: got %q want %q", got, "deepseek-v4-flash")
	}
	if got := realModelForRoute(rt, "openai"); got != "deepseek-chat" {
		t.Fatalf("openai protocol: got %q want %q", got, "deepseek-chat")
	}
}

func TestModelForAgentProtoCodexResponses(t *testing.T) {
	reg := &ModelRegistry{Models: []VirtualModel{{
		ID: "deepseek-v4-pro",
		Routes: []ModelRoute{{
			Provider:       "deepseek",
			ModelOpenAI:    "deepseek-chat",
			ModelResponses: "deepseek-v4-pro",
		}},
	}}}
	if got := reg.ModelForAgentProto("deepseek-v4-pro", "codex"); got != "deepseek-v4-pro" {
		t.Fatalf("codex proto: got %q want %q", got, "deepseek-v4-pro")
	}
}

func TestModelForAgentProtoCodexFallsBackToOpenAI(t *testing.T) {
	reg := &ModelRegistry{Models: []VirtualModel{{
		ID: "deepseek-v4-pro",
		Routes: []ModelRoute{{
			Provider:    "deepseek",
			ModelOpenAI: "deepseek-chat",
		}},
	}}}
	if got := reg.ModelForAgentProto("deepseek-v4-pro", "codex"); got != "deepseek-chat" {
		t.Fatalf("codex fallback: got %q want %q", got, "deepseek-chat")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./db/ -run TestRealModelForRouteResponses -v`
Expected: FAIL — `realModelForRoute` 无 `responses` 分支，返回空串；`ModelForAgentProto` 无 responses 查找

- [ ] **Step 3: 加字段 + 协议分支**

```go
type Provider struct {
	Name         string `json:"name"`
	OpenAIURL    string `json:"openai_url"`
	AnthropicURL string `json:"anthropic_url,omitempty"`
	ResponsesURL string `json:"responses_url,omitempty"` // base URL，透传时拼 /responses
}

type ModelRoute struct {
	Provider       string `json:"provider"`
	ModelOpenAI    string `json:"model_openai,omitempty"`
	ModelAnthropic string `json:"model_anthropic,omitempty"`
	ModelResponses string `json:"model_responses,omitempty"` // 真实 Responses 模型名
}
```

`realModelForRoute`（`db/models.go:229`）加分支：

```go
switch protocol {
case "anthropic":
	if rt.ModelAnthropic != "" {
		return rt.ModelAnthropic
	}
case "openai":
	if rt.ModelOpenAI != "" {
		return rt.ModelOpenAI
	}
case "responses":
	if rt.ModelResponses != "" {
		return rt.ModelResponses
	}
}
```

`ModelForAgentProto`（`db/models.go:258-281`）改 target 选择逻辑，codex 用 `"responses"`：

```go
targetProto := "openai"
if agentType == "claude" || agentType == "claude-code" {
	targetProto = "anthropic"
} else if agentType == "codex" {
	targetProto = "responses"
}
// Walk Routes: prefer the route that has a protocol-specific name.
for _, rt := range vm.Routes {
	if name := realModelForRoute(&rt, targetProto); name != "" {
		return name
	}
}
// 2. codex 若 responses 未配置，回退 openai 名
if targetProto == "responses" {
	for _, rt := range vm.Routes {
		if rt.ModelOpenAI != "" {
			return rt.ModelOpenAI
		}
	}
}
// Fallback to legacy fields.
if targetProto == "anthropic" && vm.Anthropic != "" {
	return vm.Anthropic
}
if vm.OpenAI != "" {
	return vm.OpenAI
}
return virtualModelID
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./db/ -v`
Expected: PASS（3 个新测试 + 既有测试）

- [ ] **Step 5: 提交**

```bash
git add db/models.go db/models_test.go
git commit -m "feat: 模型注册表新增 responses 协议字段 — ModelResponses + 协议解析分支"
```

---

### Task 2: 路由层加 ShouldPassthroughResponses

**Files:**
- Modify: `proxy/router.go:188-210`（新增方法，紧邻现有 `ShouldPassthrough`）
- Test: `proxy/router_test.go`（新建）

**Interfaces:**
- Consumes: Task 1 的 `Provider.ResponsesURL` / `ModelRoute.ModelResponses`
- Produces: `func (r *ModelRouter) ShouldPassthroughResponses(virtualModel string) bool`

- [ ] **Step 1: 写失败测试**

```go
package proxy

import (
	"testing"
	pmdb "aipmc/db"
)

func TestShouldPassthroughResponses(t *testing.T) {
	reg := pmdb.ModelRegistry{
		Version: 1,
		Providers: []pmdb.Provider{{
			Name:         "deepseek",
			OpenAIURL:    "https://api.deepseek.com/v1",
			ResponsesURL: "https://api.deepseek.com/",
		}},
		Models: []pmdb.VirtualModel{{
			ID: "deepseek-v4-flash",
			Routes: []pmdb.ModelRoute{{
				Provider:       "deepseek",
				ModelOpenAI:    "deepseek-chat",
				ModelResponses: "deepseek-v4-flash",
			}},
		}},
	}
	router := &ModelRouter{registry: &reg}
	if !router.ShouldPassthroughResponses("deepseek-v4-flash") {
		t.Fatal("expected true: provider has responses_url + route has model_responses")
	}
}

func TestShouldPassthroughResponsesFalseWhenNoResponsesURL(t *testing.T) {
	reg := pmdb.ModelRegistry{
		Version: 1,
		Providers: []pmdb.Provider{{
			Name:      "deepseek",
			OpenAIURL: "https://api.deepseek.com/v1",
		}},
		Models: []pmdb.VirtualModel{{
			ID: "deepseek-v4-flash",
			Routes: []pmdb.ModelRoute{{
				Provider:       "deepseek",
				ModelOpenAI:    "deepseek-chat",
				ModelResponses: "deepseek-v4-flash",
			}},
		}},
	}
	router := &ModelRouter{registry: &reg}
	if router.ShouldPassthroughResponses("deepseek-v4-flash") {
		t.Fatal("expected false: provider has no responses_url")
	}
}

func TestShouldPassthroughResponsesFalseForUnknownModel(t *testing.T) {
	router := &ModelRouter{registry: &pmdb.ModelRegistry{Models: []pmdb.VirtualModel{{ID: "x", Routes: []pmdb.ModelRoute{{Provider: "p", ModelOpenAI: "o"}}}}}}
	if router.ShouldPassthroughResponses("nope") {
		t.Fatal("expected false: unknown model")
	}
}
```

注意：`db` 包无测试用 registry 构造时缺 `Provider` 查找会导致 panic，需先在 `db` 建 `FindProvider` 覆盖——已存在（`db/models.go:177`）。测试里 router 直接持有 `&reg`，`FindProvider` 用指针找。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./proxy/ -run TestShouldPassthroughResponses -v`
Expected: FAIL — `ShouldPassthroughResponses` 未定义

- [ ] **Step 3: 实现方法**

在 `proxy/router.go` `ShouldPassthrough` 方法后追加：

```go
// ShouldPassthroughResponses checks whether a virtual model supports the
// OpenAI Responses API natively (i.e., at least one route has a provider with
// responses_url and the route has a ModelResponses name). Used by the handler
// to decide whether to route /v1/responses through native passthrough or
// through the translation pipeline.
func (r *ModelRouter) ShouldPassthroughResponses(virtualModel string) bool {
	if !r.IsActive() {
		return false
	}
	reg := r.getRegistry()
	vm := reg.FindModel(virtualModel)
	if vm == nil {
		return false
	}
	for _, rt := range vm.Routes {
		prov := reg.FindProvider(rt.Provider)
		if prov != nil && prov.ResponsesURL != "" && rt.ModelResponses != "" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./proxy/ -run TestShouldPassthroughResponses -v`
Expected: PASS（3 个测试）

- [ ] **Step 5: 提交**

```bash
git add proxy/router.go proxy/router_test.go
git commit -m "feat: router.ShouldPassthroughResponses — responses 协议原生支持判断"
```

---

### Task 3: 新增 Responses 原生透传 handler

**Files:**
- Create: `proxy/responses_passthrough.go`
- Modify: `proxy/proxy.go:341-350`（`/v1/responses` 分支加判断）
- Modify: `proxy/token_usage.go`（新增 `extractResponsesStreamUsage`）

**Interfaces:**
- Consumes: Task 2 的 `ShouldPassthroughResponses`、`proxy.go` 的 `handleCodexUnified`、`peekModel`/`replaceModelInBody`/`loadCurrentModel`/`startCapture`/`finishCapture`/`copyHeaders`/`extractAPIKey`
- Consumes: `router.Resolve(model, "responses")`（`proxy/router.go:80`，需确认返回 `ModelResponses` + `ResponsesURL`）
- Produces: `func handleResponsesPassthrough(w http.ResponseWriter, r *http.Request)`
- Produces: `func extractResponsesStreamUsage(body string) (inputTokens, outputTokens int)`（解析上游 `response.completed` 事件的 `usage`）

- [ ] **Step 1: 确认 `router.Resolve` 的 responses 协议分支已返回正确字段**

`router.go:133-145` 的 `buildRoute` 目前只认 `openai`/`anthropic`。需加：

```go
if protocol == "anthropic" {
	if rt.ModelAnthropic != "" {
		realModel = rt.ModelAnthropic
	}
	if prov.AnthropicURL != "" {
		baseURL = prov.AnthropicURL
	}
} else if protocol == "responses" {
	if rt.ModelResponses != "" {
		realModel = rt.ModelResponses
	}
	if prov.ResponsesURL != "" {
		baseURL = prov.ResponsesURL
	}
}
```

（归入本任务，因为透传依赖它。改完后跑 `go test ./db/ -v` 确认旧测试不破。）

- [ ] **Step 2: 写失败测试（透传 handler 的 URL 构造 + model 替换）**

```go
package proxy

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestResponsesPassthroughRequestBuilding 验证：给定原生 Responses body，
// 路由解析后目标 URL 拼 {ResponsesURL}/responses，body 的 model 被替换。
// 用一个 httptest server 作上游，捕获收到的 body 断言。
func TestResponsesPassthroughForwardsNativeBody(t *testing.T) {
	var gotURL string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"resp_x","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2}}`))
	}))
	defer upstream.Close()

	// 直接用透传 handler 所需的最小依赖：mock router 返回 route 指向 upstream
	// 为简化，这里测底层 forwardResponsesPassthrough（Task 3 内建纯函数）：
	//   构造原生 body + route → 返回 targetURL + 替换后 body
	body := []byte(`{"model":"deepseek-v4-flash","input":[{"type":"message","role":"user","content":"hi"}]}`)
	url, replaced := buildResponsesPassthrough(upstream.URL, "deepseek-v4-real", body)
	if url != strings.TrimRight(upstream.URL, "/")+"/responses" {
		t.Fatalf("url: got %q", url)
	}
	if !strings.Contains(string(replaced), `"deepseek-v4-real"`) {
		t.Fatalf("model not replaced: %s", replaced)
	}
}
```

（此测试用内建纯函数 `buildResponsesPassthrough(baseURL, realModel, body)` 隔离验证 URL 拼接 + model 替换，不依赖真实网络。）

- [ ] **Step 3: 实现 handler + 辅助纯函数**

新文件 `proxy/responses_passthrough.go`：

```go
package proxy

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"aipmc/u"
)

// buildResponsesPassthrough 构造透传目标 URL（{baseURL}/responses）并替换 body 的 model 字段。
func buildResponsesPassthrough(baseURL, realModel string, body []byte) (string, []byte) {
	return strings.TrimRight(baseURL, "/") + "/responses", replaceModelInBody(body, realModel)
}

// handleResponsesPassthrough 把 Codex 的原生 Responses body 直接转发到
// provider 的 {ResponsesURL}/responses 端点，字节级 SSE 透传，不做协议翻译。
func handleResponsesPassthrough(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()

	peekedModel := peekModel(body)
	if cm := loadCurrentModel("codex"); cm != "" {
		peekedModel = cm
		body = replaceModelInBody(body, cm)
	}

	var route *Route
	if router := loadCfg().router; router != nil && router.IsActive() {
		if peekedModel != "" {
			route = router.Resolve(peekedModel, "responses")
		}
	}
	if route != nil {
		body = replaceModelInBody(body, route.RealModel)
	} else if loadCfg().proxyModel != "" {
		body = replaceModelInBody(body, loadCfg().proxyModel)
	}

	effectiveModelName := route.RealModel
	if effectiveModelName == "" {
		effectiveModelName = loadCfg().proxyModel
	}
	if effectiveModelName == "" {
		effectiveModelName = peekedModel
	}

	agent := "codex"
	capID := startCapture(agent, r.Method, r.URL.Path, effectiveModelName, body, copyHeaders(r), nil)
	startTime := time.Now()

	targetURL := loadCfg().upstreamURL + "/responses"
	apiKey := ""
	if route != nil {
		targetURL = strings.TrimRight(route.BaseURL, "/") + "/responses"
		if route.APIKey != "" {
			apiKey = route.APIKey
		}
	}

	// 支持 streaming：优先字节级透传（上游 SSE → 下游 Codex 原样）
	if isStreamingResponses(body) {
		handleResponsesPassthroughStream(w, r, body, targetURL, apiKey, capID, effectiveModelName, startTime, agent)
		return
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		finishCapture(capID, http.StatusInternalServerError, time.Since(startTime), nil, err.Error(), "")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	proxyReq.Header = r.Header.Clone()
	if apiKey != "" {
		proxyReq.Header.Del("X-Api-Key")
		proxyReq.Header.Del("Authorization")
		proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		finishCapture(capID, http.StatusBadGateway, time.Since(startTime), nil, err.Error(), "")
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		finishCapture(capID, resp.StatusCode, time.Since(startTime), nil, string(respBody), "")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
	finishCapture(capID, resp.StatusCode, time.Since(startTime), nil, string(respBody), "")

	if in, out := extractResponsesStreamUsage(string(respBody)); in > 0 || out > 0 {
		RecordTokenUsage(TokenUsageRecord{
			Agent:            agent,
			Model:            effectiveModelName,
			PromptTokens:     in,
			CompletionTokens: out,
		})
		SetCaptureTokens(capID, in, out)
		u.LogShared("LLM", "agent=codex model=%s in_tok=%d out_tok=%d lat=%.1fs", effectiveModelName, in, out, time.Since(startTime).Seconds())
	}
}

// isStreamingResponses 从 body 判断是否 streaming。
func isStreamingResponses(body []byte) bool {
	var peek struct {
		Stream bool `json:"stream"`
	}
	if json.Unmarshal(body, &peek) == nil {
		return peek.Stream
	}
	return false
}

// handleResponsesPassthroughStream 字节级 SSE 透传 streaming 响应。
func handleResponsesPassthroughStream(w http.ResponseWriter, r *http.Request, body []byte, targetURL, apiKey, capID string, model string, startTime time.Time, agent string) {
	proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		finishCapture(capID, http.StatusInternalServerError, time.Since(startTime), nil, err.Error(), "")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	proxyReq.Header = r.Header.Clone()
	if apiKey != "" {
		proxyReq.Header.Del("X-Api-Key")
		proxyReq.Header.Del("Authorization")
		proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		finishCapture(capID, http.StatusBadGateway, time.Since(startTime), nil, err.Error(), "")
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	var captureBuf bytes.Buffer
	tee := io.TeeReader(resp.Body, &captureBuf)
	buf := make([]byte, 4096)
	for {
		n, err := tee.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}
	finishCapture(capID, resp.StatusCode, time.Since(startTime), nil, captureBuf.String(), "")
	if in, out := extractResponsesStreamUsage(captureBuf.String()); in > 0 || out > 0 {
		RecordTokenUsage(TokenUsageRecord{
			Agent:            agent,
			Model:            model,
			PromptTokens:     in,
			CompletionTokens: out,
		})
		SetCaptureTokens(capID, in, out)
		u.LogShared("LLM", "agent=codex model=%s in_tok=%d out_tok=%d lat=%.1fs", model, in, out, time.Since(startTime).Seconds())
	}
}
```

`proxy/token_usage.go` 加解析函数（镜像 `extractAnthropicStreamUsage`，但字段是 `input_tokens`/`output_tokens`，非 `input_tokens`+`cache_read`）：

```go
// extractResponsesStreamUsage 从上游 Responses API 的 SSE 中解析 token 用量。
// 上游原生 Responses 的 response.completed 事件 usage 结构为
// {input_tokens, output_tokens, total_tokens}。
func extractResponsesStreamUsage(body string) (inputTokens, outputTokens int) {
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var event struct {
			Type  string `json:"type"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &event) != nil {
			continue
		}
		if event.Type == "response.completed" || event.Usage.InputTokens > 0 || event.Usage.OutputTokens > 0 {
			if event.Usage.InputTokens > 0 {
				inputTokens = event.Usage.InputTokens
			}
			if event.Usage.OutputTokens > 0 {
				outputTokens = event.Usage.OutputTokens
			}
		}
	}
	return
}
```

- [ ] **Step 4: 接线 `/v1/responses` 路由分支**

`proxy/proxy.go:341-350`：

```go
case path == "/v1/responses" || path == "/responses":
	if r.Method == "GET" {
		if strings.ToLower(r.Header.Get("Upgrade")) == "websocket" {
			http.Error(rw, "websocket not supported", http.StatusBadRequest)
			return
		}
		writeJSON(rw, http.StatusOK, map[string]any{"object": "list", "data": []any{}})
		return
	}
	model := peekModel(body)
	if model != "" && router.ShouldPassthroughResponses(model) {
		handleResponsesPassthrough(rw, r)
	} else {
		handleCodexUnified(rw, r)
	}
```

注意：`body` 变量在 handler 顶部已由 `io.ReadAll(r.Body)` 读到，`router` 变量来自 `loadCfg().router`。`handleCodexUnified` 内部会重新 `io.ReadAll` body（`proxy.go:814`），当前代码 `r.Body` 已被重新 `NopCloser` 重建（`proxy.go:298-311`），透传 handler 也自己读 body，逻辑一致。

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./proxy/ -run TestResponsesPassthrough -v && go test ./db/ -v && go build ./...`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add proxy/responses_passthrough.go proxy/token_usage.go proxy/proxy.go proxy/router.go
git commit -m "feat: Codex Responses 原生透传 — handleResponsesPassthrough + 路由分支"
```

---

### Task 4: Web 面板表单加第三个字段

**Files:**
- Modify: `frontend/src/components/ModelRegistryEditor.jsx`

**Interfaces:**
- Consumes: 前端已有 `providers` / `models` props，`onChange` 保存
- Produces: provider 对象含 `responses_url`；route 对象含 `model_responses`

- [ ] **Step 1: ProviderModal 加 Responses URL 输入**

`ModelRegistryEditor.jsx:21-23`（Anthropic URL 之后）加：

```jsx
<Form.Item name="responses_url" label="Responses URL">
  <Input placeholder="https://api.deepseek.com/" />
</Form.Item>
```

- [ ] **Step 2: ModelModal 的 route 行加第三个输入格**

`ModelRegistryEditor.jsx:107-109`（model_anthropic 之后）加：

```jsx
<Form.Item {...rest} name={[name, "model_responses"]} style={{ marginBottom: 4, width: 170 }}>
  <Input placeholder="Responses model name" size="small" style={{ fontSize: 12 }} />
</Form.Item>
```

同时 `ModelModal` 的 `initialValues`（`:44`）和新建默认 route（`:45`）的 `model_responses` 默认空串已在 `onFinish` 的 map 里带出（`:52` 处 `model_anthropic: r.model_anthropic || ""` 之后加 `model_responses: r.model_responses || ""`）。

- [ ] **Step 3: Provider 卡片加 R 徽章 + 表加 R 协议**

`ModelRegistryEditor.jsx:274`（A 徽章后）：

```jsx
{p.responses_url && <Tag color="purple" style={{ fontSize: 9, margin: 0, padding: "0 3px" }}>R</Tag>}
```

`:211-220`（protoInfo 里 A 之后）：

```jsx
if (rt.model_responses) parts.push(`R:${rt.model_responses}`);
```

- [ ] **Step 4: 手动验证（无前端测试基建，用浏览器）**

Run: `cd frontend && npm run build`（确认无编译错误）→ 启动 proxy + web → 打开模型编辑页 → Edit 已有模型 → 看到并填入 `model_responses` → 保存 → 重新打开确认保留
Expected: 编译通过，Edit 保留三份字段

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/ModelRegistryEditor.jsx
git commit -m "feat: Web 面板模型编辑加 responses_url / model_responses 字段 — Edit 直接更新不重建"
```

---

### Task 5: Web API 解析新字段

**Files:**
- Modify: `api/config.go:158-163`（providers 解析）
- Modify: `api/config.go:188-193`（models routes 解析）

**Interfaces:**
- Consumes: Task 1 的 `Provider.ResponsesURL` / `ModelRoute.ModelResponses`
- Produces: web 面板保存的 providers/models 数组带新字段

- [ ] **Step 1: providers 解析加字段**

`api/config.go:158-163`：

```go
reg.Providers = append(reg.Providers, pmdb.Provider{
	Name:         u.Str(pm["name"]),
	OpenAIURL:    u.Str(pm["openai_url"]),
	AnthropicURL: u.Str(pm["anthropic_url"]),
	ResponsesURL: u.Str(pm["responses_url"]),
})
```

- [ ] **Step 2: models routes 解析加字段**

`api/config.go:188-193`：

```go
vm.Routes = append(vm.Routes, pmdb.ModelRoute{
	Provider:       u.Str(rm["provider"]),
	ModelOpenAI:    u.Str(rm["model_openai"]),
	ModelAnthropic: u.Str(rm["model_anthropic"]),
	ModelResponses: u.Str(rm["model_responses"]),
})
```

- [ ] **Step 3: 构建验证**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add api/config.go
git commit -m "feat: api/config.go 解析 responses_url / model_responses 字段"
```

---

### Task 6: CLI 展示新字段防丢

**Files:**
- Modify: `dispatch.go:577-612`（`models list`）
- Modify: `dispatch.go:681-688`（`provider add`）
- Modify: `dispatch.go:736-757`（`models add`）

**Interfaces:**
- Consumes: Task 1 字段

- [ ] **Step 1: `models list` 展示**

`dispatch.go:583`（provider 行）：

```go
resp := ""
if p.ResponsesURL != "" {
	resp = fmt.Sprintf(" responses=%s", p.ResponsesURL)
}
fmt.Printf("  %-15s openai=%s%s%s\n", p.Name, p.OpenAIURL, anthro, resp)
```

`dispatch.go:591-603`（route 详情）：

```go
if rt.ModelResponses != "" {
	proto += fmt.Sprintf(" responses=%s", rt.ModelResponses)
}
```

- [ ] **Step 2: `provider add` 加参数**

`dispatch.go:681` usage + `:687`：

```go
reg.AddProvider(pmdb.Provider{
	Name:         name,
	OpenAIURL:    openaiURL,
	AnthropicURL: args.Str("anthropic_url", ""),
	ResponsesURL: args.Str("responses_url", ""),
})
```

usage 行（`:681`）：`... [--responses_url <url>]`

- [ ] **Step 3: `models add` 加参数**

`dispatch.go:736` usage + `:750-754`：

```go
Routes: []pmdb.ModelRoute{{
	Provider:       provider,
	ModelAnthropic: args.Str("anthropic", ""),
	ModelOpenAI:    args.Str("openai", ""),
	ModelResponses: args.Str("responses", ""),
}},
```

usage 行（`:736`）：`... [--responses <model>]`

- [ ] **Step 4: 构建验证**

Run: `go build ./... && go test ./db/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add dispatch.go
git commit -m "feat: CLI models 展示/添加 responses 字段，防整体覆盖时丢字段"
```

---

### Task 7: 端到端手动验证

**Files:** 无代码改动

- [ ] **Step 1: 配置测试 provider + model**

```bash
aipmc models provider add --name deepseek --openai_url https://api.deepseek.com/v1 --anthropic_url https://api.deepseek.com/anthropic --responses_url https://api.deepseek.com/
aipmc models add --id deepseek-v4-pro --provider deepseek --openai deepseek-chat --anthropic deepseek-claude --responses deepseek-v4-pro
aipmc models list
```

Expected: provider 行显示 `responses=https://api.deepseek.com/`；model 行显示 `[deepseek openai=... anthropic=... responses=deepseek-v4-pro]`

- [ ] **Step 2: 起 proxy 发 Codex 请求验证透传**

Run: `aipmc proxy` → 用 `curl` 发原生 Responses 请求到 `:19530/v1/responses`：

```bash
curl -s -X POST http://localhost:19530/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer test-key" \
  -d '{"model":"deepseek-v4-pro","input":[{"type":"message","role":"user","content":"hello"}],"stream":false}'
```

Expected: 请求命中透传分支，capture（Web inspector 或 `aipmc` 日志）显示目标 URL 为 `https://api.deepseek.com/responses`，body 保持原生 `input` 结构

- [ ] **Step 3: 验证未配置 responses 回落翻译**

`aipmc models provider add --name local --openai_url http://localhost:8080/v1` + `aipmc models add --id local-test --provider local --openai test` → 同样 curl → capture 显示走翻译（URL `/chat/completions`）

- [ ] **Step 4: 验证 Web Edit 保留字段**

浏览器打开面板 → Edit `deepseek-v4-pro` → 确认三份字段都在 → 保存 → 重新打开确认保留

- [ ] **Step 5: 收尾提交（如需调整）**

```bash
git add -A && git commit -m "chore: 端到端验证 responses 透传"
```

---

## Self-Review 记录

- **Spec 覆盖**：数据模型（Task 1）、路由判断（Task 2）、透传 handler（Task 3）、Web 表单（Task 4）、Web API（Task 5）、CLI（Task 6）、验证（Task 7）。非目标（全局兜底/CLI update/自动回退）均未入 plan。
- **占位符扫描**：所有代码块为实际可粘贴内容，无 TBD/TODO。
- **类型一致性**：`ResponsesURL`/`ModelResponses` 字段名在 db/router/api/dispatch/frontend 四处一致；`ShouldPassthroughResponses`、`handleResponsesPassthrough`、`extractResponsesStreamUsage` 签名在 Task 2/3 间一致。
