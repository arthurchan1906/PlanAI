# WSL 上游模型回退设计（v1）

> 日期：2026-11-12
> 状态：待用户批准
> 关联讨论：Codex 2026-11-12 15:24 复核意见
> 目标用户场景：项目、aipmc proxy、aipmc serve、Agent 全部运行在 WSL2；只有 OpenAI 兼容模型服务（llama.cpp 等）运行在 Windows 宿主机。

## 1. 问题

WSL2 默认使用 NAT 网络。WSL 内的 `localhost` / `127.0.0.1` 指向 WSL 自己，不指向 Windows 宿主机；宿主机只能通过 WSL 默认网关（如 `172.x.x.1`）访问。该网关地址会随 WSL/Windows 重启变化。

当前 aipmc 的上游地址全部来自配置：

- `~/.aipmc/config.json` 的 `upstream_url`（默认 `http://localhost:8080/v1`）
- `~/.aipmc/models.json` 中 provider 的 `openai_url` / `anthropic_url` / `responses_url`

这些地址在 WSL 内解析到 WSL 自己，导致 proxy 转发模型请求时 connection refused。静态改写配置会在 WSL 重启后过期。

## 2. 拓扑决策

**采纳：全栈在 WSL，仅模型上游跨边界。**

```
WSL2
┌────────────────────────────────────────────────────┐
│ 项目(.pmai/data/pmai.db)                            │
│   aipmc proxy ── 读写项目库：注入/讨论/指标          │
│   aipmc serve ── Web UI                             │
│   Agent ──────── ANTHROPIC_BASE_URL=127.0.0.1:19530 │
│        │                                            │
│        └── proxy 上游请求 ──┐                       │
└────────────────────────────┼────────────────────────┘
                             │ 仅此一跳跨边界
                    Windows 宿主机
                    llama-server 0.0.0.0:8080
```

**明确否决：proxy 跑在 Windows 宿主机。**
`aipmc proxy` 不是无状态网关：它按当前工作目录找 `.pmai`，写入 `inject_log`、discussion、`graph_edges`、metrics，并读取进行中任务/commit 做上下文注入（`proxy/context_inject.go:153`、`:313`、`proxy/emit_codex.go:230`、`db/db.go:48` FindPath）。项目在 WSL 时，Windows 侧 proxy 找不到该库，AIPM 的核心数据闭环失效。

## 3. 方案对比

| 方案 | 结论 | 理由 |
|---|---|---|
| proxy 跑宿主机 | 否决 | 断项目库数据链路；叠加 `/mnt/c` SQLite 风险 |
| 静态改写配置为宿主 IP | 否决 | WSL 重启后 IP 变化，Codex/OpenCode 静态文件过期 |
| WSL 内常驻端口转发器 | 否决 | 多一个守护进程、端口占用与 serve 启停语义复杂；无必要 |
| 每次请求前 URL 重写 | 否决 | 请求层重试会重放 POST body；流式请求危险；代理选择会再次介入 |
| mirrored 网络模式 | 文档建议，不作为唯一方案 | 依赖 Windows 11 22H2+，VPN/Docker 场景有兼容性问题 |
| **DialContext 回退** | **采纳** | 连接失败时 body 尚未发送；对本地优先；自动覆盖全部调用点；宿主 IP 每次拨号实时解析 |

## 4. 组件与接口

### 4.1 `u` 包新增（纯标准库，无内部依赖）

