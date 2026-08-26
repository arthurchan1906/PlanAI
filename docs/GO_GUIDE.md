# AIPMC Go 学习索引

> 面向 Python / C / C++ / JavaScript 开发者。本文件的完整学习内容已按主题拆分到
> **[`docs/go-handbook/`](go-handbook/README.md)** 学习手册文件夹，这里是入口索引 + 速查。

**当前代码基线**：Go 1.25，~47k 行、141 个非测试文件，只用标准库 `net/http`，无 Web 框架。

---

## 学习手册（推荐从这里开始）

点击章节标题进入详细内容，每章都标注了对应的项目真实代码文件：

| # | 章节 | 一句话内容 | 对应项目代码 |
|---|---|---|---|
| [01 · 语法基础](go-handbook/01-语法基础.md) | 编译模型、文件结构、变量、函数、控制流、字符串/字节 | `u/u.go`、`u/log.go` |
| [02 · 类型系统与接口](go-handbook/02-类型系统与接口.md) | struct、interface 隐式满足、指针、类型断言、泛型 | `ai/interface.go`、`ai/noop.go` |
| [03 · 并发编程](go-handbook/03-并发编程.md) | goroutine、channel、Mutex/RWMutex/sync.Map | `session/auto.go`、`proxy/context_inject.go` |
| [04 · 错误处理与资源管理](go-handbook/04-错误处理与资源管理.md) | error 返回、defer、recover、重试指数退避 | `session/run.go`、`mcp/mcp.go` |
| [05 · 标准库实战](go-handbook/05-标准库实战.md) | net/http 三层路由器、JSON、SQLite、context、go:embed | `web/server.go`、`api/server.go`、`db/db.go` |
| [06 · 工程最佳实践](go-handbook/06-工程最佳实践.md) | 包管理、接口驱动设计、依赖注入、embedding、大型代码库 | `ai/`、`eval/pq_*.go`、`dispatch.go` |
| [07 · 项目实战地图](go-handbook/07-项目实战地图.md) | 包全景表、三层 HTTP、分阶段阅读路线 | 全项目 |
| [08 · 常见坑与心智模型](go-handbook/08-常见坑与心智模型.md) | interface nil、slice 共享、值/指针接收者、defer 求值、for-range | 全项目 |
| [09 · 语言背景与选型](go-handbook/09-语言背景与选型.md) | Go 在系统语言谱系、核心优势、Go vs Swift | — |

---

## 最需要记住的 10 件事

1. **Go 是编译语言**：`go build` 出单文件二进制，交叉编译 `GOOS=linux go build`。AIPMC 选 Go 就是因为编译产物即部署产物。
2. **没有类，没有继承**：struct + 方法，组合（embedding）代替继承，interface 隐式满足（不用 `implements`）。
3. **错误是返回值，不是异常**：`result, err := f()`，`if err != nil`。没有 try/catch。
4. **没有 `public`/`private`**：大写开头 = 导出，小写 = 包内私有，编译器强制。
5. **goroutine 是真并行**：`go f()` 启动轻量线程（2KB 栈）。不是 GIL 线程，不是 asyncio 协程。
6. **defer = 函数级 RAII**：`defer db.Close()`、`defer mu.Unlock()`，panic 也执行，LIFO 顺序。
7. **指针和 C 一样但更安全**：无指针算术，GC 管内存，`&` 局部变量安全（逃逸分析）。
8. **slice 是视图不是拷贝**：`b := a[0:2]` 共享底层数组，改 b 会改 a。
9. **大 struct 用指针接收者**：`func (t *Task)` 改原件；值接收者操作副本。
10. **查标准库用 `go doc`**：`go doc fmt.Println`，不用上网。`go build` + `go vet` 两关过了再谈逻辑。

---

## 快速入口

- **项目结构地图**：见 [07 · 项目实战地图](go-handbook/07-项目实战地图.md) 的包全景表（`u` 是地基，`proxy` 是最大包，`mcp` 是 JSON-RPC 事件循环）。
- **三层 HTTP 路由器**：`web/server.go`（前端大门）→ `api/server.go`（API 手工路由）→ `proxy/proxy.go`（代理分发）。
- **数据层**：`db/` 管连接和迁移，`store/` 管查询——纯函数，无状态。
- **从零学 Go 的分阶段路线**：`u/` → `ai/` → `main.go` → `mcp/` → `session/` → `proxy/` → `eval/`，详见 [07 · 项目实战地图](go-handbook/07-项目实战地图.md) §6。

## 历史

原 `GO_GUIDE.md` 的单文件 1600+ 行已按主题拆分进 `docs/go-handbook/`，本文件改为索引。
