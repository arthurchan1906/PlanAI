# PlanAI — AI 项目管理器

PlanAI 是一个 AI 编程代理（Agent）的统一中间层。它提供协议代理、项目管理数据捕获和 Web 管理界面，让你可以无缝使用 **Claude Code**、**Codex CLI** 和 **Gemini CLI**，并统一管理所有 Agent 产生的任务、提交、决策和讨论记录。

## 架构

```
Agent (Claude/Codex/Gemini) → Proxy (:19530) → 上游 LLM (DeepSeek/OpenAI/Ollama)
                                    │
                              Web UI (:8720)
                              项目管理 / 流量查看 / Agent 启动
```

- **Proxy**: 协议翻译（Anthropic ↔ OpenAI ↔ Gemini）、流量捕获、Anthropic 透传
- **Web UI**: 任务看板、提交历史、Agent 启动器、配置管理
- **Hooks**: Agent 工具调用时自动捕获 PM 数据
- **PM DB**: 每项目独立 SQLite，零配置

## 快速开始

### 编译

```bash
# 最小编译（需要 Go 1.21+）
go build -o aipmc .

# 完整编译（推荐 — 前端 + CGO + SM4 加密）
./build.sh          # 编译当前平台
./build.sh -f       # 跳过前端构建
```

> **编译依赖**：C 编译器（gcc）。Windows 用户安装 [MinGW-w64](https://www.mingw-w64.org/) 并将 `bin/` 加入 `%PATH%`。  
> `build.sh` 会自动检测项目内置的 `gmssl/` 目录并启用 SM4-GCM 加密凭证功能；无 GmSSL 时自动降级为纯 Go 编译（不影响核心功能）。

### 启动

```bash
# 一条命令启动（proxy + web）
./aipmc serve

# 浏览器自动打开 http://127.0.0.1:8720
```

首次启动时，在任意目录运行 `aipmc serve`：
- 如果当前目录已注册，直接使用
- 如果已有其他注册项目，会显示选择器
- 按 Enter 注册当前目录为新项目
- 新项目自动初始化 `.pmai/`，无需手动 `aipmc init`

## 命令参考

### 服务

| 命令 | 说明 |
|------|------|
| `aipmc serve` | 启动 Web UI（自动选择项目，:8720） |
| `aipmc serve -p <路径>` | 指定项目启动 |
| `aipmc proxy` | 启动代理 (:19530)，需单独运行 |

### Agent

| 命令 | 说明 |
|------|------|
| `aipmc agent <claude\|codex\|gemini>` | 启动预配置的 Agent（自动读取项目级模型覆盖） |
| `aipmc setup <claude\|codex\|gemini\|cursor\|opencode>` | 为指定 Agent 配置 hooks + MCP |
| `aipmc init` | 在当前目录初始化 `.pmai/` |

### LLM 网关

| 命令 | 说明 |
|------|------|
| `aipmc models list` | 查看当前 Provider 和虚拟模型 |
| `aipmc models provider add --name X --openai_url URL` | 添加 LLM 后端 |
| `aipmc models provider rm --name X` | 删除后端 |
| `aipmc models add --id X --provider P` | 添加虚拟模型 |
| `aipmc models rm --id X` | 删除虚拟模型 |

### API Key 管理（需 GmSSL 编译）

| 命令 | 说明 |
|------|------|
| `aipmc key init` | 初始化加密存储，设置主密码 |
| `aipmc key set <provider> <key>` | 加密保存 API Key |
| `aipmc key list` | 列出所有 Key（脱敏显示） |
| `aipmc key passwd` | 修改主密码 |

## 配置

Web UI（Settings 页面）分 4 个 Tab 管理：

| Tab | 内容 |
|-----|------|
| **AI** | 对话/Embedding 端点、模型 |
| **Proxy** | 上游 URL、Anthropic 透传端点、端口、日志 |
| **LLM 网关** | Provider + 虚拟模型增删改、全局默认模型 |
| **API Keys** | 加密存储的 Provider Key（增删改） |

### 配置文件

| 文件 | 内容 |
|------|------|
| `~/.aipmc/config.json` | 全局：Proxy 端口、上游 URL、Agent Profile、默认模型 |
| `~/.aipmc/models.json` | 全局：Provider 注册表 + 虚拟模型定义 |
| `~/.aipmc/credentials` | 全局：**SM4-GCM 加密**的 API Key（`0600` 权限） |
| `.pmai/config.json` | 项目级：模型覆盖、agent_overrides |

### API Key 优先级

```
加密 credentials
```

> `aipmc proxy` 启动时输入一次主密码即可解锁加密存储，Key 仅存在于内存中。


## 项目结构

```
PlanAI/
├── main.go              # CLI 入口
├── build.sh             # 一键编译脚本
├── proxy/               # 代理核心（协议适配、翻译、流量捕获）
├── api/                 # Web API（配置、Agent 启动器）
├── web/                 # Web 服务器（静态文件、反向代理）
├── db/                  # 数据库、配置、加密存储（SM4-GCM）
├── app/                 # 应用运行时
├── hook/                # Agent Hook 集成
├── frontend/            # React 前端 (Ant Design)
├── gmssl/               # GmSSL C 库（头文件 + MinGW DLL）
│   ├── include/gmssl/   #   71 个头文件
│   ├── lib/             #   导入库
│   └── bin/             #   libgmssl.dll
└── vendor/              # Go 模块依赖副本（可复现构建）
```

## License

MIT
