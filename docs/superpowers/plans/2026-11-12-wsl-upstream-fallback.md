# WSL 上游模型回退 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 WSL2 内的 aipmc proxy 在配置的上游模型地址是 loopback、但本地端口未监听时，自动回退到 Windows 宿主网关的同一端口；不修改任何 Agent 地址、配置文件或项目库路径。

**Architecture:** 在 `u` 包新增 WSL2 检测与宿主网关解析，并提供一个带 `DialContext` 回退的 HTTP transport。只把模型调用路径切换到该 transport（proxy 上游、vision、ai）。回退仅对 loopback 目标、仅在连接被拒绝/不可达时触发；超时和取消不触发。另加 `aipmc doctor` 诊断与 `docs/WSL.md`。

**Tech Stack:** Go 1.25.0（纯标准库，无新增依赖）；现有 `net/http`、`net`、`syscall`、`os/exec`；测试用 `httptest`、`net.Pipe`。

**Spec:** `docs/superpowers/specs/2026-11-12-wsl-upstream-fallback-design.md`

## Global Constraints

- Go 版本：`go.mod` 声明 `go 1.25.0`；不新增第三方依赖。
- 不改 `proxy` 监听地址（默认 `0.0.0.0:19530`）、不改 `serve` Web 监听地址、不改任何 Agent 面向的 base URL。
- 不把 proxy 移到 Windows 宿主机；不新增常驻转发进程。
- 回退只对 loopback 目标（`localhost`、`127.0.0.0/8`、`::1`）生效，只对 `ECONNREFUSED` / `EHOSTUNREACH` / `ENETUNREACH` 触发；超时/取消不触发。
- `AIPMC_WSL_HOST_FALLBACK=0` 必须完全关闭回退；`AIPMC_WSL_HOST` 显式值优先于自动探测。
- 非 WSL 环境下行为必须与改动前一致。
- 所有改动先写失败测试再实现（TDD）；每个任务结束跑 `gofmt`、`go vet`、相关包测试。
- 每任务一次本地 commit；**不 push**（push 需用户单独授权）。
- 不修改 `/mnt/c` 上的 SQLite 行为；只在 doctor 中告警。

---

### Task 0: 环境取证与文档落账（开工门禁）

**Files:**
- Commit: `docs/superpowers/specs/2026-11-12-wsl-upstream-fallback-design.md`
- Commit: `docs/superpowers/plans/2026-11-12-wsl-upstream-fallback.md`

**Interfaces:** 无代码接口。

- [ ] **Step 1: 在 WSL 内取证**

```bash
uname -a
cat /proc/sys/kernel/osrelease
ip route show default
ip -4 addr show
ss -ltnp | grep -E '8080|19530|8720' || true
which -a aipmc
aipmc help | head -20
pwd
```

Expected:
- kernel 含 `microsoft-standard-WSL2`
- 项目在 `/home/...`（不是 `/mnt/c/...`）
- `aipmc` 解析到 Go 版（help 中有 `setup`/`serve`/`proxy`）
- 19530/8720 没有由 Windows 侧占用；模型端口（如 8080）不在 WSL 内监听

- [ ] **Step 2: 在 Windows 宿主机取证**

```powershell
netstat -ano | findstr "8080 19530"
Get-NetFirewallRule -DisplayName "aipmc model upstream (WSL)" -ErrorAction SilentlyContinue
```

Expected: 模型服务监听在宿主机；19530 不应由宿主机上的 proxy 占用（本方案 proxy 在 WSL）。

- [ ] **Step 3: 门禁判断**

若实际拓扑与「全栈在 WSL，仅模型在宿主」不符（例如 proxy 实际跑在 Windows、或模型服务在 WSL），**停止执行**，回到设计阶段重新确认，不要按本计划硬套。

- [ ] **Step 4: 文档先落一笔 commit**

```bash
git add docs/superpowers/specs/2026-11-12-wsl-upstream-fallback-design.md docs/superpowers/plans/2026-11-12-wsl-upstream-fallback.md
git commit -m "docs: WSL 上游回退设计与施工计划"
```

---

### Task 1: WSL2 检测与宿主网关解析

**Files:**
- Create: `u/wsl.go`
- Create: `u/wsl_test.go`

**Interfaces:**
- Consumes: 无（纯标准库）
- Produces:
  - `u.IsWSL2() bool`
  - `u.KernelRelease() string`
  - `u.WSLNetworkingMode() string`
  - `u.WSLHostAddr() string`
  - `u.IsLoopbackHost(host string) bool`
  - 内部：`parseDefaultGateway(routes string) string`、`hexLEToIPv4(s string) string`

- [ ] **Step 1: Write the failing tests**

Create `u/wsl_test.go`:

