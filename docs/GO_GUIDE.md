# PlanAI Go 语法速查

> 面向 Python / C / JavaScript 开发者。每个概念用三种语言对照说明。

---

## 概念篇：Go 和其他语言的根本差异

### 1. 编译 vs 解释

```
Python:  source.py ──(解释器)──► 运行
JS:      source.js ──(V8/JIT)──► 运行
Go:      source.go ──(编译)──► 二进制文件 ──► 运行
C:       source.c  ──(编译)──► 二进制文件 ──► 运行
```

Go 和 C 一样是编译语言。但 Go 编译极快（几千行代码 < 1 秒），而且**交叉编译**一行命令：`GOOS=linux go build` 就能在 Windows 上编译出 Linux 二进制。

**PlanAI 为什么用 Go**：编译后是一个单文件（`aipmc` / `aipmc.exe`），不需要 Python 环境、不需要 `pip install`、不需要 Node.js。用户复制文件就能跑。

### 2. 没有运行时依赖

```go
//go:embed frontend/dist
var uiFS embed.FS        // React 前端打包进 Go 二进制

// Python: 你需要 manage.py collectstatic + nginx 配置
// JS:     你需要 serve 静态目录
```

Go 的 `embed` 指令在**编译时**把文件嵌进二进制。PlanAI 的整个 Web UI（React + Ant Design）就靠这一行打包进 `aipmc`。

### 3. 并发模型：goroutine vs 线程 vs async

```go
// Go — 启动一个 goroutine (轻量级线程, ~2KB 栈)
go computeEmbedding(id, content)

// Python — 线程或 asyncio
// threading.Thread(target=compute, args=(id, content)).start()
// asyncio.create_task(compute(id, content))

// JS — 异步 Promise
// Promise.resolve().then(() => compute(id, content))
```

**关键区别**：
- Python 的 `threading` 受 GIL 限制，CPU 密集任务不能真并行
- JS 的 `async/await` 是协程，没有真正的多线程
- Go 的 goroutine 是**真正的轻量线程**，可以跑成千上万个，Go runtime 自动调度到 OS 线程上

**但注意**：goroutine 不会阻止进程退出。主函数 `return` 时所有 goroutine 全部杀——这就是之前 `aipmc log` 里 `go computeEmbedding` 没执行完的原因。Web 服务是长时运行所以没问题；CLI 短命令中 goroutine 会丢失。

### 4. 没有类，没有继承

Go 没有 `class` 关键字。用 **struct + 方法** 代替：

```python
# Python
class Task:
    def __init__(self, title):
        self.title = title
    def display(self):
        return self.title

# Go
type Task struct { Title string }
func (t Task) Display() string { return t.Title }
```

没有继承，用**组合**（embedding）：

```go
type BugReport struct {
    Task         // 嵌入 Task, BugReport 自动获得 Task 的所有字段和方法
    Severity string
}
b := BugReport{Task: Task{Title: "崩溃"}, Severity: "critical"}
fmt.Println(b.Title)    // 直接访问嵌入 struct 的字段
```

### 5. Interface：隐式满足（最重要的概念）

Go 的 interface 不需要显式声明"我实现了这个接口"——只要有对应的方法就行：

```go
// 定义接口
type Embedder interface {
    Embed(texts []string) ([][]float64, error)
}

// 任何有 Embed 方法的类型自动实现 Embedder — 不需要 implements 关键字
type HTTPClient struct { ... }
func (c *HTTPClient) Embed(texts []string) ([][]float64, error) { ... }

// 使用 — HTTPClient 自动满足 Embedder
var e Embedder = &HTTPClient{endpoint: "http://..."}
```

**Python 对比**：Python 的 Protocol / ABC 需要显式继承或 `@runtime_checkable`。Go 的 interface 是**编译时检查的鸭子类型**——只要方法签名匹配就行，不需要标记。

### 6. 错误处理：返回错误而不是抛异常

Go 没有 `try/except`。所有可能失败的函数返回两个值：结果 + 错误。

```go
db, err := openDB()
if err != nil {
    return nil, err     // 向上传递
}
// 继续使用 db
```

**为什么**：Go 的设计哲学是"错误也是正常流程的一部分"。异常（Python 的 `raise`、JS 的 `throw`）会打断控制流，让代码跳跃。显式错误返回让每一步的失败处理都**可见**——没有隐藏的控制流跳转。

**代价**：代码里全是 `if err != nil`。习惯了就好。

### 7. 包管理：module 和 import path

```go
// go.mod 文件定义模块名
module aipmc

// 项目内 import — 基于 go.mod 的 module 名 + 目录路径
import "aipmc/ai"          // 指向 ai/ 目录
import "aipmc/cli"         // 指向 cli/ 目录

// 同一个 package 内的文件 — 所有符号自动共享, 不需要 import
// main.go 和 store.go 都是 package main, 可以互调函数
```

**和 Python 的区别**：Python 的 import 基于文件系统路径（`from .ai import client`）。Go 的 import 基于 module 命名空间——完全无视文件存放位置，只看 `go.mod` 里写的 module 名。

---

## 语法篇

### 1. 文件结构

```go
package main          // 必须声明包名。main = 可执行程序入口

import (              // Python: import; JS: require/import
    "fmt"             // 标准库: 格式化输出, 类似 Python print
    "strings"         // 字符串操作
    "aipmc/ai"        // 项目内模块, 路径是 go.mod 的 module 名 + 子目录
)

func main() {         // 程序入口 = C 的 main(), Python 的 if __name__ == "__main__"
    fmt.Println("hello")
}
```

### 对应关系

| Go | Python | JS |
|---|---|---|
| `package main` | 文件名就是模块 | `export` |
| `import "fmt"` | `import os` | `require("fs")` |
| `func main()` | `if __name__ == "__main__"` | 自动执行 |
| `//` 单行注释 | `#` | `//` |

---

### 2. 变量声明

```go
// 短声明（最常用）——等价于 Python 的 x = 1
x := 1                     // Go 自动推断类型 int
name := "hello"

// 显式声明
var count int              // C 风格: int count; 零值初始化 (0, "", nil)
var msg string = "world"

// 常量
const MaxSize = 100        // JS 的 const
```

