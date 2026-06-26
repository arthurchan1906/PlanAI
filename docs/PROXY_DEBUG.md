# Proxy 调试经验总结

记录 myproxy 整合进 aipmc 过程中发现的所有 bug、修复方案和各 Agent 配置方法。

---

## 一、修复的 Bug

### 1. `parametersJsonSchema` 字段名不匹配

**现象**：Gemini CLI 工具调用全部失败，`params must have required property 'file_path'`。

**根因**：Gemini CLI SDK 发送 `"parametersJsonSchema"` 字段，proxy 的 `GeminiFuncDecl` 期望 `"parameters"`。反序列化时 `Parameters` 为 `nil`，模型收不到 schema，只能返回空参数 `{}`。

**修复**：`proxy/proxy.go` 中 `GeminiFuncDecl.Parameters` 的 JSON tag 改为 `json:"parametersJsonSchema"`。

---

### 2. `max_tokens: null` 导致 llama.cpp 400 错误

**现象**：Codex 不走 proxy profile 时，每次请求 `tokens=0`，实际是上游返回 400。

**根因**：Codex 不设置 `max_output_tokens`，proxy 透传 `null` 给 llama.cpp。llama.cpp 拒绝 `max_tokens: null`，返回 `"type must be number, but is null"`。

**修复**：`proxy/responses.go` 在 `responsesToChat()` 中，如果 `MaxOutputTokens` 为 nil，默认设为 4096。`proxy/proxy.go` Gemini 翻译同理。

---

### 3. `/v1/models` 返回格式不兼容 Codex

**现象**：`codex -p proxy` 启动时报 `failed to decode models response: missing field 'slug'`。

**根因**：Codex 的 `ModelInfo` 结构要求 `slug` 字段（required）。llama.cpp 返回标准 OpenAI 格式 `{object: "list", data: [{id, object, owned_by}]}`，没有 `slug`。主 config 能工作是因为有缓存，profile 是全新 provider 无缓存。

**修复**：`proxy/proxy.go` 中 `handlePassthrough` 对 `/v1/models` 做拦截，将 llama.cpp 格式转换为 Codex 兼容格式：

```json
{
  "data": [...],        // OpenAI 标准（Cursor 用）
  "models": [{           // Codex 格式
    "slug": "qwen3.5-9b",
    "display_name": "qwen3.5-9b",
    "shell_type": "local",
    "visibility": "list",
    "supported_in_api": true,
    "truncation_policy": {"mode": "tokens", "limit": 131072},
    "supports_parallel_tool_calls": true,
    "web_search_tool_type": "text"
  }]
}
```

---

### 4. `modelIDToSlug` 生成规则

llama.cpp 的模型路径如 `/Users/dazsec/llms/Qwen3.5-9B-Q4_K_M.gguf` → 取文件名去 `.gguf` → 取前两段 → 转小写 = `qwen3.5-9b`。

---

### 5. Qwen 模型 thinking 模式

**现象**：4B 和 9B 模型默认开启 thinking mode，消耗全部 token 在推理上，不输出正文。`content` 为空，`reasoning_content` 有内容。

**修复**：启动 llama-server 时加 `--reasoning off`：

```bash
llama-server -m model.gguf --port 8080 --host 127.0.0.1 --reasoning off
```

日志确认：`init: chat template, thinking = 0`。

---

### 6. 旧 Session 历史污染

**现象**：Codex 多次 `(turn stopped)` 后重试，每次请求都带 28+ 条历史消息（含之前失败的工具调用），模型 0 输出。

**修复**：非 proxy bug。开新 session 即可解决。

---

## 二、各 Agent 接入配置

### Claude Code

```bash
export ANTHROPIC_BASE_URL=http://localhost:19530
export ANTHROPIC_AUTH_TOKEN=local
export ANTHROPIC_MODEL=gpt-4o
claude
```

注意：
- `BASE_URL` 不带 `/v1` 后缀，Claude Code 自动拼接
- `AUTH_TOKEN` 而非 `API_KEY`（第三方 provider）
- `ANTHROPIC_MODEL` 必须设，否则白名单拒绝

---

### Gemini CLI

```bash
# ~/.gemini/settings.json
{"security": {"auth": {"selectedType": "gemini-api-key"}}}

# 环境变量
export GOOGLE_GEMINI_BASE_URL=http://localhost:19530
export GEMINI_API_KEY=local
gemini -m "gpt-4o"
```

注意：
- settings.json 里 `selectedType` 优先级高于环境变量，必须改为 `"gemini-api-key"`
- 默认的 `"oauth-personal"` 会触发浏览器 OAuth 弹窗

---

### Codex CLI

`-c` 参数不可靠（TOML 值解析用 `_x_ = {raw}` hack，URL 格式会失败）。

`--oss` 不可用（强制验证 provider 存在）。

**推荐方式**：`-p` profile

```toml
# ~/.codex/proxy.config.toml
model = "gpt-4o"
model_provider = "custom"

[model_providers.custom]
name = "Local Proxy"
base_url = "http://127.0.0.1:19530/v1"
env_key_instructions = "local proxy - no key needed"
```

```bash
codex -p proxy                       # 交互模式
codex -p proxy exec "prompt"         # 非交互模式
```

注意：
- 主 `~/.codex/config.toml` 不碰
- `-m` 参数只设 model 名不切 provider

---

### Cursor

在 Settings → Models → OpenAI API 中设置：
- Base URL: `http://localhost:19530/v1`
- API Key: 任意值

直通模式，proxy 不翻译。

---

## 三、Session 恢复

各 Agent 的测试 session（hook 记录的讨论数据）清空方法：

- Gemini: 开新 session（`gemini --session-id new-uuid`）
- Codex: 开新 session（`codex -p proxy` 默认新 session）或 `codex delete <id>`
- Claude: 重启 `claude` 即新 session

---

## 四、日志位置

- proxy: `/tmp/aipmc-proxy.log`
- llama-server: stdout，`--reasoning off` 是否生效看 `init: chat template, thinking = 0`
- Gemini CLI hooks: `~/.gemini/tmp/aipmc/` 下 hook 输出
- Codex hooks: `~/.codex/hooks/` 下 hook 输出