```go
package u

import (
	"testing"
)

const sampleRoutes = `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
eth0	00000000	0101A8C0	0003	0	0	0	00000000	0	0	0
`

func TestParseDefaultGateway(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"basic", sampleRoutes, "192.168.1.1"},
		{"no header", "eth0\t00000000\t0101A8C0\t0003\t0\t0\t0\t00000000\n", "192.168.1.1"},
		{"lowest metric wins", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
eth0 00000000 0200A8C0 0003 0 0 100 00000000 0 0 0
eth0 00000000 0101A8C0 0003 0 0 10 00000000 0 0 0
`, "192.168.1.1"},
		{"prefer eth over vpn", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
tailscale0 00000000 0300A8C0 0003 0 0 0 00000000 0 0 0
eth0 00000000 0101A8C0 0003 0 0 100 00000000 0 0 0
`, "192.168.1.1"},
		{"lowest metric within eth", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
eth1 00000000 0200A8C0 0003 0 0 10 00000000 0 0 0
eth0 00000000 0101A8C0 0003 0 0 100 00000000 0 0 0
`, "192.168.0.2"},
		{"only vpn falls back to it", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
tailscale0 00000000 0300A8C0 0003 0 0 0 00000000 0 0 0
`, "192.168.0.3"},
		{"ignore lo", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
lo 00000000 0101A8C0 0003 0 0 0 00000000 0 0 0
`, ""},
		{"ignore non-default", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
eth0 0000A8C0 0101A8C0 0003 0 0 0 00FFFFFF 0 0 0
`, ""},
		{"ignore non-gateway flag", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
eth0 00000000 0101A8C0 0001 0 0 0 00000000 0 0 0
`, ""},
		{"zero gateway", `Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
eth0 00000000 00000000 0003 0 0 0 00000000 0 0 0
`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseDefaultGateway(tt.in); got != tt.want {
				t.Fatalf("parseDefaultGateway() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	cases := map[string]bool{
		"localhost": true, "LOCALHOST": true, "127.0.0.1": true,
		"127.1.2.3": true, "::1": true, "10.0.0.1": false, "example.com": false,
	}
	for host, want := range cases {
		if got := IsLoopbackHost(host); got != want {
			t.Errorf("IsLoopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestHostAddrFromMirrored(t *testing.T) {
	if got := hostAddrFrom("mirrored", []byte(sampleRoutes)); got != "" {
		t.Fatalf("mirrored should skip gateway, got %q", got)
	}
	if got := hostAddrFrom("nat", []byte(sampleRoutes)); got != "192.168.1.1" {
		t.Fatalf("nat gateway = %q", got)
	}
}

func TestWSLHostAddrOverrides(t *testing.T) {
	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "")
	t.Setenv("AIPMC_WSL_HOST", "172.20.0.1")
	if got := WSLHostAddr(); got != "172.20.0.1" {
		t.Fatalf("explicit host: got %q", got)
	}
	t.Setenv("AIPMC_WSL_HOST", "off")
	if got := WSLHostAddr(); got != "" {
		t.Fatalf("off: got %q", got)
	}
	t.Setenv("AIPMC_WSL_HOST", "172.20.0.1")
	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "0")
	if got := WSLHostAddr(); got != "" {
		t.Fatalf("kill switch: got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./u -run 'TestParseDefaultGateway|TestIsLoopbackHost|TestWSLHostAddrOverrides' -v`

Expected: build failure / undefined `parseDefaultGateway`, `IsLoopbackHost`, `WSLHostAddr`.

- [ ] **Step 3: Create `u/wsl.go`**

```go
package u

import (
	"bufio"
	"context"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const wslRouteFile = "/proc/net/route"

var (
	wslModeOnce sync.Once
	wslMode     string
)

// IsWSL2 reports whether the process runs inside a WSL2 distro. WSL1 is
// deliberately excluded: it shares the Windows network namespace, so
// localhost already reaches the host.
func IsWSL2() bool {
	if os.Getenv("AIPMC_FORCE_WSL") == "1" {
		return true
	}
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	return strings.Contains(strings.ToLower(KernelRelease()), "microsoft-standard")
}

// KernelRelease returns /proc/sys/kernel/osrelease, or "" when unavailable.
func KernelRelease() string {
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// WSLNetworkingMode returns "nat", "mirrored", or "" when unknown.
func WSLNetworkingMode() string {
	if !IsWSL2() {
		return ""
	}
	wslModeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		out, err := exec.CommandContext(ctx, "wslinfo", "--networking-mode").Output()
		if err != nil {
			return
		}
		m := strings.ToLower(strings.TrimSpace(string(out)))
		if m == "nat" || m == "mirrored" {
			wslMode = m
		}
	})
	return wslMode
}

// WSLHostAddr returns the address WSL should use to reach the Windows host.
// Priority: AIPMC_WSL_HOST_FALLBACK=0 > AIPMC_WSL_HOST explicit >
// WSL2 detection > mirrored skip > /proc/net/route default gateway.
func WSLHostAddr() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AIPMC_WSL_HOST_FALLBACK"))) {
	case "0", "off", "false", "no":
		return ""
	}
	if v := strings.TrimSpace(os.Getenv("AIPMC_WSL_HOST")); v != "" {
		switch strings.ToLower(v) {
		case "auto":
			// fall through to auto detection
		case "off", "none", "0":
			return ""
		default:
			return v
		}
	}
	if !IsWSL2() {
		return ""
	}
	b, err := os.ReadFile(wslRouteFile)
	if err != nil {
		return ""
	}
	return hostAddrFrom(WSLNetworkingMode(), b)
}

// hostAddrFrom applies the networking-mode rule before parsing routes.
func hostAddrFrom(mode string, routes []byte) string {
	if mode == "mirrored" {
		return ""
	}
	return parseDefaultGateway(string(routes))
}

// IsLoopbackHost reports whether host names a loopback endpoint.
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// parseDefaultGateway extracts the IPv4 default gateway from /proc/net/route.
// It prefers eth* interfaces because VPN interfaces (tailscale0, tun0, ...)
// often carry a lower metric but route to the VPN gateway, not the Windows
// host. Among the preferred interfaces, the lowest metric wins.
func parseDefaultGateway(routes string) string {
	type candidate struct {
		iface  string
		ip     string
		metric int
	}
	var candidates []candidate
	sc := bufio.NewScanner(strings.NewReader(routes))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 8 || fields[0] == "Iface" {
			continue
		}
		iface, dest, gate, flags := fields[0], fields[1], fields[2], fields[3]
		if iface == "lo" || dest != "00000000" {
			continue
		}
		fl, err := strconv.ParseUint(flags, 16, 32)
		if err != nil || fl&0x0003 != 0x0003 {
			continue
		}
		metric := 0
		if m, err := strconv.Atoi(fields[6]); err == nil {
			metric = m
		}
		ip := hexLEToIPv4(gate)
		if ip == "" {
			continue
		}
		candidates = append(candidates, candidate{iface: iface, ip: ip, metric: metric})
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		ei := strings.HasPrefix(candidates[i].iface, "eth")
		ej := strings.HasPrefix(candidates[j].iface, "eth")
		if ei != ej {
			return ei
		}
		return candidates[i].metric < candidates[j].metric
	})
	return candidates[0].ip
}

// hexLEToIPv4 converts the little-endian hex gateway field from
// /proc/net/route (e.g. 0101A8C0) to dotted IPv4 (192.168.1.1).
func hexLEToIPv4(s string) string {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 16, 32)
	if err != nil {
		return ""
	}
	ip := net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	if ip.Equal(net.IPv4zero) {
		return ""
	}
	return ip.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./u -run 'TestParseDefaultGateway|TestIsLoopbackHost|TestWSLHostAddrOverrides' -v`

Expected: PASS.

- [ ] **Step 5: Format, vet, run tests (no commit yet)**

```bash
gofmt -w u/wsl.go u/wsl_test.go
go vet ./u
go test ./u
```

Task 2 commits both files together, since detection and transport are one mechanism.

---

### Task 2: WSL 回退 HTTP transport

**Files:**
- Create: `u/wsl_transport.go`
- Create: `u/wsl_transport_test.go`

**Interfaces:**
- Consumes: `u.WSLHostAddr()`, `u.IsLoopbackHost()` from Task 1
- Produces:
  - `u.NewModelHTTPClient(timeout time.Duration) *http.Client`
  - `u.NewWSLFallbackTransport(base *http.Transport) *http.Transport`
  - 内部：`wslDialer`、`isConnectRefused(err error) bool`

- [ ] **Step 1: Write the failing tests**

Create `u/wsl_transport_test.go`:

```go
package u

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestIsConnectRefused(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	if !isConnectRefused(refused) {
		t.Fatal("ECONNREFUSED should trigger fallback")
	}
	notAssigned := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.EADDRNOTAVAIL)}
	if !isConnectRefused(notAssigned) {
		t.Fatal("EADDRNOTAVAIL should trigger fallback")
	}
	if isConnectRefused(context.DeadlineExceeded) {
		t.Fatal("deadline must not trigger fallback")
	}
	if isConnectRefused(errors.New("boom")) {
		t.Fatal("unknown error must not trigger fallback")
	}
}

func TestWSLDialerFallsBackOnRefused(t *testing.T) {
	var got []string
	d := &wslDialer{
		host: func() string { return "172.20.0.1" },
		dialFirst: func(ctx context.Context, network, addr string) (net.Conn, error) {
			got = append(got, "first:"+addr)
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
		},
		dialFallback: func(ctx context.Context, network, addr string) (net.Conn, error) {
			got = append(got, "fallback:"+addr)
			c1, c2 := net.Pipe()
			go c2.Close()
			return c1, nil
		},
		logf: func(string, ...any) {},
	}
	conn, err := d.DialContext(context.Background(), "tcp", "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	want := []string{"first:127.0.0.1:8080", "fallback:172.20.0.1:8080"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("dial order = %v, want %v", got, want)
	}
}

func TestWSLDialerDoesNotFallbackOnTimeout(t *testing.T) {
	var got []string
	wantErr := context.DeadlineExceeded
	d := &wslDialer{
		host: func() string { return "172.20.0.1" },
		dialFirst: func(ctx context.Context, network, addr string) (net.Conn, error) {
			got = append(got, "first:"+addr)
			return nil, wantErr
		},
		dialFallback: func(ctx context.Context, network, addr string) (net.Conn, error) {
			got = append(got, "fallback:"+addr)
			return nil, nil
		},
		logf: func(string, ...any) {},
	}
	if _, err := d.DialContext(context.Background(), "tcp", "127.0.0.1:8080"); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if len(got) != 1 {
		t.Fatalf("unexpected fallback: %v", got)
	}
}

func TestWSLDialerSkipsNonLoopback(t *testing.T) {
	var got []string
	d := &wslDialer{
		host: func() string { return "172.20.0.1" },
		dialFallback: func(ctx context.Context, network, addr string) (net.Conn, error) {
			got = append(got, addr)
			c1, c2 := net.Pipe()
			go c2.Close()
			return c1, nil
		},
		logf: func(string, ...any) {},
	}
	conn, err := d.DialContext(context.Background(), "tcp", "10.0.0.5:8080")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if len(got) != 1 || got[0] != "10.0.0.5:8080" {
		t.Fatalf("got %v", got)
	}
}

func TestModelHTTPClientFallsBackEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// Pick a port that is closed on 127.0.0.1 so the first dial is refused.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(probe.Addr().String())
	probe.Close()

	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "")
	t.Setenv("AIPMC_WSL_HOST", "192.0.2.1")

	// The fallback dialer ignores the gateway address and connects to the
	// test server, proving the transport switched addresses after refusal.
	base := &http.Transport{Proxy: http.ProxyFromEnvironment}
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, srv.Listener.Addr().String())
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: NewWSLFallbackTransport(base)}
	resp, err := client.Get("http://127.0.0.1:" + port + "/")
	if err != nil {
		t.Fatalf("fallback request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./u -run 'TestIsConnectRefused|TestWSLDialer|TestModelHTTPClient' -v`

Expected: build failure / undefined `wslDialer`, `isConnectRefused`, `NewModelHTTPClient`.

- [ ] **Step 3: Create `u/wsl_transport.go`**

```go
package u

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"
)

// NewModelHTTPClient returns an HTTP client for model upstream calls.
// timeout=0 disables the client-wide timeout (needed for streaming).
func NewModelHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: NewWSLFallbackTransport(nil),
	}
}

// NewWSLFallbackTransport clones base and installs a DialContext that retries
// loopback targets on the WSL host gateway when the local port is refused.
func NewWSLFallbackTransport(base *http.Transport) *http.Transport {
	if base == nil {
		if t, ok := http.DefaultTransport.(*http.Transport); ok {
			base = t.Clone()
		} else {
			base = &http.Transport{Proxy: http.ProxyFromEnvironment}
		}
	} else {
		base = base.Clone()
	}
	fallbackDial := base.DialContext
	if fallbackDial == nil {
		d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		fallbackDial = d.DialContext
	}
	firstDial := (&net.Dialer{Timeout: 500 * time.Millisecond}).DialContext
	d := &wslDialer{
		host:         WSLHostAddr,
		dialFirst:    firstDial,
		dialFallback: fallbackDial,
		logf:         log.Printf,
	}
	base.DialContext = d.DialContext
	return base
}

type wslDialer struct {
	host         func() string
	dialFirst    func(ctx context.Context, network, addr string) (net.Conn, error)
	dialFallback func(ctx context.Context, network, addr string) (net.Conn, error)
	logf         func(format string, args ...any)

	mu     sync.Mutex
	logged map[string]bool
}

func (d *wslDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	fallback := d.dialFallback
	if fallback == nil {
		var dialer net.Dialer
		fallback = dialer.DialContext
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || !IsLoopbackHost(host) {
		return fallback(ctx, network, addr)
	}
	if d.host == nil {
		return fallback(ctx, network, addr)
	}
	gw := d.host()
	if gw == "" {
		return fallback(ctx, network, addr)
	}
	first := d.dialFirst
	if first == nil {
		first = fallback
	}
	conn, firstErr := first(ctx, network, addr)
	if firstErr == nil {
		return conn, nil
	}
	if !isConnectRefused(firstErr) {
		return nil, firstErr
	}
	target := net.JoinHostPort(gw, port)
	d.logOnce(addr, target)
	return fallback(ctx, network, target)
}

func (d *wslDialer) logOnce(from, to string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.logged == nil {
		d.logged = make(map[string]bool)
	}
	if d.logged[from] {
		return
	}
	d.logged[from] = true
	if d.logf != nil {
		d.logf("[WSL] %s unreachable locally, falling back to host %s", from, to)
	}
}

// isConnectRefused reports whether err is a dial failure where retrying a
// different address is safe. Timeouts and cancellations are deliberately
// excluded: the local service may simply be slow.
func isConnectRefused(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EADDRNOTAVAIL) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return isConnectRefused(opErr.Err)
	}
	var sysErr *os.SyscallError
	if errors.As(err, &sysErr) {
		return isConnectRefused(sysErr.Err)
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./u -run 'TestIsConnectRefused|TestWSLDialer|TestModelHTTPClient' -v`

Expected: PASS.

- [ ] **Step 5: Run the full `u` package tests, format, vet, commit both Task 1+2 files**

```bash
gofmt -w u/wsl.go u/wsl_test.go u/wsl_transport.go u/wsl_transport_test.go
go vet ./u
go test ./u
git add u/wsl.go u/wsl_test.go u/wsl_transport.go u/wsl_transport_test.go
git commit -m "feat(u): WSL2 宿主网关探测 + 模型上游 loopback 回退 transport"
```

---

### Task 3: 将模型调用接入回退 client

**Files:**
- Modify: `proxy/proxy.go`（新增 package var；替换 2 处 `http.DefaultClient`）
- Modify: `proxy/upstream.go`（替换 2 处）
- Modify: `proxy/responses_passthrough.go`（替换 2 处）
- Modify: `proxy/anthropic_passthrough.go`（替换 1 处）
- Modify: `vision/vision.go`（新增 import + package var；替换 1 处）
- Modify: `ai/http_client.go`（新增 import；替换 client 构造）
- Create: `proxy/upstream_wsl_test.go`

**Interfaces:**
- Consumes: `u.NewModelHTTPClient(timeout time.Duration) *http.Client`
- Produces: package-level `upstreamHTTPClient` in `proxy`; `visionHTTPClient` in `vision`

- [ ] **Step 1: Write the failing proxy integration test**

Create `proxy/upstream_wsl_test.go`:

```go
package proxy

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aipmc/u"
)

func TestForwardToUpstreamUsesWSLFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// Pick a closed port on 127.0.0.1; the first dial must be refused.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, closedPort, _ := net.SplitHostPort(probe.Addr().String())
	probe.Close()

	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "")
	t.Setenv("AIPMC_WSL_HOST", "192.0.2.1")

	oldClient := upstreamHTTPClient
	oldCfg := loadCfg()
	defer func() {
		upstreamHTTPClient = oldClient
		storeCfg(oldCfg)
	}()

	base := &http.Transport{Proxy: http.ProxyFromEnvironment}
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, srv.Listener.Addr().String())
	}
	upstreamHTTPClient = &http.Client{Transport: u.NewWSLFallbackTransport(base)}
	storeCfg(&proxyCfg{upstreamURL: "http://127.0.0.1:" + closedPort, startTime: time.Now()})

	body, err := forwardToUpstream(
		"chat/completions",
		map[string]any{"model": "test"},
		"", "", "test",
		httptest.NewRequest("POST", "http://example.invalid/", nil),
	)
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body = %s", body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./proxy -run TestForwardToUpstreamUsesWSLFallback -v`

Expected: build failure — undefined `upstreamHTTPClient` (the test is written before the production var exists).

- [ ] **Step 3: Add the client var, then run the test to see the real failure**

In `proxy/proxy.go`, near the top-level `var proxyToken = newProxyToken()` block, add:

```go
// upstreamHTTPClient is used for all model-upstream requests. Its transport
// falls back from loopback to the WSL host gateway when the local port is
// refused (see u.NewWSLFallbackTransport).
var upstreamHTTPClient = u.NewModelHTTPClient(0)
```

Run: `go test ./proxy -run TestForwardToUpstreamUsesWSLFallback -v`

Expected: FAIL — `forwardToUpstream` still uses `http.DefaultClient`, so the request to the closed `127.0.0.1` port returns connection refused and no fallback happens.

- [ ] **Step 4: Replace the call sites**

Replace exactly these seven occurrences (line numbers are pre-change; use the exact strings):

- `proxy/upstream.go:153`: `resp, err := http.DefaultClient.Do(req)` → `resp, err := upstreamHTTPClient.Do(req)`
- `proxy/upstream.go:180`: `resp, err := http.DefaultClient.Do(req)` → `resp, err := upstreamHTTPClient.Do(req)`
- `proxy/proxy.go:703`: `resp, err := http.DefaultClient.Do(proxyReq)` → `resp, err := upstreamHTTPClient.Do(proxyReq)`
- `proxy/proxy.go:742`: `resp, err := http.DefaultClient.Do(proxyReq)` → `resp, err := upstreamHTTPClient.Do(proxyReq)`
- `proxy/responses_passthrough.go:84`: `resp, err := http.DefaultClient.Do(proxyReq)` → `resp, err := upstreamHTTPClient.Do(proxyReq)`
- `proxy/responses_passthrough.go:150`: `resp, err := http.DefaultClient.Do(proxyReq)` → `resp, err := upstreamHTTPClient.Do(proxyReq)`
- `proxy/anthropic_passthrough.go:92`: `resp, err := http.DefaultClient.Do(proxyReq)` → `resp, err := upstreamHTTPClient.Do(proxyReq)`

Do NOT replace `api/query.go:246` (internal proxy management call).

- [ ] **Step 5: Wire vision and ai**

In `vision/vision.go`, add `"aipmc/u"` to the import block, then add near the package-level vars:

```go
var visionHTTPClient = u.NewModelHTTPClient(0)
```

Replace `vision/vision.go:413`:

```go
resp, err := visionHTTPClient.Do(req)
```

In `ai/http_client.go`, add `"aipmc/u"` to the import block, then change `NewClient`'s HTTP client:

```go
http: u.NewModelHTTPClient(600 * time.Second),
```

- [ ] **Step 6: Run tests to verify they pass**

Run:

```bash
gofmt -w proxy/proxy.go proxy/upstream.go proxy/responses_passthrough.go proxy/anthropic_passthrough.go vision/vision.go ai/http_client.go proxy/upstream_wsl_test.go
go vet ./proxy ./vision ./ai
go test ./proxy ./vision ./ai ./u
```

Expected: PASS, including `TestForwardToUpstreamUsesWSLFallback`.

- [ ] **Step 7: Commit**

```bash
git add proxy/proxy.go proxy/upstream.go proxy/responses_passthrough.go proxy/anthropic_passthrough.go vision/vision.go ai/http_client.go proxy/upstream_wsl_test.go
git commit -m "feat(proxy,vision,ai): 模型调用接入 WSL 宿主回退 client"
```

---

### Task 4: `aipmc doctor` WSL 诊断

**Files:**
- Create: `doctor_wsl.go`
- Create: `doctor_wsl_test.go`
- Modify: `main.go`（`runDoctor`）

**Interfaces:**
- Consumes: `u.IsWSL2()`, `u.KernelRelease()`, `u.WSLNetworkingMode()`, `u.WSLHostAddr()`, `u.IsLoopbackHost()`
- Produces:
  - `wslDoctorInfo() map[string]any`
  - `upstreamDoctorChecks() ([]map[string]any, []string)`
  - `proxyDoctorStatus() map[string]any`
  - `isDrvFSPath(path string) bool`
  - `binaryShadowWarning(executable, pathLookup string) string`

- [ ] **Step 1: Write the failing tests**

Create `doctor_wsl_test.go`:

```go
package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsDrvFSPath(t *testing.T) {
	cases := map[string]bool{
		"/mnt/c/project": true,
		"/mnt/d":         true,
		"/mnt/":          false,
		"/home/user/x":   false,
		"C:\\project":    false,
	}
	for p, want := range cases {
		if got := isDrvFSPath(p); got != want {
			t.Errorf("isDrvFSPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestBinaryShadowWarning(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("a"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("b"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := binaryShadowWarning(a, a); got != "" {
		t.Fatalf("same binary should not warn: %q", got)
	}
	if got := binaryShadowWarning(a, b); got == "" {
		t.Fatal("different binaries should warn")
	}
}

func TestWSLDoctorInfo(t *testing.T) {
	t.Setenv("AIPMC_FORCE_WSL", "1")
	t.Setenv("AIPMC_WSL_HOST", "172.20.0.1")
	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "")
	info := wslDoctorInfo()
	if info == nil {
		t.Fatal("expected WSL info")
	}
	if info["host_addr"] != "172.20.0.1" {
		t.Fatalf("host_addr = %v", info["host_addr"])
	}
	if info["fallback_enabled"] != true {
		t.Fatalf("fallback_enabled = %v", info["fallback_enabled"])
	}
}

func TestUpstreamDoctorChecksLocalReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPSTREAM_URL", "http://127.0.0.1:"+port+"/v1")
	t.Setenv("AIPMC_WSL_HOST", "")
	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "0")

	checks, _ := upstreamDoctorChecks()
	if len(checks) == 0 {
		t.Fatal("expected at least one upstream")
	}
	if checks[0]["local_reachable"] != true {
		t.Fatalf("local_reachable = %v", checks[0]["local_reachable"])
	}
	if checks[0]["effective"] != "local" {
		t.Fatalf("effective = %v", checks[0]["effective"])
	}
}

func TestUpstreamDoctorHintsWhenUnreachable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AIPMC_FORCE_WSL", "1")
	t.Setenv("UPSTREAM_URL", "http://127.0.0.1:1/v1")
	t.Setenv("AIPMC_WSL_HOST", "127.0.0.1")
	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "")

	_, hints := upstreamDoctorChecks()
	if len(hints) == 0 {
		t.Fatal("expected a firewall hint")
	}
	found := false
	for _, h := range hints {
		if strings.Contains(h, "New-NetFirewallRule") {
			found = true
		}
	}
	if !found {
		t.Fatalf("hints = %v", hints)
	}
}

func TestUpstreamDoctorSkipsNonLoopback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("UPSTREAM_URL", "https://api.example.com/v1")
	t.Setenv("AIPMC_WSL_HOST_FALLBACK", "0")

	checks, _ := upstreamDoctorChecks()
	if len(checks) == 0 {
		t.Fatal("expected at least one upstream")
	}
	if checks[0]["probe"] != "skipped" {
		t.Fatalf("probe = %v", checks[0]["probe"])
	}
	if checks[0]["effective"] != "direct" {
		t.Fatalf("effective = %v", checks[0]["effective"])
	}
}

func TestDoctorProjectPath(t *testing.T) {
	if got := doctorProjectPath("/tmp/proj/.pmai/data/pmai.db", "/tmp/other"); got != "/tmp/proj" {
		t.Fatalf("db path root = %q", got)
	}
	if got := doctorProjectPath("", "/tmp/other"); got != "/tmp/other" {
		t.Fatalf("cwd fallback = %q", got)
	}
}

func TestProxyDoctorStatus(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	t.Setenv("PROXY_PORT", port)
	got := proxyDoctorStatus()
	if got["reachable"] != true {
		t.Fatalf("reachable = %v", got["reachable"])
	}
}

func TestRunDoctorAddsWSLSections(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME override is not portable on Windows")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("AIPMC_FORCE_WSL", "1")
	t.Setenv("AIPMC_WSL_HOST", "127.0.0.1")
	t.Setenv("UPSTREAM_URL", "http://127.0.0.1:1/v1")
	report := runDoctor("")
	if _, ok := report["wsl"]; !ok {
		t.Fatal("missing wsl section")
	}
	if _, ok := report["upstreams"]; !ok {
		t.Fatal("missing upstreams section")
	}
	if _, ok := report["hints"]; !ok {
		t.Fatal("missing hints section")
	}
}
```

Remove `TestIsConnectRefused` from `u/wsl_test.go` if it was placed there (it belongs in `u/wsl_transport_test.go`). Also remove unused imports in the test files.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test . -run 'TestIsDrvFSPath|TestBinaryShadowWarning|TestWSLDoctorInfo|TestUpstreamDoctor|TestDoctorProjectPath|TestProxyDoctorStatus|TestRunDoctorAddsWSLSections' -v`

Expected: build failure / undefined helpers.

- [ ] **Step 3: Create `doctor_wsl.go`**

```go
package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	pmdb "aipmc/db"
	"aipmc/u"
)

func wslDoctorInfo() map[string]any {
	if !u.IsWSL2() {
		return nil
	}
	host := u.WSLHostAddr()
	return map[string]any{
		"distro":           os.Getenv("WSL_DISTRO_NAME"),
		"kernel":           u.KernelRelease(),
		"networking_mode":  u.WSLNetworkingMode(),
		"host_addr":        host,
		"fallback_enabled": host != "",
	}
}

func isDrvFSPath(path string) bool {
	p := filepath.ToSlash(path)
	return strings.HasPrefix(p, "/mnt/") && len(strings.TrimPrefix(p, "/mnt/")) > 0
}

// doctorProjectPath derives the project root from the database path so
// running `aipmc doctor` from a subdirectory reports the project, not the cwd.
func doctorProjectPath(dbPath, cwd string) string {
	if dbPath != "" {
		return pmdb.ProjectRoot(dbPath)
	}
	return cwd
}

func binaryShadowWarning(executable, pathLookup string) string {
	if executable == "" || pathLookup == "" {
		return ""
	}
	exe, err1 := filepath.EvalSymlinks(executable)
	look, err2 := filepath.EvalSymlinks(pathLookup)
	if err1 != nil || err2 != nil {
		return ""
	}
	if filepath.Clean(exe) == filepath.Clean(look) {
		return ""
	}
	return fmt.Sprintf("PATH 中的 aipmc (%s) 与当前运行的二进制 (%s) 不是同一个；可能是旧版 PyPI 包抢占，执行 which -a aipmc 并卸载旧版", look, exe)
}

type upstreamSource struct {
	Source string
	URL    string
}

func collectUpstreamURLs() []upstreamSource {
	out := []upstreamSource{}
	seen := map[string]bool{}
	add := func(source, raw string) {
		raw = strings.TrimRight(strings.TrimSpace(raw), "/")
		if raw == "" || seen[raw] {
			return
		}
		seen[raw] = true
		out = append(out, upstreamSource{Source: source, URL: raw})
	}
	gcfg := pmdb.LoadGlobalConfig()
	global := strings.TrimSpace(gcfg.UpstreamURL)
	if global == "" {
		global = "http://localhost:8080/v1"
	}
	add("global", global)
	reg := pmdb.LoadModelRegistry()
	for _, p := range reg.Providers {
		add("provider:"+p.Name+":openai", p.OpenAIURL)
		add("provider:"+p.Name+":anthropic", p.AnthropicURL)
		add("provider:"+p.Name+":responses", p.ResponsesURL)
	}
	return out
}

func probeTCP(addr string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func urlHostPort(raw string) (host, port string, ok bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", "", false
	}
	host = parsed.Hostname()
	port = parsed.Port()
	if port == "" {
		switch strings.ToLower(parsed.Scheme) {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	return host, port, true
}

func upstreamDoctorChecks() ([]map[string]any, []string) {
	checks := []map[string]any{}
	hints := []string{}
	if u.IsWSL2() && u.WSLNetworkingMode() != "mirrored" {
		hints = append(hints, "如果使用 Windows 11 22H2+，可考虑在 .wslconfig 设置 networkingMode=mirrored 后执行 wsl --shutdown；localhost 将双向直通，可能不需要回退。")
	}
	hostAddr := u.WSLHostAddr()
	localCache := map[string]bool{}
	hostCache := map[string]bool{}
	probeLocal := func(addr string) bool {
		if v, ok := localCache[addr]; ok {
			return v
		}
		v := probeTCP(addr, 400*time.Millisecond)
		localCache[addr] = v
		return v
	}
	probeHost := func(addr string) bool {
		if v, ok := hostCache[addr]; ok {
			return v
		}
		v := probeTCP(addr, 400*time.Millisecond)
		hostCache[addr] = v
		return v
	}
	for _, src := range collectUpstreamURLs() {
		host, port, ok := urlHostPort(src.URL)
		if !ok {
			checks = append(checks, map[string]any{
				"source": src.Source, "url": src.URL, "effective": "invalid",
			})
			continue
		}
		entry := map[string]any{
			"source": src.Source,
			"url":    src.URL,
		}
		// Cloud/non-loopback endpoints are reached through HTTP_PROXY in
		// corporate environments; a raw TCP probe would produce false
		// negatives and is irrelevant to the WSL loopback fallback.
		if !u.IsLoopbackHost(host) {
			entry["effective"] = "direct"
			entry["probe"] = "skipped"
			checks = append(checks, entry)
			continue
		}
		addr := net.JoinHostPort(host, port)
		local := probeLocal(addr)
		entry["local_reachable"] = local
		hostReachable := false
		if hostAddr != "" {
			hostReachable = probeHost(net.JoinHostPort(hostAddr, port))
		}
		entry["host_reachable"] = hostReachable
		switch {
		case local:
			entry["effective"] = "local"
		case hostReachable:
			entry["effective"] = "host"
		default:
			entry["effective"] = "unreachable"
			if u.IsWSL2() {
				hints = append(hints, fmt.Sprintf(
					"上游 %s 在 WSL 本地和宿主机 %s:%s 均不可达。宿主机启动模型时加 --host 0.0.0.0，并放行防火墙：New-NetFirewallRule -DisplayName \"aipmc model upstream (WSL)\" -Direction Inbound -Protocol TCP -LocalPort %s -Action Allow -Profile Any",
					src.URL, hostAddr, port, port))
			} else {
				hints = append(hints, fmt.Sprintf("上游 %s 不可达；检查模型服务是否启动、地址/端口是否正确。", src.URL))
			}
		}
		checks = append(checks, entry)
	}
	return checks, dedupeStrings(hints)
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func proxyDoctorStatus() map[string]any {
	gcfg := pmdb.LoadGlobalConfig()
	addr := fmt.Sprintf("127.0.0.1:%d", gcfg.ProxyPort)
	return map[string]any{
		"url":       fmt.Sprintf("http://%s/__proxy/status", addr),
		"reachable": probeTCP(addr, 300*time.Millisecond),
	}
}
```

- [ ] **Step 4: Modify `runDoctor` in `main.go`**

Replace the existing function body:

```go
func runDoctor(dbPath string) map[string]any {
	problems := []string{}
	warnings := []string{}
	hints := []string{}

	if dbPath == "" {
		problems = append(problems, "No .pmai directory found. Run aipmc init first.")
	} else {
		db, err := pmdb.Open()
		if err != nil {
			problems = append(problems, fmt.Sprintf("Cannot open database: %v", err))
		} else {
			db.Close()
		}
	}

	cwd, _ := os.Getwd()
	projectPath := doctorProjectPath(dbPath, cwd)
	if isDrvFSPath(projectPath) {
		warnings = append(warnings, "项目位于 /mnt/ 下，SQLite 跨 DrvFs 不可靠，建议移到 WSL 原生文件系统（如 ~/projects/...）")
	}
	if exe, err := os.Executable(); err == nil {
		if path, err := exec.LookPath("aipmc"); err == nil {
			if w := binaryShadowWarning(exe, path); w != "" {
				warnings = append(warnings, w)
			}
		}
	}

	checks, upstreamHints := upstreamDoctorChecks()
	hints = append(hints, upstreamHints...)

	report := map[string]any{
		"ok":           len(problems) == 0,
		"problems":     problems,
		"db_path":      dbPath,
		"binary":       os.Args[0],
		"project_path": projectPath,
		"proxy":        proxyDoctorStatus(),
		"upstreams":    checks,
	}
	if w := wslDoctorInfo(); w != nil {
		report["wsl"] = w
	}
	if len(warnings) > 0 {
		report["warnings"] = warnings
	}
	if len(hints) > 0 {
		report["hints"] = hints
	}
	return report
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run:

```bash
gofmt -w doctor_wsl.go doctor_wsl_test.go main.go
go vet .
go test . -run 'TestIsDrvFSPath|TestBinaryShadowWarning|TestWSLDoctorInfo|TestUpstreamDoctor|TestDoctorProjectPath|TestProxyDoctorStatus|TestRunDoctorAddsWSLSections' -v
go test .
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add doctor_wsl.go doctor_wsl_test.go main.go
git commit -m "feat(doctor): WSL/上游可达性/路径自检"
```

---

### Task 5: 文档

**Files:**
- Create: `docs/WSL.md`
- Modify: `docs/PROXY_DEBUG.md`
- Modify: `docs/QUICKSTART.md`

**Interfaces:** 无代码接口。

- [ ] **Step 1: Create `docs/WSL.md`**

```markdown
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
```

- [ ] **Step 2: Update `docs/PROXY_DEBUG.md`**

Find the llama-server example:

```bash
llama-server -m model.gguf --port 8080 --host 127.0.0.1 --reasoning off
```

Append a note immediately after the code block:

```markdown
> WSL/跨机访问：把 `--host 127.0.0.1` 改为 `--host 0.0.0.0`，并在 Windows 防火墙放行该端口。WSL 内的 aipmc 会自动回退到宿主网关，详见 [WSL.md](WSL.md)。
```

If the file contains multiple such examples, update the first one and add the note; do not rewrite unrelated sections.

- [ ] **Step 3: Update `docs/QUICKSTART.md`**

After the "## 0. 前提" section, add:

```markdown
> **WSL 用户**：项目、proxy、serve、Agent 都放在 WSL 内，模型服务放宿主机；不要把 proxy 放到 Windows。完整步骤见 [WSL.md](WSL.md)。
```

- [ ] **Step 4: Commit**

```bash
git add docs/WSL.md docs/PROXY_DEBUG.md docs/QUICKSTART.md
git commit -m "docs: WSL 部署指南（全栈在 WSL，仅模型在宿主）"
```

---

### Task 6: 全量验证、构建与 PM 记账

**Files:** 无源码改动；只运行验证和记录。

- [ ] **Step 1: Full format/vet/test**

```bash
gofmt -l $(git diff --name-only -- '*.go')
go vet ./...
go test -count=1 ./...
```

Expected: `gofmt -l` prints nothing; vet clean; all packages pass. (Proxy tests may need a writable `~/.aipmc`; run outside restrictive sandboxes if the harness blocks it.)

- [ ] **Step 2: Cross-compile Linux and refresh distribution artifacts**

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/aipmc-linux-amd64 .
bash build.sh -f -r
ls -lh dist/aipmc-linux-amd64
```

Expected: build succeeds; `dist/` five artifacts refreshed. `dist/` is gitignored; do not commit binaries.

- [ ] **Step 3: PM 记账（新建 WSL support plan，不挂观测层工具修复）**

1. `aipm_create_roadmap(title="WSL 环境支持", status="active", priority="P1")` → 记录返回的 `roadmap_id`
2. `aipm_create_plan(title="WSL 全栈支持（2026-11）", roadmap_id=<roadmap_id>, goal="项目/proxy/serve/Agent 全在 WSL；只解决模型上游跨边界，并补齐 WSL 自检与文档。", priority="P1", status="active")` → 记录返回的 `plan_id`
3. `aipm_create_task(title="WSL 上游模型回退（全栈 WSL，仅模型在宿主）", plan_id=<plan_id>, priority="P1", status="in_progress", phase="wsl-support")`
4. `aipm_record_decision(status="accepted")`，内容：

```text
title: WSL 拓扑：全栈在 WSL，仅模型上游跨边界
background: WSL2 NAT 下 localhost 指向 WSL 自己；宿主机 IP 随重启变化；proxy 依赖项目 .pmai 库。
decision: proxy/serve/Agent 全部在 WSL；只有模型上游跨边界。模型调用使用带 DialContext 回退的 HTTP client：loopback 目标连接被拒绝/不可达时，用同一端口重试 WSL 默认网关；优先 eth* 默认路由（避免 VPN 接口），超时/取消不回退；AIPMC_WSL_HOST 显式优先，AIPMC_WSL_HOST_FALLBACK=0 可关闭。不静态改写 URL，不新增转发进程。
```

5. 各任务 commit 后，用 `aipm_record_commits` 绑定到该 task；`test_status=passed`。若批量调用因 schema 报错，退回逐条 `aipm_record_commit`。`review_status=approved` 与 task `done` 只在用户完成 WSL 手工验收后设置。

- [ ] **Step 4: WSL manual acceptance (user runs)**

```bash
cd ~/projects/foo
aipmc doctor
aipmc proxy
# 另一终端
aipmc serve
# 第三终端
aipmc agent claude
```

Record the result as a verification log via `aipm_record_verification`:
- scene: `WSL 全栈 + 宿主模型`
- device: Windows 版本 + WSL distro
- result: pass/fail
- detail: `aipmc doctor` 的 `wsl.host_addr`、`upstreams.effective`、一轮真实对话的 session id

Additional checks:

```bash
# 1. 关闭宿主机模型后，错误应可定位（connection refused / timeout）
# 2. 关闭回退后，日志中不应出现 [WSL] ... falling back
AIPMC_WSL_HOST_FALLBACK=0 aipmc proxy

# 3. /mnt/c 下 doctor 只告警、不崩溃
cd /mnt/c && aipmc doctor
```

- [ ] **Step 5: 不 push**

Stop here. Push requires explicit user authorization.

---

## Self-Review

**Spec coverage:**

- 拓扑决策、否决 proxy 跑宿主机 → Spec §2，Plan 无代码改动（正确）
- WSL 检测 → Task 1
- 宿主地址解析（/proc/net/route、mirrored、env 优先级） → Task 1
- DialContext 回退（仅 loopback、仅 refused/unreachable） → Task 2
- 模型调用接入点（proxy 7 处、vision、ai） → Task 3
- `aipmc doctor` 输出（wsl/upstreams/proxy/warnings/hints） → Task 4
- 文档（WSL.md、PROXY_DEBUG、QUICKSTART） → Task 5
- 验收（doctor、init、proxy、agent、PM 库） → Task 6
- 非目标（serve --host、wt.exe、/v1/models 探活、远程管理） → 未出现在任务中（正确）

**Placeholder scan:** 无 TBD/TODO；所有代码与命令均已展开。

**Type consistency:**

- `u.IsLoopbackHost` 在 Task 1 定义，Task 2/4 使用，名称一致。
- `u.NewModelHTTPClient` 在 Task 2 定义，Task 3 使用，签名 `time.Duration -> *http.Client` 一致。
- `upstreamHTTPClient` 在 `proxy` 包定义，Task 3 所有替换点使用同一名称。
- `wslDoctorInfo`、`upstreamDoctorChecks`、`proxyDoctorStatus` 在 Task 4 定义并在 `runDoctor` 中使用，名称一致。
- `isDrvFSPath`、`binaryShadowWarning` 在 Task 4 定义并在 `runDoctor` 中使用，名称一致。

---

## Review Amendments（Codex 2026-11-12 15:47 复核）

- **拓扑取证前置**：新增 Task 0，开工前在 WSL/Windows 两侧采集 `uname -a`、`ip route`、`ss -ltnp`；拓扑不符则停止。拓扑本身已由用户在 2026-11-12 的选项确认（全栈在 WSL，仅模型在宿主），Task 0 是运行时校验，不是重新决策。
- **网关选择**：`parseDefaultGateway` 从「最低 metric」改为「优先 `eth*`，同接口内最低 metric」；新增 Tailscale/VPN 低 metric 路由的测试。
- **doctor 非 loopback 上游**：跳过 TCP 探测，输出 `probe:"skipped"`、`effective:"direct"`；避免企业代理环境对云端点误报。
- **防火墙 hint 范围**：只在 `u.IsWSL2()` 时输出 PowerShell 命令；非 WSL 输出通用不可达提示。mirrored 模式提示放在第一条。
- **project_path**：从 `dbPath` 经 `pmdb.ProjectRoot` 反推，避免在子目录运行 doctor 时报告子目录。
- **HTTP_PROXY 表述**：明确 `Transport.DialContext` 也用于拨代理；非 loopback（含代理地址）不触发回退；字面 loopback 由 Go 内置豁免；非字面 loopback 需 `NO_PROXY`。测试覆盖非 loopback/代理地址只拨一次。
- **提交粒度**：Task 1+2 合并为一笔 commit（同一机制），Task 0 先提交 spec+plan。
- **PM 归属**：新建 `WSL 环境支持` roadmap 与 `WSL 全栈支持（2026-11）` plan，不挂观测层工具修复 plan。