### 零值

| 类型 | 零值 | Python 类比 |
|---|---|---|
| `int` | `0` | `int()` → 0 |
| `string` | `""` | `str()` → "" |
| `bool` | `false` | `bool()` → False |
| `*T` (指针) | `nil` | `None` |

---

### 3. 函数

```go
// Python: def add(a, b): return a + b
func add(a, b int) int {
    return a + b
}

// 多返回值（Python: return a, b）
func divide(a, b int) (int, error) {
    if b == 0 {
        return 0, fmt.Errorf("division by zero")  // 抛出错误
    }
    return a / b, nil  // nil = Python 的 None, 表示无错误
}

// 方法: 给 struct 绑定函数（类似 Python class 的 method）
func (s *Server) Start() error {  // (s *Server) = Python 的 self
    // s 是 Server 实例的指针
}
```

### 错误处理（最关键的区别）

```go
// Go: 显式检查每个错误
result, err := doSomething()
if err != nil {
    return nil, err   // 向上传递错误
}

// Python 等价: try/except
// try:
//     result = do_something()
// except Exception as e:
//     raise e
```

---

### 4. 类型

```go
// struct = C struct / Python dataclass
type Task struct {
    ID     string `json:"id"`       // 结构体标签 (类似 Python __annotations__)
    Title  string `json:"title"`
    Status string
}

// 创建实例——Python: Task(id="1", title="test")
t := Task{ID: "1", Title: "test"}

// 指针——Go 特有。&取地址, *解引用。类似 C 指针但无算术。
p := &t          // p 是指向 t 的指针
p.Title = "new"  // 自动解引用, 不用写 (*p).Title

// interface = Python 的 Protocol/ABC, JS 的 duck typing
type Embedder interface {
    Embed(texts []string) ([][]float64, error)
}
// 任何有 Embed 方法的类型自动实现 Embedder（隐式, 不需要 implements 关键字）

// map = Python dict / JS object
m := map[string]interface{}{"key": "value"}   // map[键类型]值类型{初始化}
v := m["key"]   // 取值, 不存在返回零值

// slice = Python list / JS array
ids := []string{"a", "b", "c"}   // 类型固定
ids = append(ids, "d")           // 追加元素（返回新 slice）
```

---

### 5. 控制流

```go
// if——和 C 一样。Go 没 while, 用 for 代替
if x > 0 {
    // ...
} else if x < 0 {
    // ...
}

// for——唯一的循环语句
for i := 0; i < 10; i++ { }     // C 风格
for _, item := range items { }   // Python: for item in items
for condition { }                // Python: while condition
for { }                          // 无限循环

// switch——不需要 break（Go 默认 break）
switch status {
case "done":
    // ...
default:
    // ...
}
```

### Python / JS 对照

| Go | Python | JS |
|---|---|---|
| `for _, x := range arr` | `for x in arr` | `for (x of arr)` |
| `for i := 0; i < n; i++` | `for i in range(n)` | `for (let i=0; i<n; i++)` |
| `switch x { case "a": }` | `match x: case "a":` (3.10+) | `switch(x) { case "a": }` |
| `if err != nil` | `if error:` | `if (error)` |

---

### 6. 指针

Go 的指针和 C 一样（存地址），但更安全——没有指针算术：

```go
x := 42
p := &x      // C: int *p = &x;    Python: 不能直接操作指针
*p = 100     // C: *p = 100;       修改 x 为 100

// struct 方法用指针接收者——避免复制整个 struct
func (t *Task) SetStatus(s string) {
    t.Status = s   // 修改原始 Task
}
// 等价 Python:
// def set_status(self, s):
//     self.status = s
```

---

### 7. 常见模式

```go
// defer——函数返回前执行（Python 的 with / finally）
db, err := openDB()
if err != nil { return err }
defer db.Close()   // 无论如何, return 前会调用 db.Close()

// goroutine——异步执行（Python: ThreadPoolExecutor / asyncio）
go computeEmbedding(id, content)  // 后台运行, 不等待

// 空接口 interface{} / any = Python 的 object / typing.Any
var data interface{} = "hello"
s, ok := data.(string)  // 类型断言（Python: isinstance(data, str)）
```

---

### 8. PlanAI 项目核心模式

#### 模式 1: 数据库操作

```go
func createTask(title string) (map[string]any, error) {
    db, err := openDB()           // 1. 打开连接
    if err != nil { return nil, err }
    defer db.Close()              // 2. 确保关闭

    id := slug("task")            // 3. 生成唯一 ID
    now := nowISO()               // 4. 当前时间戳

    _, err = db.Exec(             // 5. 执行 SQL
        "INSERT INTO tasks (id, title, created_at) VALUES (?, ?, ?)",
        id, title, now,
    )
    if err != nil { return nil, err }

    return getTask(id), nil       // 6. 读取并返回
}
```

#### 模式 2: 工具函数

```go
slug("task")    → "task-20260607-123456-a1b2c3"
nowISO()        → "2026-06-07T12:34:56"
str(nil)        → ""
jsonStr(v)      → JSON 序列化
```

#### 模式 3: MCP 工具

```go
s.addTool(MCPTool{
    Name:        "aipm_create_task",
    Description: "创建一个新 Task。",
    InputSchema: MCPInputSchema{...},
}, s.handleCreateTask)   // 绑定处理函数
```

#### 模式 4: Hook 处理

```go
func processClaudeHook() {
    data, _ := io.ReadAll(os.Stdin)     // 从 stdin 读取 JSON
    var raw struct { ... }
    json.Unmarshal(data, &raw)           // JSON → Go struct
    logDiscussion(raw.SessionID, "user", "claude-code", raw.Prompt)
}
```

#### 模式 5: 构建 pipeline

```bash
cd frontend && npx vite build        # 编译前端
go build -o dist/aipmc .             # 编译 Go（跨平台）
pkill aipmc || true                  # 停旧服务（Mac/Linux）
# taskkill //f //im aipmc.exe        # 停旧服务（Windows）
./dist/aipmc web &                   # 启动
```