```go
// IsWSL2 报告当前进程是否运行在 WSL2。
func IsWSL2() bool

// KernelRelease 返回 /proc/sys/kernel/osrelease，失败返回 ""。
func KernelRelease() string

// WSLNetworkingMode 返回 "nat" / "mirrored" / ""（未知）。
func WSLNetworkingMode() string

// WSLHostAddr 返回 WSL 访问 Windows 宿主机的地址；不可用时返回 ""。
// 优先级：AIPMC_WSL_HOST_FALLBACK=0 > AIPMC_WSL_HOST 显式值 >
//         WSL2 检测 > mirrored 跳过 > /proc/net/route 默认网关。
func WSLHostAddr() string

// IsLoopbackHost 报告 host 是否为 loopback（localhost / 127.0.0.0/8 / ::1）。
func IsLoopbackHost(host string) bool

// NewModelHTTPClient 返回面向模型上游的 HTTP client。
// timeout=0 表示不设总超时（流式透传用）。
func NewModelHTTPClient(timeout time.Duration) *http.Client

// NewWSLFallbackTransport 克隆 base 并安装 loopback 回退 DialContext。
// base 为 nil 时克隆 http.DefaultTransport。
func NewWSLFallbackTransport(base *http.Transport) *http.Transport
```

### 4.2 模型调用接入点

替换为 `u.NewModelHTTPClient(0)` 的调用点：

- `proxy/upstream.go:153`、`:180`
- `proxy/proxy.go:703`、`:742`
- `proxy/responses_passthrough.go:84`、`:150`
- `proxy/anthropic_passthrough.go:92`
- `vision/vision.go:413`

`ai/http_client.go:35` 改为 `u.NewModelHTTPClient(600 * time.Second)`。

不替换 `api/query.go:246`（内部管理调用，不应回退到宿主 proxy）。

## 5. 行为细节

### 5.1 WSL 检测

满足任一条件即视为 WSL2：

- 环境变量 `WSL_DISTRO_NAME` 非空
- `/proc/sys/kernel/osrelease` 包含 `microsoft-standard`（大小写不敏感）
- 测试钩子 `AIPMC_FORCE_WSL=1`

WSL1（kernel 含 `Microsoft` 但不是 `microsoft-standard`）不启用回退：WSL1 共享 Windows 网络命名空间，localhost 本来就通。

### 5.2 宿主地址解析

顺序：

1. `AIPMC_WSL_HOST_FALLBACK=0` / `off` → 返回 `""`（彻底关闭回退）
2. `AIPMC_WSL_HOST` 非空：
   - `auto` → 继续自动探测
   - `off` / `none` / `0` → 返回 `""`
   - 其他值 → 原样返回（显式覆盖，任何平台生效，便于容器/测试）
3. 非 WSL2 → `""`
4. `WSLNetworkingMode() == "mirrored"` → `""`（localhost 已直通宿主机）
5. 解析 `/proc/net/route`：
   - 跳过表头（首列 `Iface`）
   - `Destination == "00000000"`
   - `Iface != "lo"`
   - `Flags & 0x0003 == 0x0003`（RTF_UP | RTF_GATEWAY）
   - Gateway 按小端十六进制转 IPv4（如 `0101A8C0` → `192.168.1.1`）
   - **优先 `eth*` 接口**；同接口内取 Metric 最小者；没有 `eth*` 默认路由时才使用其他接口
   - 理由：Tailscale/WireGuard/VPN 的默认路由 metric 常更低，但指向 VPN 网关，不是 Windows 宿主机
   - 结果为 `0.0.0.0` 时返回 `""`

### 5.3 回退拨号

`wslDialer.DialContext`：

1. `net.SplitHostPort(addr)` 失败 → 直接走原拨号
2. 目标 host 不是 loopback（`localhost`、`127.0.0.0/8`、`::1`）→ 直接走原拨号
3. `WSLHostAddr() == ""` → 直接走原拨号
4. 用 500ms 超时对原地址拨号；成功 → 返回连接
5. 失败但**不是** `ECONNREFUSED` / `EHOSTUNREACH` / `ENETUNREACH` / `EADDRNOTAVAIL` → 返回原错误（超时/取消不触发回退）
6. 否则用同一端口拨宿主地址；成功 → 返回连接，并每个源地址只打一条日志：
   `[WSL] 127.0.0.1:8080 unreachable locally, falling back to host 172.x.x.1:8080`
