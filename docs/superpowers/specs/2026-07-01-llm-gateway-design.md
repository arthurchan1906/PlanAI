# AIPM Proxy → LLM 中转网关 设计文档

**日期**: 2026-07-01 | **状态**: 已实施 (Phase 1-4) | **参与者**: Claude Code, Codex CLI

---

## 背景与动机

当前 AIPM proxy 只有一个全局 upstream URL，所有 agent 请求都往同一地址发。用户需要：
- 接入多个 LLM provider（DeepSeek、OpenAI、智谱、Groq 等）
- 项目 × Agent × LLM 自由组合
- Agent 看到统一的虚拟模型名，背后自动映射到不同后端+协议

## 架构：四层模型

```
Layer 0: 虚拟模型   (Agent 看到的统一模型名，如 "deepseek-v4-pro")
Layer 1: 模型映射   (虚拟名 → Provider + 协议级真实模型名)
Layer 2: Provider   (真实 LLM 提供商的 URL + API Key)
Layer 3: 协议引擎   (已有，不动：Anthropic/Responses/Gemini → OpenAI 翻译)
```

第五维度（不属于四层，属于 Launcher 职责）：

```
Agent Profile 运行时配置
  ├── 路由：model → Layer 0 查虚拟模型
  ├── 运行时：effort / subagent_model / extra_env... → 注入环境变量
  └── 项目覆盖：.pmai/config.json 的 agent_overrides
```

## 关键设计决策

### 1. models.json 独立文件
Provider 配一次很少改，模型频繁增删。独立文件支持单独热重载 + 预设分享。

### 2. 显式 per-protocol 模型名
```json
{"anthropic": "deepseek-v4-pro[1m]", "openai": "deepseek-v4-pro"}
```
不用 suffix 简化——不同 provider 的命名差异不限于后缀。

### 3. 复用已有 Profile structs
不新造 runtime 结构。已有 `ClaudeProfile.Model` / `SubAgentModel` / `EffortLevel` 等字段直接使用。

### 4. 翻译管线不动
只改 transport 层（`forwardToUpstream` 内的 URL + model name 替换），adapter 层完全不变。

### 5. 路由决策在 handler 入口
通过 `shouldPassthrough()` 在 handler 层决定透传还是翻译，保留 Anthropic 透传的零损耗优势。

### 6. 向后兼容
`models.json` 不存在时，所有请求走旧逻辑。所有旧字段（`upstream_url`, `anthropic_url`, `proxy_model`）全部保留。

## 配置模型

### models.json
```json
{
  "version": 1,
  "providers": [
    {
      "name": "deepseek",
      "openai_url": "https://api.deepseek.com",
      "anthropic_url": "https://api.deepseek.com/anthropic",
      "api_key_env": "DEEPSEEK_API_KEY"
    }
  ],
  "models": [
    {
      "id": "deepseek-v4-pro",
      "provider": "deepseek",
      "anthropic": "deepseek-v4-pro[1m]",
      "openai": "deepseek-v4-pro",
      "tags": ["reasoning"],
      "priority": 0
    }
  ]
}
```

### .pmai/config.json (扩展)
```json
{
  "model": "deepseek-v4-pro",
  "agent_overrides": {
    "claude": {
      "model": "deepseek-v4-flash",
      "effort_level": "max"
    }
  }
}
```

## 路由决策流程

```
Agent 发送 model="deepseek-v4-pro"（虚拟模型名）
  → handler() 识别 agent 协议
  → shouldPassthrough() 判断走透传还是翻译
  → adapter.ParseRequest() 保存 VirtualModel
  → unifiedToOpenAI() 翻译
  → forwardToUpstream() resolveVirtualRoute() 拿到真实 URL + 模型名
  → 发送到正确的 provider
```

## 继承优先级（模型名 fallback）

```
agent_overrides → agent profile → project.model → DefaultModel → ProxyModel
```

## 实施文件清单

| 文件 | 操作 | Phase |
|------|------|-------|
| `db/models.go` | 新建 Provider/VirtualModel/ModelRegistry 类型 | P1 |
| `db/db.go` | AgentRuntime/AgentOverride/ResolveAgentConfig + Config 扩展 | P1+P3 |
| `proxy/router.go` | 新建 ModelRouter: Resolve/ShouldPassthrough/ListModels | P2 |
| `proxy/proxy.go` | proxyCfg.router + handler + modelsList + modelsReload | P2 |
| `proxy/upstream.go` | resolveVirtualRoute helper + forwardToUpstream 签名 | P2 |
| `proxy/anthropic_passthrough.go` | 透传路由解析 | P2 |
| `proxy/unified.go` | UnifiedReq.VirtualModel 字段 | P2 |
| `proxy/adapt_*.go` | 适配器保存 VirtualModel | P2 |
| `api/config.go` | GET/POST providers/models 支持 | P4 |
| `api/agent.go` | ResolveAgentConfig 项目级合并 | P3 |
| `main.go` | models list CLI + runAgent 补全 | P3+P4 |
| `frontend/.../SettingsView.jsx` | default_model + Provider 管理区域 | P4 |
| `frontend/.../AgentConfigView.jsx` | 虚拟模型名 tooltip | P4 |

## 讨论过程

方案经历了多轮 Claude Code 与 Codex CLI 的交叉审查和迭代修正：

1. **Codex 初版** — Backend Registry + 三阶段方案
2. **Claude 补充** — 虚拟模型层 P0、显式协议映射、models.json 独立文件、visible_to、capabilities
3. **用户补充** — 虚拟 Provider 模式、Agent runtime 配置（effort/subagent_model）
4. **Codex Review Claude 方案** — 修正路由策略（handler 入口决策而非 forward 内分流）、修正 protocols 扁平化、指出 fallback 缺失
5. **Claude 回应 Codex Review** — 接受 4 个修正点，保留全局默认值必要性论述
6. **Codex 最终 Review** — 分歧缩小到 "model 必须有全局默认 vs effort 可选"
7. **共识达成** — 四层架构 + 虚拟模型路由 + 复用已有 structs + 向后兼容

## 验证结果

- `go build` ✅
- `go vet` ✅
- `aipmc models list` ✅
- `GET /v1/models` 返回虚拟模型列表 (OpenAI + Codex 双格式) ✅
- `POST /__proxy/models/reload` 热重载 ✅
- 回归测试（删除 models.json → 旧逻辑正常运行） ✅
