# Codex 原生 Responses API 透传 + 模型注册表第三协议

日期：2026-08-06
状态：已批准（用户确认 4 个决策点）

## 背景与目标

当前模型注册表（`~/.aipmc/models.json`）每个 `Provider` 只有两个协议端点：`openai_url`（必填）和 `anthropic_url`（可选）；每个 `ModelRoute` 只有 `model_openai` / `model_anthropic` 两个真实模型名。路由决策是：

| Agent | 入站路径 | 现状 |
|---|---|---|
| Claude Code | `/v1/messages` | `ShouldPassthrough(model)` 为真 → 原生 Anthropic 透传（`handleAnthropicPassthrough`）；否则 → OpenAI 翻译管道 |
| Codex | `/v1/responses` | **永远**走翻译管道（`handleCodexUnified` → 翻译成 OpenAI Chat → `/chat/completions`） |

目标：

1. **新增第三个接口** `responses_url`（Provider 级）+ `model_responses`（Route 级）。
2. **Codex 优先原生 Responses 透传**：设置了 `responses_url` + `model_responses` 时，body 保持原生不动、直接转发到 `{responses_url}/responses`（对齐 Claude 的 anthropic 透传模式）；**未设置时回落现有翻译管道**。
3. **直接更新现有模型，不重建**：Web 面板 Edit 已支持原地改模型并保留已有 openai/anthropic 值，只需在表单 routes 里加第三个字段。

**已确认的决策点**：
- 更新入口：**Web 面板 Edit 即可**（不新增 CLI `models update` 命令）
- 只按 per-provider 配置，**不引入全局 `responses_url` 兜底**
- 原生透传上游失败时：**直接报错透出，不回退翻译**

## 参考依据

DeepSeek 官方 Codex 接入文档（`~/.codex/config.toml`）：

```toml
[model_providers.deepseek]
base_url = "https://api.deepseek.com/"
wire_api = "responses"
experimental_bearer_token = "<key>"
```

- `base_url` 是 **base URL**，Codex 自动拼 `POST {base_url}/responses` → 本项目 `ResponsesURL` 同样存 base URL，透传时拼 `/responses` 端点。
- 原生 Responses body 只需替换 `model` 字段（→ `model_responses` 真实名，如 `deepseek-v4-flash`）+ 换 `Authorization: Bearer <key>`，其余原样转发。
- 与现有 `AnthropicURL` 透传拼 `/v1/messages` 完全同构。

## 为什么不复用翻译管道

翻译管道（`handleCodexUnified`）本质是「让只支持 Chat Completions 的上游也能服务 Codex 客户端」：进站即把原生 Responses 解析成 `UnifiedReq`，再转成 Chat 格式发给 `/chat/completions`。当上游原生支持 Responses API（DeepSeek 的 `wire_api = "responses"`）时，翻译是绕路，代码里已有往返摩擦的证据：

- `proxy/responses.go:441-460`：DeepSeek 只返回 `reasoning_content` 时需 hack 提升为 message，Codex 才能看到答案
- `proxy/responses.go:164-166`：`reasoning.effort` 手动映射
- 原生 `reasoning` / `function_call` / streaming 事件精确语义在往返翻译中损失

「直接把翻译管道端点从 `chat/completions` 换成 `responses`」不可行——进站后 body 已是 Chat 格式 `{messages:[...]}`，POST 到 `/responses` 端点会被上游解析失败（该端点期望 `{input:[...]}`）。要复用只能再写一个 `unifiedToResponses` 反向转换（两次翻译，更复杂、损失更多），不如保持 body 原生走透传。

## 设计

### 1. 数据模型（`db/models.go`）

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
	ModelResponses string `json:"model_responses,omitempty"` // 真实 Responses 模型名，如 deepseek-v4-flash
}
```

- `realModelForRoute`（`db/models.go:229`）增加 `"responses"` 协议分支，返回 `rt.ModelResponses`。
- `ModelForAgentProto`（`db/models.go:258`）的 codex 分支补上 responses 查找：目前注释说 "codex → responses (falls back to openai)"，但代码里没实现。改为 codex 时优先找 `model_responses`，找不到回退 openai。
- `VirtualModel` 的 legacy 迁移不动（旧格式无 responses 字段，迁移产物 `ModelResponses` 为空字符串即正确回退）。

### 2. 路由决策（`proxy/proxy.go`）

`/v1/responses` 分支（`proxy.go:341`）改为：

```go
case path == "/v1/responses" || path == "/responses":
	if r.Method == "GET" { ... } // 不变
	model := peekModel(body)
	if model != "" && router.ShouldPassthroughResponses(model) {
		handleResponsesPassthrough(rw, r)
	} else {
		handleCodexUnified(rw, r)
	}
