# Go 学习手册（go-handbook）

> 结合 PlanAI 项目真实代码编写，面向 Python / C / C++ / JavaScript 开发者。
> 每个概念都用「你以前会的 → Go 对应」对照，文件路径可点击打开项目代码。
> 每章都包含：**语法/概念 → 原理由来（为什么这样设计）→ 思考方式（怎么想）→ 项目真实例子**。

**当前代码基线**：Go 1.25，~47k 行、141 个非测试文件，只用标准库 `net/http`，无 Web 框架。

---

## 怎么读

**第一次学 Go**：按顺序读 01 → 09。
**当参考书查**：跳着读，每章独立。
**看完一章就打开对应的项目代码**：实践出真知。

## 章节

| # | 章节 | 内容 | 对应项目代码 |
|---|---|---|---|
| [01](01-语法基础.md) | 语法基础 | 编译模型、文件结构、变量、函数、控制流、字符串/字节、热身模式 | `u/u.go`、`u/log.go` |
| [02](02-类型系统与接口.md) | 类型系统与接口 | struct、interface 隐式满足、指针、类型断言、json 标签、泛型 | `ai/interface.go`、`ai/noop.go` |
| [03](03-并发编程.md) | 并发编程 | goroutine、channel、Mutex / RWMutex / sync.Map、生产者→channel | `session/auto.go`、`proxy/context_inject.go`、`proxy/unified.go` |
| [04](04-错误处理与资源管理.md) | 错误处理与资源管理 | error 返回、%w 包装、defer、recover、重试指数退避 | `session/run.go`、`session/auto.go`、`mcp/mcp.go` |
| [05](05-标准库实战.md) | 标准库实战 | net/http 三层路由器、JSON、database/sql + SQLite、context、go:embed | `web/server.go`、`api/server.go`、`proxy/proxy.go`、`db/db.go` |
| [06](06-工程最佳实践.md) | 工程最佳实践 | 包管理、接口驱动设计、依赖注入、struct embedding、大型代码库组织 | `ai/`、`eval/pq_*.go`、`dispatch.go` |
| [07](07-项目实战地图.md) | 项目实战地图 | 包全景表、三层 HTTP、数据层分工、从零到大规模的分阶段阅读路线 | 全项目 |
| [08](08-常见坑与心智模型.md) | 常见坑与心智模型 | interface nil、slice 共享、值/指针接收者、defer 求值、for-range 复用 | 全项目 |
| [09](09-语言背景与选型.md) | 语言背景与选型 | Go 在系统语言谱系的位置、核心优势、Go vs Swift | — |

---

## 学习纪律

1. **永远 `go build ./...` + `go vet ./...`**——编译通过 = 类型对；vet 通过 = 常见错误被揪出来。
2. **读代码先找函数签名**（`func Name(...)`），再找它调的包——import 列表就是依赖图。
3. **改代码最小化**：小写函数 = 私有，改小写名编译器立刻告诉你谁还在引用它。
4. **查标准库**：`go doc fmt.Println`，不用上网。

> 修改过代码后用 `go test ./...` 跑测试；完整构建用 `./build.sh`。
