# 在 WSL2 中使用 AIPMC

> 适用拓扑：项目、`aipmc proxy`、`aipmc serve`、Agent 全在 WSL2；模型服务（llama.cpp 等）在 Windows 宿主机。

## 为什么是这个拓扑

`aipmc proxy` 不是无状态网关：它按当前目录找 `.pmai`，写入上下文注入日志、讨论、指标，并读取任务/commit 做注入。把 proxy 放到 Windows 宿主机，等于把数据面和 WSL 里的项目库劈开，AIPM 的核心闭环会失效。

所以只让**模型上游**这一跳跨边界：

```
WSL2: 项目(.pmai) + aipmc proxy + aipmc serve + Agent
                │
                └── 上游请求 → Windows 宿主机:8080
```

## 1. 安装 Linux 二进制

在仓库根目录构建或使用现成产物：

```bash
# 构建机（macOS/Linux）
bash build.sh -f -r        # 生成 dist/aipmc-linux-amd64

# WSL 内（在仓库根目录）
install -m 755 dist/aipmc-linux-amd64 ~/.local/bin/aipmc
hash -r
which -a aipmc
aipmc help                 # 应出现 setup / serve / proxy
```

如果 `which -a aipmc` 同时看到 Python 版（旧 PyPI `aipm-cli`）：

```bash
python3 -m pip uninstall -y aipm-cli   # 或 pipx uninstall aipm-cli
hash -r
```

## 2. 项目位置

把项目放在 WSL 原生文件系统（如 `~/projects/foo`），不要放 `/mnt/c/...`。SQLite 跨 DrvFs 的锁和性能都不可靠。

```bash
cd ~/projects/foo
aipmc init
aipmc doctor
```

`aipmc doctor` 会告警 `/mnt/` 路径，并显示 WSL/上游可达性。

## 3. 在 Windows 宿主机启动模型

llama.cpp 必须监听所有接口，不能只绑 `127.0.0.1`：

```powershell
llama-server.exe -m model.gguf --host 0.0.0.0 --port 8080
```

管理员 PowerShell 放行防火墙（WSL 的 vEthernet 入站默认常被拦）：

```powershell
New-NetFirewallRule -DisplayName "aipmc model upstream (WSL)" `
  -Direction Inbound -Protocol TCP -LocalPort 8080 `
  -Action Allow -Profile Any
```

只在本机使用时，也可以用 `-RemoteAddress 172.16.0.0/12` 限制来源网段。

其他模型服务：Ollama 设 `OLLAMA_HOST=0.0.0.0:11434`；LM Studio 开启 “Serve on Local Network”。端口对应改 `UPSTREAM_URL` 或 provider 的 `openai_url` 即可。

## 4. 在 WSL 内启动 AIPMC

```bash
cd ~/projects/foo
aipmc proxy        # 必须在项目目录运行（否则找不到 .pmai）
```

另开一个终端：

```bash
cd ~/projects/foo
aipmc serve        # Web UI: http://localhost:8720
aipmc agent claude # 或 codex / gemini
```

Windows 浏览器访问 `http://localhost:8720` 即可；WSL2 的 localhostForwarding 会转发到 WSL。

> WSL 的 `~/.aipmc` 与 Windows 的 `C:\Users\<you>\.aipmc` 是两份独立配置。模型注册表、凭据、agent profile 需要为 WSL 侧单独配置；不要跨两侧共用同一个 `.pmai` 库。

## 5. 上游地址

默认 `UPSTREAM_URL=http://localhost:8080/v1`。WSL 内本地端口未监听时，aipmc 会自动回退到宿主网关的同一端口，并打印：

```
[WSL] 127.0.0.1:8080 unreachable locally, falling back to host 172.x.x.1:8080
```

可用环境变量控制：

```bash
AIPMC_WSL_HOST=172.x.x.1 aipmc proxy   # 显式指定宿主地址
AIPMC_WSL_HOST=off aipmc proxy         # 关闭自动探测
AIPMC_WSL_HOST_FALLBACK=0 aipmc proxy  # 完全关闭回退
UPSTREAM_URL=http://172.x.x.1:8080/v1 aipmc proxy  # 直接指定，不走回退
```

`models.json` 里的 provider URL 同样适用回退。

## 6. mirrored 网络模式（可选）

Windows 11 22H2+ 的 WSL 2.0 支持 mirrored 模式，`localhost` 双向直通，不需要网关回退：

```ini
# C:\Users\<you>\.wslconfig
[wsl2]
networkingMode=mirrored
```

然后 `wsl --shutdown` 重启 WSL。aipmc 检测到 mirrored 会跳过网关回退。

## 7. 排错

| 现象 | 处理 |
|---|---|
| `aipmc: error: argument command: invalid choice` | PATH 里是旧 PyPI 版；`which -a aipmc`，卸载 `aipm-cli`，`hash -r` |
| `connection refused` 到宿主 | 模型是否 `--host 0.0.0.0`；防火墙是否放行；`aipmc doctor` 看 `host_reachable` |
| 企业代理环境仍不通 | 把宿主网关加入 `NO_PROXY`；或直接 `UPSTREAM_URL=http://<网关>:8080/v1` |
| Web UI 打不开 | Windows 用 `http://localhost:8720`，不要用 WSL 的 IP |
| Web UI 的 Launch Agent 没反应 | WSL 通常没有图形终端；在 WSL 终端用 `aipmc agent claude`，或页面上的 Copy Command |
| doctor 告警 `/mnt/` | 项目移到 `~/` 下 |
| 回退到了错误服务 | `AIPMC_WSL_HOST_FALLBACK=0` 关闭，或显式 `UPSTREAM_URL` |

## 8. 验收清单

```bash
cd ~/projects/foo
aipmc init
aipmc doctor          # wsl.host_addr / upstreams.effective = host
aipmc proxy           # 终端 A
aipmc serve           # 终端 B
aipmc agent claude    # 终端 C，发一轮真实对话
```

确认该会话出现在 `aipmc serve` 和 `.pmai/data/pmai.db` 中。数据落库是 AIPM 与普通网关的分界。