---

### 9. 常用标准库

| 包 | 用途 | 对应 |
|---|---|---|
| `fmt` | 格式化输出, 字符串拼接 | Python `print`, `f-string` |
| `os` | 文件系统, 环境变量, 进程 | Python `os` |
| `io` | 读写流 | Python `io` |
| `net/http` | HTTP 服务器和客户端 | Python `requests` + `flask` |
| `encoding/json` | JSON 序列化 | Python `json` |
| `database/sql` | SQL 数据库抽象 | Python `sqlite3` |
| `strings` | 字符串操作 | Python `str` 方法 |
| `path/filepath` | 文件路径操作 | Python `os.path` |
| `time` | 时间处理 | Python `datetime` |
| `sort` | 排序 | Python `sorted` |
| `crypto/sha256` | SHA256 哈希 | Python `hashlib.sha256` |

---

## 10. PlanAI 项目高频语法（补充）

这些是项目中随处可见但基础教程不讲的东西。

### 同类型参数简写

```go
// 连续多个同类型参数可以合并写类型
func logDiscussion(sessionID, role, source, content, metadataJSON string) (map[string]any, error)
//                                                               ^^^^^^ 只写一次 string

// Python 等价:
// def log_discussion(session_id: str, role: str, source: str, content: str, metadata_json: str)
```

### map[string]any 类型断言 — 项目里到处都是

```go
// 从 JSON 解析出来的 map 里取值，必须做类型断言
cfg := map[string]any{}
hooks, ok := cfg["hooks"].(map[string]any)   // 断言 cfg["hooks"] 是 map[string]any
if !ok {
    hooks = map[string]any{}                  // 断言失败，给默认值
}

// 嵌套断言
if tools, ok := hooks["PostToolUse"].([]any); ok {
    for _, t := range tools {
        if m, ok := t.(map[string]any); ok {
            cmd := m["command"].(string)      // 单返回值断言，如果不是 string 会 panic
        }
    }
}

// str() 辅助函数 — PlanAI 项目自定义，安全取字符串
func str(v any) string {
    if s, ok := v.(string); ok { return s }
    return ""
}
```

**为什么这么麻烦？** 因为 Go 没有泛型 JSON 解析（Python 的 `json.loads` 直接返回 dict/list）。静态类型语言必须手动断言。项目里 `str()` 和 `pstr()` 就是为这个写的。

### `//go:embed` 的细节

```go
//go:embed frontend/dist    // 把整个 React 构建产物嵌入二进制
var uiFS embed.FS

// 限制：
// 1. 只能嵌入当前目录或子目录的文件 — 不能用 ../  不能用绝对路径
// 2. 嵌入的是编译时的文件内容，运行时修改文件不影响嵌入内容

// Python 对比: 需要打包工具 (PyInstaller) 或者运行时读取文件系统
// C 对比:    需要手动用 xxd 转成字节数组再链接
```

### `json` 标签的常见选项

```go
type ToolInput struct {
    Command   string `json:"command"`             // 字段名映射
    FilePath  string `json:"file_path"`           // Go 用驼峰, JSON 用下划线
    Content   string `json:"content,omitempty"`   // 空值时从 JSON 中省略
    OldString string `json:"old_string,omitempty"`
    Ignored   string `json:"-"`                   // 完全不序列化
}
```

### string ↔ []byte 互转

```go
// Go 的 string 是不可变的 (和 Python 一样); []byte 是可变的
data, _ := io.ReadAll(os.Stdin)   // 返回 []byte
text := string(data)              // []byte → string (拷贝)
bytes := []byte("hello")          // string → []byte (拷贝)

// 性能提示: 如果只是临时读取不修改，用 string(data) 没问题
// 高频场景可以用 unsafe，但 PlanAI 不需要
```

### 匿名 struct — JSON 解析的利器

```go
// 定义 struct + 初始化 + JSON 反序列化 一次搞定
var raw struct {
    ToolName  string `json:"tool_name"`
    ToolInput struct {
        FilePath  string `json:"file_path"`
        OldString string `json:"old_string"`
    } `json:"tool_input"`
}
json.Unmarshal(data, &raw)

// Python 等价: raw = json.loads(data); raw["tool_input"]["file_path"]
```

---

## 11. Go 真正难理解的 5 个概念

这些不是语法问题，是心智模型问题。每个新手都会踩。

### 🔴 坑 1：interface nil ≠ 底层值 nil

```go
var p *Task = nil           // p 是 nil 指针
var e Embedder = p          // e 不是 nil！
fmt.Println(e == nil)       // false！！！  (Go 新手最震撼的时刻)

// 原因: interface 内部是 (type指针, value指针) 对
// e 的内部: (type=*Task, value=nil) — type 不是 nil, 所以 interface 不是 nil

// 正确做法: 返回具体 nil，不要返回有类型的 nil
func getEmbedder() Embedder {
    return nil  // ✅ 直接返回 nil
}
func getEmbedder() Embedder {
    var p *HTTPClient = nil
    return p    // ❌ BUG: 返回的 interface 不是 nil
}
```

### 🔴 坑 2：slice 底层共享

```go
a := []int{1, 2, 3}
b := a[0:2]          // b 和 a 共享底层数组！
b[0] = 99            // a 也变了！ → a = [99, 2, 3]

// append 可能脱离共享，也可能不脱离
b = append(b, 4)     // 容量够 — 覆盖 a[2]，a = [99, 2, 4]
b = append(b, 5, 6)  // 容量不够 — b 搬新家，脱离共享

// Python 切片是拷贝，Go 切片是视图 — 这是 C 的心智模型
```

### 🔴 坑 3：值接收者 vs 指针接收者

```go
// 值接收者 — 操作的是副本
func (t Task) SetTitleBad(s string) { t.Title = s }     // 无效！外面不变

// 指针接收者 — 操作的是原件
func (t *Task) SetTitleGood(s string) { t.Title = s }   // 正确

// interface 满足规则不同:
type Updater interface { Update() }
func (t Task) Update()  {}   // Task 满足 Updater, *Task 也满足
func (t *Task) Update() {}   // 只有 *Task 满足 Updater, Task 不满足！

// 规则: 指针类型的方法集包含值接收者的方法，反过来不行
```