7. 宿主地址也失败 → 返回宿主拨号错误

回退在 `DialContext` 内完成，请求 body 不会重放；流式响应不受影响。

### 5.4 HTTP_PROXY 处理

`http.Transport.DialContext` 也用于拨 HTTP 代理：如果请求被代理选中，`DialContext` 收到的是**代理地址**（非 loopback），本方案不会触发回退。Go 对字面 `localhost` / loopback IP 有内置直连豁免，因此常见 loopback 场景仍直连。非字面 loopback 主机名（例如解析到 127.0.0.1 的自定义域名）需在 `NO_PROXY` 显式列出，否则 Go 可能选择代理，回退不会触发。

测试必须覆盖「传入非 loopback（含代理地址）时只拨一次、不回退」。

### 5.5 `aipmc doctor`

在现有 JSON 输出中新增（保持旧字段不变）：

```json
{
  "wsl": {
    "distro": "Ubuntu-24.04",
    "kernel": "5.15.153.1-microsoft-standard-WSL2",
    "networking_mode": "nat",
    "host_addr": "172.29.240.1",
    "fallback_enabled": true
  },
  "project_path": "/home/user/proj",
  "warnings": ["项目位于 /mnt/ 下，SQLite 跨 DrvFs 不可靠，建议移到 WSL 原生文件系统"],
  "proxy": { "url": "http://127.0.0.1:19530/__proxy/status", "reachable": true },
  "upstreams": [
    {
      "source": "global",
      "url": "http://localhost:8080/v1",
      "local_reachable": false,
      "host_reachable": true,
      "effective": "host"
    }
  ],
  "hints": [
    "宿主机启动模型时加 --host 0.0.0.0，并放行防火墙：New-NetFirewallRule -DisplayName \"aipmc model upstream (WSL)\" -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow -Profile Any"
  ]
}
```

采集范围：

- 全局 `upstream_url`（空时按默认 `http://localhost:8080/v1`）
- `models.json` 中所有 provider 的 `openai_url` / `anthropic_url` / `responses_url`
- 按 URL 去重；每个唯一 host:port 只探测一次
- 探测用 TCP dial，300–500ms 超时，不发 HTTP 请求
- **非 loopback 上游跳过 TCP 探测**：企业代理环境下直连云端点必失败，但 HTTP 经代理是通的；输出 `effective="direct"`、`probe="skipped"`，避免误报
- loopback 上游同时探测本地与宿主网关，`effective = local/host/unreachable`
- `project_path` 从 `dbPath` 经 `pmdb.ProjectRoot` 反推；`dbPath` 为空时才用 cwd
- WSL2 且非 mirrored 时，第一条 hint 提示可改用 mirrored 网络模式（零代码路径）
- Windows 防火墙 hint 只在 `u.IsWSL2()` 时输出；非 WSL 输出通用的「模型服务未启动/地址端口错误」提示

`warnings` 不改变 `ok`；`problems` 才改变 `ok`。

### 5.6 环境变量

| 变量 | 取值 | 作用 |
|---|---|---|
| `AIPMC_WSL_HOST` | `auto` / IP / `off` | 显式指定或关闭宿主地址 |
| `AIPMC_WSL_HOST_FALLBACK` | `0` / `off` | 完全关闭回退（优先级最高） |
| `AIPMC_FORCE_WSL` | `1` | 仅测试用，强制 WSL 判定 |
| `UPSTREAM_URL` | URL | 已有入口，显式指定时不依赖回退 |

## 6. 错误处理与安全

- 只对 loopback 目标回退，不对任意地址重试
- 只在连接被拒绝/不可达时回退，超时与取消不触发
- 每个源地址只打一条日志，避免每请求刷屏
- `AIPMC_WSL_HOST_FALLBACK=0` 可完全关闭
- 宿主端口被其他服务占用时：回退会成功建立连接但业务请求可能返回 4xx/5xx；日志会显示回退目标，便于定位。v1 不做 `/v1/models` 探活（模型端点不一定暴露该路径，误判成本高于收益）
- 回退不改变 TLS SNI：若上游是 HTTPS 且证书绑定原主机名，回退后握手可能失败。本地模型服务通常是 HTTP，文档注明
- 不改任何 Agent 地址、不改 proxy 监听地址、不改项目库路径