```

新增 `router.ShouldPassthroughResponses(virtualModel string) bool`（`proxy/router.go`），镜像现有 `ShouldPassthrough`（`proxy/router.go:188`）：任一 route 的 provider 有 `ResponsesURL` 且该 route 有 `ModelResponses` 时返回 true。需注意 `proxy.go:341` 分支当前读 `r.Body` 到 `body`，但 `handleCodexUnified` 内部会重新读 body，需保持 body 重建逻辑一致（透传 handler 自己读 body）。

### 3. 透传 handler（新文件 `proxy/responses_passthrough.go`）

镜像 `handleAnthropicPassthrough`（`proxy/anthropic_passthrough.go:17`）全部步骤，agent 换成 `"codex"`：

1. 读原始 body
2. currentModel override：`loadCurrentModel("codex")` 非空时替换 peeked model 与 body
3. 路由解析：`router.Resolve(model, "responses")` → 返回 `Route{RealModel: model_responses, BaseURL: responses_url}`
4. 替换 body 的 `model` 为 `route.RealModel`；无路由时用 `loadCfg().proxyModel` 兜底
5. capture 启动（agent=codex）
6. 目标 URL：`strings.TrimRight(route.BaseURL, "/") + "/responses"`
7. 鉴权：路由有 key 时覆写 `Authorization: Bearer <key>`（删旧 `X-Api-Key`/`Authorization` 再设，与 anthropic 一致）；无 key 保留原请求头
8. 非 streaming：直接转发 + 返回上游响应；streaming：字节级 SSE 透传 + capture
9. token 用量：从 `response.completed` SSE 事件解析（复用 `extractAnthropicStreamUsage` 的解析模式，写 `extractResponsesStreamUsage`）
10. 上游 4xx/5xx → `http.Error` 直接透出错误，**不回退翻译**

### 4. 管理入口

**Web 面板 `frontend/src/components/ModelRegistryEditor.jsx`**：
- `ProviderModal`（:9）：加 "Responses URL" 输入项（`name="responses_url"`）
- `ModelModal`（:31）：routes 每行加第三个输入格（`name="model_responses"`，placeholder "Responses model name"）；`initialValues` 已有 `model_responses` 时自动带出 → Edit 既有模型不丢值
- Provider 卡片加 `R` 徽章（对齐现有 `A` 徽章，:274）
- Protocol Names 列加 `R:xxx`（对齐 `O:`/`A:`，:211-220）
- 新建 route 默认 `model_responses: ""`

**Web API `api/config.go`**：`providers[]` 解析加 `ResponsesURL: u.Str(pm["responses_url"])`（:158-163）；`models[]` routes 解析加 `ModelResponses: u.Str(rm["model_responses"])`（:188-193）

**CLI `dispatch.go`**（防整体覆盖写时静默丢字段）：
- `models list`：Provider 行加 `responses=...`；模型 route 详情加 `responses=...`
- `models provider add`：加 `--responses_url` 参数（:681 usage 同步）
- `models add`：加 `--responses` 参数（:736 usage 同步）

### 5. 错误处理与回退

| 场景 | 行为 |
|---|---|
| 模型未配置 `responses_url` / `model_responses` | 回落现有翻译管道（`handleCodexUnified`），既有行为不变 |
| 已配置但上游 4xx/5xx | 透出上游错误，不回退（已确认） |
| 已配置但上游网络失败 | 透出 502，不回退 |
| 模型不存在 / 路由无法解析 | 回落翻译管道（与 anthropic 分支的 `ShouldPassthrough` 假 → 翻译一致） |

### 6. 验证

- `go build ./...`
- 手动验证清单：
  1. Web 面板给已有 provider 加 `responses_url`、给已有 model 的 route 加 `model_responses` → 保存后 `aipmc models list` 能看到三份字段，Edit 再次打开仍保留
  2. 未配置 responses 的模型发 `/v1/responses` → capture 显示走翻译（URL 指向 `/chat/completions`）
  3. 配置了 responses 的模型发 `/v1/responses` → capture 显示透传（URL 指向 `{responses_url}/responses`，body 为原生 Responses）
  4. 透传上游返回 4xx → Codex 收到错误，无自动回退

## 影响面

- 改动文件：`db/models.go`、`proxy/router.go`、`proxy/proxy.go`、`proxy/responses_passthrough.go`（新增）、`api/config.go`、`frontend/src/components/ModelRegistryEditor.jsx`、`dispatch.go`
- 不改动：`handleAnthropicPassthrough`（Claude 已满足「优先 anthropic 否则翻译」）、`handleCodexUnified`（作为回落保留）、`proxy/responses.go` 双向翻译（作为回落路径保留）

## 非目标

- 不新增全局 `responses_url` 配置兜底（已确认）
- 不新增 CLI `models update` 命令（Web Edit 已覆盖，已确认）
- 不做透传失败的自动回退（已确认）
- 不改造 AIPM 自身 LLM 调用（`ai/http_client.go` 等仍走 `/chat/completions`）