### 🔴 坑 4：defer 的参数求值时机

```go
x := 1
defer fmt.Println(x)  // 参数 x 在 defer 这一行就求值 → 最终打印 1
x = 2                 // 改变不了上面

// 对比: 闭包捕获的是变量本身
x = 1
defer func() { fmt.Println(x) }()  // 最终打印 2 (执行时才取值)
x = 2
```

### 🔴 坑 5：for-range 的变量复用

```go
// Go 1.21 及以前 — 经典 bug:
for _, t := range tasks {
    go func() { process(t) }()
    // BUG: 所有 goroutine 都用了最后一次循环的 t
    // 因为 t 变量在整个循环中复用同一个地址
}

// 修复 (Go 1.21):
for _, t := range tasks {
    t := t                      // 用 := 创建循环体内新变量
    go func() { process(t) }()  // OK
}

// Go 1.22+ 已自动修复 (PlanAI 项目用新版，但这个坑值得知道)
```

---

## 12. Go 是系统语言吗？

**是，但有明确边界。**

Go 在系统语言的谱系中处于 **C 和 Java/C# 之间**：

```
裸机系统 ← → 应用软件

C ----- Rust ----- Go ----- Java/C# ----- Python/JS
无GC    无GC     有GC     有GC/JIT    解释型
内核    浏览器   云基础   企业应用    脚本/原型
```

| 系统语言特征 | C | Rust | Go |
|---|---|---|---|
| 编译到原生机器码 | ✅ | ✅ | ✅ |
| 静态链接、零依赖分发 | ✅ | ✅ | ✅ |
| 无 GC（适合实时/内核） | ✅ | ✅ | ❌ |
| 直接内存布局控制 | ✅ | ✅ | ✅ (struct 内存布局确定) |
| 写 OS 内核 | ✅ | ✅ | ❌ (需要 runtime + GC) |
| 裸机嵌入式 | ✅ | ✅ | ❌ (TinyGo 可以) |
| C ABI(cgo) | ✅ 原生 | ✅ | ✅ 但有调用开销 |
| 自动内存管理 | ❌ | ❌ (编译时) | ✅ GC |
| 指针算术 | ✅ | ✅ (unsafe) | ❌ (unsafe 里有但不推荐) |

**Go 的定位是"云基础设施系统语言"**。Google 设计它来替代 C++ 写：

- 网络服务（HTTP/gRPC 服务端）
- 基础设施工具（Docker, Kubernetes, Terraform）
- CLI 工具
- 代理、负载均衡、API 网关

不是用来替代 C 写操作系统内核或固件的。

### Go 不"系统级"的地方

1. **有 GC**：STW（Stop The World）延迟不适合实时系统。不过 Go 的 GC 延迟已经做到了亚毫秒级，对 99% 的服务端场景够用。

2. **需要 runtime**：每个 Go 二进制都包含一个 ~2MB 的 runtime（调度器、GC、内存分配器）。不能像 C 那样直接在裸硬件上跑。

3. **没有 SIMD 内置支持**：Go 的编译器不做自动向量化。高性能数值计算不如 C++/Rust。（但可以通过汇编补充。）

### Go 的核心优势

**1. 编译产物即部署产物**

```bash
go build -o aipmc .              # 产出单一文件（Windows: aipmc.exe）
scp aipmc server:/usr/local/bin/ # 部署完成
```

不需要 Python 环境、不需要 `pip install`、不需要 `npm install`、不需要 Docker、不需要担心 glibc 版本、不需要担心 `.so` / `.dll` 缺失。这就是 PlanAI 选 Go 的核心原因——用户下载一个文件就能用。

**2. 编译速度**

Go 的编译器从头设计为快速编译。几千行代码 < 1 秒，几万行也只要几秒。对比：
- C++ 模板展开 + 头文件重解析 → 慢
- Rust borrow checker + 泛型单态化 → 慢

Go 的编译快到你可以把 `go build` 当 `python script.py` 用。

**3. goroutine — 不是 async/await**

```go
// Go: 阻塞的代码看起来就是阻塞的——不用涂颜色
go func() {
    data, _ := http.Get(url)     // 自动让出 CPU，不阻塞 OS 线程
    db.Exec("INSERT ...", data)  // 同上
}()
```

JavaScript 的 async/await 有"函数颜色"问题——async 函数只能被 async 函数调用。Python 的 asyncio 也有同样问题。Go 的运行时自动处理阻塞——所有代码写起来都是同步的，跑起来都是异步的。

**4. 隐式 interface — 不需要声明**

不需要 Java 的 `implements`，不需要 C++ 的虚基类，不需要 Rust 的 `impl Trait for Type`。只要方法签名匹配就行。这让写测试 mock 和依赖注入极其轻量——不需要 mock 框架，定义一个本地 interface 就够。

**5. 1.0 兼容承诺**

2012 年 Go 1.0 发布的代码，到今天不需要改一行就能编译。这在主流语言里独一无二。Python 2→3 分裂了十年，Rust 的 edition 机制需要手动迁移。Go 的兼容承诺意味着一劳永逸。

**6. 内置工具链**

不需要 ESLint（`go vet`），不需要 Prettier（`go fmt`），不需要 pytest（`go test`），不需要 pip/npm（`go mod`）。一切内置。团队协作零配置。

**7. defer — 资源释放的确定性**

```go
f, _ := os.Open("file")
defer f.Close()   // 函数返回前一定执行——无论正常返回还是 panic

// Python: with open("file") as f: (with 语句只在一层函数有效)
// C:     必须手动 fclose(f)，忘了就是内存泄漏
// Rust:  Drop trait (和 defer 类似但依赖所有权)
```

---

## 13. Go vs Swift — 理念对比

Go 和 Swift 长得有点像（都去掉了 C 的分号需要？不，Swift 不需要分号），而且诞生时间相近（Go 2009, Swift 2014），但它们的背景和目标差异很大。