## 7. 测试策略

纯函数与注入测试：

- `/proc/net/route` 解析：正常、多默认路由、`eth*` 优先于低 metric 的 VPN 接口、同 `eth*` 取最低 metric、无默认、`lo`、gateway `0.0.0.0`、非法 hex
- loopback 判定：`localhost`、`127.0.0.1`、`127.1.2.3`、`::1`、`10.0.0.1`
- 回退错误分类：`ECONNREFUSED`、`EHOSTUNREACH`、`ENETUNREACH` 触发；`context.DeadlineExceeded`、`context.Canceled` 不触发
- `WSLHostAddr` 优先级：kill switch、显式覆盖、mirrored 跳过
- 拨号器：假拨号器断言调用顺序 `[127.0.0.1:port, gateway:port]`；非 loopback（含 HTTP 代理地址）只调用一次；超时不回退
- doctor：非 loopback 上游 `probe="skipped"`；`project_path` 从 dbPath 反推；非 WSL 不输出 PowerShell 防火墙命令
- 端到端：用自定义 `base.DialContext` 把回退拨号接到本地 httptest server；请求一个 127.0.0.1 上已关闭的端口，断言回退后拿到响应

集成验证：

- `go test ./...`
- `go vet ./...`
- `GOOS=linux GOARCH=amd64 go build`
- `bash build.sh -f -r` 刷新 `dist/`

## 8. 验收标准

在用户的 Windows + WSL2 环境：

0. 环境取证：`uname -a` 含 `microsoft-standard-WSL2`；`ip route show default` 有 `eth*` 默认路由；`ss -ltnp` 显示模型端口不在 WSL 内；19530 不由 Windows 侧占用
1. `aipmc doctor` 输出 `wsl`、`upstreams`、`host_addr`，且上游 `effective = host`
2. 项目在 WSL 原生文件系统；`aipmc init` 成功
3. `aipmc proxy` 在项目目录启动
4. `aipmc agent claude` 发起一轮真实对话成功
5. 该会话在 `aipmc serve` 和项目 `.pmai/data/pmai.db` 中可见
6. 关闭宿主机模型后，错误信息可定位；`AIPMC_WSL_HOST_FALLBACK=0` 时不尝试宿主地址
7. `/mnt/c` 下运行 `aipmc doctor` 只告警，不崩溃；`project_path` 是项目根而不是当前子目录
8. 非 WSL 环境运行 `aipmc doctor` 不输出 PowerShell 防火墙命令

## 9. 非目标（v1）

- `aipmc serve --host`
- WSL 下 Web UI 终端启动集成（`wt.exe`）
- `/v1/models` 探活
- 远程 proxy 管理（reload/stop/restart）
- 在 `/mnt/c` 上支持 SQLite
- proxy 跑宿主机或跨机 proxy

## 10. 风险与回滚

| 风险 | 缓解 |
|---|---|
| 回退误伤（宿主 8080 是别的服务） | 仅 loopback + refused；日志；`AIPMC_WSL_HOST_FALLBACK=0` |
| 企业代理拦截宿主地址 | 文档 `NO_PROXY`；测试覆盖字面 loopback |
| 防火墙未放行 | doctor 输出可粘贴的 `New-NetFirewallRule`；docs/WSL.md |
| 宿主 IP 变化 | 每次拨号实时解析 `/proc/net/route`，不缓存 |
| HTTPS 证书不匹配 | 文档注明本地模型用 HTTP；回退错误原样返回 |
| 回滚 | 撤销 `u` transport 接入即可；配置与数据结构零变更 |