### 语言设计对照

| 维度 | Go | Swift |
|---|---|---|
| **创造者** | Google (Rob Pike, Ken Thompson, Robert Griesemer) | Apple (Chris Lattner) |
| **目标领域** | 服务器基础设施、CLI 工具、云原生 | Apple 平台应用 (iOS/macOS)、未来向服务器延伸 |
| **编译** | 编译到原生机器码、静态链接 | 编译到原生机器码、依赖 Swift runtime |
| **内存管理** | GC (Tracing Garbage Collection) | ARC (Automatic Reference Counting, 编译时插入 retain/release) |
| **并发** | goroutine + channel (CSP 模型) | async/await + Actor (Swift 5.5+) |
| **错误处理** | `(result, error)` 多返回值 | `throw / try / catch` (和 Java 类似) |
| **类型系统** | 结构体 + 隐式 interface | struct + class + protocol + enum + extension |
| **继承** | 没有继承，只有组合（embedding） | class 支持单继承，struct/protocol 用组合 |
| **泛型** | 有（Go 1.18+），但受限 | 有，表达能力更强 |
| **语法复杂度** | 极简，25 个关键字 | 丰富，~100 个关键字/保留字 |
| **标准库规模** | 小而精 | 大而全（尤其 Apple 平台 SDK） |

### 相同点

**1. 都强调安全和可读性**

```swift
// Swift: 可选类型防止 nil 崩溃
var name: String? = nil
print(name?.count ?? 0)   // 安全解包

// Go: 零值 + 显式错误处理防止 nil 崩溃
var name string            // 零值是 ""，不是 nil
db, err := openDB()
if err != nil { ... }     // 显式处理
```

**2. 都重视值类型**

```swift
// Swift: struct 是值类型 (拷贝语义)
struct Task { var title: String }

// Go: struct 是值类型 (赋值 = 拷贝)
type Task struct { Title string }
t2 := t1   // 完整拷贝
```

**3. 都用 protocol/interface 做抽象**

```swift
// Swift: protocol (需要显式声明遵守)
protocol Identifiable {
    var id: String { get }
}
struct Task: Identifiable { ... }  // 显式写 : Identifiable

// Go: interface (隐式满足)
type Identifiable interface {
    ID() string
}
type Task struct { ... }
func (t Task) ID() string { ... }
// Task 自动实现 Identifiable — 不需要声明
```

### 关键差异

**1. 哲学：极简 vs 渐进披露**

```
Go:  "Less is more" — 砍掉一切可以砍的
     - 没有 class
     - 没有继承  
     - 没有异常
     - 没有三元运算符
     - 没有泛型 (直到 1.18)
     - 循环只有 for 一种
     - 25 个关键字

Swift: "Progressive disclosure" — 简单的事简单，复杂的事可能
     - struct / class / enum / protocol / extension / actor
     - 继承 + 协议 + 扩展
     - try/catch 异常
     - 模式匹配
     - 属性包装器
     - 结果构建器 (SwiftUI DSL)
     - ~100 个关键字/保留字
```

Go 选了"呆子路线"——让所有人都写一样的代码。Swift 选了"表达能力路线"——让高手写出更优雅的代码。这两种理念没有优劣，但决定了在团队里的使用感受：Go 代码几乎读起来都一样，Swift 代码风格差异可以非常大。

**2. 内存管理：GC vs ARC**

这是最大的底层差异：

```
Go GC (Tracing):        运行时扫描堆，标记存活对象，回收死对象
Swift ARC (Reference):  编译时在合适位置插入 retain/release 指令
```

| | Go GC | Swift ARC |
|---|---|---|
| **运行时开销** | 有 (STW 暂停，亚毫秒级) | 无暂停，但 retain/release 有 CPU 开销 |
| **循环引用** | 自动处理 | 需要 weak/unowned 手动打破 |
| **吞吐量** | 高 (批量回收) | 略低 (每次引用操作都有计数开销) |
| **可预测性** | STW 不可预测 | 释放时机精确（引用数归零立即可释放） |
| **编程负担** | 零 — 什么都不用管 | 需要思考对象所有权图 |

**实际影响**：写 Go 时你从不需要关心内存。写 Swift 时你需要理解 `weak` vs `strong` vs `unowned`，尤其闭包捕获列表。

**3. 错误处理：返回 vs 抛出**

```go
// Go: 错误是正常返回值
func divide(a, b int) (int, error) {
    if b == 0 { return 0, fmt.Errorf("div by zero") }
    return a / b, nil
}
result, err := divide(10, 0)
if err != nil { ... }

// Swift: 错误是异常路径
func divide(_ a: Int, by b: Int) throws -> Int {
    guard b != 0 else { throw MathError.divByZero }
    return a / b
}
do {
    let result = try divide(10, by: 0)
} catch {
    ...
}
```

Go 把错误当作正常数据流——每一步的错误都看得见。Swift 把错误当作控制流分支——用 `try/catch` 跳转。Go 的方式更冗长但更透明；Swift 的方式更简洁但会隐藏错误路径。

**4. 并发模型：CSP vs 结构化并发**

```go
// Go: goroutine + channel (CSP 模型)
ch := make(chan string)
go func() { ch <- fetchData() }()
result := <-ch                      // 阻塞等待

// Swift: async/await + Task (结构化并发)
func load() async throws -> String {
    async let data = fetch()        // 并发的子任务
    return try await data
}
```

Go 的 goroutine 是更底层的并发原语——你管理 goroutine 和 channel。Swift 的结构化并发强制了父子任务关系——子任务不能比父任务活得更长。Swift 更安全（不会泄漏 goroutine），Go 更灵活。

### 为什么 Go 赢了服务器端，Swift 没走出 Apple 生态？

1. **部署简单性**：Go 编译成单一静态二进制，Swift 依赖 runtime 库和 Apple 平台的 Foundation 框架。在 Linux 服务器上，Go 是原生公民，Swift 是客人。

2. **工具链**：`go build` 一行命令出二进制。Swift 的跨平台编译需要配置 Swift 工具链 + 平台 SDK，复杂度高一个数量级。

3. **标准库哲学**：Go 标准库自带生产级 HTTP 服务器。Swift 的 HTTP 支持长期依赖社区（Vapor 等框架），直到最近才有官方的 Swift HTTP Types。

4. **生态引力**：Docker + Kubernetes + Terraform + Prometheus 全部是 Go 写的。这意味着如果你做云基础设施，Go 是默认选项。Swift 没有这样的"杀手级生态项目"在服务器端。

5. **文化**：Go 团队来自 Unix/C 文化（Ken Thompson 是 Unix 和 C 的共同发明人），天然面向服务器。Swift 团队来自 Apple/编译器文化，天然面向应用开发。

### 一句话总结

| | Go | Swift |
|---|---|---|
| **一句话** | "把所有复杂东西砍掉，只留够用的" | "简单的东西很简单，复杂的东西也能写" |
| **最适合** | 服务端、CLI 工具、基础设施 | Apple 平台应用、系统框架 |
| **写代码感受** | 所有 Go 代码长得差不多——无聊但可预测 | 同一功能有 5 种写法——自由但有选择负担 |
| **学习曲线** | 短。25 关键字，一天上手 | 长。SwiftUI + protocol + actor + ... |

两者都在各自的领地做到了最好。Go 统治了云基础设施，Swift 统治了 Apple 平台。它们不直接竞争——交叉地带（服务器端 Swift vs 桌面端 Go）目前都是小众。


---

# 实战篇 — 通过 AIPM 代码学 Go 核心模式

> 以下每个主题都用项目真实代码讲解。文件路径点击可打开。

---

## 1. Interface 驱动设计 — `ai/interface.go` + `ai/http_client.go` + `ai/noop.go`

这是 Go 最重要也最常用的设计模式：定义接口 → 写真实实现 → 写空实现（用于降级/测试）。

### 接口定义 (`ai/interface.go`)

```go
type Summarizer interface {
    Summarize(text, instruction string) (string, error)
}
```

只有 4 行。不需要声明"谁实现了它"——Go 编译器自动检测。

### 真实实现 (`ai/http_client.go`)

```go
type Client struct {
    endpoint string
    model    string
    apiKey   string
}

// Client 自动实现 Summarizer — 因为有 Summarize 方法
func (c *Client) Summarize(text, instruction string) (string, error) {
    // ... HTTP 调用真实 LLM ...
}
```

注意：`Client` 的 struct 定义里**没有任何 `implements Summarizer` 标记**。Go 的 interface 满足是隐式的——编译器看到 `Client` 有 `Summarize(text, instruction string) (string, error)` 方法，就自动认为它实现了 `Summarizer`。

### 空实现 / 降级 (`ai/noop.go`)

```go
type noopSummarizer struct{}

func (noopSummarizer) Summarize(text, instruction string) (string, error) {
    return "", nil  // 不报错，返回空结果
}
```

### 消费方代码怎么用 (`dispatch.go`)

```go
var summarizer ai.Summarizer           // 声明为接口类型
if application.AI != nil && application.AI.Enabled() {
    summarizer = application.AI        // 有 AI → 用真实实现
}                                      // 否则 summarizer = nil
result, _ := session.Run(session.RunOpts{
    Summarizer: summarizer,            // 可以是 nil！
})
```

**关键**：`session.Run` 接收 `ai.Summarizer` 类型，但允许 nil 传入——内部有 nil check，AI 不可用时自动降级：

```go
// session/summary.go
func GenerateL2Summary(messages []map[string]any, review ReviewResult, summarizer ai.Summarizer) string {
    if summarizer == nil {
        return ""   // 优雅降级
    }
    // ...
}
```

**为什么这样设计**：不用到处写 `if config.AIEnabled { ... }`。调用方只需决定"有没有 summarizer"，实现方自己处理 nil。这比 Python 的 optional + None check 更清晰——接口本身就是 nil-able 的。

### 对比：Python 怎么做这件事

```python
# Python 的 Protocol（需要显式标记或 runtime check）
from typing import Protocol

class Summarizer(Protocol):
    def summarize(self, text: str, instruction: str) -> str: ...

# 或者更常见：直接传 Optional[Callable]
def generate_l2_summary(messages, summarizer=None):
    if summarizer is None:
        return ""   # 同样的 nil check
```

Go 版本有两个优势：
- 接口在编译时检查——拼错方法名或参数类型，编译器立即报错
- 不需要 `Optional[Callable]` 这种复杂类型标注——`ai.Summarizer` 就是 nil-able

---

## 2. Goroutine 后台任务 — `session/auto.go` + `main.go`

### 启动后台 goroutine (`main.go`)

```go
// serveCommand() 中，srv.Listen() 之前：
session.RunAuto(application.AI, 30*time.Minute)
```

### 实现 (`session/auto.go`)

```go
func RunAuto(summarizer ai.Summarizer, interval time.Duration) {
    go func() {                    // ← goroutine 在这里启动
        time.Sleep(5 * time.Second) // 等数据库就绪
        runOnce(summarizer)
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for range ticker.C {       // ← 每 30 分钟触发一次
            runOnce(summarizer)
        }
    }()
}
```

**逐行解释**：
1. `go func() { ... }()` — 启动一个 goroutine。这个匿名函数在**新 goroutine** 中执行，不会阻塞主函数
2. `time.Sleep(5 * time.Second)` — 暂停 5 秒等 DB 连接池初始化
3. `ticker := time.NewTicker(30 * time.Minute)` — 创建一个每 30 分钟触发一次的定时器
4. `defer ticker.Stop()` — 函数退出时停止 ticker，释放资源
5. `for range ticker.C { ... }` — 每次 ticker 触发时执行 `runOnce`

**注意**：`defer` 在 goroutine 退出时执行。如果主进程结束（`srv.Listen()` 返回），这个 goroutine 会被强制杀死，`defer` 不保证执行。Web 服务是长时运行的所以没关系；CLI 短命令中 goroutine 的 defer 可能不执行。

### 和 Python 的对比

```python
# Python asyncio
async def run_auto(summarizer, interval):
    await asyncio.sleep(5)
    while True:
        await run_once(summarizer)
        await asyncio.sleep(interval)

asyncio.create_task(run_auto(summarizer, 1800))
```

Go 的 goroutine 更像轻量级 OS 线程——可以被 Go runtime 调度到多个 CPU 核上并行执行。Python 的 asyncio 是协程——所有协程跑在同一个线程里。

---

## 3. sync 包 — Mutex / RWMutex / Map

### `sync.Mutex` — 互斥锁 (`u/log.go`)

```go
var logMu sync.Mutex

func LogShared(tag string, format string, args ...any) {
    logMu.Lock()
    defer logMu.Unlock()
    // ... 写文件 ...  ← 同一时刻只有一个 goroutine 能执行这段
}
```

**为什么用 Mutex**：日志函数被 pipeline goroutine 和 proxy handler 并发调用。不加锁会导致文件写入交错、数据损坏。

**`defer logMu.Unlock()`** 保证 unlock 一定会执行——即使写文件 panic 也能 unlock。这是 Go 的惯用写法：lock 后立即 defer unlock。

### `sync.RWMutex` — 读写锁 (`proxy/context_inject.go`)

```go
var sessionCache struct {
    mu        sync.RWMutex   // ← 注意：是 RWMutex，不是 Mutex
    goals     []string
    updatedAt time.Time
}

func getCachedSessionGoals() []string {
    sessionCache.mu.RLock()          // ← 读锁 — 多个 goroutine 可同时持有
    if cacheValid() {
        defer sessionCache.mu.RUnlock()
        return sessionCache.goals
    }
    sessionCache.mu.RUnlock()        // ← 释放读锁
    
    sessionCache.mu.Lock()           // ← 写锁 — 独占
    defer sessionCache.mu.Unlock()
    // ... 查数据库并更新缓存 ...
}
```

**为什么要区分读写锁**：`goals` 被**频繁读取**（每次 LLM 请求都读一次）但**很少写入**（60s TTL 才刷新一次）。RWMutex 允许多个 goroutine 同时读——10 个并发的 LLM 请求不会互相阻塞。

这是 Go 中比 Mutex 更细腻的并发控制：如果你的数据结构**读多写少**，用 RWMutex 替代 Mutex 能大幅减少锁竞争。

### `sync.Map` — 并发安全 map (`proxy/context_inject.go`)

```go
var injectTracker sync.Map  // 不需要 make，零值即可用

func shouldInject(key string) bool {
    last, ok := injectTracker.Load(key)
    if !ok || time.Since(last.(time.Time)) > 5*time.Minute {
        injectTracker.Store(key, time.Now())
        return true
    }
    return false
}
```

**为什么不用普通 `map` + RWMutex**：普通 map 不是并发安全的——两个 goroutine 同时读/写会 crash。通常做法是 `map + sync.RWMutex`，但 `sync.Map` 在**读写分离、key 稳定**的场景下更简洁。不过要注意 `sync.Map` 的值是 `any` 类型，取值时需要 .(type) 类型断言。

---

## 4. Struct Embedding — Go 的"继承"替代方案

Go 没有类继承。用 struct embedding 复用字段和方法。

### 例子 (`proxy/context_inject.go`)

```go
var sessionCache struct {      // ← 匿名 struct
    mu        sync.RWMutex     // ← embedding: sessionCache 自动获得 Lock/Unlock/Rlock/RUnlock
    goals     []string
    updatedAt time.Time
    ttl       time.Duration
}
```

**关键**：`sync.RWMutex` 被匿名嵌入。效果：
- `sessionCache.mu.Lock()` — 通过字段名访问（也可以省略 `.mu` 直接用 `sessionCache.Lock()`，但不推荐——会掩盖调用意图）

### 更典型的例子 (`session/knowledge.go`)

```go
type CrossSessionKnowledge struct {
    SessionsAnalyzed int            `json:"sessions_analyzed"`
    FilePatterns     []FilePattern  `json:"file_patterns"`
    // ...
    GeneratedAt      string         `json:"generated_at"`
}
```

struct tag (`json:"..."`) 控制 JSON 序列化的字段名。Python 没有对应的机制——通常是手动写 `to_dict()` / `from_dict()` 方法。

---

## 5. Error Handling — 返回而非抛出

### 标准模式

```go
result, err := someFunction()
if err != nil {
    return nil, fmt.Errorf("step X failed: %w", err)  // %w 包装错误，保留原错误链
}
// 继续用 result
```

### 项目中的实际例子 (`session/run.go`)

```go
messages, err := store.GetSessionMessages(s.SessionID)
if err != nil {
    continue  // 这个 session 有异常，跳过它继续下一个
}

review := ReviewSession(s.SessionID, s.Source, messages, len(mergedRows), nil)
```

**`continue` 而非 `return`**：这是一个批处理循环。单个 session 失败不应该阻止其他 session 的处理。Go 的错误处理让你可以在每一层精确决定"这个错误应该向上传递还是就地处理"。

### nil-able interface — Go 特有的陷阱

```go
var summarizer ai.Summarizer = getClient()
if summarizer != nil {
    summarizer.Summarize(...)  // 可能 panic!
}
```

如果 `getClient()` 返回 `(*Client)(nil)`（即一个 nil 指针但类型是 `*Client`），`summarizer != nil` 会返回 **true**——因为 interface 值包含 (type, value) 两个部分，即使 value 是 nil，type 不为 nil 时 interface 就不是 nil。

**正确做法**：返回 interface 的方法应显式返回 nil：

```go
func newSummarizer() ai.Summarizer {
    if !enabled {
        return nil  // ← 直接返回 nil，不是 nil 指针
    }
    return &Client{...}
}
```

---

## 6. HTTP Handler 模式 — `proxy/proxy.go`

### 基本结构

```go
func handler(w http.ResponseWriter, r *http.Request) {
    path := r.URL.Path
    agent := detectAgent(path)
    
    rw := &responseWrapper{ResponseWriter: w, status: 200}
    
    // 读请求体
    body, _ := io.ReadAll(r.Body)
    r.Body = io.NopCloser(bytes.NewReader(body))  // ← 恢复 r.Body（读完就没）
    
    // 路由分发
    switch {
    case path == "/v1/messages":
        handleClaudeUnified(rw, r)
    case path == "/v1/responses":
        handleCodexUnified(rw, r)
    }
    
    recordTraffic(agent, r.Method, path, rw.status, rw.size)
}
```

### 关键技术点

**`io.NopCloser` 恢复 `r.Body`**：`http.Request.Body` 是 `io.ReadCloser`，只能读一次。`io.ReadAll` 读完后必须用 `io.NopCloser(bytes.NewReader(body))` 重建，否则后续 handler 读不到数据。

**`responseWrapper` 捕获状态码**：
```go
type responseWrapper struct {
    http.ResponseWriter
    status int
    size   int
}
func (rw *responseWrapper) WriteHeader(code int) {
    rw.status = code
    rw.ResponseWriter.WriteHeader(code)
}
```
因为 `http.ResponseWriter` 的 status code 只能写一次且写入后不可读。Wrapper 拦截 `WriteHeader` 记录状态码供统计使用。这是 Go 中扩展接口行为的标准做法——wrapper 实现相同接口，拦截感兴趣的方法，其余透传给原始实现。

---

## 7. JSON 处理 — marshal / unmarshal / 类型断言

### `json.Marshal` — Go struct → JSON

```go
type ReviewResult struct {
    SessionID string `json:"session_id"`
    Intent    string `json:"intent"`
    Score     int    `json:"-"`
}

r := ReviewResult{SessionID: "abc123", Intent: "coding", Score: 70}
b, _ := json.Marshal(r)
// → {"session_id":"abc123","intent":"coding"}
//   Score 被忽略 — json:"-" 表示跳过
```

### `json.Unmarshal` — JSON → Go struct

```go
var l2 SessionL2Summary
if err := json.Unmarshal([]byte(ss.Summary), &l2); err != nil {
    continue  // JSON 格式异常，跳过
}
```

**`&l2`** 传指针——Unmarshal 需要修改结构体的内容，所以必须传地址。

### 处理动态 JSON — `map[string]any`

项目中对未知结构的 JSON 常用 `map[string]any`：

```go
var raw map[string]any
json.Unmarshal(body, &raw)
messages, ok := raw["messages"].([]any)  // 类型断言
if !ok {
    return body  // 格式不对，原样返回
}
```

**`raw["messages"].([]any)`** 是类型断言——把 `any` 类型断言为 `[]any`。如果类型不匹配，`ok` 为 false。这是 Go 处理动态 JSON 的关键技巧。

---

## 8. Defer — 资源清理的金标准

### 三个最常见用法

```go
// 1. 关闭文件
f, _ := os.Open("data.txt")
defer f.Close()

// 2. 释放锁
mu.Lock()
defer mu.Unlock()

// 3. 停止 ticker
ticker := time.NewTicker(30 * time.Minute)
defer ticker.Stop()
```

**执行顺序**：后进先出（LIFO）。如果注册了多个 defer，最后 defer 的最先执行。

**实际例子 (`session/auto.go`)**：

```go
go func() {
    time.Sleep(5 * time.Second)
    runOnce(summarizer)
    ticker := time.NewTicker(interval)
    defer ticker.Stop()          // goroutine 退出时停止 ticker
    for range ticker.C {
        runOnce(summarizer)
    }
}()
```

`defer` 在函数返回时执行，不是块结束时——注意 goroutine 内部也是一个函数作用域。

---

## 9. 包管理 — Go module 工作原理

### `go.mod` 文件

```
module aipmc          ← 模块名——所有内部 import 都以此开头

go 1.21               ← Go 版本

require (
    modernc.org/sqlite v1.29.5
    // ...
)
```

### 项目内引用

```go
import "aipmc/ai"      // 指向项目根目录下的 ai/ 子目录
import "aipmc/store"   // 指向 store/ 子目录
```

`aipmc` 是 module 名，`ai` 是相对于 module root 的目录路径。两者拼接就是 Go 编译器查找代码的路径。

### 可见性规则

```go
func PublicFunction() {}   // 大写开头 = 导出（其他包可用）
func privateFunction() {}  // 小写开头 = 包内私有

type ExportedType struct {}  // 大写 = 导出
var privateVar string         // 小写 = 私有
```

没有 `public` / `private` 关键字——靠首字母大小写区分。这是 Go 最简洁的设计之一：一眼看出可见性。

---

## 10. 字符串和字节 — string vs []byte

```go
s := "hello"           // string — 只读，UTF-8 编码
b := []byte(s)          // []byte — 可读写，用于 I/O、JSON 解析
s2 := string(b)         // []byte → string
```

### 项目中的转换链路

```go
// proxy/context_inject.go
body []byte                      // HTTP 请求体原始字节
↓
var raw map[string]any           // 需要先转为 map 才能操作
json.Unmarshal(body, &raw)       // []byte → map
↓
raw["instructions"] = ...        // 修改
↓
b, _ := json.Marshal(raw)        // map → []byte
return b                          // 返回修改后的字节
```

**为什么 `json.Unmarshal` 接受 `[]byte` 而非 `string`**：JSON 数据本质是字节流。接受 `[]byte` 避免了 `string(data)` 的额外内存分配。这是 Go 的零拷贝哲学——能用 []byte 的地方不用 string。

---

## 学习建议

1. **先读 `ai/interface.go` + `ai/http_client.go` + `ai/noop.go`** — 理解 Go 的 interface 驱动设计，这是 Go 的核心思维模式
2. **再读 `proxy/context_inject.go`** — 了解 sync.RWMutex、sync.Map、函数式 insert、JSON 动态处理
3. **再读 `session/auto.go` + `main.go:412`** — 理解 goroutine、ticker、defer
4. **最后读 `proxy/proxy.go`** — 理解 HTTP handler、中间件模式、response wrapper

修改代码时先跑 `go vet ./...`（静态分析）再跑 `go build`（编译），两关都过了才算 safe。
